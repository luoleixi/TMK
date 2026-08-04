package translator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const bailianTranslateURL = "https://dashscope.aliyuncs.com/api/v1/services/aigc/text-generation/generation"

var langNames = map[string]string{
	"zh": "中文", "en": "英文", "ja": "日文", "ko": "韩文",
	"fr": "法文", "de": "德文", "es": "西班牙文", "ru": "俄文",
}

type bailianTranslator struct {
	apiKey string
	client *http.Client
}

func NewBailian(apiKey string) Translator {
	return &bailianTranslator{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *bailianTranslator) Translate(ctx context.Context, sourceLang, targetLang, text string) (string, error) {
	if text == "" {
		return "", nil
	}

	srcName := langNames[sourceLang]
	if srcName == "" {
		srcName = sourceLang
	}
	tgtName := langNames[targetLang]
	if tgtName == "" {
		tgtName = targetLang
	}

	prompt := fmt.Sprintf("将以下%s文本翻译成%s，只返回翻译结果，不要解释：%s", srcName, tgtName, text)
	return t.Generate(ctx, "你是一个翻译助手，只返回翻译结果。", prompt)
}

func (t *bailianTranslator) Generate(ctx context.Context, systemPrompt, text string) (string, error) {
	if text == "" {
		return "", nil
	}

	reqBody := map[string]any{
		"model": "qwen-turbo",
		"input": map[string]any{
			"messages": []map[string]string{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": text},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal translate request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", bailianTranslateURL, bytes.NewReader(jsonData))
	if err != nil {
		return "", fmt.Errorf("create translate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send translate request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read translate response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("translate request failed: status=%d body=%s", resp.StatusCode, string(body))
	}

	var result struct {
		Output struct {
			Text string `json:"text"`
		} `json:"output"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode translate response: %w", err)
	}
	if result.Code != "" {
		return "", fmt.Errorf("translate service error: %s %s", result.Code, result.Message)
	}

	if result.Output.Text != "" {
		return result.Output.Text, nil
	}
	return "", fmt.Errorf("translate response missing output text")
}
