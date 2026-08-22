package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"tmk-glance/internal/model"
)

var (
	ErrObjectInUse       = errors.New("object is referenced by a dataset")
	ErrDatasetImmutable  = errors.New("dataset is not editable")
	ErrDatasetEmpty      = errors.New("dataset has no items")
	ErrInvalidObjectPair = errors.New("dataset item requires a ready audio object and a ready text object")
	ErrDuplicateAudio    = errors.New("audio object already exists in dataset")
)

func migrateDatasetSQLite(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS storage_objects (
			id            TEXT PRIMARY KEY,
			owner_user_id TEXT NOT NULL,
			kind          TEXT NOT NULL,
			original_name TEXT NOT NULL,
			storage_key   TEXT NOT NULL UNIQUE,
			content_type  TEXT NOT NULL,
			size_bytes    INTEGER NOT NULL,
			sha256        TEXT NOT NULL,
			status        TEXT NOT NULL DEFAULT 'ready',
			created_at    TEXT NOT NULL,
			deleted_at    TEXT,
			FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE RESTRICT
		);
		CREATE INDEX IF NOT EXISTS idx_storage_objects_owner_created ON storage_objects(owner_user_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_storage_objects_sha256 ON storage_objects(sha256);

		CREATE TABLE IF NOT EXISTS datasets (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			language    TEXT NOT NULL,
			status      TEXT NOT NULL DEFAULT 'draft',
			revision    INTEGER NOT NULL DEFAULT 1,
			item_count  INTEGER NOT NULL DEFAULT 0,
			created_by  TEXT NOT NULL,
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL,
			ready_at    TEXT,
			FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT
		);
		CREATE INDEX IF NOT EXISTS idx_datasets_status_updated ON datasets(status, updated_at);

		CREATE TABLE IF NOT EXISTS dataset_items (
			id                       TEXT PRIMARY KEY,
			dataset_id               TEXT NOT NULL,
			sequence                 INTEGER NOT NULL,
			audio_object_id          TEXT NOT NULL,
			reference_text_object_id TEXT NOT NULL,
			reference_segments_json TEXT NOT NULL DEFAULT '[]',
			notes                    TEXT NOT NULL DEFAULT '',
			created_by               TEXT NOT NULL,
			created_at               TEXT NOT NULL,
			UNIQUE (dataset_id, sequence),
			UNIQUE (dataset_id, audio_object_id),
			FOREIGN KEY (dataset_id) REFERENCES datasets(id) ON DELETE CASCADE,
			FOREIGN KEY (audio_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT,
			FOREIGN KEY (reference_text_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT,
			FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT
		);
		CREATE INDEX IF NOT EXISTS idx_dataset_items_dataset ON dataset_items(dataset_id, sequence);
	`)
	if err != nil {
		return err
	}
	return ensureDatasetSegmentsSQLite(db)
}

func migrateDatasetMySQL(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS storage_objects (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			owner_user_id VARCHAR(36) NOT NULL,
			kind VARCHAR(16) NOT NULL,
			original_name VARCHAR(255) NOT NULL,
			storage_key VARCHAR(512) NOT NULL UNIQUE,
			content_type VARCHAR(100) NOT NULL,
			size_bytes BIGINT NOT NULL,
			sha256 CHAR(64) NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'ready',
			created_at VARCHAR(40) NOT NULL,
			deleted_at VARCHAR(40) NULL,
			INDEX idx_storage_objects_owner_created (owner_user_id, created_at),
			INDEX idx_storage_objects_sha256 (sha256),
			CONSTRAINT fk_storage_objects_owner FOREIGN KEY (owner_user_id) REFERENCES users(id) ON DELETE RESTRICT
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS datasets (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			name VARCHAR(160) NOT NULL,
			description TEXT NOT NULL,
			language VARCHAR(32) NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'draft',
			revision INT NOT NULL DEFAULT 1,
			item_count INT NOT NULL DEFAULT 0,
			created_by VARCHAR(36) NOT NULL,
			created_at VARCHAR(40) NOT NULL,
			updated_at VARCHAR(40) NOT NULL,
			ready_at VARCHAR(40) NULL,
			INDEX idx_datasets_status_updated (status, updated_at),
			CONSTRAINT fk_datasets_creator FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS dataset_items (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			dataset_id VARCHAR(36) NOT NULL,
			sequence INT NOT NULL,
			audio_object_id VARCHAR(36) NOT NULL,
			reference_text_object_id VARCHAR(36) NOT NULL,
			reference_segments_json LONGTEXT NOT NULL,
			notes TEXT NOT NULL,
			created_by VARCHAR(36) NOT NULL,
			created_at VARCHAR(40) NOT NULL,
			UNIQUE KEY uq_dataset_item_sequence (dataset_id, sequence),
			UNIQUE KEY uq_dataset_item_audio (dataset_id, audio_object_id),
			INDEX idx_dataset_items_dataset (dataset_id, sequence),
			CONSTRAINT fk_dataset_items_dataset FOREIGN KEY (dataset_id) REFERENCES datasets(id) ON DELETE CASCADE,
			CONSTRAINT fk_dataset_items_audio FOREIGN KEY (audio_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT,
			CONSTRAINT fk_dataset_items_text FOREIGN KEY (reference_text_object_id) REFERENCES storage_objects(id) ON DELETE RESTRICT,
			CONSTRAINT fk_dataset_items_creator FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE RESTRICT
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return ensureDatasetSegmentsMySQL(db)
}

func ensureDatasetSegmentsSQLite(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(dataset_items)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		found = found || name == "reference_segments_json"
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE dataset_items ADD COLUMN reference_segments_json TEXT NOT NULL DEFAULT '[]'`)
	return err
}

func ensureDatasetSegmentsMySQL(db *sql.DB) error {
	var nullable string
	err := db.QueryRow(`SELECT is_nullable FROM information_schema.columns
		WHERE table_schema=DATABASE() AND table_name='dataset_items' AND column_name='reference_segments_json'`).Scan(&nullable)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := db.Exec(`ALTER TABLE dataset_items ADD COLUMN reference_segments_json LONGTEXT NULL`); err != nil {
			return err
		}
		nullable = "YES"
	} else if err != nil {
		return err
	}
	if nullable == "NO" {
		return nil
	}
	if _, err := db.Exec(`UPDATE dataset_items SET reference_segments_json='[]' WHERE reference_segments_json IS NULL`); err != nil {
		return err
	}
	_, err = db.Exec(`ALTER TABLE dataset_items MODIFY reference_segments_json LONGTEXT NOT NULL`)
	return err
}

func (s *SessionStore) CreateStorageObject(object *model.StorageObject) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO storage_objects
		(id, owner_user_id, kind, original_name, storage_key, content_type, size_bytes, sha256, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, object.ID, object.OwnerUserID, object.Kind,
		object.OriginalName, object.StorageKey, object.ContentType, object.SizeBytes, object.SHA256,
		object.Status, formatTime(object.CreatedAt))
	return err
}

func (s *SessionStore) GetStorageObject(id string) (*model.StorageObject, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanStorageObject(s.db.QueryRow(`SELECT id, owner_user_id, kind, original_name, storage_key,
		content_type, size_bytes, sha256, status, created_at, deleted_at FROM storage_objects WHERE id=?`, id))
}

func (s *SessionStore) ListStorageObjects(kind string, limit, offset int) ([]model.StorageObject, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	where := ` WHERE status='ready'`
	var args []any
	if kind != "" {
		where += ` AND kind=?`
		args = append(args, kind)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM storage_objects`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id, owner_user_id, kind, original_name, storage_key, content_type,
		size_bytes, sha256, status, created_at, deleted_at FROM storage_objects`+where+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	objects := make([]model.StorageObject, 0)
	for rows.Next() {
		object, _, err := scanStorageObject(rows)
		if err != nil {
			return nil, 0, err
		}
		objects = append(objects, *object)
	}
	return objects, total, rows.Err()
}

func (s *SessionStore) StorageUsage(ownerUserID string) (int64, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ownerBytes, totalBytes int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM storage_objects WHERE owner_user_id=? AND status='ready'`, ownerUserID).Scan(&ownerBytes); err != nil {
		return 0, 0, err
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM storage_objects WHERE status='ready'`).Scan(&totalBytes); err != nil {
		return 0, 0, err
	}
	return ownerBytes, totalBytes, nil
}

func (s *SessionStore) BeginDeleteStorageObject(id string) (*model.StorageObject, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	object, ok, err := scanStorageObject(tx.QueryRow(`SELECT id, owner_user_id, kind, original_name, storage_key,
		content_type, size_bytes, sha256, status, created_at, deleted_at FROM storage_objects WHERE id=?`, id))
	if err != nil || !ok {
		return object, ok, err
	}
	var references int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM dataset_items WHERE audio_object_id=? OR reference_text_object_id=?`, id, id).Scan(&references); err != nil {
		return nil, false, err
	}
	if references > 0 {
		return nil, false, ErrObjectInUse
	}
	if object.Status == model.ObjectStatusReady {
		result, err := tx.Exec(`UPDATE storage_objects SET status='deleting', deleted_at=? WHERE id=? AND status='ready'`, formatTime(time.Now()), id)
		if err != nil {
			return nil, false, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, false, err
		}
		if count != 1 {
			return nil, false, errors.New("storage object changed while deletion started")
		}
	} else if object.Status != model.ObjectStatusDeleting {
		return nil, false, errors.New("storage object is not deletable")
	}
	return object, true, tx.Commit()
}

func (s *SessionStore) RestoreStorageObject(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE storage_objects SET status='ready', deleted_at=NULL WHERE id=? AND status='deleting'`, id)
	return err
}

func (s *SessionStore) DeleteStorageObjectRecord(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM storage_objects WHERE id=? AND status='deleting'`, id)
	return err
}

func (s *SessionStore) CreateDataset(dataset *model.Dataset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO datasets
		(id, name, description, language, status, revision, item_count, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`, dataset.ID, dataset.Name, dataset.Description,
		dataset.Language, dataset.Status, dataset.Revision, dataset.CreatedBy,
		formatTime(dataset.CreatedAt), formatTime(dataset.UpdatedAt))
	return err
}

func (s *SessionStore) GetDataset(id string) (*model.Dataset, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanDataset(s.db.QueryRow(`SELECT id, name, description, language, status, revision, item_count,
		created_by, created_at, updated_at, ready_at FROM datasets WHERE id=?`, id))
}

func (s *SessionStore) ListDatasets(status string, limit, offset int) ([]model.Dataset, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	where := ""
	var args []any
	if status != "" {
		where = ` WHERE status=?`
		args = append(args, status)
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM datasets`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id, name, description, language, status, revision, item_count,
		created_by, created_at, updated_at, ready_at FROM datasets`+where+` ORDER BY updated_at DESC LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	datasets := make([]model.Dataset, 0)
	for rows.Next() {
		dataset, _, err := scanDataset(rows)
		if err != nil {
			return nil, 0, err
		}
		datasets = append(datasets, *dataset)
	}
	return datasets, total, rows.Err()
}

func (s *SessionStore) UpdateDatasetDraft(id, name, description, language string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE datasets SET name=?, description=?, language=?, revision=revision+1, updated_at=? WHERE id=? AND status='draft'`,
		name, description, language, formatTime(time.Now()), id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *SessionStore) AddDatasetItem(item *model.DatasetItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM datasets WHERE id=?`, item.DatasetID).Scan(&status); err != nil {
		return err
	}
	if status != model.DatasetStatusDraft {
		return ErrDatasetImmutable
	}
	var audioKind, audioStatus, audioName string
	var audioSize int64
	if err := tx.QueryRow(`SELECT kind, status, original_name, size_bytes FROM storage_objects WHERE id=?`, item.AudioObjectID).
		Scan(&audioKind, &audioStatus, &audioName, &audioSize); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidObjectPair
		}
		return err
	}
	var textKind, textStatus, textName string
	var textSize int64
	if err := tx.QueryRow(`SELECT kind, status, original_name, size_bytes FROM storage_objects WHERE id=?`, item.ReferenceTextObjectID).
		Scan(&textKind, &textStatus, &textName, &textSize); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidObjectPair
		}
		return err
	}
	if audioKind != model.ObjectKindAudio || textKind != model.ObjectKindText || audioStatus != model.ObjectStatusReady || textStatus != model.ObjectStatusReady {
		return ErrInvalidObjectPair
	}
	var duplicate int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM dataset_items WHERE dataset_id=? AND audio_object_id=?`, item.DatasetID, item.AudioObjectID).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate > 0 {
		return ErrDuplicateAudio
	}
	if err := tx.QueryRow(`SELECT COALESCE(MAX(sequence), 0)+1 FROM dataset_items WHERE dataset_id=?`, item.DatasetID).Scan(&item.Sequence); err != nil {
		return err
	}
	if item.ReferenceSegments == nil {
		item.ReferenceSegments = []model.ReferenceSegment{}
	}
	referenceSegments, err := json.Marshal(item.ReferenceSegments)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO dataset_items
		(id, dataset_id, sequence, audio_object_id, reference_text_object_id, reference_segments_json, notes, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.DatasetID, item.Sequence, item.AudioObjectID,
		item.ReferenceTextObjectID, string(referenceSegments), item.Notes, item.CreatedBy, formatTime(item.CreatedAt)); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE datasets SET item_count=item_count+1, revision=revision+1, updated_at=? WHERE id=?`, formatTime(time.Now()), item.DatasetID); err != nil {
		return err
	}
	item.AudioOriginalName, item.AudioSizeBytes = audioName, audioSize
	item.TextOriginalName, item.TextSizeBytes = textName, textSize
	return tx.Commit()
}

func (s *SessionStore) ListDatasetItems(datasetID string) ([]model.DatasetItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT i.id, i.dataset_id, i.sequence, i.audio_object_id, ao.original_name, ao.size_bytes,
		i.reference_text_object_id, txt.original_name, txt.size_bytes, i.reference_segments_json, i.notes, i.created_by, i.created_at
		FROM dataset_items i
		JOIN storage_objects ao ON ao.id=i.audio_object_id
		JOIN storage_objects txt ON txt.id=i.reference_text_object_id
		WHERE i.dataset_id=? ORDER BY i.sequence`, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.DatasetItem, 0)
	for rows.Next() {
		var item model.DatasetItem
		var createdAt, referenceSegments string
		if err := rows.Scan(&item.ID, &item.DatasetID, &item.Sequence, &item.AudioObjectID, &item.AudioOriginalName,
			&item.AudioSizeBytes, &item.ReferenceTextObjectID, &item.TextOriginalName, &item.TextSizeBytes,
			&referenceSegments, &item.Notes, &item.CreatedBy, &createdAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(referenceSegments), &item.ReferenceSegments); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, err
		}
		item.CreatedAt = parsed
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *SessionStore) DeleteDatasetItem(datasetID, itemID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRow(`SELECT status FROM datasets WHERE id=?`, datasetID).Scan(&status); err != nil {
		return false, err
	}
	if status != model.DatasetStatusDraft {
		return false, ErrDatasetImmutable
	}
	result, err := tx.Exec(`DELETE FROM dataset_items WHERE id=? AND dataset_id=?`, itemID, datasetID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE datasets SET item_count=item_count-1, revision=revision+1, updated_at=? WHERE id=?`, formatTime(time.Now()), datasetID); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *SessionStore) MarkDatasetReady(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var status string
	var itemCount int
	if err := s.db.QueryRow(`SELECT status, item_count FROM datasets WHERE id=?`, id).Scan(&status, &itemCount); err != nil {
		return false, err
	}
	if status != model.DatasetStatusDraft {
		return false, ErrDatasetImmutable
	}
	if itemCount == 0 {
		return false, ErrDatasetEmpty
	}
	now := formatTime(time.Now())
	result, err := s.db.Exec(`UPDATE datasets SET status='ready', revision=revision+1, updated_at=?, ready_at=? WHERE id=? AND status='draft'`, now, now, id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *SessionStore) ArchiveDataset(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE datasets SET status='archived', updated_at=? WHERE id=? AND status='ready'`, formatTime(time.Now()), id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *SessionStore) DeleteDraftDataset(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`DELETE FROM datasets WHERE id=? AND status='draft'`, id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func scanStorageObject(row interface{ Scan(...any) error }) (*model.StorageObject, bool, error) {
	var object model.StorageObject
	var createdAt string
	var deletedAt sql.NullString
	if err := row.Scan(&object.ID, &object.OwnerUserID, &object.Kind, &object.OriginalName, &object.StorageKey,
		&object.ContentType, &object.SizeBytes, &object.SHA256, &object.Status, &createdAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, false, err
	}
	object.CreatedAt = parsed
	if deletedAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, deletedAt.String)
		if err != nil {
			return nil, false, err
		}
		object.DeletedAt = &parsed
	}
	return &object, true, nil
}

func scanDataset(row interface{ Scan(...any) error }) (*model.Dataset, bool, error) {
	var dataset model.Dataset
	var createdAt, updatedAt string
	var readyAt sql.NullString
	if err := row.Scan(&dataset.ID, &dataset.Name, &dataset.Description, &dataset.Language, &dataset.Status,
		&dataset.Revision, &dataset.ItemCount, &dataset.CreatedBy, &createdAt, &updatedAt, &readyAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var err error
	if dataset.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return nil, false, err
	}
	if dataset.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return nil, false, err
	}
	if readyAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, readyAt.String)
		if err != nil {
			return nil, false, err
		}
		dataset.ReadyAt = &parsed
	}
	return &dataset, true, nil
}
