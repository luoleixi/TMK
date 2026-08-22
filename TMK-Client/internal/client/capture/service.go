package capture

import (
	"fmt"
	"log"
	"strings"

	"tmk-client/internal/audio"
)

type CaptureService struct {
	capture     interface{ Stop() }
	micDeviceID int // -2=system audio, -1=default mic, >=0=specific mic
	sendAudio   func([]byte) error
}

const DeviceSystemAudio = -2

func NewService(sendAudio func([]byte) error) *CaptureService {
	return &CaptureService{micDeviceID: DeviceSystemAudio, sendAudio: sendAudio}
}

// SetMicrophoneDevice sets the capture device ID.
// Use -2 for system audio (WASAPI loopback), -1 for default mic, or specific device ID.
func (s *CaptureService) SetMicrophoneDevice(deviceID int) {
	s.micDeviceID = deviceID
	log.Printf("[capture] device set to %d", deviceID)
}

// ListCaptureDevices returns all available capture sources.
// First entry is system audio (WASAPI loopback), followed by microphone devices.
func (s *CaptureService) ListCaptureDevices() []audio.Device {
	sysAudio := audio.Device{ID: DeviceSystemAudio, Name: "系统音频 (WASAPI Loopback)", Type: "system_audio"}
	def := audio.DefaultDevice()
	def.Name = "默认设备 (" + def.Name + ")"
	devices := audio.ListDevices()
	result := make([]audio.Device, 0, len(devices)+2)
	result = append(result, sysAudio, def)
	return append(result, devices...)
}

// StartCapture begins capture for the requested source type.
// system_audio always uses WASAPI loopback; microphone uses the selected device.
func (s *CaptureService) StartCapture(sourceType string) error {
	if s.capture != nil {
		s.StopCapture()
	}

	var count int
	onData := func(pcm []byte) {
		count++
		if count == 1 {
			log.Printf("[capture] first audio chunk received: %d bytes", len(pcm))
		}
		if count%50 == 1 {
			log.Printf("[capture] sent %d chunks (%d bytes each)", count, len(pcm))
		}
		if s.sendAudio != nil {
			if err := s.sendAudio(pcm); err != nil {
				if err.Error() != "not connected" {
					log.Printf("[capture] send error: %v", err)
				}
			}
		} else {
			log.Printf("[capture] audio sink is nil")
		}
	}

	sourceType = strings.TrimSpace(sourceType)
	if sourceType == "" {
		if s.micDeviceID == DeviceSystemAudio {
			sourceType = "system_audio"
		} else {
			sourceType = "microphone"
		}
	}

	switch sourceType {
	case "system_audio":
		c, err := audio.StartLoopbackCapture(onData)
		if err != nil {
			return err
		}
		s.capture = c
		log.Printf("[capture] started, source=system_audio (WASAPI loopback)")
		return nil
	case "microphone":
		deviceID := s.micDeviceID
		if deviceID < 0 {
			deviceID = audio.DefaultDevice().ID
		}
		c, err := audio.StartCapture(deviceID, onData)
		if err != nil {
			return err
		}
		s.capture = c
		log.Printf("[capture] started, source=microphone device=%d", deviceID)
		return nil
	default:
		return fmt.Errorf("unsupported capture source %q", sourceType)
	}
}

// StopCapture stops audio capture.
func (s *CaptureService) StopCapture() {
	if s.capture != nil {
		s.capture.Stop()
		s.capture = nil
	}
}
