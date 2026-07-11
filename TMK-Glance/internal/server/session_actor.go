package server

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type sessionActor struct {
	conn          *websocket.Conn
	sessionID     string
	store         *store.SessionStore
	translatorSvc translator.Translator

	asrEngine asr.ASR
	asrCtx    context.Context
	asrCancel context.CancelFunc
	scheduler *translationScheduler
	audioCh   chan []byte

	writeMu    sync.Mutex
	audioCount int
	seq        int64
}

func newSessionActor(conn *websocket.Conn, sessionID string, sessionStore *store.SessionStore, translatorSvc translator.Translator) *sessionActor {
	return &sessionActor{
		conn:          conn,
		sessionID:     sessionID,
		store:         sessionStore,
		translatorSvc: translatorSvc,
		asrCancel:     func() {},
	}
}

func (a *sessionActor) run() {
	defer a.cleanup()

	for {
		msgType, msg, err := a.conn.ReadMessage()
		if err != nil {
			a.asrCancel()
			return
		}

		if msgType == websocket.BinaryMessage {
			a.handleAudio(msg)
			continue
		}

		var wsMsg struct {
			Type       string `json:"type"`
			SourceLang string `json:"source_lang"`
			TargetLang string `json:"target_lang"`
		}
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			a.writeJSON(gin.H{"type": "error", "message": "invalid websocket message"})
			continue
		}

		if !a.handleControl(wsMsg.Type) {
			return
		}
	}
}

func (a *sessionActor) cleanup() {
	a.asrCancel()
	if a.scheduler != nil {
		a.scheduler.stop()
		a.scheduler = nil
	}
	if a.asrEngine != nil {
		if err := a.asrEngine.Close(); err != nil {
			log.Printf("[asr] close failed, session=%s err=%v", a.sessionID, err)
		}
	}
	if _, err := a.store.End(a.sessionID); err != nil {
		log.Printf("[db] end session failed: %v", err)
	}
}

func (a *sessionActor) handleAudio(msg []byte) {
	if a.audioCh == nil {
		return
	}
	a.audioCh <- msg
	a.audioCount++
	if a.audioCount%50 == 1 {
		log.Printf("[ws] received %d audio chunks, session=%s", a.audioCount, a.sessionID)
	}
}

func (a *sessionActor) handleControl(msgType string) bool {
	switch msgType {
	case "start":
		a.startInterpret()
	case "audio":
		a.writeJSON(gin.H{
			"type":    "error",
			"message": "audio must be sent as binary PCM frames, not JSON text",
		})
	case "ping":
		a.writeJSON(gin.H{"type": "pong", "timestamp_ms": time.Now().UnixMilli()})
	case "stop":
		a.asrCancel()
		if a.scheduler != nil {
			a.scheduler.stop()
			a.scheduler = nil
		}
		a.writeJSON(gin.H{"type": "stopped", "timestamp_ms": time.Now().UnixMilli()})
		return false
	default:
		a.writeJSON(gin.H{"type": "error", "message": "unknown message type"})
	}
	return true
}

func (a *sessionActor) startInterpret() {
	ok, err := a.store.Activate(a.sessionID)
	if err != nil {
		log.Printf("[db] activate session failed: %v", err)
		a.writeJSON(gin.H{"type": "error", "message": "database error"})
		return
	}
	if !ok {
		a.writeJSON(gin.H{"type": "error", "message": "session not ready"})
		return
	}

	ses, _, err := a.store.Get(a.sessionID)
	if err != nil {
		log.Printf("[db] get active session failed: %v", err)
		a.writeJSON(gin.H{"type": "error", "message": "database error"})
		return
	}

	a.asrCtx, a.asrCancel = context.WithCancel(context.Background())
	a.asrEngine = newASR(ses.SourceLang)
	a.audioCh = make(chan []byte, 8)

	resultCh, err := a.asrEngine.Recognize(a.asrCtx, a.audioCh)
	if err != nil {
		if _, dbErr := a.store.Fail(a.sessionID); dbErr != nil {
			log.Printf("[db] fail session failed: %v", dbErr)
		}
		a.writeJSON(gin.H{"type": "error", "message": err.Error()})
		return
	}

	a.scheduler = newTranslationScheduler(a.asrCtx, a.sessionID, ses.SourceLang, ses.TargetLang, a.translatorSvc, a.store, a.writeJSON)
	a.scheduler.start()

	a.writeJSON(gin.H{"type": "started", "timestamp_ms": time.Now().UnixMilli()})

	scheduler := a.scheduler
	go a.consumeASRResults(resultCh, scheduler)
}

func (a *sessionActor) consumeASRResults(resultCh <-chan asr.Result, scheduler *translationScheduler) {
	for r := range resultCh {
		a.seq++
		seq := a.seq
		a.writeJSON(gin.H{
			"type":      "transcript",
			"seq":       seq,
			"text":      r.Text,
			"is_final":  r.IsFinal,
			"timestamp": time.Now().UnixMilli(),
		})
		scheduler.submit(translationJob{Seq: seq, Text: r.Text, IsFinal: r.IsFinal})
	}
}

func (a *sessionActor) writeJSON(v any) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if err := a.conn.WriteJSON(v); err != nil {
		log.Printf("[ws] write failed, session=%s err=%v", a.sessionID, err)
	}
}
