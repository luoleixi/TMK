package asr

import (
	"context"
	"testing"
	"time"
)

func TestLoadTestASRContinuouslyConsumesAudio(t *testing.T) {
	engine := NewLoadTest()
	audio := make(chan []byte, 60)
	results, err := engine.Recognize(context.Background(), audio)
	if err != nil {
		t.Fatal(err)
	}
	for range 50 {
		audio <- make([]byte, 3200)
	}
	select {
	case result := <-results:
		if result.Text == "" {
			t.Fatal("expected deterministic transcript")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for load-test ASR result")
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
}
