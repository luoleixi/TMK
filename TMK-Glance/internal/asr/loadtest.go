package asr

import "context"

// loadTestASR continuously consumes PCM frames and emits deterministic results.
// It is enabled only by the explicit loadtest provider and never calls an external service.
type loadTestASR struct {
	cancel context.CancelFunc
}

func NewLoadTest() ASR { return &loadTestASR{} }

func (l *loadTestASR) Recognize(ctx context.Context, audioCh <-chan []byte) (<-chan Result, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel
	out := make(chan Result, 8)
	go func() {
		defer close(out)
		frames := 0
		for {
			select {
			case <-streamCtx.Done():
				return
			case _, ok := <-audioCh:
				if !ok {
					return
				}
				frames++
				if frames%10 != 0 {
					continue
				}
				result := Result{Text: "负载测试语音片段", IsFinal: frames%50 == 0}
				select {
				case out <- result:
				case <-streamCtx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func (l *loadTestASR) Close() error {
	if l.cancel != nil {
		l.cancel()
	}
	return nil
}
