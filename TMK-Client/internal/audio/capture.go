package audio

import (
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	sampleRate   = 16000
	bitsPerSample = 16
	channels     = 1
	chunkMs      = 100

	bufferBytes = sampleRate * (bitsPerSample / 8) * channels * chunkMs / 1000 // 3200
	bufferCount = 4
)

type OnData func(pcm []byte)

type Capture struct {
	mu       sync.Mutex
	deviceID int
	waveIn   uintptr
	headers  []waveHdr
	buffers  [][]byte
	running  bool
	callback OnData
}

type waveFormatEx struct {
	wFormatTag      uint16
	nChannels       uint16
	nSamplesPerSec  uint32
	nAvgBytesPerSec uint32
	nBlockAlign     uint16
	wBitsPerSample  uint16
	cbSize          uint16
}

type waveHdr struct {
	lpData          uintptr
	dwBufferLength  uint32
	dwBytesRecorded uint32
	dwUser          uintptr
	dwFlags         uint32
	dwLoops         uint32
	lpNext          uintptr
	reserved        uintptr
}

var (
	winmmDLL              = syscall.MustLoadDLL(winmm)
	procWaveInOpen        = winmmDLL.MustFindProc("waveInOpen")
	procWaveInPrepareHdr  = winmmDLL.MustFindProc("waveInPrepareHeader")
	procWaveInAddBuffer   = winmmDLL.MustFindProc("waveInAddBuffer")
	procWaveInStart       = winmmDLL.MustFindProc("waveInStart")
	procWaveInStop        = winmmDLL.MustFindProc("waveInStop")
	procWaveInReset       = winmmDLL.MustFindProc("waveInReset")
	procWaveInUnprepareHdr = winmmDLL.MustFindProc("waveInUnprepareHeader")
	procWaveInClose       = winmmDLL.MustFindProc("waveInClose")

	wimData = uintptr(0x3C0)
)

// StartCapture opens the device and begins streaming PCM data via callback.
// deviceID: WAVE_MAPPER (-1) for default, or specific device ID from ListDevices().
func StartCapture(deviceID int, cb OnData) (*Capture, error) {
	c := &Capture{
		deviceID: deviceID,
		callback: cb,
		running:  true,
		buffers:  make([][]byte, bufferCount),
		headers:  make([]waveHdr, bufferCount),
	}

	wfx := waveFormatEx{
		wFormatTag:      1, // WAVE_FORMAT_PCM
		nChannels:       channels,
		nSamplesPerSec:  sampleRate,
		nAvgBytesPerSec: sampleRate * channels * (bitsPerSample / 8),
		nBlockAlign:     channels * (bitsPerSample / 8),
		wBitsPerSample:  bitsPerSample,
	}

	// waveInOpen with CALLBACK_FUNCTION (0x30000)
	const callbackFunction = 0x30000
	cbPtr := syscall.NewCallback(waveInProc)
	r, _, _ := procWaveInOpen.Call(
		uintptr(unsafe.Pointer(&c.waveIn)),
		uintptr(deviceID),
		uintptr(unsafe.Pointer(&wfx)),
		cbPtr,
		uintptr(unsafe.Pointer(c)),
		callbackFunction,
	)
	if r != 0 {
		return nil, fmt.Errorf("waveInOpen failed: %d", r)
	}

	// prepare and add all buffers
	for i := 0; i < bufferCount; i++ {
		c.buffers[i] = make([]byte, bufferBytes)
		c.headers[i] = waveHdr{
			lpData:         uintptr(unsafe.Pointer(&c.buffers[i][0])),
			dwBufferLength: bufferBytes,
		}
		r, _, _ = procWaveInPrepareHdr.Call(c.waveIn, uintptr(unsafe.Pointer(&c.headers[i])), uintptr(unsafe.Sizeof(c.headers[i])))
		if r != 0 {
			c.close()
			return nil, fmt.Errorf("waveInPrepareHeader[%d] failed: %d", i, r)
		}
		r, _, _ = procWaveInAddBuffer.Call(c.waveIn, uintptr(unsafe.Pointer(&c.headers[i])), uintptr(unsafe.Sizeof(c.headers[i])))
		if r != 0 {
			c.close()
			return nil, fmt.Errorf("waveInAddBuffer[%d] failed: %d", i, r)
		}
	}

	r, _, _ = procWaveInStart.Call(c.waveIn)
	if r != 0 {
		c.close()
		return nil, fmt.Errorf("waveInStart failed: %d", r)
	}

	log.Printf("[audio] capture started: device=%d, rate=%d, buffer=%dx%d", deviceID, sampleRate, bufferCount, bufferBytes)
	return c, nil
}

func (c *Capture) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	procWaveInStop.Call(c.waveIn)
	procWaveInReset.Call(c.waveIn)

	// Wait for in-flight callbacks to see running=false and return.
	time.Sleep(50 * time.Millisecond)

	c.mu.Lock()
	for i := range c.headers {
		procWaveInUnprepareHdr.Call(c.waveIn, uintptr(unsafe.Pointer(&c.headers[i])), uintptr(unsafe.Sizeof(c.headers[i])))
	}
	procWaveInClose.Call(c.waveIn)
	c.mu.Unlock()
	log.Printf("[audio] capture stopped")
}

func (c *Capture) close() {
	procWaveInClose.Call(c.waveIn)
}

func waveInProc(hwi, uMsg, dwInstance, dwParam1, dwParam2 uintptr) uintptr {
	if uMsg != wimData {
		return 0
	}

	c := (*Capture)(unsafe.Pointer(dwInstance))
	hdr := (*waveHdr)(unsafe.Pointer(dwParam1))

	if !c.running {
		return 0
	}

	if hdr.dwBytesRecorded > 0 {
		data := make([]byte, hdr.dwBytesRecorded)
		copy(data, unsafe.Slice((*byte)(unsafe.Pointer(hdr.lpData)), hdr.dwBytesRecorded))
		c.callback(data)
	} else {
		log.Printf("[audio] WIM_DATA with 0 bytes recorded — device may be silent or not capturing")
	}

	// re-add buffer (already prepared, no need to re-prepare)
	procWaveInAddBuffer.Call(c.waveIn, uintptr(unsafe.Pointer(hdr)), uintptr(unsafe.Sizeof(*hdr)))
	return 0
}
