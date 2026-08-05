package export

import (
	"testing"
	"time"
)

func TestSafeFileName(t *testing.T) {
	if got := safeFileName(`  meeting: Q&A?.  `); got != "meeting- Q&A-" {
		t.Fatalf("safe filename = %q", got)
	}
}

func TestFormatSRTTime(t *testing.T) {
	duration := time.Hour + 2*time.Minute + 3*time.Second + 45*time.Millisecond
	if got := formatSRTTime(duration); got != "01:02:03,045" {
		t.Fatalf("SRT time = %q", got)
	}
}
