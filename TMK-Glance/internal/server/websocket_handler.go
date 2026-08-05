package server

import (
	"log"
	"net/http"
	"time"

	"tmk-glance/internal/segmenter"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func handleInterpret(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	sessionID := c.Query("session_id")
	if _, ok, err := sessionStore.Get(sessionID); err != nil {
		log.Printf("[db] validate session failed: %v", err)
		_ = conn.WriteJSON(gin.H{"type": "error", "message": "database error"})
		return
	} else if !ok {
		_ = conn.WriteJSON(gin.H{"type": "error", "message": "invalid session_id"})
		return
	}

	log.Printf("[ws] client connected, session: %s", sessionID)
	actor := newSessionActor(conn, sessionID, sessionStore, translateSvc, segmenter.Config{
		MaxRunes:        asrCfg.ASR.Segmenter.MaxRunes,
		MaxDuration:     time.Duration(asrCfg.ASR.Segmenter.MaxDurationMS) * time.Millisecond,
		SoftCommitDelay: time.Duration(asrCfg.ASR.Segmenter.SoftCommitDelayMS) * time.Millisecond,
	})
	actor.run()
}
