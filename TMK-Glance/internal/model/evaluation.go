package model

import "time"

const (
	EvaluationJobQueued              = "queued"
	EvaluationJobRunning             = "running"
	EvaluationJobSucceeded           = "succeeded"
	EvaluationJobCompletedWithErrors = "completed_with_errors"
	EvaluationJobFailed              = "failed"
	EvaluationJobCancelled           = "cancelled"
	EvaluationJobDeadLettered        = "dead_lettered"

	EvaluationResultSucceeded = "succeeded"
	EvaluationResultFailed    = "failed"
)

type EvaluationConfig struct {
	ASRProvider                  string `json:"asr_provider"`
	MaxSentenceSilenceMS         int    `json:"max_sentence_silence_ms"`
	SemanticPunctuationEnabled   bool   `json:"semantic_punctuation_enabled"`
	MultiThresholdModeEnabled    bool   `json:"multi_threshold_mode_enabled"`
	PunctuationPredictionEnabled bool   `json:"punctuation_prediction_enabled"`
	SegmenterEnabled             bool   `json:"segmenter_enabled"`
	MaxRunes                     int    `json:"max_runes"`
	MaxDurationMS                int    `json:"max_duration_ms"`
	SoftCommitDelayMS            int    `json:"soft_commit_delay_ms"`
}

type EvaluationJob struct {
	ID                    string           `json:"id"`
	DatasetID             string           `json:"dataset_id"`
	DatasetRevision       int              `json:"dataset_revision"`
	DatasetLanguage       string           `json:"dataset_language"`
	Status                string           `json:"status"`
	Config                EvaluationConfig `json:"config"`
	TotalItems            int              `json:"total_items"`
	CompletedItems        int              `json:"completed_items"`
	SucceededItems        int              `json:"succeeded_items"`
	FailedItems           int              `json:"failed_items"`
	ASRCharDistance       int64            `json:"asr_char_distance"`
	ASRCharUnits          int64            `json:"asr_char_units"`
	SegmentedCharDistance int64            `json:"segmented_char_distance"`
	SegmentedCharUnits    int64            `json:"segmented_char_units"`
	ASRWordDistance       int64            `json:"asr_word_distance"`
	ASRWordUnits          int64            `json:"asr_word_units"`
	SegmentedWordDistance int64            `json:"segmented_word_distance"`
	SegmentedWordUnits    int64            `json:"segmented_word_units"`
	SegmentMatched        int64            `json:"segment_matched"`
	SegmentPredicted      int64            `json:"segment_predicted"`
	SegmentReference      int64            `json:"segment_reference"`
	ErrorMessage          string           `json:"error_message,omitempty"`
	RequestedBy           string           `json:"requested_by"`
	CreatedAt             time.Time        `json:"created_at"`
	StartedAt             *time.Time       `json:"started_at,omitempty"`
	CompletedAt           *time.Time       `json:"completed_at,omitempty"`
	AttemptCount          int              `json:"attempt_count"`
	MaxAttempts           int              `json:"max_attempts"`
	NextAttemptAt         *time.Time       `json:"next_attempt_at,omitempty"`
	LeaseOwner            string           `json:"-"`
	LeaseExpiresAt        *time.Time       `json:"lease_expires_at,omitempty"`
	HeartbeatAt           *time.Time       `json:"heartbeat_at,omitempty"`
}

func (j EvaluationJob) ASRCER() float64 { return metricRate(j.ASRCharDistance, j.ASRCharUnits) }
func (j EvaluationJob) SegmentedCER() float64 {
	return metricRate(j.SegmentedCharDistance, j.SegmentedCharUnits)
}
func (j EvaluationJob) ASRWER() float64 { return metricRate(j.ASRWordDistance, j.ASRWordUnits) }
func (j EvaluationJob) SegmentedWER() float64 {
	return metricRate(j.SegmentedWordDistance, j.SegmentedWordUnits)
}
func (j EvaluationJob) SegmentPrecision() float64 {
	return metricRate(j.SegmentMatched, j.SegmentPredicted)
}
func (j EvaluationJob) SegmentRecall() float64 {
	return metricRate(j.SegmentMatched, j.SegmentReference)
}
func (j EvaluationJob) SegmentF1() float64 {
	return f1(j.SegmentPrecision(), j.SegmentRecall())
}

type EvaluationResult struct {
	ID                    string    `json:"id"`
	JobID                 string    `json:"job_id"`
	DatasetItemID         string    `json:"dataset_item_id"`
	Sequence              int       `json:"sequence"`
	Status                string    `json:"status"`
	ReferenceText         string    `json:"reference_text"`
	ASRText               string    `json:"asr_text"`
	SegmentedText         string    `json:"segmented_text"`
	SegmentsJSON          string    `json:"-"`
	SegmentCount          int       `json:"segment_count"`
	ASRCharDistance       int64     `json:"asr_char_distance"`
	ASRCharUnits          int64     `json:"asr_char_units"`
	SegmentedCharDistance int64     `json:"segmented_char_distance"`
	SegmentedCharUnits    int64     `json:"segmented_char_units"`
	ASRWordDistance       int64     `json:"asr_word_distance"`
	ASRWordUnits          int64     `json:"asr_word_units"`
	SegmentedWordDistance int64     `json:"segmented_word_distance"`
	SegmentedWordUnits    int64     `json:"segmented_word_units"`
	SegmentMatched        int64     `json:"segment_matched"`
	SegmentPredicted      int64     `json:"segment_predicted"`
	SegmentReference      int64     `json:"segment_reference"`
	ErrorMessage          string    `json:"error_message,omitempty"`
	StartedAt             time.Time `json:"started_at"`
	CompletedAt           time.Time `json:"completed_at"`
}

func (r EvaluationResult) ASRCER() float64 { return metricRate(r.ASRCharDistance, r.ASRCharUnits) }
func (r EvaluationResult) SegmentedCER() float64 {
	return metricRate(r.SegmentedCharDistance, r.SegmentedCharUnits)
}
func (r EvaluationResult) ASRWER() float64 { return metricRate(r.ASRWordDistance, r.ASRWordUnits) }
func (r EvaluationResult) SegmentedWER() float64 {
	return metricRate(r.SegmentedWordDistance, r.SegmentedWordUnits)
}
func (r EvaluationResult) SegmentPrecision() float64 {
	return metricRate(r.SegmentMatched, r.SegmentPredicted)
}
func (r EvaluationResult) SegmentRecall() float64 {
	return metricRate(r.SegmentMatched, r.SegmentReference)
}
func (r EvaluationResult) SegmentF1() float64 {
	return f1(r.SegmentPrecision(), r.SegmentRecall())
}

type EvaluationWorkItem struct {
	DatasetItemID     string
	Sequence          int
	AudioStorageKey   string
	AudioOriginalName string
	TextStorageKey    string
	TextOriginalName  string
	ReferenceSegments []ReferenceSegment
}

func metricRate(distance, units int64) float64 {
	if units == 0 {
		if distance == 0 {
			return 0
		}
		return 1
	}
	return float64(distance) / float64(units)
}

func f1(precision, recall float64) float64 {
	if precision+recall == 0 {
		return 0
	}
	return 2 * precision * recall / (precision + recall)
}
