package asr

import "testing"

func TestNewBailianNormalizesSentenceSilence(t *testing.T) {
	engine, ok := NewBailian("key", "zh", BailianOptions{MaxSentenceSilenceMS: 100}).(*bailianASR)
	if !ok {
		t.Fatal("NewBailian returned an unexpected ASR implementation")
	}
	if engine.options.MaxSentenceSilenceMS != 600 {
		t.Fatalf("max sentence silence = %d, want 600", engine.options.MaxSentenceSilenceMS)
	}

	engine = NewBailian("key", "zh", BailianOptions{MaxSentenceSilenceMS: 750}).(*bailianASR)
	if engine.options.MaxSentenceSilenceMS != 750 {
		t.Fatalf("valid max sentence silence = %d, want 750", engine.options.MaxSentenceSilenceMS)
	}
}
