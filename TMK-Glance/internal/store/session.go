package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"tmk-glance/internal/model"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

const (
	DriverSQLite = "sqlite"
	DriverMySQL  = "mysql"
)

type SessionStore struct {
	mu     sync.Mutex
	db     *sql.DB
	driver string
}

func NewSessionStore(driver, dbPath, dsn string) (*SessionStore, error) {
	driver = strings.ToLower(strings.TrimSpace(driver))
	if driver == "" {
		driver = DriverSQLite
	}
	if driver == DriverSQLite && dbPath == "" {
		dbPath = "./tmk.db"
	}
	if driver == DriverMySQL && dsn == "" {
		return nil, errors.New("mysql storage requires storage.dsn or DB_DSN")
	}

	openDSN := dbPath
	if driver == DriverMySQL {
		openDSN = dsn
	}

	db, err := sql.Open(driver, openDSN)
	if err != nil {
		return nil, err
	}
	if err := configureDB(db, driver); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db, driver); err != nil {
		db.Close()
		return nil, err
	}
	return &SessionStore{db: db, driver: driver}, nil
}

func configureDB(db *sql.DB, driver string) error {
	switch driver {
	case DriverSQLite:
		db.SetMaxOpenConns(1)
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			return err
		}
		if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
			return err
		}
	case DriverMySQL:
		db.SetMaxOpenConns(20)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(30 * time.Minute)
	default:
		return fmt.Errorf("unsupported storage driver %q", driver)
	}
	return db.Ping()
}

func migrate(db *sql.DB, driver string) error {
	switch driver {
	case DriverSQLite:
		return migrateSQLite(db)
	case DriverMySQL:
		return migrateMySQL(db)
	default:
		return fmt.Errorf("unsupported storage driver %q", driver)
	}
}

func migrateSQLite(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT NOT NULL UNIQUE,
			user_id      TEXT,
			source_lang  TEXT NOT NULL,
			target_lang  TEXT NOT NULL,
			input_type   TEXT NOT NULL DEFAULT 'system_audio',
			status       TEXT NOT NULL DEFAULT 'ready',
			record_count INTEGER NOT NULL DEFAULT 0,
			brief        TEXT NOT NULL DEFAULT '',
			summary      TEXT NOT NULL DEFAULT '',
			created_at   TEXT NOT NULL,
			ended_at     TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL
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
	if err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN summary TEXT NOT NULL DEFAULT ''`)
	if err != nil && !isDuplicateColumnError(err) {
		return err
	}
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN brief TEXT NOT NULL DEFAULT ''`)
	if err != nil && !isDuplicateColumnError(err) {
		return err
	}
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN user_id TEXT`)
	if err != nil && !isDuplicateColumnError(err) {
		return err
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_sessions_user_created ON sessions(user_id, created_at)`)
	if err != nil {
		return err
	}
	if err := migrateAuthSQLite(db); err != nil {
		return err
	}
	if err := migrateDatasetSQLite(db); err != nil {
		return err
	}
	return migrateEvaluationSQLite(db)
}

func migrateMySQL(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id           BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			session_id   VARCHAR(64) NOT NULL UNIQUE,
			user_id      VARCHAR(36) NULL,
			source_lang  VARCHAR(32) NOT NULL,
			target_lang  VARCHAR(32) NOT NULL,
			input_type   VARCHAR(32) NOT NULL DEFAULT 'system_audio',
			status       VARCHAR(32) NOT NULL DEFAULT 'ready',
			record_count INT NOT NULL DEFAULT 0,
			brief        VARCHAR(96) NOT NULL DEFAULT '',
			summary      TEXT NOT NULL,
			created_at   VARCHAR(40) NOT NULL,
			ended_at     VARCHAR(40) NULL,
			INDEX idx_sessions_created_at (created_at),
			INDEX idx_sessions_user_created (user_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS records (
			id                BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			session_id        VARCHAR(64) NOT NULL,
			sequence          INT NOT NULL,
			source_text       TEXT NOT NULL,
			translated_text   TEXT NOT NULL,
			confidence        DOUBLE NOT NULL DEFAULT 0.0,
			audio_duration_ms INT NOT NULL DEFAULT 0,
			created_at        VARCHAR(40) NOT NULL,
			INDEX idx_records_session_sequence (session_id, sequence),
			CONSTRAINT fk_records_session FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
	`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN summary TEXT NULL`)
	if err != nil && !isDuplicateColumnError(err) {
		return err
	}
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN brief VARCHAR(96) NULL`)
	if err != nil && !isDuplicateColumnError(err) {
		return err
	}
	_, err = db.Exec(`ALTER TABLE sessions ADD COLUMN user_id VARCHAR(36) NULL`)
	if err != nil && !isDuplicateColumnError(err) {
		return err
	}
	_, err = db.Exec(`CREATE INDEX idx_sessions_user_created ON sessions(user_id, created_at)`)
	if err != nil && !isDuplicateIndexError(err) {
		return err
	}
	_, err = db.Exec(`UPDATE sessions SET summary='' WHERE summary IS NULL`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE sessions SET brief='' WHERE brief IS NULL`)
	if err != nil {
		return err
	}
	if err := migrateAuthMySQL(db); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE sessions ADD CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL`)
	if err != nil && !isDuplicateConstraintError(err) {
		return err
	}
	if err := migrateDatasetMySQL(db); err != nil {
		return err
	}
	return migrateEvaluationMySQL(db)
}

func (s *SessionStore) Create(ses *model.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO sessions (session_id, user_id, source_lang, target_lang, input_type, status, record_count, brief, summary, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 0, '', '', ?)`,
		ses.ID, nullableString(ses.UserID), ses.SourceLang, ses.TargetLang, ses.InputType, ses.Status, ses.CreatedAt.Format(time.RFC3339Nano),
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
		`SELECT session_id, user_id, source_lang, target_lang, input_type, status, record_count, brief, summary, created_at, ended_at
		 FROM sessions WHERE session_id = ?`, id,
	)
	return scanSession(row)
}

func (s *SessionStore) List() ([]*model.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(
		`SELECT session_id, user_id, source_lang, target_lang, input_type, status, record_count, brief, summary, created_at, ended_at
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

func (s *SessionStore) Search(userID, keyword, sourceLang, targetLang string, dateFrom, dateTo *time.Time, limit, offset int) ([]*model.Session, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	query := `SELECT DISTINCT s.session_id, s.user_id, s.source_lang, s.target_lang, s.input_type, s.status, s.record_count, s.brief, s.summary, s.created_at, s.ended_at
		FROM sessions s
		LEFT JOIN records r ON r.session_id = s.session_id
		WHERE 1=1`
	countQuery := `SELECT COUNT(DISTINCT s.session_id)
		FROM sessions s
		LEFT JOIN records r ON r.session_id = s.session_id
		WHERE 1=1`
	var args []any

	add := func(clause string, values ...any) {
		query += clause
		countQuery += clause
		args = append(args, values...)
	}
	add(" AND s.user_id = ?", userID)
	if sourceLang != "" {
		add(" AND s.source_lang = ?", sourceLang)
	}
	if targetLang != "" {
		add(" AND s.target_lang = ?", targetLang)
	}
	if dateFrom != nil {
		add(" AND s.created_at >= ?", dateFrom.Format(time.RFC3339Nano))
	}
	if dateTo != nil {
		add(" AND s.created_at <= ?", dateTo.Format(time.RFC3339Nano))
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		add(" AND (s.source_lang LIKE ? OR s.target_lang LIKE ? OR s.brief LIKE ? OR r.source_text LIKE ? OR r.translated_text LIKE ?)", like, like, like, like, like)
	}

	var total int
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query += " ORDER BY s.created_at DESC LIMIT ? OFFSET ?"
	rows, err := s.db.Query(query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []*model.Session
	for rows.Next() {
		ses, ok, err := scanSessionFromRows(rows)
		if err != nil {
			return nil, 0, err
		}
		if ok {
			result = append(result, ses)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return result, total, nil
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

func (s *SessionStore) IsSessionOwner(sessionID, userID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM sessions WHERE session_id=? AND user_id=?`, sessionID, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *SessionStore) ClaimUnownedSessions(userID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE sessions SET user_id=? WHERE user_id IS NULL OR user_id=''`, userID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SessionStore) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM sessions WHERE session_id=?`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *SessionStore) DeleteMany(ids []string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var deleted int
	for _, id := range ids {
		res, err := tx.Exec(`DELETE FROM sessions WHERE session_id=?`, id)
		if err != nil {
			return 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		deleted += int(n)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

func (s *SessionStore) UpdateSummary(id, summary string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE sessions SET summary=? WHERE session_id=?`, summary, id)
	return err
}

func (s *SessionStore) UpdateBrief(id, brief string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE sessions SET brief=? WHERE session_id=? AND brief=''`, brief, id)
	return err
}

// Close closes the database connection.
func (s *SessionStore) Close() error {
	return s.db.Close()
}

// ---------- internal helpers ----------

func scanSession(row interface{ Scan(...interface{}) error }) (*model.Session, bool, error) {
	var (
		id           string
		userID       sql.NullString
		sourceLang   string
		targetLang   string
		inputType    string
		status       string
		recordCount  int
		brief        sql.NullString
		summary      sql.NullString
		createdAtStr string
		endedAtStr   sql.NullString
	)
	if err := row.Scan(&id, &userID, &sourceLang, &targetLang, &inputType, &status, &recordCount, &brief, &summary, &createdAtStr, &endedAtStr); err != nil {
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
		UserID:      userID.String,
		SourceLang:  sourceLang,
		TargetLang:  targetLang,
		InputType:   inputType,
		Status:      status,
		RecordCount: recordCount,
		Brief:       brief.String,
		Summary:     summary.String,
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

func isDuplicateColumnError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate column") || strings.Contains(msg, "duplicate column name")
}

func isDuplicateIndexError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key name") || strings.Contains(msg, "already exists")
}

func isDuplicateConstraintError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate foreign key constraint name") || strings.Contains(msg, "constraint") && strings.Contains(msg, "already exists")
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
