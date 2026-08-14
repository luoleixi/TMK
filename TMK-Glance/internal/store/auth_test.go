package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"tmk-glance/internal/model"
)

func TestLegacySQLiteMigrationPreservesAndClaimsSessions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open(DriverSQLite, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL UNIQUE,
			source_lang TEXT NOT NULL,
			target_lang TEXT NOT NULL,
			input_type TEXT NOT NULL DEFAULT 'system_audio',
			status TEXT NOT NULL DEFAULT 'ready',
			record_count INTEGER NOT NULL DEFAULT 0,
			brief TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			ended_at TEXT
		);
		INSERT INTO sessions (session_id, source_lang, target_lang, created_at)
		VALUES ('legacy-session', 'zh', 'en', '2026-01-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewSessionStore(DriverSQLite, dbPath, "")
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	if err := store.CreateUser(&model.User{ID: "owner", Email: "owner@example.com", PasswordHash: "unused",
		Role: model.RoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimUnownedSessions("owner")
	if err != nil || claimed != 1 {
		t.Fatalf("claim legacy sessions count=%d err=%v", claimed, err)
	}
	session, ok, err := store.Get("legacy-session")
	if err != nil || !ok || session.UserID != "owner" {
		t.Fatalf("migrated session=%+v ok=%v err=%v", session, ok, err)
	}
}
