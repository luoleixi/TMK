package translator

import "context"

type Translator interface {
	Translate(ctx context.Context, sourceLang, targetLang, text string) (string, error)
	Generate(ctx context.Context, systemPrompt, text string) (string, error)
}
