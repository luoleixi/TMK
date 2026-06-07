package asr

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const bailianWSURL = "wss://dashscope.aliyuncs.com/api-ws/v1/inference"

type bailianASR struct {
	apiKey   string
	language string
	conn     *websocket.Conn
	mu       sync.RWMutex
}

func NewBailian(apiKey, language string) ASR {
	if language == "" {
		language = "zh"
	}
	return &bailianASR{apiKey: apiKey, language: language}
}

func (b *bailianASR) Recognize(ctx context.Context, audioCh <-chan []byte) (<-chan Result, error) {
	header := http.Header{
		"Authorization":              {fmt.Sprintf("Bearer %s", b.apiKey)},
		"X-DashScope-DataInspection": {"enable"},
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, bailianWSURL, header)
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	log.Printf("[asr:bailian] connected")

	b.mu.Lock()
	b.conn = conn
	b.mu.Unlock()

	taskID := uuid.New().String()

	// send run-task
	if err := b.sendRunTask(taskID); err != nil {
		conn.Close()
		return nil, err
	}

	// wait for task-started
	if err := b.waitTaskStarted(conn, 10*time.Second); err != nil {
		conn.Close()
		return nil, err
	}
	log.Printf("[asr:bailian] task started: %s", taskID)

	out := make(chan Result, 32)

	// audio sender
	go func() {
		defer func() {
			b.sendFinishTask(taskID)
			b.mu.Lock()
			b.conn = nil
			b.mu.Unlock()
			conn.Close()
			log.Printf("[asr:bailian] stopped")
		}()

		var bcnt int
		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-audioCh:
				if !ok {
					return
				}
				bcnt++
				if bcnt%50 == 1 {
					log.Printf("[asr:bailian] sent %d audio chunks to Bailian", bcnt)
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
					log.Printf("[asr:bailian] write audio error: %v", err)
					return
				}
			}
		}
	}()

	// result receiver
	go func() {
		defer close(out)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("[asr:bailian] read: %v", err)
				}
				return
			}
			var event struct {
				Header struct {
					Event        string `json:"event"`
					ErrorCode    string `json:"error_code"`
					ErrorMessage string `json:"error_message"`
				} `json:"header"`
				Payload struct {
					Output struct {
						Sentence struct {
							Text    string `json:"text"`
							EndTime *int64 `json:"end_time"`
							BeginTime int64 `json:"begin_time"`
							SentenceEnd bool `json:"sentence_end"`
						} `json:"sentence"`
					} `json:"output"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(raw, &event); err != nil {
				continue
			}

			switch event.Header.Event {
			case "result-generated":
				text := event.Payload.Output.Sentence.Text
				isFinal := event.Payload.Output.Sentence.SentenceEnd
				if text != "" {
					log.Printf("[asr:bailian] result: %q (final=%v)", text, isFinal)
					select {
					case out <- Result{Text: text, IsFinal: isFinal}:
					case <-ctx.Done():
						return
					}
				}
			case "task-finished":
				log.Printf("[asr:bailian] task finished")
				return
			case "task-failed":
				log.Printf("[asr:bailian] task failed: %s %s", event.Header.ErrorCode, event.Header.ErrorMessage)
				return
			default:
				log.Printf("[asr:bailian] unexpected event: %s (raw: %s)", event.Header.Event, string(raw))
			}
		}
	}()

	return out, nil
}

func (b *bailianASR) sendRunTask(taskID string) error {
	msg := map[string]any{
		"header": map[string]any{
			"action":    "run-task",
			"task_id":   taskID,
			"streaming": "duplex",
		},
		"payload": map[string]any{
			"task_group": "audio",
			"task":       "asr",
			"function":   "recognition",
			"model":      "paraformer-realtime-v2",
			"parameters": map[string]any{
				"language":    b.language,
				"sample_rate": 16000,
				"format":      "pcm",
			},
			"input": map[string]any{},
		},
	}

	b.mu.RLock()
	conn := b.conn
	b.mu.RUnlock()

	return conn.WriteJSON(msg)
}

func (b *bailianASR) sendFinishTask(taskID string) {
	msg := map[string]any{
		"header": map[string]any{
			"action":    "finish-task",
			"task_id":   taskID,
			"streaming": "duplex",
		},
		"payload": map[string]any{"input": map[string]any{}},
	}

	b.mu.RLock()
	conn := b.conn
	b.mu.RUnlock()

	if conn != nil {
		conn.WriteJSON(msg)
	}
}

func (b *bailianASR) waitTaskStarted(conn *websocket.Conn, timeout time.Duration) error {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return fmt.Errorf("timeout waiting for task-started")
		default:
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read during start: %w", err)
		}
		var event struct {
			Header struct {
				Event        string `json:"event"`
				ErrorCode    string `json:"error_code"`
				ErrorMessage string `json:"error_message"`
			} `json:"header"`
		}
		json.Unmarshal(raw, &event)

		switch event.Header.Event {
		case "task-started":
			return nil
		case "task-failed":
			return fmt.Errorf("task-failed: %s %s", event.Header.ErrorCode, event.Header.ErrorMessage)
		}
	}
}

func (b *bailianASR) Close() error {
	b.mu.RLock()
	conn := b.conn
	b.mu.RUnlock()
	if conn != nil {
		conn.Close()
	}
	return nil
}
