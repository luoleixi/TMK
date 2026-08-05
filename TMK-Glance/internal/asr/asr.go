package asr

import "context"

type Result struct {
	Text        string `json:"text"`
	IsFinal     bool   `json:"is_final"`
	BeginTimeMS int64  `json:"begin_time_ms,omitempty"`
	EndTimeMS   int64  `json:"end_time_ms,omitempty"`
}

type ASR interface {
	Recognize(ctx context.Context, audioCh <-chan []byte) (<-chan Result, error)
	Close() error
}
