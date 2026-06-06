package asr

import "context"

type Result struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
}

type ASR interface {
	Recognize(ctx context.Context, audioCh <-chan []byte) (<-chan Result, error)
	Close() error
}
