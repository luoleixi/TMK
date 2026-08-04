package server

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	audioQueueSize = 32
	sendQueueSize  = 128
	wsWriteWait    = 10 * time.Second
	wsPongWait     = 60 * time.Second
	wsPingPeriod   = (wsPongWait * 9) / 10
	wsReadLimit    = 1 << 20
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

	sendCh           chan any
	done             chan struct{}
	audioCount       int
	droppedAudioCnt  int
	droppedSendCount int
	seq              int64
}

func newSessionActor(conn *websocket.Conn, sessionID string, sessionStore *store.SessionStore, translatorSvc translator.Translator) *sessionActor {
	return &sessionActor{
		conn:          conn,
		sessionID:     sessionID,
		store:         sessionStore,
		translatorSvc: translatorSvc,
		asrCancel:     func() {},
		sendCh:        make(chan any, sendQueueSize),
		done:          make(chan struct{}),
	}
}

func (a *sessionActor) run() {
	writerDone := make(chan struct{})
	go a.writePump(writerDone)

	a.readPump()
	a.cleanup()
	close(a.done)
	<-writerDone
}

func (a *sessionActor) readPump() {
	a.conn.SetReadLimit(wsReadLimit)
	a.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	a.conn.SetPongHandler(func(string) error {
		a.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})
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
	if ended, err := a.store.End(a.sessionID); err != nil {
		log.Printf("[db] end session failed: %v", err)
	} else if ended {
		queueSessionBrief(a.sessionID)
	}
}

func (a *sessionActor) handleAudio(msg []byte) {
	if a.audioCh == nil {
		return
	}
	select {
	case a.audioCh <- msg:
		a.audioCount++
		if a.audioCount%50 == 1 {
			log.Printf("[ws] received %d audio chunks, session=%s", a.audioCount, a.sessionID)
		}
	default:
		a.droppedAudioCnt++
		if a.droppedAudioCnt%50 == 1 {
			log.Printf("[ws] drop audio chunk, session=%s dropped=%d", a.sessionID, a.droppedAudioCnt)
		}
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
	a.audioCh = make(chan []byte, audioQueueSize)

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
	select {
	case a.sendCh <- v:
	case <-a.done:
	default:
		a.droppedSendCount++
		if a.droppedSendCount%50 == 1 {
			log.Printf("[ws] drop outbound message, session=%s dropped=%d", a.sessionID, a.droppedSendCount)
		}
	}
}

func (a *sessionActor) writePump(done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()

	for {
		select {
		case msg := <-a.sendCh:
			if !a.writeQueuedJSON(msg) {
				return
			}
		case <-ticker.C:
			a.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := a.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("[ws] ping failed, session=%s err=%v", a.sessionID, err)
				return
			}
		case <-a.done:
			a.drainSendQueue()
			return
		}
	}
}

func (a *sessionActor) drainSendQueue() {
	for {
		select {
		case msg := <-a.sendCh:
			if !a.writeQueuedJSON(msg) {
				return
			}
		default:
			return
		}
	}
}

func (a *sessionActor) writeQueuedJSON(msg any) bool {
	a.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
	if err := a.conn.WriteJSON(msg); err != nil {
		log.Printf("[ws] write failed, session=%s err=%v", a.sessionID, err)
		return false
	}
	return true
}
