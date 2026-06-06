package translator

import "fmt"

type mockTranslator struct{}

func NewMock() Translator {
	return &mockTranslator{}
}

func (m *mockTranslator) Translate(sourceLang, targetLang, text string) (string, error) {
	return fmt.Sprintf("[%s→%s] %s", sourceLang, targetLang, text), nil
}
