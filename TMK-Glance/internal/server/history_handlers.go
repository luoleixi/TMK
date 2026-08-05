package server

import (
	"log"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func handleListHistory(c *gin.Context) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	sourceLang, targetLang := c.Query("source_lang"), c.Query("target_lang")
	dateFrom, dateTo, keyword := c.Query("date_from"), c.Query("date_to"), c.Query("keyword")
	var fromTime, toTime *time.Time
	if t, err := time.Parse(time.RFC3339, dateFrom); err == nil && dateFrom != "" {
		fromTime = &t
	}
	if t, err := time.Parse(time.RFC3339, dateTo); err == nil && dateTo != "" {
		toTime = &t
	}

	filtered, total, err := sessionStore.Search(keyword, sourceLang, targetLang, fromTime, toTime, limit, offset)
	if err != nil {
		log.Printf("[db] search history failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "list history failed"})
		return
	}
	for _, ses := range filtered {
		if ses.Brief == "" && ses.RecordCount > 0 && ses.Status != "active" {
			queueSessionBrief(ses.ID)
		}
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{
		"total": total, "offset": offset, "limit": limit, "sessions": filtered,
	}})
}

func handleGetHistory(c *gin.Context) {
	ses, ok, err := sessionStore.Get(c.Param("id"))
	if err != nil {
		log.Printf("[db] get history session failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "get history failed"})
		return
	}
	if !ok {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	records, _, err := sessionStore.Records(ses.ID)
	if err != nil {
		log.Printf("[db] get history records failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "get history failed"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{
		"session_id": ses.ID, "source_lang": ses.SourceLang, "target_lang": ses.TargetLang,
		"duration_seconds": durationSeconds(ses), "brief": ses.Brief, "summary": ses.Summary,
		"created_at": ses.CreatedAt, "ended_at": ses.EndedAt, "records": records,
	}})
}

func handleDeleteHistory(c *gin.Context) {
	ok, err := sessionStore.Delete(c.Param("id"))
	if err != nil {
		log.Printf("[db] delete history failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "delete history failed"})
		return
	}
	if !ok {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok"})
}

func handleDeleteHistoryBatch(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if len(req.IDs) == 0 {
		c.JSON(400, gin.H{"code": 400, "message": "ids are required"})
		return
	}
	deleted, err := sessionStore.DeleteMany(req.IDs)
	if err != nil {
		log.Printf("[db] batch delete history failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "delete history failed"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{"deleted": deleted}})
}
