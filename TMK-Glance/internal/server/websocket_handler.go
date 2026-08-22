package server

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"tmk-glance/internal/observability"
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
	logger := observability.Logger(c.Request.Context()).With("component", "websocket", "session_id", sessionID)
	if !a.requireSessionOwner(c, sessionID) {
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Warn("websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()
	connectedAt := time.Now()
	a.metrics.WebSocketOpened()
	defer func() {
		a.metrics.WebSocketClosed()
		logger.Info("websocket disconnected", "duration_ms", time.Since(connectedAt).Milliseconds())
	}()

	if _, ok, err := a.store.Get(sessionID); err != nil {
		logger.Error("websocket session validation failed", "error", err)
		_ = conn.WriteJSON(gin.H{"type": "error", "message": "database error"})
		return
	} else if !ok {
		_ = conn.WriteJSON(gin.H{"type": "error", "message": "invalid session_id"})
		return
	}

	logger.Info("websocket connected")
	actor := newSessionActor(conn, sessionID, a.store, a.translator, segmenter.Config{
		Enabled:         a.cfg.ASR.Segmenter.Enabled,
		MaxRunes:        a.cfg.ASR.Segmenter.MaxRunes,
		MaxDuration:     time.Duration(a.cfg.ASR.Segmenter.MaxDurationMS) * time.Millisecond,
		SoftCommitDelay: time.Duration(a.cfg.ASR.Segmenter.SoftCommitDelayMS) * time.Millisecond,
	}, a.asrFactory, a.queueSessionBrief, a.metrics)
	actor.logger = logger
	actor.run()
}
