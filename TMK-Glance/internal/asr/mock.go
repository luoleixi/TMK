package asr

import (
	"context"
	"log"
	"math/rand"
	"time"
)

type mockASR struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func NewMock() ASR {
	return &mockASR{}
}

func (m *mockASR) Recognize(ctx context.Context, audioCh <-chan []byte) (<-chan Result, error) {
	m.ctx, m.cancel = context.WithCancel(ctx)
	out := make(chan Result, 8)

	go func() {
		defer close(out)

		samples := []string{
			"你好", "你好世界", "你好世界欢迎", "你好世界欢迎使用",
			"今天天气", "今天天气不错", "今天天气不错适合",
			"机器学习", "机器学习是", "机器学习是人工智能",
		}
		base := samples[rand.Intn(len(samples)/3)*3]

		var text string
		for i := 0; i < len([]rune(base)); i++ {
			select {
			case <-m.ctx.Done():
				return
			case <-audioCh:
				// simulate ASR partial recognition
			case <-time.After(3 * time.Second):
				return
			}

			text = string([]rune(base)[:i+1])
			select {
			case out <- Result{Text: text, IsFinal: false}:
				log.Printf("[asr:mock] interim: %s", text)
			case <-m.ctx.Done():
				return
			}
		}

		select {
		case out <- Result{Text: text, IsFinal: true}:
			log.Printf("[asr:mock] final: %s", text)
		case <-m.ctx.Done():
		}
	}()

	return out, nil
}

func (m *mockASR) Close() error {
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}
