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

	var count int
	c, err := audio.StartCapture(deviceID, func(pcm []byte) {
		count++
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
