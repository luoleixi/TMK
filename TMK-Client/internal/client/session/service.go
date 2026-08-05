package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	runtimeconfig "changeme/internal/client/runtime"

	"github.com/gorilla/websocket"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type SessionService struct {
	mu           sync.Mutex
	writeMu      sync.Mutex
	conn         *websocket.Conn
	running      bool
	paused       bool
	sessionID    string
	epoch        uint64
	httpClient   *http.Client
	apiURL       func() string
	webSocketURL func(string, url.Values) (string, error)
	dialer       *websocket.Dialer
}

func NewService() *SessionService {
	return &SessionService{
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		apiURL:       runtimeconfig.BackendAPIURL,
		webSocketURL: runtimeconfig.BackendWebSocketURL,
		dialer:       websocket.DefaultDialer,
	}
}

func (s *SessionService) CreateSession(sourceLang, targetLang, inputType string) (string, error) {
	body, err := json.Marshal(map[string]string{"source_lang": sourceLang, "target_lang": targetLang, "input_type": inputType})
	if err != nil {
		return "", fmt.Errorf("encode session: %w", err)
	}
	resp, err := s.httpClient.Post(s.apiURL()+"/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode session: %w", err)
	}
	if result.Data.ID == "" {
		return "", fmt.Errorf("create session: no session_id returned")
	}
	s.mu.Lock()
	s.sessionID = result.Data.ID
	s.mu.Unlock()
	return result.Data.ID, nil
}

func (s *SessionService) StartInterpret() error {
	s.mu.Lock()
	sessionID, old := s.sessionID, s.conn
	if old != nil {
		s.conn = nil
		s.running = false
		s.paused = false
		s.epoch++
	}
	s.mu.Unlock()
	if sessionID == "" {
		return fmt.Errorf("no active session")
	}
	if old != nil {
		_ = s.writeControl(old, map[string]string{"type": "stop"})
		_ = old.Close()
	}

	q := url.Values{"session_id": []string{sessionID}}
	wsURL, err := s.webSocketURL("/interpret", q)
	if err != nil {
		return err
	}
	conn, _, err := s.dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}

	s.mu.Lock()
	s.conn = conn
	s.running = true
	s.paused = false
	s.epoch++
	epoch := s.epoch
	s.mu.Unlock()
	if err := s.writeControl(conn, map[string]string{"type": "start"}); err != nil {
		_ = conn.Close()
		s.mu.Lock()
		if s.conn == conn && s.epoch == epoch {
			s.conn = nil
			s.running = false
		}
		s.mu.Unlock()
		return fmt.Errorf("send start: %w", err)
	}
	log.Printf("[ws] connected, session: %s", sessionID)
	go s.readLoop(conn, epoch)
	return nil
}

func (s *SessionService) readLoop(conn *websocket.Conn, epoch uint64) {
	defer func() {
		s.mu.Lock()
		if s.conn == conn && s.epoch == epoch {
			s.conn = nil
			s.running = false
			s.paused = false
		}
		s.mu.Unlock()
		log.Printf("[ws] disconnected")
	}()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(message, &envelope); err != nil {
			log.Printf("[ws] decode event: %v", err)
			continue
		}
		switch envelope.Type {
		case "started":
			application.Get().Event.Emit("stream-reset", true)
		case "transcript":
			var event TranscriptMsg
			if err := json.Unmarshal(message, &event); err == nil {
				application.Get().Event.Emit("transcript", event)
			}
		case "translation":
			var event TranslationMsg
			if err := json.Unmarshal(message, &event); err == nil {
				application.Get().Event.Emit("translation", event)
			}
		case "error":
			log.Printf("[ws] error: %s", message)
		}
	}
}

func (s *SessionService) SendAudio(data []byte) error {
	s.mu.Lock()
	conn, running := s.conn, s.running
	s.mu.Unlock()
	if conn == nil || !running {
		return fmt.Errorf("not connected")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func (s *SessionService) StopInterpret() error {
	s.mu.Lock()
	conn := s.conn
	s.conn = nil
	s.running = false
	s.paused = false
	s.epoch++
	s.mu.Unlock()
	if conn != nil {
		_ = s.writeControl(conn, map[string]string{"type": "stop"})
		time.Sleep(100 * time.Millisecond)
		_ = conn.Close()
	}
	return nil
}

func (s *SessionService) PauseInterpret() error {
	s.mu.Lock()
	if s.conn == nil || !s.running {
		s.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	conn := s.conn
	s.running = false
	s.paused = true
	s.mu.Unlock()
	return s.writeControl(conn, map[string]string{"type": "pause"})
}

func (s *SessionService) ResumeInterpret() error {
	s.mu.Lock()
	if s.conn == nil || !s.paused {
		s.mu.Unlock()
		return fmt.Errorf("not paused or not connected")
	}
	conn := s.conn
	s.running = true
	s.paused = false
	s.mu.Unlock()
	return s.writeControl(conn, map[string]string{"type": "resume"})
}

func (s *SessionService) writeControl(conn *websocket.Conn, value any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return conn.WriteJSON(value)
}
