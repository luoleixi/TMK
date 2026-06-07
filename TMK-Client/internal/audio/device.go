package audio

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	winmm          = "winmm.dll"
	prodNameLen    = 32
	waveMapper     = -1 // WAVE_MAPPER: 默认设备
)

type Device struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"` // "microphone" | "system_audio"
}

// ListDevices enumerates all WaveIn input devices
func ListDevices() []Device {
	num := waveInGetNumDevs()
	devices := make([]Device, 0, num+1)

	for i := 0; i < num; i++ {
		name, err := waveInGetDevCaps(i)
		if err != nil {
			continue
		}
		t := "microphone"
		devices = append(devices, Device{ID: i, Name: name, Type: t})
	}
	return devices
}

// DefaultDevice returns the WAVE_MAPPER device
func DefaultDevice() Device {
	name, _ := waveInGetDevCaps(waveMapper)
	return Device{ID: waveMapper, Name: name, Type: "microphone"}
}

func waveInGetNumDevs() int {
	dll, _ := syscall.LoadDLL(winmm)
	proc, _ := dll.FindProc("waveInGetNumDevs")
	n, _, _ := proc.Call()
	return int(n)
}

func waveInGetDevCaps(id int) (string, error) {
	dll, err := syscall.LoadDLL(winmm)
	if err != nil {
		return "", err
	}
	proc, err := dll.FindProc("waveInGetDevCapsW")
	if err != nil {
		return "", err
	}

	var caps struct {
		mid          uint16
		pid          uint16
		driverVer    uint32
		productName  [prodNameLen]uint16
		formats      uint32
		channels     uint16
		reserved     uint16
		manufacturer uint16
		prodFeatures uint16
		support      uint32
		instID       [8]byte
	}

	r, _, _ := proc.Call(uintptr(id), uintptr(unsafe.Pointer(&caps)), uintptr(unsafe.Sizeof(caps)))
	if r != 0 {
		return "", fmt.Errorf("waveInGetDevCaps failed: %d", r)
	}

	return syscall.UTF16ToString(caps.productName[:]), nil
}
