package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"tmk-glance/internal/model"
)

func migrateAuthSQLite(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id                   TEXT PRIMARY KEY,
			email                TEXT NOT NULL UNIQUE COLLATE NOCASE,
			display_name         TEXT NOT NULL DEFAULT '',
			password_hash        TEXT NOT NULL,
			role                 TEXT NOT NULL DEFAULT 'user',
			status               TEXT NOT NULL DEFAULT 'active',
			must_change_password INTEGER NOT NULL DEFAULT 0,
			created_at           TEXT NOT NULL,
			updated_at           TEXT NOT NULL,
			last_login_at        TEXT
		);
		CREATE TABLE IF NOT EXISTS auth_tokens (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL,
			kind       TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			revoked_at TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_auth_tokens_user_kind ON auth_tokens(user_id, kind);
		CREATE INDEX IF NOT EXISTS idx_auth_tokens_expiry ON auth_tokens(expires_at);
		CREATE TABLE IF NOT EXISTS audit_logs (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_user_id TEXT,
			action         TEXT NOT NULL,
			resource_type TEXT NOT NULL DEFAULT '',
			resource_id   TEXT NOT NULL DEFAULT '',
			ip_address    TEXT NOT NULL DEFAULT '',
			user_agent    TEXT NOT NULL DEFAULT '',
			result        TEXT NOT NULL,
			details_json  TEXT NOT NULL DEFAULT '{}',
			created_at    TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at);
		CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_logs(actor_user_id, created_at);
	`)
	return err
}

func migrateAuthMySQL(db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			email VARCHAR(254) NOT NULL UNIQUE,
			display_name VARCHAR(100) NOT NULL DEFAULT '',
			password_hash VARCHAR(255) NOT NULL,
			role VARCHAR(20) NOT NULL DEFAULT 'user',
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
			created_at VARCHAR(40) NOT NULL,
			updated_at VARCHAR(40) NOT NULL,
			last_login_at VARCHAR(40) NULL,
			INDEX idx_users_status_role (status, role)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS auth_tokens (
			id VARCHAR(36) NOT NULL PRIMARY KEY,
			user_id VARCHAR(36) NOT NULL,
			kind VARCHAR(16) NOT NULL,
			token_hash CHAR(64) NOT NULL UNIQUE,
			expires_at VARCHAR(40) NOT NULL,
			created_at VARCHAR(40) NOT NULL,
			revoked_at VARCHAR(40) NULL,
			INDEX idx_auth_tokens_user_kind (user_id, kind),
			INDEX idx_auth_tokens_expiry (expires_at),
			CONSTRAINT fk_auth_tokens_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			actor_user_id VARCHAR(36) NULL,
			action VARCHAR(80) NOT NULL,
			resource_type VARCHAR(40) NOT NULL DEFAULT '',
			resource_id VARCHAR(64) NOT NULL DEFAULT '',
			ip_address VARCHAR(64) NOT NULL DEFAULT '',
			user_agent VARCHAR(512) NOT NULL DEFAULT '',
			result VARCHAR(20) NOT NULL,
			details_json TEXT NOT NULL,
			created_at VARCHAR(40) NOT NULL,
			INDEX idx_audit_created (created_at),
			INDEX idx_audit_actor (actor_user_id, created_at),
			CONSTRAINT fk_audit_actor FOREIGN KEY (actor_user_id) REFERENCES users(id) ON DELETE SET NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (s *SessionStore) CreateUser(user *model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO users
		(id, email, display_name, password_hash, role, status, must_change_password, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, strings.ToLower(strings.TrimSpace(user.Email)), user.DisplayName, user.PasswordHash,
		user.Role, user.Status, user.MustChangePassword, formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	return err
}

func (s *SessionStore) UserCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *SessionStore) GetUserByEmail(email string) (*model.User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanUser(s.db.QueryRow(`SELECT id, email, display_name, password_hash, role, status,
		must_change_password, created_at, updated_at, last_login_at FROM users WHERE email=?`,
		strings.ToLower(strings.TrimSpace(email))))
}

func (s *SessionStore) GetUserByID(id string) (*model.User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanUser(s.db.QueryRow(`SELECT id, email, display_name, password_hash, role, status,
		must_change_password, created_at, updated_at, last_login_at FROM users WHERE id=?`, id))
}

func (s *SessionStore) ListUsers(limit, offset int) ([]model.User, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.Query(`SELECT id, email, display_name, password_hash, role, status,
		must_change_password, created_at, updated_at, last_login_at
		FROM users ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := make([]model.User, 0)
	for rows.Next() {
		user, _, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, *user)
	}
	return users, total, rows.Err()
}

func (s *SessionStore) UpdateUser(id, displayName, role, status string, mustChangePassword bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result, err := s.db.Exec(`UPDATE users SET display_name=?, role=?, status=?, must_change_password=?, updated_at=? WHERE id=?`,
		displayName, role, status, mustChangePassword, formatTime(time.Now()), id)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (s *SessionStore) UpdateUserWithAdminGuard(id, displayName, role, status string, mustChangePassword bool) (bool, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return false, false, err
	}
	defer tx.Rollback()
	var currentRole, currentStatus string
	if err := tx.QueryRow(`SELECT role, status FROM users WHERE id=?`, id).Scan(&currentRole, &currentStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}
	if currentRole == model.RoleAdmin && currentStatus == model.UserStatusActive && (role != model.RoleAdmin || status != model.UserStatusActive) {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role=? AND status=?`, model.RoleAdmin, model.UserStatusActive).Scan(&count); err != nil {
			return false, false, err
		}
		if count <= 1 {
			return false, true, nil
		}
	}
	result, err := tx.Exec(`UPDATE users SET display_name=?, role=?, status=?, must_change_password=?, updated_at=? WHERE id=?`,
		displayName, role, status, mustChangePassword, formatTime(time.Now()), id)
	if err != nil {
		return false, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, false, err
	}
	return count > 0, false, tx.Commit()
}

func (s *SessionStore) UpdatePassword(userID, passwordHash string, mustChangePassword bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE users SET password_hash=?, must_change_password=?, updated_at=? WHERE id=?`,
		passwordHash, mustChangePassword, formatTime(time.Now()), userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE auth_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, formatTime(time.Now()), userID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SessionStore) MarkLogin(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := formatTime(time.Now())
	_, err := s.db.Exec(`UPDATE users SET last_login_at=?, updated_at=? WHERE id=?`, now, now, userID)
	return err
}

func (s *SessionStore) CreateToken(token model.AuthToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO auth_tokens (id, user_id, kind, token_hash, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, token.ID, token.UserID, token.Kind, token.TokenHash,
		formatTime(token.ExpiresAt), formatTime(token.CreatedAt))
	return err
}

func (s *SessionStore) ResolveToken(tokenHash, kind string, now time.Time) (*model.User, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanUser(s.db.QueryRow(`SELECT u.id, u.email, u.display_name, u.password_hash, u.role, u.status,
		u.must_change_password, u.created_at, u.updated_at, u.last_login_at
		FROM auth_tokens t JOIN users u ON u.id=t.user_id
		WHERE t.token_hash=? AND t.kind=? AND t.revoked_at IS NULL AND t.expires_at>?`,
		tokenHash, kind, formatTime(now)))
}

func (s *SessionStore) RotateRefreshToken(oldHash string, access, refresh model.AuthToken) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE auth_tokens SET revoked_at=? WHERE token_hash=? AND kind='refresh' AND revoked_at IS NULL AND expires_at>?`,
		formatTime(time.Now()), oldHash, formatTime(time.Now()))
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 1 {
		return false, err
	}
	for _, token := range []model.AuthToken{access, refresh} {
		if _, err := tx.Exec(`INSERT INTO auth_tokens (id, user_id, kind, token_hash, expires_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, token.ID, token.UserID, token.Kind, token.TokenHash,
			formatTime(token.ExpiresAt), formatTime(token.CreatedAt)); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

func (s *SessionStore) RevokeToken(tokenHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE auth_tokens SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, formatTime(time.Now()), tokenHash)
	return err
}

func (s *SessionStore) RevokeUserTokens(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`UPDATE auth_tokens SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, formatTime(time.Now()), userID)
	return err
}

func (s *SessionStore) WriteAudit(event model.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`INSERT INTO audit_logs
		(actor_user_id, action, resource_type, resource_id, ip_address, user_agent, result, details_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, nullableString(event.ActorUserID), event.Action,
		event.ResourceType, event.ResourceID, event.IPAddress, event.UserAgent, event.Result,
		event.DetailsJSON, formatTime(event.CreatedAt))
	return err
}

func scanUser(row interface{ Scan(...any) error }) (*model.User, bool, error) {
	var user model.User
	var createdAt, updatedAt string
	var lastLogin sql.NullString
	if err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &user.PasswordHash, &user.Role, &user.Status,
		&user.MustChangePassword, &createdAt, &updatedAt, &lastLogin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var err error
	if user.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt); err != nil {
		return nil, false, err
	}
	if user.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt); err != nil {
		return nil, false, err
	}
	if lastLogin.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, lastLogin.String)
		if err != nil {
			return nil, false, err
		}
		user.LastLoginAt = &parsed
	}
	return &user, true, nil
}

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
