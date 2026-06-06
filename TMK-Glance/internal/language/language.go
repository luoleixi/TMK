package language

type Lang struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	TTSVoices []string `json:"tts_voices"`
}

var All = []Lang{
	{Code: "zh", Name: "中文", TTSVoices: []string{"longanyang"}},
	{Code: "en", Name: "English", TTSVoices: []string{"longanyang"}},
	{Code: "ja", Name: "日本語", TTSVoices: []string{"longanyang"}},
	{Code: "ko", Name: "한국어", TTSVoices: []string{"longanyang"}},
	{Code: "fr", Name: "Français", TTSVoices: []string{"longanyang"}},
	{Code: "de", Name: "Deutsch", TTSVoices: []string{"longanyang"}},
	{Code: "es", Name: "Español", TTSVoices: []string{"longanyang"}},
	{Code: "ru", Name: "Русский", TTSVoices: []string{"longanyang"}},
}

var validCodes = func() map[string]bool {
	m := make(map[string]bool, len(All))
	for _, l := range All {
		m[l.Code] = true
	}
	return m
}()

func IsValid(code string) bool {
	return validCodes[code]
}

func IsValidPair(source, target string) bool {
	return IsValid(source) && IsValid(target) && source != target
}

func CodeToName(code string) string {
	for _, l := range All {
		if l.Code == code {
			return l.Name
		}
	}
	return ""
}
