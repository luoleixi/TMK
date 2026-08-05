package server

import (
	"log"
	"time"

	"tmk-glance/internal/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (a *Application) handleCreateSession(c *gin.Context) {
	var req struct {
		SourceLang string `json:"source_lang"`
		TargetLang string `json:"target_lang"`
		InputType  string `json:"input_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if req.SourceLang == "" || req.TargetLang == "" {
		c.JSON(400, gin.H{"code": 400, "message": "source_lang and target_lang are required"})
		return
	}
	if req.InputType == "" {
		req.InputType = "system_audio"
	}

	ses := &model.Session{
		ID: uuid.New().String(), SourceLang: req.SourceLang, TargetLang: req.TargetLang,
		InputType: req.InputType, Status: "ready", CreatedAt: time.Now(),
	}
	if err := a.store.Create(ses); err != nil {
		log.Printf("[db] create session failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "create session failed"})
		return
	}
	c.JSON(201, gin.H{"code": 0, "message": "ok", "data": ses})
}

func (a *Application) handleGetSession(c *gin.Context) {
	ses, ok, err := a.store.Get(c.Param("id"))
	if err != nil {
		log.Printf("[db] get session failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "get session failed"})
		return
	}
	if !ok {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": ses})
}

func (a *Application) handleStopSession(c *gin.Context) {
	ok, err := a.store.End(c.Param("id"))
	if err != nil {
		log.Printf("[db] stop session failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "stop session failed"})
		return
	}
	if !ok {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	a.queueSessionBrief(c.Param("id"))
	c.JSON(200, gin.H{"code": 0, "message": "ok"})
}

func durationSeconds(ses *model.Session) int {
	if ses.EndedAt == nil {
		return int(time.Since(ses.CreatedAt).Seconds())
	}
	return int(ses.EndedAt.Sub(ses.CreatedAt).Seconds())
}
