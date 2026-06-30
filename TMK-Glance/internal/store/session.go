package store

import (
	"database/sql"
	"errors"
	"sort"
	"sync"
	"time"

	"tmk-glance/internal/model"

	_ "modernc.org/sqlite"
)

type SessionStore struct {
	mu sync.Mutex
	db *sql.DB
}

func NewSessionStore(dbPath string) (*SessionStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &SessionStore{db: db}, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT NOT NULL UNIQUE,
			source_lang  TEXT NOT NULL,
			target_lang  TEXT NOT NULL,
			input_type   TEXT NOT NULL DEFAULT 'system_audio',
			status       TEXT NOT NULL DEFAULT 'ready',
			record_count INTEGER NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL,
			ended_at     TEXT
		);
		CREATE TABLE IF NOT EXISTS records (
			id               INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id       TEXT NOT NULL,
			sequence         INTEGER NOT NULL,
			source_text      TEXT NOT NULL,
			translated_text  TEXT NOT NULL,
			confidence       REAL NOT NULL DEFAULT 0.0,
			audio_duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at       TEXT NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
		);
	`)
	return err
}

func (s *SessionStore) Create(ses *model.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO sessions (session_id, source_lang, target_lang, input_type, status, record_count, created_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		ses.ID, ses.SourceLang, ses.TargetLang, ses.InputType, ses.Status, ses.CreatedAt.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SessionStore) Get(id string) (*model.Session, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getTx(id)
}

func (s *SessionStore) getTx(id string) (*model.Session, bool, error) {
	row := s.db.QueryRow(
		`SELECT session_id, source_lang, target_lang, input_type, status, record_count, created_at, ended_at
		 FROM sessions WHERE session_id = ?`, id,
	)
	return scanSession(row)
}

func (s *SessionStore) List() ([]*model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(
		`SELECT session_id, source_lang, target_lang, input_type, status, record_count, created_at, ended_at
		 FROM sessions ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*model.Session
	for rows.Next() {
		ses, ok, err := scanSessionFromRows(rows)
		if err != nil {
			return nil, err
		}
		if ok {
			result = append(result, ses)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SessionStore) Activate(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		`UPDATE sessions SET status='active' WHERE session_id=? AND status='ready'`, id,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *SessionStore) End(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE sessions SET status='completed', ended_at=? WHERE session_id=? AND status='active'`, now, id,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *SessionStore) Fail(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE sessions SET status='error', ended_at=? WHERE session_id=? AND status='active'`, now, id,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *SessionStore) AddRecord(sessionID string, r model.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var maxSeq int
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(sequence), 0) FROM records WHERE session_id=?`, sessionID,
	).Scan(&maxSeq); err != nil {
		return err
	}

	if _, err := tx.Exec(
		`INSERT INTO records (session_id, sequence, source_text, translated_text, confidence, audio_duration_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, maxSeq+1, r.SourceText, r.TranslatedText, r.Confidence, r.AudioDurationMs, r.CreatedAt.Format(time.RFC3339Nano),
	); err != nil {
		return err
	}

	if _, err := tx.Exec(
		`UPDATE sessions SET record_count=(SELECT COUNT(*) FROM records WHERE session_id=?) WHERE session_id=?`,
		sessionID, sessionID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SessionStore) Records(sessionID string) ([]model.Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT id, session_id, sequence, source_text, translated_text, confidence, audio_duration_ms, created_at
		 FROM records WHERE session_id=? ORDER BY sequence ASC`, sessionID,
	)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var recs []model.Record
	for rows.Next() {
		var rec model.Record
		var createdAt string
		if err := rows.Scan(&rec.ID, &rec.SessionID, &rec.Sequence, &rec.SourceText, &rec.TranslatedText,
			&rec.Confidence, &rec.AudioDurationMs, &createdAt); err != nil {
			return nil, false, err
		}
		if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			rec.CreatedAt = t
		}
		recs = append(recs, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	// Ensure we sort by sequence (already ordered in SQL, but belt and suspenders)
	sort.Slice(recs, func(i, j int) bool { return recs[i].Sequence < recs[j].Sequence })
	return recs, true, nil
}

// Close closes the database connection.
func (s *SessionStore) Close() error {
	return s.db.Close()
}

// ---------- internal helpers ----------

func scanSession(row interface{ Scan(...interface{}) error }) (*model.Session, bool, error) {
	var (
		id           string
		sourceLang   string
		targetLang   string
		inputType    string
		status       string
		recordCount  int
		createdAtStr string
		endedAtStr   sql.NullString
	)
	if err := row.Scan(&id, &sourceLang, &targetLang, &inputType, &status, &recordCount, &createdAtStr, &endedAtStr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdAtStr)
	if err != nil {
		return nil, false, err
	}
	ses := &model.Session{
		ID:          id,
		SourceLang:  sourceLang,
		TargetLang:  targetLang,
		InputType:   inputType,
		Status:      status,
		RecordCount: recordCount,
		CreatedAt:   createdAt,
	}
	if endedAtStr.Valid {
		if t, err := time.Parse(time.RFC3339Nano, endedAtStr.String); err == nil {
			ses.EndedAt = &t
		} else {
			return nil, false, err
		}
	}
	return ses, true, nil
}

func scanSessionFromRows(rows *sql.Rows) (*model.Session, bool, error) {
	return scanSession(rows)
}
