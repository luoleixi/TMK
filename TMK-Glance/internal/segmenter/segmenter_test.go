package segmenter

import (
	"strings"
	"testing"
	"time"
)

func testSegmenter() *Segmenter {
	return New(Config{MaxRunes: 8, MaxDuration: 5 * time.Second, SoftCommitDelay: 300 * time.Millisecond})
}

func TestCumulativePartialIsRevisionedAndDeduplicated(t *testing.T) {
	s := testSegmenter()
	now := time.Now()
	first := s.Push(Input{Text: "\u4eca\u5929\u5929\u6c14"}, now)
	if len(first) != 1 || first[0].ID != 1 || first[0].Revision != 1 || first[0].IsFinal {
		t.Fatalf("unexpected first output: %+v", first)
	}
	if duplicate := s.Push(Input{Text: "\u4eca\u5929\u5929\u6c14"}, now); len(duplicate) != 0 {
		t.Fatalf("duplicate output: %+v", duplicate)
	}
	second := s.Push(Input{Text: "\u4eca\u5929\u5929\u6c14\u5f88\u597d"}, now)
	if len(second) != 1 || second[0].ID != 1 || second[0].Revision != 2 {
		t.Fatalf("unexpected revised output: %+v", second)
	}
}

func TestMaxRunesSplitsCumulativeHypothesisWithoutDuplicates(t *testing.T) {
	s := testSegmenter()
	now := time.Now()
	first := s.Push(Input{Text: "\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u4e03\u516b\u4e5d\u5341"}, now)
	if len(first) != 2 || !first[0].IsFinal || first[0].Text != "\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u4e03\u516b" || first[1].Text != "\u4e5d\u5341" {
		t.Fatalf("unexpected split: %+v", first)
	}
	second := s.Push(Input{Text: "\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u4e03\u516b\u4e5d\u5341\u7532\u4e59"}, now)
	if len(second) != 1 || second[0].Text != "\u4e5d\u5341\u7532\u4e59" || second[0].ID != 2 {
		t.Fatalf("cumulative text was duplicated: %+v", second)
	}
}

func TestProviderFinalUsesSoftCommit(t *testing.T) {
	s := testSegmenter()
	now := time.Now()
	output := s.Push(Input{Text: "\u8fd9\u662f\u4e00\u53e5\u8bdd", IsFinal: true}, now)
	if len(output) != 1 || output[0].IsFinal {
		t.Fatalf("provider final should remain provisional: %+v", output)
	}
	if early := s.Tick(now.Add(299 * time.Millisecond)); len(early) != 0 {
		t.Fatalf("committed too early: %+v", early)
	}
	final := s.Tick(now.Add(300 * time.Millisecond))
	if len(final) != 1 || !final[0].IsFinal || final[0].Reason != ReasonSoftCommit {
		t.Fatalf("unexpected soft commit: %+v", final)
	}
}

func TestIncompleteShortPauseContinuesSameSegment(t *testing.T) {
	s := testSegmenter()
	now := time.Now()
	s.Push(Input{Text: "\u56e0\u4e3a", IsFinal: true}, now)
	output := s.Push(Input{Text: "\u4e0b\u96e8", IsFinal: false}, now.Add(100*time.Millisecond))
	if len(output) != 1 || output[0].ID != 1 || output[0].Text != "\u56e0\u4e3a\u4e0b\u96e8" {
		t.Fatalf("short pause was not merged: %+v", output)
	}
}

func TestCompleteThoughtDoesNotMergeNextProviderSentence(t *testing.T) {
	s := testSegmenter()
	now := time.Now()
	s.Push(Input{Text: "\u4eca\u5929\u4e0b\u96e8", IsFinal: true}, now)
	output := s.Push(Input{Text: "\u8bb0\u5f97\u5e26\u4f1e", IsFinal: false}, now.Add(100*time.Millisecond))
	if len(output) != 2 || !output[0].IsFinal || output[0].ID != 1 || output[1].ID != 2 {
		t.Fatalf("independent sentences were merged: %+v", output)
	}
}

func TestStablePunctuationCreatesBoundary(t *testing.T) {
	s := testSegmenter()
	now := time.Now()
	s.Push(Input{Text: "\u4f60\u597d\u3002\u4eca"}, now)
	output := s.Push(Input{Text: "\u4f60\u597d\u3002\u4eca\u5929"}, now.Add(50*time.Millisecond))
	if len(output) != 2 || !output[0].IsFinal || output[0].Text != "\u4f60\u597d\u3002" || output[1].Text != "\u4eca\u5929" {
		t.Fatalf("stable punctuation did not split: %+v", output)
	}
}

func TestDurationAndFlush(t *testing.T) {
	s := testSegmenter()
	now := time.Now()
	s.Push(Input{Text: "\u6301\u7eed\u8bf4\u8bdd"}, now)
	output := s.Push(Input{Text: "\u6301\u7eed\u8bf4\u8bdd\u4e0d\u505c", BeginTimeMS: 0, EndTimeMS: 5000}, now.Add(5*time.Second))
	if len(output) != 1 || !output[0].IsFinal || output[0].Reason != ReasonMaxDuration {
		t.Fatalf("duration did not force a final segment: %+v", output)
	}

	s2 := testSegmenter()
	s2.Push(Input{Text: "\u5f85\u5237\u65b0"}, now)
	flushed := s2.Flush(now)
	if len(flushed) != 1 || !flushed[0].IsFinal || flushed[0].Reason != ReasonFlush {
		t.Fatalf("unexpected flush: %+v", flushed)
	}
}

func TestMaxRunesCountsUnicodeCodePoints(t *testing.T) {
	s := New(Config{MaxRunes: 4, MaxDuration: time.Hour, SoftCommitDelay: time.Second})
	output := s.Push(Input{Text: strings.Repeat("\u8bed", 5)}, time.Now())
	if len(output) != 2 || output[0].Text != strings.Repeat("\u8bed", 4) || output[1].Text != "\u8bed" {
		t.Fatalf("unexpected unicode split: %+v", output)
	}
}

func TestFinalPunctuationCannotBypassMaxRunes(t *testing.T) {
	s := New(Config{MaxRunes: 4, MaxDuration: time.Hour, SoftCommitDelay: time.Second})
	output := s.Push(Input{Text: "\u4e00\u4e8c\u4e09\u56db\u4e94\u516d\u3002", IsFinal: true}, time.Now())
	if len(output) != 2 || output[0].Text != "\u4e00\u4e8c\u4e09\u56db" || output[0].Reason != ReasonMaxLength || output[1].Text != "\u4e94\u516d\u3002" {
		t.Fatalf("final punctuation bypassed max runes: %+v", output)
	}
}
