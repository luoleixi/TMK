package main

import (
	"log"

	"changeme/internal/audio"
)

var (
	captureSvc   *CaptureService
	sessionSvc   *SessionService
)

type CaptureService struct {
	capture *audio.Capture
}

func (s *CaptureService) StartCapture(deviceID int) error {
	if s.capture != nil {
		s.StopCapture()
	}

	c, err := audio.StartCapture(deviceID, func(pcm []byte) {
		if sessionSvc != nil {
			if err := sessionSvc.SendAudio(pcm); err != nil {
				log.Printf("[capture] send audio: %v", err)
			}
		}
	})
	if err != nil {
		return err
	}
	s.capture = c
	log.Printf("[capture] started, device=%d", deviceID)
	return nil
}

func (s *CaptureService) StopCapture() {
	if s.capture != nil {
		s.capture.Stop()
		s.capture = nil
	}
}
