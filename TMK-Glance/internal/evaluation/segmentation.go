package evaluation

import (
	"tmk-glance/internal/model"
	"tmk-glance/internal/segmenter"
)

// SegmentationMetrics compares final predicted segments with ordered human labels.
// Matching is an LCS so a bad split/merge does not receive credit for reordering text.
type SegmentationMetrics struct {
	Matched   int64
	Predicted int64
	Reference int64
}

func CompareSegments(reference []model.ReferenceSegment, predicted []segmenter.Segment) SegmentationMetrics {
	gold := make([]string, 0, len(reference))
	for _, value := range reference {
		if text := normalizedSegmentText(value.Text); text != "" {
			gold = append(gold, text)
		}
	}
	if len(gold) == 0 {
		return SegmentationMetrics{}
	}
	actual := make([]string, 0, len(predicted))
	for _, value := range predicted {
		if !value.IsFinal {
			continue
		}
		if text := normalizedSegmentText(value.Text); text != "" {
			actual = append(actual, text)
		}
	}
	return SegmentationMetrics{Matched: int64(segmentLCS(gold, actual)), Predicted: int64(len(actual)), Reference: int64(len(gold))}
}

func normalizedSegmentText(value string) string {
	return string(normalizedChars(value))
}

func segmentLCS(left, right []string) int {
	if len(left) < len(right) {
		left, right = right, left
	}
	previous := make([]int, len(right)+1)
	current := make([]int, len(right)+1)
	for _, a := range left {
		for j, b := range right {
			if a == b {
				current[j+1] = previous[j] + 1
			} else if current[j] > previous[j+1] {
				current[j+1] = current[j]
			} else {
				current[j+1] = previous[j+1]
			}
		}
		previous, current = current, previous
		for i := range current {
			current[i] = 0
		}
	}
	return previous[len(right)]
}
