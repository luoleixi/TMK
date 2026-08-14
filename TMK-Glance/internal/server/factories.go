package server

import (
	"log"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/config"
	"tmk-glance/internal/model"
	"tmk-glance/internal/translator"
)

func newASR(cfg *config.Config, language string) asr.ASR {
	switch cfg.ASR.Provider {
	case "bailian":
		key := cfg.ASR.Bailian.APIKey
		if key == "" {
			log.Fatal("[asr] DASHSCOPE_API_KEY required when asr.provider=bailian")
		}
		log.Println("[asr] using Bailian (DashScope)")
		return asr.NewBailian(key, language, asr.BailianOptions{
			MaxSentenceSilenceMS:         cfg.ASR.Bailian.MaxSentenceSilenceMS,
			SemanticPunctuationEnabled:   cfg.ASR.Bailian.SemanticPunctuationEnabled,
			MultiThresholdModeEnabled:    cfg.ASR.Bailian.MultiThresholdModeEnabled,
			PunctuationPredictionEnabled: cfg.ASR.Bailian.PunctuationPredictionEnabled,
		})
	default:
		log.Println("[asr] using Mock")
		return asr.NewMock()
	}
}

func newEvaluationASR(cfg *config.Config, language string, snapshot model.EvaluationConfig) asr.ASR {
	if snapshot.ASRProvider != "bailian" {
		return asr.NewMock()
	}
	return asr.NewBailian(cfg.ASR.Bailian.APIKey, language, asr.BailianOptions{
		MaxSentenceSilenceMS:         snapshot.MaxSentenceSilenceMS,
		SemanticPunctuationEnabled:   snapshot.SemanticPunctuationEnabled,
		MultiThresholdModeEnabled:    snapshot.MultiThresholdModeEnabled,
		PunctuationPredictionEnabled: snapshot.PunctuationPredictionEnabled,
	})
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
