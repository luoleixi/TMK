package translator

import (
	"context"
	"fmt"
)

type mockTranslator struct{}

func NewMock() Translator {
	return &mockTranslator{}
}

func (m *mockTranslator) Translate(ctx context.Context, sourceLang, targetLang, text string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	return fmt.Sprintf("[%s→%s] %s", sourceLang, targetLang, text), nil
}

func (m *mockTranslator) Generate(ctx context.Context, systemPrompt, text string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	return text, nil
}
