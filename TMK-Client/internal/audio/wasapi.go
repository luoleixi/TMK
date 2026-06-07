//go:build windows

package audio

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	wca "github.com/moutend/go-wca/pkg/wca"
)

// WasapiCapture captures system audio output via WASAPI loopback.
type WasapiCapture struct {
	mu            sync.Mutex
	running       bool
	callback      OnData
	stopCh        chan struct{}
	doneCh        chan struct{}
	audioClient   *wca.IAudioClient
	captureClient *wca.IAudioCaptureClient
	device        *wca.IMMDevice
	enumerator    *wca.IMMDeviceEnumerator
}

// StartLoopbackCapture begins WASAPI loopback capture from the default playback device.
func StartLoopbackCapture(cb OnData) (*WasapiCapture, error) {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	}

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator, 0,
		wca.CLSCTX_INPROC_SERVER,
		wca.IID_IMMDeviceEnumerator,
		&enumerator,
	); err != nil {
		return nil, fmt.Errorf("wasapi: CoCreateInstance enumerator: %w", err)
	}

	var device *wca.IMMDevice
	if err := enumerator.GetDefaultAudioEndpoint(
		uint32(wca.ERender), uint32(wca.EConsole), &device,
	); err != nil {
		enumerator.Release()
		return nil, fmt.Errorf("wasapi: GetDefaultAudioEndpoint: %w", err)
	}

	var audioClient *wca.IAudioClient
	if err := device.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &audioClient); err != nil {
		device.Release()
		enumerator.Release()
		return nil, fmt.Errorf("wasapi: Activate IAudioClient: %w", err)
	}

	var mixFormat *wca.WAVEFORMATEX
	if err := audioClient.GetMixFormat(&mixFormat); err != nil {
		audioClient.Release()
		device.Release()
		enumerator.Release()
		return nil, fmt.Errorf("wasapi: GetMixFormat: %w", err)
	}

	srcRate := mixFormat.NSamplesPerSec
	srcChannels := mixFormat.NChannels
	isFloat := isFloatFormat(mixFormat)

	log.Printf("[audio:wasapi] mix format: %dHz %dch %d-bit (float=%v)",
		srcRate, srcChannels, mixFormat.WBitsPerSample, isFloat)

	if err := audioClient.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		wca.AUDCLNT_STREAMFLAGS_LOOPBACK,
		0, 0, mixFormat, nil,
	); err != nil {
		ole.CoTaskMemFree(uintptr(unsafe.Pointer(mixFormat)))
		audioClient.Release()
		device.Release()
		enumerator.Release()
		return nil, fmt.Errorf("wasapi: Initialize loopback: %w", err)
	}

	ole.CoTaskMemFree(uintptr(unsafe.Pointer(mixFormat)))

	var captureClient *wca.IAudioCaptureClient
	if err := audioClient.GetService(wca.IID_IAudioCaptureClient, &captureClient); err != nil {
		audioClient.Release()
		device.Release()
		enumerator.Release()
		return nil, fmt.Errorf("wasapi: GetService IAudioCaptureClient: %w", err)
	}

	if err := audioClient.Start(); err != nil {
		captureClient.Release()
		audioClient.Release()
		device.Release()
		enumerator.Release()
		return nil, fmt.Errorf("wasapi: Start: %w", err)
	}

	wc := &WasapiCapture{
		running:       true,
		callback:      cb,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		audioClient:   audioClient,
		captureClient: captureClient,
		device:        device,
		enumerator:    enumerator,
	}

	log.Printf("[audio:wasapi] loopback capture started: %dHz %dch", srcRate, srcChannels)

	go wc.captureLoop(srcRate, srcChannels, isFloat)

	return wc, nil
}

// Stop stops WASAPI loopback capture and releases all resources.
func (w *WasapiCapture) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	close(w.stopCh)
	<-w.doneCh

	w.audioClient.Stop()
	w.captureClient.Release()
	w.audioClient.Release()
	w.device.Release()
	w.enumerator.Release()
	ole.CoUninitialize()

	log.Printf("[audio:wasapi] capture stopped")
}

func (w *WasapiCapture) captureLoop(srcRate uint32, srcChannels uint16, isFloat bool) {
	defer close(w.doneCh)

	var (
		acc     = make([]byte, 0, bufferBytes*2)
		rs      = newResampler(srcRate)
		ticker  = time.NewTicker(10 * time.Millisecond)
		monoBuf = make([]float32, 0, 512)
	)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			running := w.running
			w.mu.Unlock()
			if !running {
				return
			}

			monoBuf = monoBuf[:0]
			w.readPackets(&monoBuf, srcChannels, isFloat)

			if len(monoBuf) == 0 {
				continue
			}

			var out []int16
			for _, s := range monoBuf {
				rs.feed(float64(s), &out)
			}

			for _, sample := range out {
				var b [2]byte
				binary.LittleEndian.PutUint16(b[:], uint16(int16(sample)))
				acc = append(acc, b[0], b[1])
			}

			for len(acc) >= bufferBytes {
				chunk := make([]byte, bufferBytes)
				copy(chunk, acc[:bufferBytes])
				acc = acc[bufferBytes:]
				if w.callback != nil {
					w.callback(chunk)
				}
			}
		}
	}
}

func (w *WasapiCapture) readPackets(monoBuf *[]float32, channels uint16, isFloat bool) {
	for {
		var packetSize uint32
		if err := w.captureClient.GetNextPacketSize(&packetSize); err != nil || packetSize == 0 {
			return
		}

		var (
			data          *byte
			framesToRead  uint32
			flags         uint32
		)
		if err := w.captureClient.GetBuffer(&data, &framesToRead, &flags, nil, nil); err != nil {
			return
		}

		if flags&wca.AUDCLNT_BUFFERFLAGS_SILENT != 0 {
			// silent packet: emit zero samples
			for i := uint32(0); i < framesToRead; i++ {
				*monoBuf = append(*monoBuf, 0)
			}
		} else {
			frameBytes := framesToRead * uint32(channels) * 4 // 4 bytes per float32
			raw := unsafe.Slice(data, frameBytes)

			if isFloat && channels == 2 {
				// optimized path: float32 stereo → mono
				for i := uint32(0); i < framesToRead; i++ {
					off := i * 8 // 2 channels * 4 bytes
					left := math.Float32frombits(binary.LittleEndian.Uint32(raw[off : off+4]))
					right := math.Float32frombits(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
					*monoBuf = append(*monoBuf, (left+right)*0.5)
				}
			} else if isFloat && channels == 1 {
				for i := uint32(0); i < framesToRead; i++ {
					off := i * 4
					s := math.Float32frombits(binary.LittleEndian.Uint32(raw[off : off+4]))
					*monoBuf = append(*monoBuf, s)
				}
			} else if !isFloat && channels == 2 {
				// int16 stereo → mono
				for i := uint32(0); i < framesToRead; i++ {
					off := i * 4 // 2 channels * 2 bytes
					left := float32(int16(binary.LittleEndian.Uint16(raw[off:off+2]))) / 32768.0
					right := float32(int16(binary.LittleEndian.Uint16(raw[off+2:off+4]))) / 32768.0
					*monoBuf = append(*monoBuf, (left+right)*0.5)
				}
			} else {
				// generic fallback
				bytesPerFrame := channels * 2
				for i := uint32(0); i < framesToRead; i++ {
					var sum float32
					for ch := uint16(0); ch < channels; ch++ {
						off := i*uint32(bytesPerFrame) + uint32(ch)*2
						sum += float32(int16(binary.LittleEndian.Uint16(raw[off:off+2]))) / 32768.0
					}
					*monoBuf = append(*monoBuf, sum/float32(channels))
				}
			}
		}

		w.captureClient.ReleaseBuffer(framesToRead)
	}
}

// ---- format helpers ----

func isFloatFormat(fmt *wca.WAVEFORMATEX) bool {
	switch fmt.WFormatTag {
	case 0x0003: // WAVE_FORMAT_IEEE_FLOAT
		return true
	case 0x0001: // WAVE_FORMAT_PCM
		return false
	case 0xFFFE: // WAVE_FORMAT_EXTENSIBLE – check SubFormat GUID at offset 24
		subFirstDWORD := *(*uint32)(unsafe.Pointer(uintptr(unsafe.Pointer(fmt)) + 24))
		return subFirstDWORD == 0x00000003 // KSDATAFORMAT_SUBTYPE_IEEE_FLOAT
	default:
		return false
	}
}

// ---- resampler ----

type resampler struct {
	srcRate     float64
	frac        float64 // fractional position in source space [0, 1)
	prev        float64
	initialized bool
}

func newResampler(srcRate uint32) *resampler {
	return &resampler{srcRate: float64(srcRate)}
}

// feed processes one mono source sample (normalized to [-1, 1]).
// Completed target samples are appended to out.
func (r *resampler) feed(sample float64, out *[]int16) {
	if !r.initialized {
		r.prev = sample
		r.initialized = true
		return
	}

	step := r.srcRate / 16000.0 // source samples per target sample

	for r.frac < 1.0 {
		val := r.prev + r.frac*(sample-r.prev)
		v := int32(math.Round(val * 32767))
		switch {
		case v > 32767:
			v = 32767
		case v < -32768:
			v = -32768
		}
		*out = append(*out, int16(v))
		r.frac += step
	}
	r.frac -= 1.0
	r.prev = sample
}
