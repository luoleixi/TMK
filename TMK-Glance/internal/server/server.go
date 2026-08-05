package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/config"
	"tmk-glance/internal/health"
	"tmk-glance/internal/language"
	"tmk-glance/internal/model"
	"tmk-glance/internal/segmenter"
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
var briefJobs sync.Map

func SetupRouter(cfg *config.Config) *gin.Engine {
	asrCfg = cfg
	translateSvc = newTranslator(cfg)

	var err error
	sessionStore, err = store.NewSessionStore(cfg.Storage.Driver, cfg.Storage.DBPath, cfg.Storage.DSN)
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
		v1.POST("/history/:id/summary", handleSummarizeHistory)
		v1.DELETE("/history/:id", handleDeleteHistory)
		v1.POST("/history/delete", handleDeleteHistoryBatch)
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
	if err := sessionStore.Create(ses); err != nil {
		log.Printf("[db] create session failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "create session failed"})
		return
	}

	c.JSON(201, gin.H{"code": 0, "message": "ok", "data": ses})
}

func handleGetSession(c *gin.Context) {
	ses, ok, err := sessionStore.Get(c.Param("id"))
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

func handleStopSession(c *gin.Context) {
	ok, err := sessionStore.End(c.Param("id"))
	if err != nil {
		log.Printf("[db] stop session failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "stop session failed"})
		return
	}
	if !ok {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	queueSessionBrief(c.Param("id"))
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
	result, err := translateSvc.Translate(c.Request.Context(), req.SourceLang, req.TargetLang, req.Text)
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
	keyword := c.Query("keyword")

	var fromTime, toTime *time.Time
	if dateFrom != "" {
		if t, err := time.Parse(time.RFC3339, dateFrom); err == nil {
			fromTime = &t
		}
	}
	if dateTo != "" {
		if t, err := time.Parse(time.RFC3339, dateTo); err == nil {
			toTime = &t
		}
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

	c.JSON(200, gin.H{
		"code":    0,
		"message": "ok",
		"data": gin.H{
			"session_id":       ses.ID,
			"source_lang":      ses.SourceLang,
			"target_lang":      ses.TargetLang,
			"duration_seconds": durationSeconds(ses),
			"brief":            ses.Brief,
			"summary":          ses.Summary,
			"created_at":       ses.CreatedAt,
			"ended_at":         ses.EndedAt,
			"records":          records,
		},
	})
}

func handleSummarizeHistory(c *gin.Context) {
	ses, ok, err := sessionStore.Get(c.Param("id"))
	if err != nil {
		log.Printf("[db] get summary session failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "summarize history failed"})
		return
	}
	if !ok {
		c.JSON(404, gin.H{"code": 404, "message": "session not found"})
		return
	}
	if ses.Summary != "" {
		c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{"summary": ses.Summary}})
		return
	}
	records, _, err := sessionStore.Records(ses.ID)
	if err != nil {
		log.Printf("[db] get summary records failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "summarize history failed"})
		return
	}
	if len(records) == 0 {
		c.JSON(400, gin.H{"code": 400, "message": "no records to summarize"})
		return
	}
	summary, err := summarizeRecords(c.Request.Context(), records)
	if err != nil {
		log.Printf("[summary] generate failed: %v", err)
		c.JSON(502, gin.H{"code": 502, "message": err.Error()})
		return
	}
	if err := sessionStore.UpdateSummary(ses.ID, summary); err != nil {
		log.Printf("[db] update summary failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "save summary failed"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{"summary": summary}})
}

func summarizeRecords(ctx context.Context, records []model.Record) (string, error) {
	content := buildConversationText(records, true)
	systemPrompt := "你是同声传译会话摘要助手。请使用中文输出不超过120字的摘要，包含主要话题、结论和待办事项；只返回摘要正文。"
	return translateSvc.Generate(ctx, systemPrompt, content)
}

func generateBrief(ctx context.Context, records []model.Record) (string, error) {
	content := buildConversationText(records, false)
	systemPrompt := "你是会话主题命名助手。请用8到24个中文字符概括会话主要内容，输出一个简短主题短语，不要写‘总结’或‘摘要’，不要解释。"
	brief, err := translateSvc.Generate(ctx, systemPrompt, content)
	if err != nil {
		return "", err
	}
	brief = normalizeBrief(brief)
	if brief == "" {
		return "", fmt.Errorf("empty brief")
	}
	return brief, nil
}

func buildConversationText(records []model.Record, includeTranslation bool) string {
	var b strings.Builder
	for _, r := range records {
		if includeTranslation {
			fmt.Fprintf(&b, "原文：%s\n译文：%s\n", r.SourceText, r.TranslatedText)
		} else if strings.TrimSpace(r.SourceText) != "" {
			b.WriteString(r.SourceText)
			b.WriteByte('\n')
		}
	}
	return truncateRunes(strings.TrimSpace(b.String()), 8000)
}

func normalizeBrief(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	value = strings.Trim(value, "\"'“”‘’ ")
	for _, prefix := range []string{"AI总结：", "AI 总结：", "总结：", "摘要：", "主题："} {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	value = strings.TrimRight(value, "。！？.!?；;，,")
	return truncateRunes(value, 24)
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func queueSessionBrief(sessionID string) {
	if sessionID == "" {
		return
	}
	if _, loaded := briefJobs.LoadOrStore(sessionID, struct{}{}); loaded {
		return
	}
	go func() {
		defer briefJobs.Delete(sessionID)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		ses, ok, err := sessionStore.Get(sessionID)
		if err != nil || !ok || ses.Brief != "" || ses.RecordCount == 0 {
			if err != nil {
				log.Printf("[brief] get session failed, session=%s err=%v", sessionID, err)
			}
			return
		}
		records, _, err := sessionStore.Records(sessionID)
		if err != nil {
			log.Printf("[brief] get records failed, session=%s err=%v", sessionID, err)
			return
		}
		brief, err := generateBrief(ctx, records)
		if err != nil {
			log.Printf("[brief] generate failed, session=%s err=%v", sessionID, err)
			return
		}
		if err := sessionStore.UpdateBrief(sessionID, brief); err != nil {
			log.Printf("[brief] save failed, session=%s err=%v", sessionID, err)
		}
	}()
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
	if _, ok, err := sessionStore.Get(sessionID); err != nil {
		log.Printf("[db] validate session failed: %v", err)
		conn.WriteJSON(gin.H{"type": "error", "message": "database error"})
		return
	} else if !ok {
		conn.WriteJSON(gin.H{"type": "error", "message": "invalid session_id"})
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
		return asr.NewBailian(key, language, asr.BailianOptions{
			MaxSentenceSilenceMS:         asrCfg.ASR.Bailian.MaxSentenceSilenceMS,
			SemanticPunctuationEnabled:   asrCfg.ASR.Bailian.SemanticPunctuationEnabled,
			MultiThresholdModeEnabled:    asrCfg.ASR.Bailian.MultiThresholdModeEnabled,
			PunctuationPredictionEnabled: asrCfg.ASR.Bailian.PunctuationPredictionEnabled,
		})
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
