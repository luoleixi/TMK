package server

import (
	"testing"

	"tmk-glance/internal/model"
)

func TestEvaluationJobProgressIncludesEveryCompletedItemOnce(t *testing.T) {
	view := newEvaluationJobView(&model.EvaluationJob{TotalItems: 4, CompletedItems: 2, SucceededItems: 1, FailedItems: 1})
	if view.Progress != 0.5 {
		t.Fatalf("progress=%v, want 0.5", view.Progress)
	}
}
