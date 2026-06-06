package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const backendURL = "http://localhost:8080/api/v1"

type SessionService struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	running  bool
	sessionID string
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

	url := fmt.Sprintf("ws://localhost:8080/api/v1/interpret?session_id=%s", sessionID)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("ws dial: %w", err)
	}

	s.mu.Lock()
	s.conn = conn
	s.running = true
	s.mu.Unlock()

	log.Printf("[ws] connected, session: %s", sessionID)

	// send start
	conn.WriteJSON(map[string]string{"type": "start"})

	// read loop
	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			conn.Close()
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
				application.Get().Event.Emit("transcript", t)

			case "translation":
				var t TranslationMsg
				json.Unmarshal(msg, &t)
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
	defer s.mu.Unlock()

	if s.conn != nil {
		s.conn.WriteJSON(map[string]string{"type": "stop"})
		time.Sleep(100 * time.Millisecond)
		s.conn.Close()
		s.conn = nil
	}
	s.running = false
	return nil
}
