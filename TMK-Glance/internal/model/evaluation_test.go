package model

import (
	"math"
	"testing"
)

func TestEvaluationJobRatesUseCorpusLevelAggregation(t *testing.T) {
	job := EvaluationJob{
		ASRCharDistance:       3,
		ASRCharUnits:          12,
		SegmentedCharDistance: 1,
		SegmentedCharUnits:    12,
		ASRWordDistance:       2,
		ASRWordUnits:          8,
		SegmentedWordDistance: 1,
		SegmentedWordUnits:    8,
	}
	assertMetricRate(t, "asr CER", job.ASRCER(), 0.25)
	assertMetricRate(t, "segmented CER", job.SegmentedCER(), 1.0/12.0)
	assertMetricRate(t, "asr WER", job.ASRWER(), 0.25)
	assertMetricRate(t, "segmented WER", job.SegmentedWER(), 0.125)
}

func TestMetricRateHandlesEmptyReference(t *testing.T) {
	assertMetricRate(t, "both empty", metricRate(0, 0), 0)
	assertMetricRate(t, "insertion into empty reference", metricRate(2, 0), 1)
}

func TestSegmentationRates(t *testing.T) {
	job := EvaluationJob{SegmentMatched: 3, SegmentPredicted: 4, SegmentReference: 3}
	assertMetricRate(t, "segment precision", job.SegmentPrecision(), 0.75)
	assertMetricRate(t, "segment recall", job.SegmentRecall(), 1)
	assertMetricRate(t, "segment F1", job.SegmentF1(), 6.0/7.0)
}

func assertMetricRate(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("%s=%v, want %v", name, got, want)
	}
}
