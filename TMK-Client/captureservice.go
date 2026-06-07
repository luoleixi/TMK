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
	capture interface{ Stop() }
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
				log.Printf("[capture] send error: %v", err)
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
	deviceID := audio.DefaultDevice().ID
	var err error
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
