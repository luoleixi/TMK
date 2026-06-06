package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	bailianTokenURL   = "https://dashscope.aliyuncs.com/api/v1/tokens"
	bailianRealtimeWS = "wss://dashscope.aliyuncs.com/api-ws/v1/realtime"
)

// ---------- Token ----------

type tokenResp struct {
	Token struct {
		Token      string `json:"token"`
		ExpireTime int64  `json:"expire_time"`
	} `json:"token"`
}

func fetchToken(apiKey string) (string, error) {
	req, _ := http.NewRequest(http.MethodPost, bailianTokenURL, nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("fetch token: HTTP %d %s", resp.StatusCode, string(body))
	}

	var tr tokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", fmt.Errorf("fetch token: %w", err)
	}
	if tr.Token.Token == "" {
		return "", errors.New("fetch token: empty token in response")
	}
	return tr.Token.Token, nil
}

// ---------- Bailian ASR ----------

type bailianASR struct {
	apiKey string
	token  string
	mu     sync.Mutex
}

func NewBailian(apiKey string) ASR {
	return &bailianASR{apiKey: apiKey}
}

func (b *bailianASR) Recognize(ctx context.Context, audioCh <-chan []byte) (<-chan Result, error) {
	token, err := fetchToken(b.apiKey)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.token = token
	b.mu.Unlock()

	wsURL := fmt.Sprintf("%s?token=%s", bailianRealtimeWS, token)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}
	log.Printf("[asr:bailian] connected")

	out := make(chan Result, 32)
	taskID := fmt.Sprintf("tmk_%d", time.Now().UnixNano())

	// start transcription
	startMsg := map[string]any{
		"header": map[string]any{
			"task_id": taskID,
			"action":  "StartTranscription",
		},
		"payload": map[string]any{
			"format":      "pcm",
			"sample_rate": 16000,
		},
	}
	if err := conn.WriteJSON(startMsg); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send StartTranscription: %w", err)
	}

	var wg sync.WaitGroup

	// goroutine A: audioCh → WS binary frames
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			// send StopTranscription before closing write side
			stopMsg := map[string]any{
				"header": map[string]any{"task_id": taskID, "action": "StopTranscription"},
			}
			conn.WriteJSON(stopMsg)
			conn.Close()
			log.Printf("[asr:bailian] stopped")
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case data, ok := <-audioCh:
				if !ok {
					return
				}
				if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
					log.Printf("[asr:bailian] write audio: %v", err)
					return
				}
			}
		}
	}()

	// goroutine B: read WS → Result
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(out)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				if !errors.Is(err, net.ErrClosed) && ctx.Err() == nil {
					log.Printf("[asr:bailian] read: %v", err)
				}
				return
			}
			var resp struct {
				Header  struct{ Event string `json:"event"` } `json:"header"`
				Payload struct {
					Result  string `json:"result"`
					IsFinal bool   `json:"is_final"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(raw, &resp); err != nil {
				log.Printf("[asr:bailian] parse: %v", err)
				continue
			}
			if resp.Header.Event == "TranscriptionResultChanged" {
				select {
				case out <- Result{Text: resp.Payload.Result, IsFinal: resp.Payload.IsFinal}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// cleanup goroutine
	go func() {
		wg.Wait()
		conn.Close()
	}()

	return out, nil
}

func (b *bailianASR) Close() error { return nil }
