package evaluation

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"tmk-glance/internal/asr"
	"tmk-glance/internal/model"
	"tmk-glance/internal/objectstore"
	"tmk-glance/internal/store"
)

type deterministicASR struct{}

func (deterministicASR) Recognize(ctx context.Context, audio <-chan []byte) (<-chan asr.Result, error) {
	output := make(chan asr.Result, 1)
	go func() {
		defer close(output)
		for range audio {
		}
		time.Sleep(80 * time.Millisecond)
		select {
		case output <- asr.Result{Text: "hello world", IsFinal: true, BeginTimeMS: 0, EndTimeMS: 1000}:
		case <-ctx.Done():
		}
	}()
	return output, nil
}

func (deterministicASR) Close() error { return nil }

func TestManagerProcessesPersistedEvaluationJob(t *testing.T) {
	database, err := store.NewSessionStore(store.DriverSQLite, filepath.Join(t.TempDir(), "evaluation.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	objects, err := objectstore.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	user := &model.User{ID: "admin", Email: "admin@example.com", PasswordHash: "unused", Role: model.RoleAdmin,
		Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateUser(user); err != nil {
		t.Fatal(err)
	}
	audioBytes := pcmWAV(bytes.Repeat([]byte{0, 0}, 3200))
	audioSize, audioHash, err := objects.Put(context.Background(), "audio.wav", bytes.NewReader(audioBytes), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	textBytes := []byte("Hello, world!")
	textSize, textHash, err := objects.Put(context.Background(), "reference.txt", bytes.NewReader(textBytes), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, object := range []*model.StorageObject{
		{ID: "audio", OwnerUserID: user.ID, Kind: model.ObjectKindAudio, OriginalName: "audio.wav", StorageKey: "audio.wav", ContentType: "audio/wav", SizeBytes: audioSize, SHA256: audioHash, Status: model.ObjectStatusReady, CreatedAt: now},
		{ID: "text", OwnerUserID: user.ID, Kind: model.ObjectKindText, OriginalName: "reference.txt", StorageKey: "reference.txt", ContentType: "text/plain", SizeBytes: textSize, SHA256: textHash, Status: model.ObjectStatusReady, CreatedAt: now},
	} {
		if err := database.CreateStorageObject(object); err != nil {
			t.Fatal(err)
		}
	}
	dataset := &model.Dataset{ID: "dataset", Name: "Evaluation", Language: "en", Status: model.DatasetStatusDraft,
		Revision: 1, CreatedBy: user.ID, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateDataset(dataset); err != nil {
		t.Fatal(err)
	}
	if err := database.AddDatasetItem(&model.DatasetItem{ID: "item", DatasetID: dataset.ID, AudioObjectID: "audio",
		ReferenceTextObjectID: "text", ReferenceSegments: []model.ReferenceSegment{{Text: "hello world"}},
		CreatedBy: user.ID, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.MarkDatasetReady(dataset.ID); err != nil {
		t.Fatal(err)
	}
	job := &model.EvaluationJob{ID: "job", DatasetID: dataset.ID, Status: model.EvaluationJobQueued, RequestedBy: user.ID,
		CreatedAt: now, Config: model.EvaluationConfig{ASRProvider: "test", SegmenterEnabled: true, MaxRunes: 40, MaxDurationMS: 5000, SoftCommitDelayMS: 10}}
	if err := database.CreateEvaluationJob(job); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(database, objects, func(string, model.EvaluationConfig) asr.ASR { return deterministicASR{} }, Config{
		Workers: 1, PollInterval: 5 * time.Millisecond, ItemTimeout: time.Second, MaxTextBytes: 1 << 20,
		LeaseDuration: 30 * time.Millisecond, HeartbeatInterval: 5 * time.Millisecond,
		ReaperInterval: 5 * time.Millisecond, RetryBase: 5 * time.Millisecond,
	})
	if err := manager.Start(); err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, _, err := database.GetEvaluationJob(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == model.EvaluationJobSucceeded {
			if current.CompletedItems != 1 || current.AttemptCount != 1 || current.ASRCER() != 0 || current.SegmentedCER() != 0 || current.SegmentF1() != 1 {
				t.Fatalf("job=%+v", current)
			}
			results, total, err := database.ListEvaluationResults(job.ID, 10, 0)
			if err != nil || total != 1 || results[0].Status != model.EvaluationResultSucceeded ||
				results[0].SegmentCount != 1 || results[0].SegmentF1() != 1 {
				t.Fatalf("results=%+v total=%d err=%v", results, total, err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("evaluation job did not finish")
}
