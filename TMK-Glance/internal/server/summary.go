package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"tmk-glance/internal/model"

	"github.com/gin-gonic/gin"
)

func (a *Application) handleSummarizeHistory(c *gin.Context) {
	ses, ok, err := a.store.Get(c.Param("id"))
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
	records, _, err := a.store.Records(ses.ID)
	if err != nil {
		log.Printf("[db] get summary records failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "summarize history failed"})
		return
	}
	if len(records) == 0 {
		c.JSON(400, gin.H{"code": 400, "message": "no records to summarize"})
		return
	}
	summary, err := a.summarizeRecords(c.Request.Context(), records)
	if err != nil {
		log.Printf("[summary] generate failed: %v", err)
		c.JSON(502, gin.H{"code": 502, "message": err.Error()})
		return
	}
	if err := a.store.UpdateSummary(ses.ID, summary); err != nil {
		log.Printf("[db] update summary failed: %v", err)
		c.JSON(500, gin.H{"code": 500, "message": "save summary failed"})
		return
	}
	c.JSON(200, gin.H{"code": 0, "message": "ok", "data": gin.H{"summary": summary}})
}

func (a *Application) summarizeRecords(ctx context.Context, records []model.Record) (string, error) {
	content := buildConversationText(records, true)
	systemPrompt := "你是同声传译会话摘要助手。请使用中文输出不超过120字的摘要，包含主要话题、结论和待办事项；只返回摘要正文。"
	return a.translator.Generate(ctx, systemPrompt, content)
}

func (a *Application) generateBrief(ctx context.Context, records []model.Record) (string, error) {
	content := buildConversationText(records, false)
	systemPrompt := "你是会话主题命名助手。请用8到24个中文字符概括会话主要内容，输出一个简短主题短语，不要写‘总结’或‘摘要’，不要解释。"
	brief, err := a.translator.Generate(ctx, systemPrompt, content)
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

func (a *Application) queueSessionBrief(sessionID string) {
	if sessionID == "" {
		return
	}
	if _, loaded := a.briefJobs.LoadOrStore(sessionID, struct{}{}); loaded {
		return
	}
	go func() {
		defer a.briefJobs.Delete(sessionID)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		ses, ok, err := a.store.Get(sessionID)
		if err != nil || !ok || ses.Brief != "" || ses.RecordCount == 0 {
			if err != nil {
				log.Printf("[brief] get session failed, session=%s err=%v", sessionID, err)
			}
			return
		}
		records, _, err := a.store.Records(sessionID)
		if err != nil {
			log.Printf("[brief] get records failed, session=%s err=%v", sessionID, err)
			return
		}
		brief, err := a.generateBrief(ctx, records)
		if err != nil {
			log.Printf("[brief] generate failed, session=%s err=%v", sessionID, err)
			return
		}
		if err := a.store.UpdateBrief(sessionID, brief); err != nil {
			log.Printf("[brief] save failed, session=%s err=%v", sessionID, err)
		}
	}()
}
