package main

import (
	"log"

	"changeme/internal/audio"
)

var (
	captureSvc *CaptureService
	sessionSvc *SessionService
)

type CaptureService struct {
	capture     interface{ Stop() }
	micDeviceID int // device ID for microphone mode, -1 = default
}

func NewCaptureService() *CaptureService {
	return &CaptureService{micDeviceID: -1}
}

// SetMicrophoneDevice sets the microphone device ID to use.
// Call before StartCapture with sourceType="microphone".
func (s *CaptureService) SetMicrophoneDevice(deviceID int) {
	s.micDeviceID = deviceID
	log.Printf("[capture] microphone device set to %d", deviceID)
}

// ListCaptureDevices returns all available waveIn devices.
// The first entry is the system default (WAVE_MAPPER).
func (s *CaptureService) ListCaptureDevices() []audio.Device {
	def := audio.DefaultDevice()
	def.Name = "默认设备 (" + def.Name + ")"
	devices := audio.ListDevices()
	return append([]audio.Device{def}, devices...)
}

// StartCapture begins audio capture for the given source type.
// sourceType: "system_audio" → WASAPI loopback, "microphone" → default mic
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
		if sessionSvc != nil {
			if err := sessionSvc.SendAudio(pcm); err != nil {
				if err.Error() != "not connected" {
					log.Printf("[capture] send error: %v", err)
				}
			}
		} else {
			log.Printf("[capture] sessionSvc is nil!")
		}
	}

	if sourceType == "system_audio" {
		c, err := audio.StartLoopbackCapture(onData)
		if err != nil {
			return err
		}
		s.capture = c
		log.Printf("[capture] started, source=system_audio (WASAPI loopback)")
		return nil
	}

	// microphone: use winmm via StartCapture
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
}

// StopCapture stops audio capture.
func (s *CaptureService) StopCapture() {
	if s.capture != nil {
		s.capture.Stop()
		s.capture = nil
	}
}
