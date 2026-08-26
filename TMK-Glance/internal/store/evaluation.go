package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"tmk-glance/internal/model"
)

var (
	ErrDatasetNotReady = errors.New("dataset is not ready")
	ErrJobNotRunning   = errors.New("evaluation job is not running")
	ErrLeaseLost       = errors.New("evaluation job lease was lost")
)

func migrateEvaluationSQLite(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS evaluation_jobs (
			id TEXT PRIMARY KEY,
			dataset_id TEXT NOT NULL,
			dataset_revision INTEGER NOT NULL,
			dataset_language TEXT NOT NULL,
			status TEXT NOT NULL,
			config_json TEXT NOT NULL,
			total_items INTEGER NOT NULL,
			completed_items INTEGER NOT NULL DEFAULT 0,
			succeeded_items INTEGER NOT NULL DEFAULT 0,
			failed_items INTEGER NOT NULL DEFAULT 0,
			asr_char_distance INTEGER NOT NULL DEFAULT 0,
			asr_char_units INTEGER NOT NULL DEFAULT 0,
			segmented_char_distance INTEGER NOT NULL DEFAULT 0,
			segmented_char_units INTEGER NOT NULL DEFAULT 0,
			asr_word_distance INTEGER NOT NULL DEFAULT 0,
			asr_word_units INTEGER NOT NULL DEFAULT 0,
			segmented_word_distance INTEGER NOT NULL DEFAULT 0,
			segmented_word_units INTEGER NOT NULL DEFAULT 0,
			segment_matched INTEGER NOT NULL DEFAULT 0,
			segment_predicted INTEGER NOT NULL DEFAULT 0,
			segment_reference INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			requested_by TEXT NOT NULL,
			created_at TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT,
			attempt_count INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 3,
			next_attempt_at TEXT,
			lease_owner TEXT,
			lease_expires_at TEXT,
			heartbeat_at TEXT,
			FOREIGN KEY (dataset_id) REFERENCES datasets(id) ON DELETE RESTRICT,
			FOREIGN KEY (requested_by) REFERENCES users(id) ON DELETE RESTRICT
		);
		CREATE INDEX IF NOT EXISTS idx_evaluation_jobs_status_created ON evaluation_jobs(status, created_at);
		CREATE INDEX IF NOT EXISTS idx_evaluation_jobs_dataset_created ON evaluation_jobs(dataset_id, created_at);

		CREATE TABLE IF NOT EXISTS evaluation_results (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			dataset_item_id TEXT NOT NULL,
			sequence INTEGER NOT NULL,
			status TEXT NOT NULL,
			reference_text TEXT NOT NULL DEFAULT '',
			asr_text TEXT NOT NULL DEFAULT '',
			segmented_text TEXT NOT NULL DEFAULT '',
			segments_json TEXT NOT NULL DEFAULT '[]',
			segment_count INTEGER NOT NULL DEFAULT 0,
			asr_char_distance INTEGER NOT NULL DEFAULT 0,
			asr_char_units INTEGER NOT NULL DEFAULT 0,
			segmented_char_distance INTEGER NOT NULL DEFAULT 0,
			segmented_char_units INTEGER NOT NULL DEFAULT 0,
			asr_word_distance INTEGER NOT NULL DEFAULT 0,
			asr_word_units INTEGER NOT NULL DEFAULT 0,
			segmented_word_distance INTEGER NOT NULL DEFAULT 0,
			segmented_word_units INTEGER NOT NULL DEFAULT 0,
			segment_matched INTEGER NOT NULL DEFAULT 0,
			segment_predicted INTEGER NOT NULL DEFAULT 0,
			segment_reference INTEGER NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			completed_at TEXT NOT NULL,
			UNIQUE (job_id, dataset_item_id),
			FOREIGN KEY (job_id) REFERENCES evaluation_jobs(id) ON DELETE CASCADE,
			FOREIGN KEY (dataset_item_id) REFERENCES dataset_items(id) ON DELETE RESTRICT
		);
		CREATE INDEX IF NOT EXISTS idx_evaluation_results_job_sequence ON evaluation_results(job_id, sequence);
	`)
	if err != nil {
		return err
	}
	if err := ensureEvaluationMetricColumnsSQLite(db); err != nil {
		return err
	}
	if err := ensureEvaluationReliabilitySQLite(db); err != nil {
		return err
	}
	return ensureEvaluationABSQLite(db)
}

func migrateEvaluationMySQL(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS evaluation_jobs (
			id VARCHAR(36) NOT NULL PRIMARY KEY, dataset_id VARCHAR(36) NOT NULL, dataset_revision INT NOT NULL,
			dataset_language VARCHAR(32) NOT NULL,
			status VARCHAR(32) NOT NULL, config_json JSON NOT NULL, total_items INT NOT NULL,
			completed_items INT NOT NULL DEFAULT 0, succeeded_items INT NOT NULL DEFAULT 0, failed_items INT NOT NULL DEFAULT 0,
			asr_char_distance BIGINT NOT NULL DEFAULT 0, asr_char_units BIGINT NOT NULL DEFAULT 0,
			segmented_char_distance BIGINT NOT NULL DEFAULT 0, segmented_char_units BIGINT NOT NULL DEFAULT 0,
			asr_word_distance BIGINT NOT NULL DEFAULT 0, asr_word_units BIGINT NOT NULL DEFAULT 0,
			segmented_word_distance BIGINT NOT NULL DEFAULT 0, segmented_word_units BIGINT NOT NULL DEFAULT 0,
			segment_matched BIGINT NOT NULL DEFAULT 0, segment_predicted BIGINT NOT NULL DEFAULT 0,
			segment_reference BIGINT NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL, requested_by VARCHAR(36) NOT NULL, created_at VARCHAR(40) NOT NULL,
			started_at VARCHAR(40) NULL, completed_at VARCHAR(40) NULL,
			attempt_count INT NOT NULL DEFAULT 0, max_attempts INT NOT NULL DEFAULT 3,
			next_attempt_at VARCHAR(40) NULL, lease_owner VARCHAR(100) NULL,
			lease_expires_at VARCHAR(40) NULL, heartbeat_at VARCHAR(40) NULL,
			INDEX idx_evaluation_jobs_status_created (status, created_at),
			INDEX idx_evaluation_jobs_dataset_created (dataset_id, created_at),
			INDEX idx_evaluation_jobs_claim (status, next_attempt_at, created_at),
			CONSTRAINT fk_evaluation_jobs_dataset FOREIGN KEY (dataset_id) REFERENCES datasets(id) ON DELETE RESTRICT,
			CONSTRAINT fk_evaluation_jobs_requester FOREIGN KEY (requested_by) REFERENCES users(id) ON DELETE RESTRICT
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS evaluation_results (
			id VARCHAR(36) NOT NULL PRIMARY KEY, job_id VARCHAR(36) NOT NULL, dataset_item_id VARCHAR(36) NOT NULL,
			sequence INT NOT NULL, status VARCHAR(20) NOT NULL, reference_text LONGTEXT NOT NULL, asr_text LONGTEXT NOT NULL,
			segmented_text LONGTEXT NOT NULL, segments_json LONGTEXT NOT NULL, segment_count INT NOT NULL DEFAULT 0,
			asr_char_distance BIGINT NOT NULL DEFAULT 0, asr_char_units BIGINT NOT NULL DEFAULT 0,
			segmented_char_distance BIGINT NOT NULL DEFAULT 0, segmented_char_units BIGINT NOT NULL DEFAULT 0,
			asr_word_distance BIGINT NOT NULL DEFAULT 0, asr_word_units BIGINT NOT NULL DEFAULT 0,
			segmented_word_distance BIGINT NOT NULL DEFAULT 0, segmented_word_units BIGINT NOT NULL DEFAULT 0,
			segment_matched BIGINT NOT NULL DEFAULT 0, segment_predicted BIGINT NOT NULL DEFAULT 0,
			segment_reference BIGINT NOT NULL DEFAULT 0,
			error_message TEXT NOT NULL, started_at VARCHAR(40) NOT NULL, completed_at VARCHAR(40) NOT NULL,
			UNIQUE KEY uq_evaluation_result_item (job_id, dataset_item_id),
			INDEX idx_evaluation_results_job_sequence (job_id, sequence),
			CONSTRAINT fk_evaluation_results_job FOREIGN KEY (job_id) REFERENCES evaluation_jobs(id) ON DELETE CASCADE,
			CONSTRAINT fk_evaluation_results_item FOREIGN KEY (dataset_item_id) REFERENCES dataset_items(id) ON DELETE RESTRICT
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	if err := ensureEvaluationMetricColumnsMySQL(db); err != nil {
		return err
	}
	if err := ensureEvaluationReliabilityMySQL(db); err != nil {
		return err
	}
	return ensureEvaluationABMySQL(db)
}

func ensureEvaluationABSQLite(db *sql.DB) error {
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS evaluation_runs (id TEXT PRIMARY KEY,dataset_id TEXT NOT NULL,dataset_revision INTEGER NOT NULL,dataset_language TEXT NOT NULL,mode TEXT NOT NULL,status TEXT NOT NULL,total_items INTEGER NOT NULL DEFAULT 0,completed_items INTEGER NOT NULL DEFAULT 0,input_status TEXT NOT NULL DEFAULT 'pending',input_total INTEGER NOT NULL DEFAULT 0,input_completed INTEGER NOT NULL DEFAULT 0,lease_owner TEXT,lease_expires_at TEXT,created_by TEXT NOT NULL,config_json TEXT NOT NULL,created_at TEXT NOT NULL,completed_at TEXT,error_message TEXT NOT NULL DEFAULT '')`, `CREATE TABLE IF NOT EXISTS evaluation_inputs (id TEXT PRIMARY KEY,run_id TEXT NOT NULL,dataset_item_id TEXT NOT NULL,asr_provider TEXT NOT NULL,asr_config_hash TEXT NOT NULL,transcript_json TEXT NOT NULL DEFAULT '',status TEXT NOT NULL,error_message TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,completed_at TEXT,UNIQUE(run_id,dataset_item_id))`} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	for _, c := range []struct{ name, typ string }{{"input_status", "TEXT NOT NULL DEFAULT 'pending'"}, {"input_total", "INTEGER NOT NULL DEFAULT 0"}, {"input_completed", "INTEGER NOT NULL DEFAULT 0"}, {"lease_owner", "TEXT"}, {"lease_expires_at", "TEXT"}} {
		columns, err := sqliteColumnNames(db, "evaluation_runs")
		if err != nil {
			return err
		}
		if !columns[c.name] {
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE evaluation_runs ADD COLUMN %s %s`, c.name, c.typ)); err != nil {
				return err
			}
		}
	}
	columns, err := sqliteColumnNames(db, "evaluation_jobs")
	if err != nil {
		return err
	}
	for _, c := range []struct{ name, typ string }{{"run_id", "TEXT"}, {"variant", "TEXT"}, {"pair_key", "TEXT"}} {
		if !columns[c.name] {
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE evaluation_jobs ADD COLUMN %s %s`, c.name, c.typ)); err != nil {
				return err
			}
		}
	}
	return nil
}
func ensureEvaluationABMySQL(db *sql.DB) error {
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS evaluation_runs (id VARCHAR(36) PRIMARY KEY,dataset_id VARCHAR(36) NOT NULL,dataset_revision INT NOT NULL,dataset_language VARCHAR(32) NOT NULL,mode VARCHAR(16) NOT NULL,status VARCHAR(32) NOT NULL,total_items INT NOT NULL DEFAULT 0,completed_items INT NOT NULL DEFAULT 0,input_status VARCHAR(20) NOT NULL DEFAULT 'pending',input_total INT NOT NULL DEFAULT 0,input_completed INT NOT NULL DEFAULT 0,lease_owner VARCHAR(100) NULL,lease_expires_at VARCHAR(40) NULL,created_by VARCHAR(36) NOT NULL,config_json JSON NOT NULL,created_at VARCHAR(40) NOT NULL,completed_at VARCHAR(40) NULL,error_message TEXT NOT NULL,INDEX idx_evaluation_runs_created(created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, `CREATE TABLE IF NOT EXISTS evaluation_inputs (id VARCHAR(36) PRIMARY KEY,run_id VARCHAR(36) NOT NULL,dataset_item_id VARCHAR(36) NOT NULL,asr_provider VARCHAR(64) NOT NULL,asr_config_hash VARCHAR(128) NOT NULL,transcript_json LONGTEXT NOT NULL,status VARCHAR(20) NOT NULL,error_message TEXT NOT NULL,created_at VARCHAR(40) NOT NULL,completed_at VARCHAR(40) NULL,UNIQUE KEY uq_evaluation_input_item(run_id,dataset_item_id),CONSTRAINT fk_evaluation_input_run FOREIGN KEY(run_id) REFERENCES evaluation_runs(id) ON DELETE CASCADE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	for _, c := range []struct{ name, typ string }{{"run_id", "VARCHAR(36) NULL"}, {"variant", "VARCHAR(32) NULL"}, {"pair_key", "VARCHAR(64) NULL"}} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='evaluation_jobs' AND column_name=?`, c.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE evaluation_jobs ADD COLUMN %s %s`, c.name, c.typ)); err != nil {
				return err
			}
		}
	}
	for _, c := range []struct{ name, typ string }{{"input_status", "VARCHAR(20) NOT NULL DEFAULT 'pending'"}, {"input_total", "INT NOT NULL DEFAULT 0"}, {"input_completed", "INT NOT NULL DEFAULT 0"}, {"lease_owner", "VARCHAR(100) NULL"}, {"lease_expires_at", "VARCHAR(40) NULL"}} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='evaluation_runs' AND column_name=?`, c.name).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE evaluation_runs ADD COLUMN %s %s`, c.name, c.typ)); err != nil {
				return err
			}
		}
	}
	return nil
}

var segmentationMetricColumns = []string{"segment_matched", "segment_predicted", "segment_reference"}

var evaluationReliabilityColumns = []struct {
	name, sqliteType, mysqlType string
}{
	{"attempt_count", "INTEGER NOT NULL DEFAULT 0", "INT NOT NULL DEFAULT 0"},
	{"max_attempts", "INTEGER NOT NULL DEFAULT 3", "INT NOT NULL DEFAULT 3"},
	{"next_attempt_at", "TEXT", "VARCHAR(40) NULL"},
	{"lease_owner", "TEXT", "VARCHAR(100) NULL"},
	{"lease_expires_at", "TEXT", "VARCHAR(40) NULL"},
	{"heartbeat_at", "TEXT", "VARCHAR(40) NULL"},
}

func ensureEvaluationReliabilitySQLite(db *sql.DB) error {
	columns, err := sqliteColumnNames(db, "evaluation_jobs")
	if err != nil {
		return err
	}
	for _, column := range evaluationReliabilityColumns {
		if !columns[column.name] {
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE evaluation_jobs ADD COLUMN %s %s`, column.name, column.sqliteType)); err != nil {
				return err
			}
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_evaluation_jobs_claim ON evaluation_jobs(status, next_attempt_at, created_at)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE evaluation_jobs SET status='queued', next_attempt_at=?, error_message='recovered from legacy worker state'
		WHERE status='running' AND lease_expires_at IS NULL`, formatEvaluationTime(time.Now()))
	return err
}

func ensureEvaluationReliabilityMySQL(db *sql.DB) error {
	for _, column := range evaluationReliabilityColumns {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema=DATABASE() AND table_name='evaluation_jobs' AND column_name=?`, column.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE evaluation_jobs ADD COLUMN %s %s`, column.name, column.mysqlType)); err != nil {
				return err
			}
		}
	}
	_, err := db.Exec(`CREATE INDEX idx_evaluation_jobs_claim ON evaluation_jobs(status, next_attempt_at, created_at)`)
	if err != nil && !isDuplicateIndexError(err) {
		return err
	}
	_, err = db.Exec(`UPDATE evaluation_jobs SET status='queued', next_attempt_at=?, error_message='recovered from legacy worker state'
		WHERE status='running' AND lease_expires_at IS NULL`, formatEvaluationTime(time.Now()))
	return err
}

func ensureEvaluationMetricColumnsSQLite(db *sql.DB) error {
	for _, table := range []string{"evaluation_jobs", "evaluation_results"} {
		columns, err := sqliteColumnNames(db, table)
		if err != nil {
			return err
		}
		for _, column := range segmentationMetricColumns {
			if columns[column] {
				continue
			}
			if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s INTEGER NOT NULL DEFAULT 0`, table, column)); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureEvaluationMetricColumnsMySQL(db *sql.DB) error {
	for _, table := range []string{"evaluation_jobs", "evaluation_results"} {
		for _, column := range segmentationMetricColumns {
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
				WHERE table_schema=DATABASE() AND table_name=? AND column_name=?`, table, column).Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s BIGINT NOT NULL DEFAULT 0`, table, column)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func sqliteColumnNames(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (s *SessionStore) CreateEvaluationJob(job *model.EvaluationJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if job.MaxAttempts < 1 {
		job.MaxAttempts = 3
	}
	var status string
	if err := tx.QueryRow(`SELECT status, revision, language, item_count FROM datasets WHERE id=?`, job.DatasetID).
		Scan(&status, &job.DatasetRevision, &job.DatasetLanguage, &job.TotalItems); err != nil {
		return err
	}
	if status != model.DatasetStatusReady || job.TotalItems == 0 {
		return ErrDatasetNotReady
	}
	configJSON, err := json.Marshal(job.Config)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO evaluation_jobs
		(id, run_id, variant, pair_key, dataset_id, dataset_revision, dataset_language, status, config_json, total_items, error_message,
		 requested_by, created_at, max_attempts)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?)`, job.ID, nullString(job.RunID), nullString(job.Variant), nullString(job.PairKey), job.DatasetID, job.DatasetRevision, job.DatasetLanguage,
		job.Status, string(configJSON), job.TotalItems, job.RequestedBy, formatTime(job.CreatedAt), job.MaxAttempts)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SessionStore) CreateEvaluationABRun(run *model.EvaluationRun, jobs []*model.EvaluationJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err = tx.QueryRow(`SELECT status,revision,language,item_count FROM datasets WHERE id=?`, run.DatasetID).Scan(&status, &run.DatasetRevision, &run.DatasetLanguage, &run.TotalItems); err != nil {
		return err
	}
	if status != model.DatasetStatusReady || run.TotalItems == 0 {
		return ErrDatasetNotReady
	}
	run.Mode = "ab"
	run.Status = model.EvaluationJobQueued
	configJSON, _ := json.Marshal(run.Config)
	run.InputStatus, run.InputTotal = "pending", run.TotalItems
	_, err = tx.Exec(`INSERT INTO evaluation_runs(id,dataset_id,dataset_revision,dataset_language,mode,status,total_items,input_status,input_total,created_by,config_json,created_at,error_message) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.DatasetID, run.DatasetRevision, run.DatasetLanguage, run.Mode, run.Status, run.TotalItems, run.InputStatus, run.InputTotal, run.CreatedBy, string(configJSON), formatTime(run.CreatedAt), "")
	if err != nil {
		return err
	}
	for _, job := range jobs {
		job.DatasetRevision, job.DatasetLanguage, job.TotalItems = run.DatasetRevision, run.DatasetLanguage, run.TotalItems
		raw, _ := json.Marshal(job.Config)
		if _, err = tx.Exec(`INSERT INTO evaluation_jobs(id,run_id,variant,pair_key,dataset_id,dataset_revision,dataset_language,status,config_json,total_items,error_message,requested_by,created_at,max_attempts) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, job.ID, run.ID, job.Variant, job.PairKey, run.DatasetID, run.DatasetRevision, run.DatasetLanguage, model.EvaluationJobQueued, string(raw), run.TotalItems, "", job.RequestedBy, formatTime(job.CreatedAt), job.MaxAttempts); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT id FROM dataset_items WHERE dataset_id=? ORDER BY sequence`, run.DatasetID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			return err
		}
		inputID := uuid.NewString()
		if _, err := tx.Exec(`INSERT INTO evaluation_inputs(id,run_id,dataset_item_id,asr_provider,asr_config_hash,transcript_json,status,error_message,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, inputID, run.ID, itemID, run.Config.ASRProvider, "", "", "pending", "", formatTime(run.CreatedAt)); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SessionStore) GetEvaluationRun(id string) (*model.EvaluationRun, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getEvaluationRunLocked(id)
}
func (s *SessionStore) getEvaluationRunLocked(id string) (*model.EvaluationRun, bool, error) {
	var run model.EvaluationRun
	var raw, created string
	var completed sql.NullString
	var inputStatus, leaseOwner sql.NullString
	var inputTotal, inputCompleted int
	var leaseExpires sql.NullString
	err := s.db.QueryRow(`SELECT id,dataset_id,dataset_revision,dataset_language,mode,status,total_items,completed_items,input_status,input_total,input_completed,lease_owner,lease_expires_at,created_by,config_json,created_at,completed_at,error_message FROM evaluation_runs WHERE id=?`, id).Scan(&run.ID, &run.DatasetID, &run.DatasetRevision, &run.DatasetLanguage, &run.Mode, &run.Status, &run.TotalItems, &run.CompletedItems, &inputStatus, &inputTotal, &inputCompleted, &leaseOwner, &leaseExpires, &run.CreatedBy, &raw, &created, &completed, &run.ErrorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err = json.Unmarshal([]byte(raw), &run.Config); err != nil {
		return nil, false, err
	}
	run.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	run.InputStatus, run.InputTotal, run.InputCompleted, run.LeaseOwner = inputStatus.String, inputTotal, inputCompleted, leaseOwner.String
	if leaseExpires.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, leaseExpires.String)
		run.LeaseExpiresAt = &parsed
	}
	if completed.Valid {
		v, _ := time.Parse(time.RFC3339Nano, completed.String)
		run.CompletedAt = &v
	}
	return &run, true, nil
}

func (s *SessionStore) GetEvaluationInput(runID, itemID string) (*model.EvaluationInput, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var input model.EvaluationInput
	var created, completed sql.NullString
	err := s.db.QueryRow(`SELECT id,run_id,dataset_item_id,asr_provider,asr_config_hash,transcript_json,status,error_message,created_at,completed_at FROM evaluation_inputs WHERE run_id=? AND dataset_item_id=?`, runID, itemID).Scan(&input.ID, &input.RunID, &input.DatasetItemID, &input.ASRProvider, &input.ASRConfigHash, &input.TranscriptJSON, &input.Status, &input.ErrorMessage, &created, &completed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return &input, err == nil, err
}
func (s *SessionStore) SaveEvaluationInput(input *model.EvaluationInput, completedAt *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var completed any
	if completedAt != nil {
		completed = formatTime(*completedAt)
	}
	args := []any{input.ID, input.RunID, input.DatasetItemID, input.ASRProvider, input.ASRConfigHash, input.TranscriptJSON, input.Status, input.ErrorMessage, formatTime(time.Now()), completed}
	var err error
	if s.driver == DriverMySQL {
		_, err = s.db.Exec(`INSERT INTO evaluation_inputs(id,run_id,dataset_item_id,asr_provider,asr_config_hash,transcript_json,status,error_message,created_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE transcript_json=VALUES(transcript_json),status=VALUES(status),error_message=VALUES(error_message),completed_at=VALUES(completed_at)`, args...)
	} else {
		_, err = s.db.Exec(`INSERT INTO evaluation_inputs(id,run_id,dataset_item_id,asr_provider,asr_config_hash,transcript_json,status,error_message,created_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(run_id,dataset_item_id) DO UPDATE SET transcript_json=excluded.transcript_json,status=excluded.status,error_message=excluded.error_message,completed_at=excluded.completed_at`, args...)
	}
	return err
}

func (s *SessionStore) MarkEvaluationInputFailed(runID, datasetItemID, message string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE evaluation_inputs SET status='failed',error_message=?,completed_at=? WHERE run_id=? AND dataset_item_id=? AND status<>'succeeded'`, message, formatEvaluationTime(now), runID, datasetItemID)
	return err
}

func (s *SessionStore) ListEvaluationRunJobs(runID string) ([]model.EvaluationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(evaluationJobSelect+` WHERE run_id=? ORDER BY variant`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []model.EvaluationJob
	for rows.Next() {
		job, ok, err := scanEvaluationJob(rows)
		if err != nil {
			return nil, err
		}
		if ok {
			jobs = append(jobs, *job)
		}
	}
	return jobs, rows.Err()
}

func (s *SessionStore) ActivateEvaluationJob(id, workerID string, now time.Time, leaseDuration time.Duration) (*model.EvaluationJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE evaluation_jobs SET status='running',started_at=COALESCE(started_at,?),attempt_count=attempt_count+1,lease_owner=?,lease_expires_at=?,heartbeat_at=? WHERE id=? AND status='queued'`, formatEvaluationTime(now), workerID, formatEvaluationTime(now.Add(leaseDuration)), formatEvaluationTime(now), id)
	if err != nil {
		return nil, false, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return nil, false, nil
	}
	return scanEvaluationJob(s.db.QueryRow(evaluationJobSelect+` WHERE id=?`, id))
}

func (s *SessionStore) ClaimEvaluationRun(runID, workerID string, now time.Time, leaseDuration time.Duration) (*model.EvaluationRun, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE evaluation_runs SET status='running',lease_owner=?,lease_expires_at=? WHERE id=? AND status IN ('pending','running') AND (lease_expires_at IS NULL OR lease_expires_at<=? OR lease_owner=?)`, workerID, formatEvaluationTime(now.Add(leaseDuration)), runID, formatEvaluationTime(now), workerID)
	if err != nil {
		return nil, false, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return nil, false, nil
	}
	return s.getEvaluationRunLocked(runID)
}
func (s *SessionStore) HeartbeatEvaluationRun(runID, workerID string, now time.Time, leaseDuration time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.db.Exec(`UPDATE evaluation_runs SET lease_expires_at=? WHERE id=? AND status='running' AND lease_owner=? AND lease_expires_at>?`, formatEvaluationTime(now.Add(leaseDuration)), runID, workerID, formatEvaluationTime(now))
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n == 1, nil
}
func (s *SessionStore) ReleaseEvaluationRun(runID, workerID string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.db.Exec(`UPDATE evaluation_runs SET status='pending',lease_owner=NULL,lease_expires_at=NULL WHERE id=? AND lease_owner=?`, runID, workerID)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n == 1, nil
}
func (s *SessionStore) AggregateEvaluationRun(runID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var inputs, inputDone, inputFailed int
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status='succeeded'),0), COALESCE(SUM(status='failed'),0) FROM evaluation_inputs WHERE run_id=?`, runID).Scan(&inputs, &inputDone, &inputFailed); err != nil {
		return err
	}
	var variants, variantDone, variantErrors int
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(status IN ('succeeded','completed_with_errors')),0), COALESCE(SUM(status='completed_with_errors'),0) FROM evaluation_jobs WHERE run_id=?`, runID).Scan(&variants, &variantDone, &variantErrors); err != nil {
		return err
	}
	status := "running"
	if inputs > 0 && inputFailed == inputs {
		status = "failed"
	} else if variants == 2 && variantDone == 2 {
		if inputFailed > 0 || variantErrors > 0 {
			status = "completed_with_errors"
		} else {
			status = "completed"
		}
	}
	inputStatus := "running"
	if inputs > 0 && inputDone == inputs {
		inputStatus = "succeeded"
	} else if inputFailed > 0 && inputDone+inputFailed == inputs {
		inputStatus = "failed"
	}
	completedAt := any(nil)
	if status == "completed" || status == "completed_with_errors" || status == "failed" {
		completedAt = formatEvaluationTime(now)
	}
	_, err := s.db.Exec(`UPDATE evaluation_runs SET input_completed=?,input_status=?,completed_items=?,status=?,completed_at=COALESCE(?,completed_at),lease_owner=NULL,lease_expires_at=NULL WHERE id=?`, inputDone, inputStatus, variantDone, status, completedAt, runID)
	return err
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *SessionStore) GetEvaluationJob(id string) (*model.EvaluationJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanEvaluationJob(s.db.QueryRow(evaluationJobSelect+` WHERE id=?`, id))
}

func (s *SessionStore) ListEvaluationJobs(status string, limit, offset int) ([]model.EvaluationJob, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	where := ""
	var args []any
	if status != "" {
		where = ` WHERE status=?`
		args = append(args, status)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM evaluation_jobs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(evaluationJobSelect+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	jobs := make([]model.EvaluationJob, 0)
	for rows.Next() {
		job, _, err := scanEvaluationJob(rows)
		if err != nil {
			return nil, 0, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, total, rows.Err()
}

func (s *SessionStore) RecoverExpiredEvaluationJobs(now time.Time, retryBase time.Duration) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id, attempt_count, max_attempts FROM evaluation_jobs
		WHERE status='running' AND lease_expires_at IS NOT NULL AND lease_expires_at<=?`, formatEvaluationTime(now))
	if err != nil {
		return 0, 0, err
	}
	type expiredJob struct {
		id                    string
		attempts, maxAttempts int
	}
	jobs := make([]expiredJob, 0)
	for rows.Next() {
		var job expiredJob
		if err := rows.Scan(&job.id, &job.attempts, &job.maxAttempts); err != nil {
			rows.Close()
			return 0, 0, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Close(); err != nil {
		return 0, 0, err
	}
	requeued, deadLettered := 0, 0
	for _, job := range jobs {
		if job.attempts >= job.maxAttempts {
			result, err := tx.Exec(`UPDATE evaluation_jobs SET status=?, error_message=?, completed_at=?,
				lease_owner=NULL, lease_expires_at=NULL, heartbeat_at=NULL WHERE id=? AND status='running' AND lease_expires_at<=?`,
				model.EvaluationJobDeadLettered, "worker lease expired after maximum attempts", formatTime(now), job.id, formatEvaluationTime(now))
			if err != nil {
				return 0, 0, err
			}
			if count, _ := result.RowsAffected(); count == 1 {
				deadLettered++
			}
			continue
		}
		next := now.Add(retryDelay(job.attempts, retryBase))
		result, err := tx.Exec(`UPDATE evaluation_jobs SET status='queued', error_message=?, next_attempt_at=?,
			lease_owner=NULL, lease_expires_at=NULL, heartbeat_at=NULL WHERE id=? AND status='running' AND lease_expires_at<=?`,
			"worker lease expired", formatEvaluationTime(next), job.id, formatEvaluationTime(now))
		if err != nil {
			return 0, 0, err
		}
		if count, _ := result.RowsAffected(); count == 1 {
			requeued++
		}
	}
	return requeued, deadLettered, tx.Commit()
}

func (s *SessionStore) ClaimNextEvaluationJob(workerID string, now time.Time, leaseDuration time.Duration) (*model.EvaluationJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		job, ok, err := scanEvaluationJob(s.db.QueryRow(evaluationJobSelect+` WHERE status='queued'
			AND (next_attempt_at IS NULL OR next_attempt_at<=?) AND (run_id IS NULL OR variant='segmenter_on') ORDER BY created_at LIMIT 1`, formatEvaluationTime(now)))
		if err != nil || !ok {
			return job, ok, err
		}
		nowText, expires := formatEvaluationTime(now), formatEvaluationTime(now.Add(leaseDuration))
		result, err := s.db.Exec(`UPDATE evaluation_jobs SET status='running', started_at=COALESCE(started_at,?), completed_at=NULL,
			attempt_count=attempt_count+1, error_message='', next_attempt_at=NULL, lease_owner=?, lease_expires_at=?, heartbeat_at=?
			WHERE id=? AND status='queued' AND (next_attempt_at IS NULL OR next_attempt_at<=?)`,
			nowText, workerID, expires, nowText, job.ID, nowText)
		if err != nil {
			return nil, false, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, false, err
		}
		if count == 1 {
			expiresAt := now.Add(leaseDuration)
			job.Status, job.StartedAt, job.HeartbeatAt, job.LeaseExpiresAt = model.EvaluationJobRunning, &now, &now, &expiresAt
			job.LeaseOwner, job.AttemptCount = workerID, job.AttemptCount+1
			return job, true, nil
		}
	}
}

func (s *SessionStore) HeartbeatEvaluationJob(id, workerID string, now time.Time, leaseDuration time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE evaluation_jobs SET heartbeat_at=?, lease_expires_at=?
		WHERE id=? AND status='running' AND lease_owner=? AND lease_expires_at>?`,
		formatEvaluationTime(now), formatEvaluationTime(now.Add(leaseDuration)), id, workerID, formatEvaluationTime(now))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *SessionStore) RetryEvaluationJob(id, workerID, message string, now time.Time, retryBase time.Duration) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var attempts, maxAttempts int
	if err := tx.QueryRow(`SELECT attempt_count, max_attempts FROM evaluation_jobs
		WHERE id=? AND status='running' AND lease_owner=?`, id, workerID).Scan(&attempts, &maxAttempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	status := model.EvaluationJobQueued
	nextAttempt, completedAt := any(formatEvaluationTime(now.Add(retryDelay(attempts, retryBase)))), any(nil)
	if attempts >= maxAttempts {
		status, nextAttempt, completedAt = model.EvaluationJobDeadLettered, nil, formatTime(now)
	}
	result, err := tx.Exec(`UPDATE evaluation_jobs SET status=?, error_message=?, next_attempt_at=?, completed_at=?,
		lease_owner=NULL, lease_expires_at=NULL, heartbeat_at=NULL WHERE id=? AND status='running' AND lease_owner=?`,
		status, message, nextAttempt, completedAt, id, workerID)
	if err != nil {
		return "", false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return status, false, err
	}
	return status, true, tx.Commit()
}

func (s *SessionStore) ReleaseEvaluationJob(id, workerID string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE evaluation_jobs SET status='queued', next_attempt_at=?, error_message='worker shutting down',
		attempt_count=CASE WHEN attempt_count>0 THEN attempt_count-1 ELSE 0 END,
		lease_owner=NULL, lease_expires_at=NULL, heartbeat_at=NULL
		WHERE id=? AND status='running' AND lease_owner=?`, formatEvaluationTime(now), id, workerID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func retryDelay(attempt int, base time.Duration) time.Duration {
	if base <= 0 {
		base = 5 * time.Second
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := base * time.Duration(1<<min(attempt-1, 8))
	if delay > 15*time.Minute {
		return 15 * time.Minute
	}
	return delay
}

func (s *SessionStore) ListEvaluationWorkItems(datasetID string) ([]model.EvaluationWorkItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT i.id, i.sequence, ao.storage_key, ao.original_name, txt.storage_key, txt.original_name,
		i.reference_segments_json
		FROM dataset_items i JOIN storage_objects ao ON ao.id=i.audio_object_id
		JOIN storage_objects txt ON txt.id=i.reference_text_object_id
		WHERE i.dataset_id=? AND ao.status='ready' AND txt.status='ready' ORDER BY i.sequence`, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.EvaluationWorkItem, 0)
	for rows.Next() {
		var item model.EvaluationWorkItem
		var referenceSegments string
		if err := rows.Scan(&item.DatasetItemID, &item.Sequence, &item.AudioStorageKey, &item.AudioOriginalName,
			&item.TextStorageKey, &item.TextOriginalName, &referenceSegments); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(referenceSegments), &item.ReferenceSegments); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SessionStore) CompletedEvaluationItemIDs(jobID string) (map[string]bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT dataset_item_id FROM evaluation_results WHERE job_id=?`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}

func (s *SessionStore) SaveEvaluationResult(result *model.EvaluationResult, workerID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status, leaseOwner string
	var leaseExpires sql.NullString
	if err := tx.QueryRow(`SELECT status, COALESCE(lease_owner,''), lease_expires_at FROM evaluation_jobs WHERE id=?`, result.JobID).
		Scan(&status, &leaseOwner, &leaseExpires); err != nil {
		return err
	}
	if status != model.EvaluationJobRunning || leaseOwner != workerID || !leaseExpires.Valid || leaseExpires.String <= formatEvaluationTime(now) {
		return ErrLeaseLost
	}
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM evaluation_results WHERE job_id=? AND dataset_item_id=?`, result.JobID, result.DatasetItemID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return tx.Commit()
	}
	_, err = tx.Exec(`INSERT INTO evaluation_results
		(id, job_id, dataset_item_id, sequence, status, reference_text, asr_text, segmented_text, segments_json,
		segment_count, asr_char_distance, asr_char_units, segmented_char_distance, segmented_char_units,
		asr_word_distance, asr_word_units, segmented_word_distance, segmented_word_units,
		segment_matched, segment_predicted, segment_reference, error_message, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, result.ID, result.JobID,
		result.DatasetItemID, result.Sequence, result.Status, result.ReferenceText, result.ASRText, result.SegmentedText,
		result.SegmentsJSON, result.SegmentCount, result.ASRCharDistance, result.ASRCharUnits,
		result.SegmentedCharDistance, result.SegmentedCharUnits, result.ASRWordDistance, result.ASRWordUnits,
		result.SegmentedWordDistance, result.SegmentedWordUnits, result.SegmentMatched, result.SegmentPredicted,
		result.SegmentReference, result.ErrorMessage,
		formatTime(result.StartedAt), formatTime(result.CompletedAt))
	if err != nil {
		return err
	}
	succeeded, failed := 0, 1
	if result.Status == model.EvaluationResultSucceeded {
		succeeded, failed = 1, 0
	}
	updateResult, err := tx.Exec(`UPDATE evaluation_jobs SET completed_items=completed_items+1,
		succeeded_items=succeeded_items+?, failed_items=failed_items+?,
		asr_char_distance=asr_char_distance+?, asr_char_units=asr_char_units+?,
		segmented_char_distance=segmented_char_distance+?, segmented_char_units=segmented_char_units+?,
		asr_word_distance=asr_word_distance+?, asr_word_units=asr_word_units+?,
		segmented_word_distance=segmented_word_distance+?, segmented_word_units=segmented_word_units+?,
		segment_matched=segment_matched+?, segment_predicted=segment_predicted+?, segment_reference=segment_reference+?
		WHERE id=? AND status='running' AND lease_owner=? AND lease_expires_at>?`, succeeded, failed, result.ASRCharDistance, result.ASRCharUnits,
		result.SegmentedCharDistance, result.SegmentedCharUnits, result.ASRWordDistance, result.ASRWordUnits,
		result.SegmentedWordDistance, result.SegmentedWordUnits, result.SegmentMatched, result.SegmentPredicted,
		result.SegmentReference, result.JobID, workerID, formatEvaluationTime(now))
	if err != nil {
		return err
	}
	if count, err := updateResult.RowsAffected(); err != nil || count != 1 {
		if err != nil {
			return err
		}
		return ErrLeaseLost
	}
	return tx.Commit()
}

func (s *SessionStore) FinishEvaluationJob(id, workerID string, failed bool, message string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := model.EvaluationJobSucceeded
	if failed {
		status = model.EvaluationJobFailed
	} else {
		var failedItems int
		if err := s.db.QueryRow(`SELECT failed_items FROM evaluation_jobs WHERE id=?`, id).Scan(&failedItems); err != nil {
			return false, err
		}
		if failedItems > 0 {
			status = model.EvaluationJobCompletedWithErrors
		}
	}
	where := ` WHERE id=? AND status='running' AND lease_owner=? AND lease_expires_at>?`
	if !failed {
		where += ` AND completed_items=total_items`
	}
	result, err := s.db.Exec(`UPDATE evaluation_jobs SET status=?, error_message=?, completed_at=?,
		lease_owner=NULL, lease_expires_at=NULL, heartbeat_at=NULL`+where,
		status, message, formatTime(now), id, workerID, formatEvaluationTime(now))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *SessionStore) CancelEvaluationJob(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE evaluation_jobs SET status='cancelled', completed_at=?, next_attempt_at=NULL,
		lease_owner=NULL, lease_expires_at=NULL, heartbeat_at=NULL WHERE id=? AND status IN ('queued','running')`, formatTime(time.Now()), id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *SessionStore) RetryDeadLetteredEvaluationJob(id string, now time.Time, additionalAttempts int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if additionalAttempts < 1 {
		additionalAttempts = 1
	}
	result, err := s.db.Exec(`UPDATE evaluation_jobs SET status='queued', max_attempts=attempt_count+?,
		next_attempt_at=?, completed_at=NULL, error_message='', lease_owner=NULL, lease_expires_at=NULL, heartbeat_at=NULL
		WHERE id=? AND status IN ('dead_lettered','failed')`, additionalAttempts, formatEvaluationTime(now), id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *SessionStore) ListEvaluationResults(jobID string, limit, offset int) ([]model.EvaluationResult, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM evaluation_results WHERE job_id=?`, jobID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(evaluationResultSelect+` WHERE job_id=? ORDER BY sequence LIMIT ? OFFSET ?`, jobID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	results := make([]model.EvaluationResult, 0)
	for rows.Next() {
		result, err := scanEvaluationResult(rows)
		if err != nil {
			return nil, 0, err
		}
		results = append(results, *result)
	}
	return results, total, rows.Err()
}

const evaluationJobSelect = `SELECT id, run_id, variant, pair_key, dataset_id, dataset_revision, dataset_language, status, config_json, total_items,
	completed_items, succeeded_items, failed_items, asr_char_distance, asr_char_units,
	segmented_char_distance, segmented_char_units, asr_word_distance, asr_word_units,
	segmented_word_distance, segmented_word_units, segment_matched, segment_predicted, segment_reference,
	error_message, requested_by, created_at, started_at, completed_at, attempt_count, max_attempts,
	next_attempt_at, lease_owner, lease_expires_at, heartbeat_at
	FROM evaluation_jobs`

func scanEvaluationJob(row interface{ Scan(...any) error }) (*model.EvaluationJob, bool, error) {
	var job model.EvaluationJob
	var configJSON, createdAt string
	var startedAt, completedAt, nextAttemptAt, leaseExpiresAt, heartbeatAt, leaseOwner sql.NullString
	var runID, variant, pairKey sql.NullString
	err := row.Scan(&job.ID, &runID, &variant, &pairKey, &job.DatasetID, &job.DatasetRevision, &job.DatasetLanguage, &job.Status, &configJSON, &job.TotalItems,
		&job.CompletedItems, &job.SucceededItems, &job.FailedItems, &job.ASRCharDistance, &job.ASRCharUnits,
		&job.SegmentedCharDistance, &job.SegmentedCharUnits, &job.ASRWordDistance, &job.ASRWordUnits,
		&job.SegmentedWordDistance, &job.SegmentedWordUnits, &job.SegmentMatched, &job.SegmentPredicted,
		&job.SegmentReference, &job.ErrorMessage, &job.RequestedBy,
		&createdAt, &startedAt, &completedAt, &job.AttemptCount, &job.MaxAttempts,
		&nextAttemptAt, &leaseOwner, &leaseExpiresAt, &heartbeatAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	job.RunID, job.Variant, job.PairKey = runID.String, variant.String, pairKey.String
	if err := json.Unmarshal([]byte(configJSON), &job.Config); err != nil {
		return nil, false, err
	}
	var parseErr error
	if job.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAt); parseErr != nil {
		return nil, false, parseErr
	}
	if startedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, startedAt.String)
		if err != nil {
			return nil, false, err
		}
		job.StartedAt = &parsed
	}
	if completedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return nil, false, err
		}
		job.CompletedAt = &parsed
	}
	job.LeaseOwner = leaseOwner.String
	if job.NextAttemptAt, err = parseNullableTime(nextAttemptAt); err != nil {
		return nil, false, err
	}
	if job.LeaseExpiresAt, err = parseNullableTime(leaseExpiresAt); err != nil {
		return nil, false, err
	}
	if job.HeartbeatAt, err = parseNullableTime(heartbeatAt); err != nil {
		return nil, false, err
	}
	return &job, true, nil
}

func parseNullableTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatEvaluationTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

const evaluationResultSelect = `SELECT id, job_id, dataset_item_id, sequence, status, reference_text,
	asr_text, segmented_text, segments_json, segment_count, asr_char_distance, asr_char_units,
	segmented_char_distance, segmented_char_units, asr_word_distance, asr_word_units,
	segmented_word_distance, segmented_word_units, segment_matched, segment_predicted, segment_reference,
	error_message, started_at, completed_at FROM evaluation_results`

func scanEvaluationResult(row interface{ Scan(...any) error }) (*model.EvaluationResult, error) {
	var result model.EvaluationResult
	var startedAt, completedAt string
	err := row.Scan(&result.ID, &result.JobID, &result.DatasetItemID, &result.Sequence, &result.Status,
		&result.ReferenceText, &result.ASRText, &result.SegmentedText, &result.SegmentsJSON, &result.SegmentCount,
		&result.ASRCharDistance, &result.ASRCharUnits, &result.SegmentedCharDistance, &result.SegmentedCharUnits,
		&result.ASRWordDistance, &result.ASRWordUnits, &result.SegmentedWordDistance, &result.SegmentedWordUnits,
		&result.SegmentMatched, &result.SegmentPredicted, &result.SegmentReference,
		&result.ErrorMessage, &startedAt, &completedAt)
	if err != nil {
		return nil, err
	}
	if result.StartedAt, err = time.Parse(time.RFC3339Nano, startedAt); err != nil {
		return nil, err
	}
	if result.CompletedAt, err = time.Parse(time.RFC3339Nano, completedAt); err != nil {
		return nil, err
	}
	return &result, nil
}
