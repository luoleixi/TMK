package server

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tmk-glance/internal/segmenter"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}}

func (a *Application) handleInterpret(c *gin.Context) {
	sessionID := c.Query("session_id")
	if !a.requireSessionOwner(c, sessionID) {
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	a.metrics.WebSocketOpened()
	defer a.metrics.WebSocketClosed()

	if _, ok, err := a.store.Get(sessionID); err != nil {
		log.Printf("[db] validate session failed: %v", err)
		_ = conn.WriteJSON(gin.H{"type": "error", "message": "database error"})
		return
	} else if !ok {
		_ = conn.WriteJSON(gin.H{"type": "error", "message": "invalid session_id"})
		return
	}

	log.Printf("[ws] client connected, session: %s", sessionID)
	actor := newSessionActor(conn, sessionID, a.store, a.translator, segmenter.Config{
		Enabled:         a.cfg.ASR.Segmenter.Enabled,
		MaxRunes:        a.cfg.ASR.Segmenter.MaxRunes,
		MaxDuration:     time.Duration(a.cfg.ASR.Segmenter.MaxDurationMS) * time.Millisecond,
		SoftCommitDelay: time.Duration(a.cfg.ASR.Segmenter.SoftCommitDelayMS) * time.Millisecond,
	}, a.asrFactory, a.queueSessionBrief, a.metrics)
	actor.run()
}
