package main

import (
	"log"
	"strings"

	"changeme/internal/audio"
)

var (
	captureSvc *CaptureService
	sessionSvc *SessionService
)

type CaptureService struct {
	capture *audio.Capture
}

// StartCapture begins audio capture for the given source type.
// sourceType: "system_audio" → Stereo Mix, "microphone" → default mic
func (s *CaptureService) StartCapture(sourceType string) error {
	if s.capture != nil {
		s.StopCapture()
	}

	// map sourceType to device
	deviceID := audio.DefaultDevice().ID
	if strings.EqualFold(sourceType, "system_audio") {
		for _, d := range audio.ListDevices() {
			name := strings.ToLower(d.Name)
			if strings.Contains(name, "stereo") || strings.Contains(name, "mix") ||
				strings.Contains(name, "立体声") || strings.Contains(name, "混音") ||
				strings.Contains(name, "loopback") || strings.Contains(name, "wave out") {
				deviceID = d.ID
				log.Printf("[capture] found Stereo Mix: %s (id=%d)", d.Name, deviceID)
				break
			}
		}
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
	log.Printf("[capture] started, source=%s device=%d", sourceType, deviceID)
	return nil
}

// StopCapture stops audio capture.
func (s *CaptureService) StopCapture() {
	if s.capture != nil {
		s.capture.Stop()
		s.capture = nil
	}
}
