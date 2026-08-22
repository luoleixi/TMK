package store

import (
	"os"
	"strings"
	"testing"
	"time"

	"tmk-glance/internal/model"

	"github.com/go-sql-driver/mysql"
)

const mysqlTestDSNEnv = "TMK_TEST_MYSQL_DSN"

func newMySQLIntegrationStore(t *testing.T) *SessionStore {
	t.Helper()
	dsn := os.Getenv(mysqlTestDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run MySQL integration tests", mysqlTestDSNEnv)
	}
	parsed, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse MySQL test DSN: %v", err)
	}
	if !strings.HasSuffix(strings.ToLower(parsed.DBName), "_test") {
		t.Fatalf("refusing to clean MySQL database %q: test database name must end with _test", parsed.DBName)
	}
	database, err := NewSessionStore(DriverMySQL, "", dsn)
	if err != nil {
		t.Fatalf("open MySQL test database: %v", err)
	}
	for _, table := range []string{
		"evaluation_results", "evaluation_jobs", "dataset_items", "datasets", "storage_objects",
		"records", "sessions", "auth_tokens", "audit_logs", "users",
		"event_inbox", "event_outbox",
	} {
		if _, err := database.db.Exec("DELETE FROM " + table); err != nil {
			_ = database.Close()
			t.Fatalf("clean MySQL table %s: %v", table, err)
		}
	}
	return database
}

func TestMySQLMigrationsAndCoreLifecycle(t *testing.T) {
	database := newMySQLIntegrationStore(t)
	dsn := os.Getenv(mysqlTestDSNEnv)
	if err := database.Close(); err != nil {
		t.Fatalf("close first MySQL connection: %v", err)
	}
	var err error
	database, err = NewSessionStore(DriverMySQL, "", dsn)
	if err != nil {
		t.Fatalf("repeat MySQL migrations: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	now := time.Now().UTC().Truncate(time.Millisecond)
	admin := &model.User{ID: "mysql-admin", Email: "Admin@Example.com", DisplayName: "MySQL Admin",
		PasswordHash: "hash", Role: model.RoleAdmin, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateUser(admin); err != nil {
		t.Fatalf("create user: %v", err)
	}
	gotUser, ok, err := database.GetUserByEmail("ADMIN@example.COM")
	if err != nil || !ok || gotUser.ID != admin.ID || gotUser.Email != "admin@example.com" {
		t.Fatalf("case-insensitive user lookup: user=%+v ok=%v err=%v", gotUser, ok, err)
	}
	updated, protected, err := database.UpdateUserWithAdminGuard(admin.ID, admin.DisplayName, model.RoleUser, model.UserStatusActive, false)
	if err != nil || updated || !protected {
		t.Fatalf("last-admin guard: updated=%v protected=%v err=%v", updated, protected, err)
	}

	session := &model.Session{ID: "mysql-session", UserID: admin.ID, SourceLang: "zh", TargetLang: "en",
		InputType: "system_audio", Status: "ready", CreatedAt: now}
	if err := database.Create(session); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := database.AddRecord(session.ID, model.Record{SourceText: "你好", TranslatedText: "hello",
		Confidence: 0.98, AudioDurationMs: 800, CreatedAt: now}); err != nil {
		t.Fatalf("add record: %v", err)
	}
	storedSession, ok, err := database.Get(session.ID)
	if err != nil || !ok || storedSession.RecordCount != 1 {
		t.Fatalf("session record count: session=%+v ok=%v err=%v", storedSession, ok, err)
	}

	audio := &model.StorageObject{ID: "mysql-audio", OwnerUserID: admin.ID, Kind: model.ObjectKindAudio,
		OriginalName: "sample.wav", StorageKey: "mysql/sample.wav", ContentType: "audio/wav", SizeBytes: 1024,
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: model.ObjectStatusReady, CreatedAt: now}
	text := &model.StorageObject{ID: "mysql-text", OwnerUserID: admin.ID, Kind: model.ObjectKindText,
		OriginalName: "sample.txt", StorageKey: "mysql/sample.txt", ContentType: "text/plain", SizeBytes: 12,
		SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Status: model.ObjectStatusReady, CreatedAt: now}
	for _, object := range []*model.StorageObject{audio, text} {
		if err := database.CreateStorageObject(object); err != nil {
			t.Fatalf("create storage object %s: %v", object.ID, err)
		}
	}
	dataset := &model.Dataset{ID: "mysql-dataset", Name: "MySQL integration", Language: "zh",
		Status: model.DatasetStatusDraft, Revision: 1, CreatedBy: admin.ID, CreatedAt: now, UpdatedAt: now}
	if err := database.CreateDataset(dataset); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	item := &model.DatasetItem{ID: "mysql-item", DatasetID: dataset.ID, AudioObjectID: audio.ID,
		ReferenceTextObjectID: text.ID, ReferenceSegments: []model.ReferenceSegment{{Text: "你好", BeginTimeMS: 10, EndTimeMS: 500}},
		CreatedBy: admin.ID, CreatedAt: now}
	if err := database.AddDatasetItem(item); err != nil {
		t.Fatalf("add dataset item: %v", err)
	}
	if changed, err := database.MarkDatasetReady(dataset.ID); err != nil || !changed {
		t.Fatalf("mark dataset ready: changed=%v err=%v", changed, err)
	}

	job := &model.EvaluationJob{ID: "mysql-job", DatasetID: dataset.ID, Status: model.EvaluationJobQueued,
		Config: model.EvaluationConfig{ASRProvider: "mock", SegmenterEnabled: true}, RequestedBy: admin.ID, CreatedAt: now}
	if err := database.CreateEvaluationJob(job); err != nil {
		t.Fatalf("create evaluation job: %v", err)
	}
	workerID := "mysql-worker"
	claimed, ok, err := database.ClaimNextEvaluationJob(workerID, now, time.Minute)
	if err != nil || !ok || claimed.ID != job.ID || claimed.TotalItems != 1 || claimed.AttemptCount != 1 || claimed.MaxAttempts != 3 {
		t.Fatalf("claim evaluation job: job=%+v ok=%v err=%v", claimed, ok, err)
	}
	if renewed, err := database.HeartbeatEvaluationJob(job.ID, workerID, now.Add(500*time.Millisecond), time.Minute); err != nil || !renewed {
		t.Fatalf("heartbeat renewed=%v err=%v", renewed, err)
	}
	result := &model.EvaluationResult{ID: "mysql-result", JobID: job.ID, DatasetItemID: item.ID, Sequence: 1,
		Status: model.EvaluationResultSucceeded, ReferenceText: "你好", ASRText: "你号", SegmentedText: "你好",
		SegmentsJSON: `[{"text":"你好"}]`, SegmentCount: 1, ASRCharDistance: 1, ASRCharUnits: 2,
		SegmentedCharUnits: 2, ASRWordDistance: 1, ASRWordUnits: 1, SegmentedWordUnits: 1,
		SegmentMatched: 1, SegmentPredicted: 1, SegmentReference: 1, StartedAt: now, CompletedAt: now.Add(time.Second)}
	if err := database.SaveEvaluationResult(result, workerID, now.Add(time.Second)); err != nil {
		t.Fatalf("save evaluation result: %v", err)
	}
	if err := database.SaveEvaluationResult(result, workerID, now.Add(1500*time.Millisecond)); err != nil {
		t.Fatalf("idempotent evaluation result: %v", err)
	}
	if finished, err := database.FinishEvaluationJob(job.ID, workerID, false, "", now.Add(2*time.Second)); err != nil || !finished {
		t.Fatalf("finish evaluation job: finished=%v err=%v", finished, err)
	}
	storedJob, ok, err := database.GetEvaluationJob(job.ID)
	if err != nil || !ok || storedJob.Status != model.EvaluationJobSucceeded || storedJob.CompletedItems != 1 || storedJob.ASRCharDistance != 1 || storedJob.SegmentMatched != 1 {
		t.Fatalf("aggregated evaluation job: job=%+v ok=%v err=%v", storedJob, ok, err)
	}

	if deleted, err := database.Delete(session.ID); err != nil || !deleted {
		t.Fatalf("delete session: deleted=%v err=%v", deleted, err)
	}
	var recordCount int
	if err := database.db.QueryRow(`SELECT COUNT(*) FROM records WHERE session_id=?`, session.ID).Scan(&recordCount); err != nil || recordCount != 0 {
		t.Fatalf("record cascade: count=%d err=%v", recordCount, err)
	}
}
