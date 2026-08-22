package evaluation

import (
	"testing"

	"tmk-glance/internal/model"
	"tmk-glance/internal/segmenter"
)

func TestCompareSegmentsUsesOrderedLCS(t *testing.T) {
	reference := []model.ReferenceSegment{{Text: "Hello"}, {Text: "world"}, {Text: "again"}}
	predicted := []segmenter.Segment{{Text: "Hello", IsFinal: true}, {Text: "wrong", IsFinal: true}, {Text: "world", IsFinal: true}, {Text: "again", IsFinal: true}}
	metrics := CompareSegments(reference, predicted)
	if metrics.Matched != 3 || metrics.Predicted != 4 || metrics.Reference != 3 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestCompareSegmentsIgnoresPartialAndPunctuation(t *testing.T) {
	metrics := CompareSegments([]model.ReferenceSegment{{Text: "你好，世界！"}}, []segmenter.Segment{
		{Text: "partial", IsFinal: false}, {Text: "你好 世界", IsFinal: true},
	})
	if metrics.Matched != 1 || metrics.Predicted != 1 || metrics.Reference != 1 {
		t.Fatalf("metrics=%+v", metrics)
	}
}

func TestCompareSegmentsWithoutHumanLabelsIsNotEvaluable(t *testing.T) {
	metrics := CompareSegments(nil, []segmenter.Segment{{Text: "prediction", IsFinal: true}})
	if metrics != (SegmentationMetrics{}) {
		t.Fatalf("metrics=%+v, want zero-value non-evaluable metrics", metrics)
	}
}
