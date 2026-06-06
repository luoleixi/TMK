package translator

type Translator interface {
	Translate(sourceLang, targetLang, text string) (string, error)
}
