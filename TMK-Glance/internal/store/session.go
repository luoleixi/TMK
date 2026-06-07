package store

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"time"

	"tmk-glance/internal/model"
)

type SessionStore struct {
	db *sql.DB
}

func NewSessionStore(dbDir string) *SessionStore {
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Fatalf("[store] create data dir: %v", err)
	}
	dbPath := filepath.Join(dbDir, "sessions.db")

	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		log.Fatalf("[store] open db: %v", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			source_lang TEXT NOT NULL,
			target_lang TEXT NOT NULL,
			input_type TEXT NOT NULL DEFAULT 'system_audio',
			status TEXT NOT NULL DEFAULT 'active',
			record_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL,
			ended_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			source_text TEXT NOT NULL,
			translated_text TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);
		CREATE INDEX IF NOT EXISTS idx_records_session ON records(session_id);
	`); err != nil {
		log.Fatalf("[store] migrate: %v", err)
	}

	log.Printf("[store] sqlite opened: %s", dbPath)
	return &SessionStore{db: db}
}

func (s *SessionStore) Create(ses *model.Session) {
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, source_lang, target_lang, input_type, status, created_at)
		 VALUES (?, ?, ?, ?, 'active', ?)`,
		ses.ID, ses.SourceLang, ses.TargetLang, ses.InputType, ses.CreatedAt,
	)
	if err != nil {
		log.Printf("[store] create session: %v", err)
	}
}

func (s *SessionStore) Get(id string) (*model.Session, bool) {
	ses := &model.Session{}
	var endedAt sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, source_lang, target_lang, input_type, status, record_count, created_at, ended_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(&ses.ID, &ses.SourceLang, &ses.TargetLang, &ses.InputType,
		&ses.Status, &ses.RecordCount, &ses.CreatedAt, &endedAt)
	if err != nil {
		return nil, false
	}
	if endedAt.Valid {
		ses.EndedAt = &endedAt.Time
	}
	return ses, true
}

func (s *SessionStore) End(id string) bool {
	now := time.Now()
	res, err := s.db.Exec(
		`UPDATE sessions SET status = 'ended', ended_at = ? WHERE id = ? AND status = 'active'`,
		now, id,
	)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func (s *SessionStore) AddRecord(sessionID string, r model.Record) {
	_, err := s.db.Exec(
		`INSERT INTO records (session_id, source_text, translated_text, timestamp)
		 VALUES (?, ?, ?, ?)`,
		sessionID, r.SourceText, r.TranslatedText, r.Timestamp,
	)
	if err != nil {
		log.Printf("[store] add record: %v", err)
		return
	}
	s.db.Exec(
		`UPDATE sessions SET record_count = (SELECT COUNT(*) FROM records WHERE session_id = ?) WHERE id = ?`,
		sessionID, sessionID,
	)
}

func (s *SessionStore) Records(sessionID string) ([]model.Record, bool) {
	rows, err := s.db.Query(
		`SELECT id, session_id, source_text, translated_text, timestamp
		 FROM records WHERE session_id = ? ORDER BY id`, sessionID,
	)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	var recs []model.Record
	for rows.Next() {
		var r model.Record
		rows.Scan(&r.ID, &r.SessionID, &r.SourceText, &r.TranslatedText, &r.Timestamp)
		recs = append(recs, r)
	}
	return recs, true
}

// ListHistory returns all ended sessions, most recent first.
func (s *SessionStore) ListHistory() []model.Session {
	rows, err := s.db.Query(
		`SELECT id, source_lang, target_lang, input_type, status, record_count, created_at, ended_at
		 FROM sessions WHERE status = 'ended'
		 ORDER BY created_at DESC LIMIT 50`,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var sessions []model.Session
	for rows.Next() {
		var ses model.Session
		var endedAt sql.NullTime
		rows.Scan(&ses.ID, &ses.SourceLang, &ses.TargetLang, &ses.InputType,
			&ses.Status, &ses.RecordCount, &ses.CreatedAt, &endedAt)
		if endedAt.Valid {
			ses.EndedAt = &endedAt.Time
		}
		sessions = append(sessions, ses)
	}
	return sessions
}

// Close closes the database.
func (s *SessionStore) Close() error {
	return s.db.Close()
}
