package server

import (
	"strings"
	"testing"

	"tmk-glance/internal/model"
)

func TestNormalizeBrief(t *testing.T) {
	brief := normalizeBrief("  AI 总结：跨团队讨论实时翻译产品的发布计划、风险与后续负责人安排。  ")
	if strings.HasPrefix(brief, "AI 总结") {
		t.Fatalf("brief retained label: %q", brief)
	}
	if got := len([]rune(brief)); got > 24 {
		t.Fatalf("brief length = %d, want <= 24", got)
	}
}

func TestBuildConversationTextForBriefUsesSourceOnly(t *testing.T) {
	records := []model.Record{{SourceText: "讨论客户端发布计划", TranslatedText: "client release plan"}}
	text := buildConversationText(records, false)
	if text != records[0].SourceText {
		t.Fatalf("brief input = %q, want source text only", text)
	}
}
