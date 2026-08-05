package server

import (
	"log"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/config"
	"tmk-glance/internal/translator"
)

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
