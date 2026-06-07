package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const backendURL = "http://localhost:8080/api/v1"

type SessionService struct {
	mu              sync.Mutex
	conn            *websocket.Conn
	running         bool
	sessionID       string
	subtitleWindow  application.Window
}

type TranscriptMsg struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
	Timestamp int64 `json:"timestamp"`
}

type TranslationMsg struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
	Timestamp int64 `json:"timestamp"`
}

// SetSubtitleWindow sets the subtitle window reference for show/hide control
func (s *SessionService) SetSubtitleWindow(w application.Window) {
	s.subtitleWindow = w
}

// CreateSession creates a new translation session on the backend
func (s *SessionService) CreateSession(sourceLang, targetLang, inputType string) (string, error) {
	body := map[string]string{
		"source_lang": sourceLang,
		"target_lang": targetLang,
		"input_type":  inputType,
	}
	data, _ := json.Marshal(body)
	resp, err := http.Post(backendURL+"/sessions", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code int `json:"code"`
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Data.ID == "" {
		return "", fmt.Errorf("create session: no session_id returned")
	}

	s.mu.Lock()
	s.sessionID = result.Data.ID
	s.mu.Unlock()

	return result.Data.ID, nil
}

// StartInterpret connects to the backend WebSocket for real-time translation
func (s *SessionService) StartInterpret() error {
	s.mu.Lock()
	sessionID := s.sessionID
	s.mu.Unlock()

	if sessionID == "" {
		return fmt.Errorf("no active session")
	}

	// Close existing connection if already running
	s.mu.Lock()
	if s.conn != nil {
		old := s.conn
		s.conn = nil
		s.running = false
		s.mu.Unlock()
		old.WriteJSON(map[string]string{"type": "stop"})
		old.Close()
	} else {
		s.mu.Unlock()
	}

	url := fmt.Sprintf("ws://localhost:8080/api/v1/interpret?session_id=%s", sessionID)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}

	s.mu.Lock()
	s.conn = conn
	s.running = true
	s.mu.Unlock()

	if s.subtitleWindow != nil {
		s.subtitleWindow.Show()
	}

	log.Printf("[ws] connected, session: %s", sessionID)

	// send start
	conn.WriteJSON(map[string]string{"type": "start"})

	// read loop
	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			log.Printf("[ws] disconnected")
		}()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var base struct{ Type string }
			json.Unmarshal(msg, &base)

			switch base.Type {
			case "transcript":
				var t TranscriptMsg
				json.Unmarshal(msg, &t)
				log.Printf("[ws] received transcript: %q (final=%v)", t.Text, t.IsFinal)
				application.Get().Event.Emit("transcript", t)

			case "translation":
				var t TranslationMsg
				json.Unmarshal(msg, &t)
				log.Printf("[ws] received translation: %q (final=%v)", t.Text, t.IsFinal)
				application.Get().Event.Emit("translation", t)

			case "error":
				log.Printf("[ws] error: %s", string(msg))
			}
		}
	}()

	return nil
}

// SendAudio sends a PCM audio chunk to the backend
func (s *SessionService) SendAudio(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil || !s.running {
		return fmt.Errorf("not connected")
	}
	return s.conn.WriteMessage(websocket.BinaryMessage, data)
}

// StopInterpret ends the translation session
func (s *SessionService) StopInterpret() error {
	s.mu.Lock()
	conn := s.conn
	s.conn = nil
	s.running = false
	s.mu.Unlock()

	if conn != nil {
		conn.WriteJSON(map[string]string{"type": "stop"})
		time.Sleep(100 * time.Millisecond)
		conn.Close()
	}
	return nil
}

// PauseInterpret pauses the current interpret session without closing the connection
func (s *SessionService) PauseInterpret() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil || !s.running {
		return fmt.Errorf("not connected")
	}
	s.running = false
	return s.conn.WriteJSON(map[string]string{"type": "pause"})
}

// ResumeInterpret resumes a paused interpret session
func (s *SessionService) ResumeInterpret() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil || s.running {
		return fmt.Errorf("not paused or not connected")
	}
	s.running = true
	return s.conn.WriteJSON(map[string]string{"type": "resume"})
}

// ---------- history ----------

type HistorySession struct {
	ID          string `json:"id"`
	SourceLang  string `json:"source_lang"`
	TargetLang  string `json:"target_lang"`
	Status      string `json:"status"`
	RecordCount int    `json:"record_count"`
	CreatedAt   string `json:"created_at"`
	EndedAt     string `json:"ended_at,omitempty"`
}

type HistoryRecord struct {
	ID              int     `json:"id"`
	SessionID       string  `json:"session_id"`
	Sequence        int     `json:"sequence"`
	SourceText      string  `json:"source_text"`
	TranslatedText  string  `json:"translated_text"`
	Confidence      float64 `json:"confidence"`
	AudioDurationMs int     `json:"audio_duration_ms"`
	CreatedAt       string  `json:"created_at"`
}

type HistoryDetail struct {
	SessionID       string          `json:"session_id"`
	SourceLang      string          `json:"source_lang"`
	TargetLang      string          `json:"target_lang"`
	DurationSeconds int             `json:"duration_seconds"`
	CreatedAt       string          `json:"created_at"`
	EndedAt         string          `json:"ended_at,omitempty"`
	Records         []HistoryRecord `json:"records"`
}

// ListHistory fetches paginated session history from the backend
func (s *SessionService) ListHistory(offset, limit int) ([]HistorySession, int, error) {
	u, _ := url.Parse(backendURL + "/history")
	q := url.Values{}
	q.Set("offset", fmt.Sprint(offset))
	q.Set("limit", fmt.Sprint(limit))
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, 0, fmt.Errorf("list history: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code int `json:"code"`
		Data struct {
			Total    int              `json:"total"`
			Sessions []HistorySession `json:"sessions"`
		} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Data.Sessions, result.Data.Total, nil
}

// GetHistory fetches a single history session with all its records
func (s *SessionService) GetHistory(sessionID string) (*HistoryDetail, error) {
	resp, err := http.Get(backendURL + "/history/" + sessionID)
	if err != nil {
		return nil, fmt.Errorf("get history: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Code int            `json:"code"`
		Data HistoryDetail  `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Data.SessionID == "" {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return &result.Data, nil
}
