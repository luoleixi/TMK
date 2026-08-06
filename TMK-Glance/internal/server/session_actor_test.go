package server

import (
	"testing"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/segmenter"
)

func TestProviderStreamPreservesUpstreamBoundaries(t *testing.T) {
	stream := newProviderStream()
	partial := stream.push(asr.Result{Text: "long cumulative text"})
	if len(partial) != 1 || partial[0].ID != 1 || partial[0].Revision != 1 || partial[0].IsFinal {
		t.Fatalf("unexpected partial: %+v", partial)
	}
	revision := stream.push(asr.Result{Text: "long cumulative text grows"})
	if len(revision) != 1 || revision[0].ID != 1 || revision[0].Revision != 2 || revision[0].IsFinal {
		t.Fatalf("unexpected revision: %+v", revision)
	}
	final := stream.push(asr.Result{Text: "long cumulative text grows", IsFinal: true})
	if len(final) != 1 || final[0].ID != 1 || final[0].Revision != 3 || !final[0].IsFinal || final[0].Reason != segmenter.ReasonProviderFinal {
		t.Fatalf("unexpected provider final: %+v", final)
	}
	next := stream.push(asr.Result{Text: "next sentence"})
	if len(next) != 1 || next[0].ID != 2 || next[0].Revision != 1 {
		t.Fatalf("provider segment ID did not advance: %+v", next)
	}
}

func TestProviderStreamFlushesUnfinishedUpstreamText(t *testing.T) {
	stream := newProviderStream()
	stream.push(asr.Result{Text: "unfinished"})
	flushed := stream.flush()
	if len(flushed) != 1 || !flushed[0].IsFinal || flushed[0].Reason != segmenter.ReasonFlush || flushed[0].Text != "unfinished" {
		t.Fatalf("unexpected provider flush: %+v", flushed)
	}
}
