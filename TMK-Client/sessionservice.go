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

const backendURL = "http://117.72.159.185:8080/api/v1"

type SessionService struct {
	mu        sync.Mutex
	writeMu   sync.Mutex
	conn      *websocket.Conn
	running   bool
	paused    bool
	sessionID string
	epoch     uint64
}

type TranscriptMsg struct {
	Text      string `json:"text"`
	IsFinal   bool   `json:"is_final"`
	Timestamp int64  `json:"timestamp"`
}

type TranslationMsg struct {
	Text      string `json:"text"`
	IsFinal   bool   `json:"is_final"`
	Timestamp int64  `json:"timestamp"`
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
	old := s.conn
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

	url := fmt.Sprintf("ws://117.72.159.185:8080/api/v1/interpret?session_id=%s", sessionID)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
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

	log.Printf("[ws] connected, session: %s", sessionID)

	// send start
	if err := s.writeControl(conn, map[string]string{"type": "start"}); err != nil {
		conn.Close()
		s.mu.Lock()
		if s.conn == conn && s.epoch == epoch {
			s.conn = nil
			s.running = false
		}
		s.mu.Unlock()
		return fmt.Errorf("send start: %w", err)
	}

	// read loop
	go func() {
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
	conn := s.conn
	running := s.running
	s.mu.Unlock()
	if conn == nil || !running {
		return fmt.Errorf("not connected")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

// StopInterpret ends the translation session
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

// PauseInterpret pauses the current interpret session without closing the connection
func (s *SessionService) PauseInterpret() error {
	s.mu.Lock()
	conn := s.conn
	if conn == nil || !s.running {
		s.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	s.running = false
	s.paused = true
	s.mu.Unlock()
	return s.writeControl(conn, map[string]string{"type": "pause"})
}

// ResumeInterpret resumes a paused interpret session
func (s *SessionService) ResumeInterpret() error {
	s.mu.Lock()
	conn := s.conn
	if conn == nil || !s.paused {
		s.mu.Unlock()
		return fmt.Errorf("not paused or not connected")
	}
	s.running = true
	s.paused = false
	s.mu.Unlock()
	return s.writeControl(conn, map[string]string{"type": "resume"})
}

func (s *SessionService) writeControl(conn *websocket.Conn, v any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return conn.WriteJSON(v)
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
	return s.SearchHistory(offset, limit, "", "", "")
}

func (s *SessionService) SearchHistory(offset, limit int, keyword, dateFrom, dateTo string) ([]HistorySession, int, error) {
	u, _ := url.Parse(backendURL + "/history")
	q := url.Values{}
	q.Set("offset", fmt.Sprint(offset))
	q.Set("limit", fmt.Sprint(limit))
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	if dateFrom != "" {
		q.Set("date_from", dateFrom)
	}
	if dateTo != "" {
		q.Set("date_to", dateTo)
	}
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
		Code int           `json:"code"`
		Data HistoryDetail `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Data.SessionID == "" {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return &result.Data, nil
}

func (s *SessionService) DeleteHistory(sessionID string) error {
	req, err := http.NewRequest(http.MethodDelete, backendURL+"/history/"+sessionID, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete history: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("delete history failed: status %d", resp.StatusCode)
	}
	return nil
}

func (s *SessionService) DeleteHistoryBatch(ids []string) (int, error) {
	body := map[string][]string{"ids": ids}
	data, _ := json.Marshal(body)
	resp, err := http.Post(backendURL+"/history/delete", "application/json", bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("delete history batch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("delete history batch failed: status %d", resp.StatusCode)
	}
	var result struct {
		Data struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.Data.Deleted, nil
}
