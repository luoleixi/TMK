package store

import (
	"path/filepath"
	"testing"
	"time"

	"tmk-glance/internal/model"
)

func TestSessionBriefPersistence(t *testing.T) {
	store, err := NewSessionStore(DriverSQLite, filepath.Join(t.TempDir(), "sessions.db"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session := &model.Session{
		ID:         "brief-session",
		SourceLang: "zh",
		TargetLang: "en",
		InputType:  "system_audio",
		Status:     "ready",
		CreatedAt:  time.Now(),
	}
	if err := store.Create(session); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateBrief(session.ID, "客户端发布计划"); err != nil {
		t.Fatal(err)
	}

	stored, ok, err := store.Get(session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("session not found")
	}
	if stored.Brief != "客户端发布计划" {
		t.Fatalf("brief = %q", stored.Brief)
	}
}
