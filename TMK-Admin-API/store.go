package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
)

type UserRecord struct {
	ID, Email, PasswordHash, Role, Status string
	DisplayName                           string
}
type UserStore struct{ db *sql.DB }

type ChangeRequest struct {
	ID          string          `json:"id"`
	Environment string          `json:"environment"`
	Type        string          `json:"type"`
	Target      string          `json:"target"`
	OldValue    json.RawMessage `json:"old_value,omitempty"`
	NewValue    json.RawMessage `json:"new_value"`
	ReleaseID   string          `json:"release_id,omitempty"`
	CommitSHA   string          `json:"commit_sha,omitempty"`
	Status      string          `json:"status"`
	RequestedBy string          `json:"requested_by"`
	ApprovedBy  string          `json:"approved_by,omitempty"`
	Reason      string          `json:"reason"`
	Result      json.RawMessage `json:"result,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	ApprovedAt  *time.Time      `json:"approved_at,omitempty"`
	ExecutedAt  *time.Time      `json:"executed_at,omitempty"`
	ExpiresAt   *time.Time      `json:"expires_at,omitempty"`
}

type SegmenterRuntimeConfig struct {
	Enabled        bool           `json:"enabled"`
	RolloutPercent int            `json:"rollout_percent"`
	Version        string         `json:"version"`
	Config         map[string]any `json:"config"`
	Revision       int64          `json:"revision"`
	Status         string         `json:"status"`
	ChangedBy      string         `json:"changed_by,omitempty"`
	ChangeReason   string         `json:"change_reason,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	AppliedAt      *time.Time     `json:"applied_at,omitempty"`
}

func NewUserStore(dsn string) (*UserStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("ADMIN_API_DB_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	s := &UserStore{db: db}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *UserStore) migrate() error {
	for _, q := range []string{`CREATE TABLE IF NOT EXISTS admin_users (id VARCHAR(36) PRIMARY KEY,email VARCHAR(254) NOT NULL UNIQUE,password_hash VARCHAR(255) NOT NULL,display_name VARCHAR(100) NOT NULL DEFAULT '',role VARCHAR(32) NOT NULL DEFAULT 'viewer',status VARCHAR(20) NOT NULL DEFAULT 'active',created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, `CREATE TABLE IF NOT EXISTS admin_roles (name VARCHAR(32) PRIMARY KEY,description VARCHAR(255) NOT NULL DEFAULT '') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, `CREATE TABLE IF NOT EXISTS admin_permissions (name VARCHAR(64) PRIMARY KEY,description VARCHAR(255) NOT NULL DEFAULT '') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, `CREATE TABLE IF NOT EXISTS admin_user_roles (user_id VARCHAR(36) NOT NULL,role_name VARCHAR(32) NOT NULL,PRIMARY KEY(user_id,role_name),FOREIGN KEY(user_id) REFERENCES admin_users(id) ON DELETE CASCADE,FOREIGN KEY(role_name) REFERENCES admin_roles(name) ON DELETE CASCADE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, `CREATE TABLE IF NOT EXISTS admin_role_permissions (role_name VARCHAR(32) NOT NULL,permission_name VARCHAR(64) NOT NULL,PRIMARY KEY(role_name,permission_name),FOREIGN KEY(role_name) REFERENCES admin_roles(name) ON DELETE CASCADE,FOREIGN KEY(permission_name) REFERENCES admin_permissions(name) ON DELETE CASCADE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, `CREATE TABLE IF NOT EXISTS admin_refresh_tokens (token_hash CHAR(64) PRIMARY KEY,user_id VARCHAR(36) NOT NULL,expires_at DATETIME(6) NOT NULL,revoked_at DATETIME(6) NULL,created_at DATETIME(6) NOT NULL,FOREIGN KEY(user_id) REFERENCES admin_users(id) ON DELETE CASCADE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`, `CREATE TABLE IF NOT EXISTS admin_audit_logs (id BIGINT AUTO_INCREMENT PRIMARY KEY,actor_user_id VARCHAR(36),action VARCHAR(80) NOT NULL,resource_type VARCHAR(40) NOT NULL,resource_id VARCHAR(64) NOT NULL DEFAULT '',result VARCHAR(20) NOT NULL,details_json JSON NOT NULL,request_id VARCHAR(100) NOT NULL DEFAULT '',created_at DATETIME(6) NOT NULL,INDEX idx_admin_audit_created(created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	_, _ = s.db.Exec(`INSERT IGNORE INTO admin_roles(name,description) VALUES ('admin','full access'),('viewer','read-only access')`)
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS admin_segmenter_runtime_configs (id TINYINT PRIMARY KEY,enabled BOOLEAN NOT NULL,rollout_percent INT NOT NULL,version VARCHAR(64) NOT NULL,config_json JSON NOT NULL,revision BIGINT NOT NULL,status VARCHAR(20) NOT NULL,changed_by VARCHAR(36) NOT NULL,change_reason VARCHAR(255) NOT NULL,created_at DATETIME(6) NOT NULL,applied_at DATETIME(6) NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS change_requests (id VARCHAR(64) PRIMARY KEY,environment VARCHAR(20) NOT NULL,type VARCHAR(32) NOT NULL,target VARCHAR(120) NOT NULL,old_value_json JSON NULL,new_value_json JSON NOT NULL,release_id VARCHAR(120) NOT NULL DEFAULT '',commit_sha VARCHAR(64) NOT NULL DEFAULT '',status VARCHAR(32) NOT NULL,requested_by VARCHAR(36) NOT NULL,approved_by VARCHAR(36) NOT NULL DEFAULT '',reason VARCHAR(255) NOT NULL,result JSON NULL,created_at DATETIME(6) NOT NULL,approved_at DATETIME(6) NULL,executed_at DATETIME(6) NULL,expires_at DATETIME(6) NULL,INDEX idx_change_requests_env_status(environment,status),INDEX idx_change_requests_release(release_id,commit_sha)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`); err != nil {
		return err
	}
	return nil
}

func (s *UserStore) CreateChangeRequest(ctx context.Context, request ChangeRequest) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO change_requests(id,environment,type,target,old_value_json,new_value_json,release_id,commit_sha,status,requested_by,reason,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, request.ID, request.Environment, request.Type, request.Target, nullJSON(request.OldValue), request.NewValue, request.ReleaseID, request.CommitSHA, request.Status, request.RequestedBy, request.Reason, request.CreatedAt, nullTime(request.ExpiresAt))
	return err
}

func nullJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
func nullTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

const changeRequestSelect = `SELECT id,environment,type,target,COALESCE(old_value_json,'null'),new_value_json,release_id,commit_sha,status,requested_by,approved_by,reason,COALESCE(result,'null'),created_at,approved_at,executed_at,expires_at FROM change_requests`

func scanChangeRequest(row interface{ Scan(...any) error }) (ChangeRequest, error) {
	var value ChangeRequest
	var oldValue, newValue, result []byte
	var approved, executed, expires sql.NullTime
	err := row.Scan(&value.ID, &value.Environment, &value.Type, &value.Target, &oldValue, &newValue, &value.ReleaseID, &value.CommitSHA, &value.Status, &value.RequestedBy, &value.ApprovedBy, &value.Reason, &result, &value.CreatedAt, &approved, &executed, &expires)
	if err != nil {
		return value, err
	}
	value.OldValue, value.NewValue, value.Result = oldValue, newValue, result
	if approved.Valid {
		value.ApprovedAt = &approved.Time
	}
	if executed.Valid {
		value.ExecutedAt = &executed.Time
	}
	if expires.Valid {
		value.ExpiresAt = &expires.Time
	}
	return value, nil
}
func (s *UserStore) GetChangeRequest(ctx context.Context, id string) (ChangeRequest, error) {
	return scanChangeRequest(s.db.QueryRowContext(ctx, changeRequestSelect+` WHERE id=?`, id))
}
func (s *UserStore) ListChangeRequests(ctx context.Context, environment, status string) ([]ChangeRequest, error) {
	rows, err := s.db.QueryContext(ctx, changeRequestSelect+` WHERE (?='' OR environment=?) AND (?='' OR status=?) ORDER BY created_at DESC LIMIT 200`, environment, environment, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []ChangeRequest
	for rows.Next() {
		value, err := scanChangeRequest(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *UserStore) ApproveChangeRequest(ctx context.Context, id, actor string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE change_requests SET status='approved',approved_by=?,approved_at=? WHERE id=? AND status='pending_approval' AND requested_by<>? AND (expires_at IS NULL OR expires_at>?)`, actor, now, id, actor, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}
func (s *UserStore) TransitionChangeRequest(ctx context.Context, id, from, to string, result json.RawMessage, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE change_requests SET status=?,result=?,executed_at=CASE WHEN ? IN ('succeeded','failed','rolled_back') THEN ? ELSE executed_at END WHERE id=? AND status=?`, to, nullJSON(result), to, now, id, from)
	if err != nil {
		return false, err
	}
	count, err := res.RowsAffected()
	return count == 1, err
}

func (s *UserStore) AuthorizeChangeRequest(ctx context.Context, environment, releaseID, commitSHA, token string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM change_requests WHERE environment=? AND release_id=? AND commit_sha=? AND status='executing' AND expires_at>NOW(6) AND JSON_UNQUOTE(JSON_EXTRACT(result,'$.approval_token'))=?`, environment, releaseID, commitSHA, token).Scan(&count)
	return count == 1, err
}

func (s *UserStore) GetSegmenterRuntimeConfig(ctx context.Context) (SegmenterRuntimeConfig, error) {
	var value SegmenterRuntimeConfig
	var raw []byte
	var applied sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT enabled,rollout_percent,version,config_json,revision,status,changed_by,change_reason,created_at,applied_at FROM admin_segmenter_runtime_configs WHERE id=1`).Scan(&value.Enabled, &value.RolloutPercent, &value.Version, &raw, &value.Revision, &value.Status, &value.ChangedBy, &value.ChangeReason, &value.CreatedAt, &applied)
	if errors.Is(err, sql.ErrNoRows) {
		return SegmenterRuntimeConfig{Enabled: true, RolloutPercent: 100, Version: "rule-v2", Config: map[string]any{}, Status: "applied"}, nil
	}
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal(raw, &value.Config); err != nil {
		return value, err
	}
	if applied.Valid {
		value.AppliedAt = &applied.Time
	}
	return value, nil
}
func (s *UserStore) SaveSegmenterRuntimeConfig(ctx context.Context, value SegmenterRuntimeConfig) error {
	raw, err := json.Marshal(value.Config)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_segmenter_runtime_configs(id,enabled,rollout_percent,version,config_json,revision,status,changed_by,change_reason,created_at) VALUES(1,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),rollout_percent=VALUES(rollout_percent),version=VALUES(version),config_json=VALUES(config_json),revision=VALUES(revision),status=VALUES(status),changed_by=VALUES(changed_by),change_reason=VALUES(change_reason),created_at=VALUES(created_at),applied_at=NULL`, value.Enabled, value.RolloutPercent, value.Version, raw, value.Revision, value.Status, value.ChangedBy, value.ChangeReason, value.CreatedAt)
	return err
}
func (s *UserStore) MarkSegmenterApplied(ctx context.Context, revision int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE admin_segmenter_runtime_configs SET status='applied',applied_at=NOW(6) WHERE id=1 AND revision=?`, revision)
	return err
}
func (s *UserStore) Close() error { return s.db.Close() }
func (s *UserStore) FindByEmail(ctx context.Context, email string) (UserRecord, error) {
	var u UserRecord
	err := s.db.QueryRowContext(ctx, `SELECT id,email,password_hash,display_name,role,status FROM admin_users WHERE email=?`, strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Role, &u.Status)
	return u, err
}
func (s *UserStore) Register(ctx context.Context, email, password, name string) (UserRecord, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return UserRecord{}, err
	}
	user := UserRecord{ID: fmt.Sprintf("usr_%d", time.Now().UnixNano()), Email: strings.ToLower(strings.TrimSpace(email)), PasswordHash: string(hash), DisplayName: name, Role: "viewer", Status: "active"}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_users(id,email,password_hash,display_name,role,status,created_at,updated_at) VALUES(?,?,?,?,?,?,NOW(6),NOW(6))`, user.ID, user.Email, user.PasswordHash, user.DisplayName, user.Role, user.Status)
	return user, err
}
func (s *UserStore) EnsureBootstrap(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return errors.New("bootstrap admin credentials are required")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&count); err != nil || count > 0 {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_users(id,email,password_hash,display_name,role,status,created_at,updated_at) VALUES(?,?,?,'Administrator','admin','active',NOW(6),NOW(6))`, fmt.Sprintf("usr_%d", time.Now().UnixNano()), strings.ToLower(email), hash)
	return err
}
