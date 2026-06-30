package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/config"
	"tmk-glance/internal/health"
	"tmk-glance/internal/language"
	"tmk-glance/internal/model"
	"tmk-glance/internal/store"
	"tmk-glance/internal/translator"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

var sessionStore *store.SessionStore
var translateSvc translator.Translator

func SetupRouter(cfg *config.Config) *gin.Engine {
	asrCfg = cfg
	translateSvc = newTranslator(cfg)

	var err error
	sessionStore, err = store.NewSessionStore(cfg.Storage.DBPath)
	if err != nil {
		log.Fatalf("[db] init failed: %v", err)
	}

	r := gin.Default()

	r.Use(corsMiddleware())

	r.GET("/api/health", handleHealth)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/languages", handleLanguages)
		v1.GET("/audio/devices", handleAudioDevices)
		v1.POST("/sessions", handleCreateSession)
		v1.GET("/sessions/:id", handleGetSession)
		v1.POST("/sessions/:id/stop", handleStopSession)

		v1.GET("/history", handleListHistory)
		v1.GET("/history/:id", handleGetHistory)
		v1.POST("/translate", handleTranslate)
		v1.GET("/interpret", handleInterpret)
	}

	return r
}

// ---------- health ----------

func handleHealth(c *gin.Context) {
	c.JSON(200, gin.H{
		"status":    health.Status(),
		"timestamp": time.Now().Unix(),
		"version":   "1.0.0",
		"services":  health.Services(),
	})
}

// ---------- languages ----------

func handleLanguages(c *gin.Context) {
	c.JSON(200, gin.H{"languages": language.All})
}

// ---------- audio devices ----------

func handleAudioDevices(c *gin.Context) {
	c.JSON(200, gin.H{
		"inputs": []gin.H{
			{"id": "mic_0", "name": "内置麦克风", "type": "microphone", "is_default": true},
			{"id": "sys_0", "name": "系统音频 (Stereo Mix)", "type": "system_audio", "is_default": false},
		},
		"outputs": []gin.H{
			{"id": "spk_0", "name": "内置扬声器", "type": "speaker", "is_default": true},
		},
	})
}

// ---------- sessions ----------

func handleCreateSession(c *gin.Context) {
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
		ID:         uuid.New().String(),
		SourceLang: req.SourceLang,
		TargetLang: req.TargetLang,
		InputType:  req.InputType,
		Status:     "ready",
		CreatedAt:  time.Now(),
	}
	sessionStore.Create(ses)

	c.JSON(201, gin.H{"code": 0, "message": "ok", "data": ses})
}

func handleGetSession(c *gin.Context) {
	ses, ok := sessionStore.Get(c.Param("id"))
	if !ok {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": ses})
}

func handleStopSession(c *gin.Context) {
	if !sessionStore.End(c.Param("id")) {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok"})
}

// ---------- translate ----------

func handleTranslate(c *gin.Context) {
	var req struct {
		Text       string `json:"text"`
		SourceLang string `json:"source_lang"`
		TargetLang string `json:"target_lang"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"code": 400, "message": err.Error()})
		return
	}
	if req.Text == "" || req.SourceLang == "" || req.TargetLang == "" {
		c.JSON(400, gin.H{"code": 400, "message": "text, source_lang and target_lang are required"})
		return
	}
	result, err := translateSvc.Translate(req.SourceLang, req.TargetLang, req.Text)
	if err != nil {
		c.JSON(502, gin.H{"code": 502, "message": err.Error()})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{"translated_text": result}})
}

// ---------- history ----------

func handleListHistory(c *gin.Context) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit < 1 || limit > 100 {
		limit = 20
	}
	sourceLang := c.Query("source_lang")
	targetLang := c.Query("target_lang")
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")

	all := sessionStore.List()
	filtered := make([]*model.Session, 0)

	var fromTime, toTime time.Time
	if dateFrom != "" {
		if t, err := time.Parse(time.RFC3339, dateFrom); err == nil {
			fromTime = t
		}
	}
	if dateTo != "" {
		if t, err := time.Parse(time.RFC3339, dateTo); err == nil {
			toTime = t
		}
	}

	for _, ses := range all {
		if sourceLang != "" && ses.SourceLang != sourceLang {
			continue
		}
		if targetLang != "" && ses.TargetLang != targetLang {
			continue
		}
		if !fromTime.IsZero() && ses.CreatedAt.Before(fromTime) {
			continue
		}
		if !toTime.IsZero() && ses.CreatedAt.After(toTime) {
			continue
		}
		filtered = append(filtered, ses)
	}

	total := len(filtered)
	if offset >= total {
		offset = 0
		filtered = nil
	}
	if offset < total {
		end := offset + limit
		if end > total {
			end = total
		}
		filtered = filtered[offset:end]
	}

	c.JSON(200, gin.H{
		"code":    0,
		"message": "ok",
		"data": gin.H{
			"total":    total,
			"offset":   offset,
			"limit":    limit,
			"sessions": filtered,
		},
	})
}

func handleGetHistory(c *gin.Context) {
	ses, ok := sessionStore.Get(c.Param("id"))
	if !ok {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	records, _ := sessionStore.Records(ses.ID)

	c.JSON(200, gin.H{
		"code":    0,
		"message": "ok",
		"data": gin.H{
			"session_id":       ses.ID,
			"source_lang":      ses.SourceLang,
			"target_lang":      ses.TargetLang,
			"duration_seconds": durationSeconds(ses),
			"created_at":       ses.CreatedAt,
			"ended_at":         ses.EndedAt,
			"records":          records,
		},
	})
}

func durationSeconds(ses *model.Session) int {
	if ses.EndedAt == nil {
		return int(time.Since(ses.CreatedAt).Seconds())
	}
	return int(ses.EndedAt.Sub(ses.CreatedAt).Seconds())
}

// ---------- WebSocket ----------

func handleInterpret(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	sessionID := c.Query("session_id")
	if _, ok := sessionStore.Get(sessionID); !ok {
		conn.WriteJSON(gin.H{"type": "error", "message": "invalid session_id"})
		return
	}
	log.Printf("[ws] client connected, session: %s", sessionID)

	// Mark session completed when handler returns.
	defer sessionStore.End(sessionID)

	var (
		asrEngine asr.ASR
		asrCtx    context.Context
		asrCancel context.CancelFunc = func() {}
		audioCh   chan []byte
		cnt       int
		writeMu   sync.Mutex
	)
	writeJSON := func(v any) {
		writeMu.Lock()
		conn.WriteJSON(v)
		writeMu.Unlock()
	}
	defer func() {
		asrCancel()
		if asrEngine != nil {
			asrEngine.Close()
		}
	}()

	for {
		msgType, msg, err := conn.ReadMessage()
		if err != nil {
			asrCancel()
			break
		}
		// binary = raw PCM audio
		if msgType == websocket.BinaryMessage {
			if audioCh != nil {
				audioCh <- msg
				cnt++
				if cnt%50 == 1 {
					log.Printf("[ws] received %d audio chunks", cnt)
				}
			}
			continue
		}
		var wsMsg struct {
			Type       string `json:"type"`
			SourceLang string `json:"source_lang"`
			TargetLang string `json:"target_lang"`
		}
		json.Unmarshal(msg, &wsMsg)

		switch wsMsg.Type {
		case "start":
			if !sessionStore.Activate(sessionID) {
				writeJSON(gin.H{"type": "error", "message": "session not ready"})
				continue
			}
			ses, _ := sessionStore.Get(sessionID)
			asrCtx, asrCancel = context.WithCancel(context.Background())
			asrEngine = newASR(ses.SourceLang)
			audioCh = make(chan []byte, 8)

			resultCh, err := asrEngine.Recognize(asrCtx, audioCh)
			if err != nil {
				sessionStore.Fail(sessionID)
				writeJSON(gin.H{"type": "error", "message": err.Error()})
				return
			}

			sourceLang := ses.SourceLang
			targetLang := ses.TargetLang

			writeJSON(gin.H{"type": "started", "timestamp_ms": time.Now().UnixMilli()})

			go func() {
				for r := range resultCh {
					writeJSON(gin.H{
						"type":      "transcript",
						"text":      r.Text,
						"is_final":  r.IsFinal,
						"timestamp": time.Now().UnixMilli(),
					})
					if r.Text != "" {
						translated, err := translateSvc.Translate(sourceLang, targetLang, r.Text)
						payload := gin.H{
							"type":      "translation",
							"text":      translated,
							"is_final":  r.IsFinal,
							"timestamp": time.Now().UnixMilli(),
						}
						if err != nil {
							log.Printf("[translate] fallback to source text: %v", err)
							translated = r.Text
							payload["text"] = translated
							payload["warning"] = "translate_failed_fallback_to_source"
						}
						writeJSON(payload)
						if r.IsFinal {
							sessionStore.AddRecord(sessionID, model.Record{
								SessionID:      sessionID,
								SourceText:     r.Text,
								TranslatedText: translated,
								CreatedAt:      time.Now(),
							})
						}
					}
				}
			}()

		case "audio":
			writeJSON(gin.H{
				"type":    "error",
				"message": "audio must be sent as binary PCM frames, not JSON text",
			})

		case "ping":
			writeJSON(gin.H{"type": "pong", "timestamp_ms": time.Now().UnixMilli()})

		case "stop":
			asrCancel()
			writeJSON(gin.H{"type": "stopped", "timestamp_ms": time.Now().UnixMilli()})
			return
		}
	}
}

// ---------- ASR factory ----------

var asrCfg *config.Config

func newASR(language string) asr.ASR {
	switch asrCfg.ASR.Provider {
	case "bailian":
		key := asrCfg.ASR.Bailian.APIKey
		if key == "" {
			log.Fatal("[asr] DASHSCOPE_API_KEY required when asr.provider=bailian")
		}
		log.Println("[asr] using Bailian (DashScope)")
		return asr.NewBailian(key, language)
	default:
		log.Println("[asr] using Mock")
		return asr.NewMock()
	}
}

// ---------- Translator factory ----------

func newTranslator(cfg *config.Config) translator.Translator {
	switch cfg.Translator.Provider {
	case "bailian":
		key := cfg.Translator.Bailian.APIKey
		if key == "" {
			log.Fatal("[translator] DASHSCOPE_API_KEY required when translator.provider=bailian")
		}
		log.Println("[translator] using Bailian (qwen-turbo)")
		return translator.NewBailian(key)
	default:
		log.Println("[translator] using Mock")
		return translator.NewMock()
	}
}

// ---------- middleware ----------

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(200)
			return
		}
		c.Next()
	}
}
