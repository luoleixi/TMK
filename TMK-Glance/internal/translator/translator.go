package translator

import "context"

type Translator interface {
	Translate(ctx context.Context, sourceLang, targetLang, text string) (string, error)
}
