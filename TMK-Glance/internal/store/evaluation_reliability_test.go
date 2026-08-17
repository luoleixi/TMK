package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tmk-glance/internal/model"
)

func newReliabilityJob(t *testing.T, maxAttempts int) (*SessionStore, *model.EvaluationJob, *model.DatasetItem) {
	t.Helper()
	database, err := NewSessionStore(DriverSQLite, filepath.Join(t.TempDir(), "reliability.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	now := time.Now().UTC()
	user := &model.User{ID: "worker-admin", Email: "worker@example.com", PasswordHash: "hash", Role: model.RoleAdmin,
		Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	for _, object := range []*model.StorageObject{
		{ID: "worker-audio", OwnerUserID: user.ID, Kind: model.ObjectKindAudio, OriginalName: "a.wav", StorageKey: "a.wav", ContentType: "audio/wav", SizeBytes: 1, SHA256: "audio", Status: model.ObjectStatusReady, CreatedAt: now},
		{ID: "worker-text", OwnerUserID: user.ID, Kind: model.ObjectKindText, OriginalName: "a.txt", StorageKey: "a.txt", ContentType: "text/plain", SizeBytes: 1, SHA256: "text", Status: model.ObjectStatusReady, CreatedAt: now},
	} {
		if err := database.CreateStorageObject(object); err != nil {
			t.Fatal(err)
		}
	}
	dataset := &model.Dataset{ID: "worker-dataset", Name: "Reliability", Language: "en", Status: model.DatasetStatusDraft,
		Revision: 1, CreatedBy: user.ID, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateDataset(dataset); err != nil {
		t.Fatal(err)
	}
	item := &model.DatasetItem{ID: "worker-item", DatasetID: dataset.ID, AudioObjectID: "worker-audio",
		ReferenceTextObjectID: "worker-text", CreatedBy: user.ID, CreatedAt: now}
	if err := database.AddDatasetItem(item); err != nil {
		t.Fatal(err)
	}
	if changed, err := database.MarkDatasetReady(dataset.ID); err != nil || !changed {
		t.Fatalf("mark dataset ready changed=%v err=%v", changed, err)
	}
	job := &model.EvaluationJob{ID: "worker-job", DatasetID: dataset.ID, Status: model.EvaluationJobQueued,
		RequestedBy: user.ID, CreatedAt: now, MaxAttempts: maxAttempts}
	if err := database.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	return database, job, item
}

func TestEvaluationLeaseFencesWorkersAndResultIsIdempotent(t *testing.T) {
	database, job, item := newReliabilityJob(t, 3)
	now := time.Now().UTC()
	claimed, ok, err := database.ClaimNextEvaluationJob("worker-one", now, time.Minute)
	if err != nil || !ok || claimed.AttemptCount != 1 || claimed.LeaseOwner != "worker-one" {
		t.Fatalf("claim job=%+v ok=%v err=%v", claimed, ok, err)
	}
	if second, ok, err := database.ClaimNextEvaluationJob("worker-two", now, time.Minute); err != nil || ok || second != nil {
		t.Fatalf("second claim job=%+v ok=%v err=%v", second, ok, err)
	}
	if renewed, err := database.HeartbeatEvaluationJob(job.ID, "worker-two", now.Add(time.Second), time.Minute); err != nil || renewed {
		t.Fatalf("wrong-owner heartbeat renewed=%v err=%v", renewed, err)
	}
	result := &model.EvaluationResult{ID: "worker-result", JobID: job.ID, DatasetItemID: item.ID, Sequence: 1,
		Status: model.EvaluationResultSucceeded, SegmentsJSON: "[]", ASRCharDistance: 1, ASRCharUnits: 2,
		StartedAt: now, CompletedAt: now.Add(time.Second)}
	if err := database.SaveEvaluationResult(result, "worker-two", now.Add(time.Second)); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("wrong-owner save error=%v", err)
	}
	if err := database.SaveEvaluationResult(result, "worker-one", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveEvaluationResult(result, "worker-one", now.Add(2*time.Second)); err != nil {
		t.Fatalf("idempotent save: %v", err)
	}
	stored, _, err := database.GetEvaluationJob(job.ID)
	if err != nil || stored.CompletedItems != 1 || stored.ASRCharDistance != 1 || stored.ASRCharUnits != 2 {
		t.Fatalf("duplicate result changed aggregate job=%+v err=%v", stored, err)
	}
}

func TestEvaluationRetryBackoffDeadLetterAndManualRetry(t *testing.T) {
	database, job, _ := newReliabilityJob(t, 2)
	now := time.Now().UTC()
	if _, ok, err := database.ClaimNextEvaluationJob("worker-one", now, time.Minute); err != nil || !ok {
		t.Fatalf("first claim ok=%v err=%v", ok, err)
	}
	status, changed, err := database.RetryEvaluationJob(job.ID, "worker-one", "temporary database error", now, 5*time.Second)
	if err != nil || !changed || status != model.EvaluationJobQueued {
		t.Fatalf("retry status=%s changed=%v err=%v", status, changed, err)
	}
	if claimed, ok, err := database.ClaimNextEvaluationJob("worker-two", now.Add(4*time.Second), time.Minute); err != nil || ok || claimed != nil {
		t.Fatalf("claimed before backoff job=%+v ok=%v err=%v", claimed, ok, err)
	}
	if _, ok, err := database.ClaimNextEvaluationJob("worker-two", now.Add(6*time.Second), time.Minute); err != nil || !ok {
		t.Fatalf("second claim ok=%v err=%v", ok, err)
	}
	status, changed, err = database.RetryEvaluationJob(job.ID, "worker-two", "second infrastructure error", now.Add(7*time.Second), 5*time.Second)
	if err != nil || !changed || status != model.EvaluationJobDeadLettered {
		t.Fatalf("dead letter status=%s changed=%v err=%v", status, changed, err)
	}
	stored, _, err := database.GetEvaluationJob(job.ID)
	if err != nil || stored.Status != model.EvaluationJobDeadLettered || stored.AttemptCount != 2 || stored.CompletedAt == nil {
		t.Fatalf("dead-lettered job=%+v err=%v", stored, err)
	}
	if retried, err := database.RetryDeadLetteredEvaluationJob(job.ID, now.Add(8*time.Second), 3); err != nil || !retried {
		t.Fatalf("manual retry retried=%v err=%v", retried, err)
	}
	stored, _, _ = database.GetEvaluationJob(job.ID)
	if stored.Status != model.EvaluationJobQueued || stored.MaxAttempts != 5 || stored.CompletedAt != nil {
		t.Fatalf("manually retried job=%+v", stored)
	}
}

func TestExpiredEvaluationLeaseIsRequeuedWithoutDiscardingProgress(t *testing.T) {
	database, job, item := newReliabilityJob(t, 3)
	now := time.Now().UTC()
	if _, ok, err := database.ClaimNextEvaluationJob("expired-worker", now, time.Second); err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	result := &model.EvaluationResult{ID: "preserved-result", JobID: job.ID, DatasetItemID: item.ID, Sequence: 1,
		Status: model.EvaluationResultSucceeded, SegmentsJSON: "[]", StartedAt: now, CompletedAt: now.Add(100 * time.Millisecond)}
	if err := database.SaveEvaluationResult(result, "expired-worker", now.Add(100*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	requeued, dead, err := database.RecoverExpiredEvaluationJobs(now.Add(2*time.Second), time.Second)
	if err != nil || requeued != 1 || dead != 0 {
		t.Fatalf("recover requeued=%d dead=%d err=%v", requeued, dead, err)
	}
	completed, err := database.CompletedEvaluationItemIDs(job.ID)
	if err != nil || !completed[item.ID] {
		t.Fatalf("progress was not preserved completed=%v err=%v", completed, err)
	}
}

func TestReleaseEvaluationJobDoesNotConsumeAttempt(t *testing.T) {
	database, job, _ := newReliabilityJob(t, 3)
	now := time.Now().UTC()
	claimed, ok, err := database.ClaimNextEvaluationJob("worker-a", now, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim job: ok=%v err=%v", ok, err)
	}
	if claimed.AttemptCount != 1 {
		t.Fatalf("attempt count after claim = %d, want 1", claimed.AttemptCount)
	}

	released, err := database.ReleaseEvaluationJob(job.ID, "worker-a", now.Add(time.Second))
	if err != nil || !released {
		t.Fatalf("release job: released=%v err=%v", released, err)
	}

	got, _, err := database.GetEvaluationJob(job.ID)
	if err != nil {
		t.Fatalf("get released job: %v", err)
	}
	if got.Status != model.EvaluationJobQueued {
		t.Fatalf("status = %q, want %q", got.Status, model.EvaluationJobQueued)
	}
	if got.AttemptCount != 0 {
		t.Fatalf("attempt count after graceful release = %d, want 0", got.AttemptCount)
	}
}

func TestEvaluationTimeFormattingSortsAcrossWholeSecond(t *testing.T) {
	wholeSecond := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	justBefore := wholeSecond.Add(-time.Nanosecond)
	if formatEvaluationTime(justBefore) >= formatEvaluationTime(wholeSecond) {
		t.Fatalf("sortable timestamps out of order: %q >= %q", formatEvaluationTime(justBefore), formatEvaluationTime(wholeSecond))
	}
}
