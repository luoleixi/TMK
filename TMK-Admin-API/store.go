package main

import (
	"context"
	"database/sql"
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
	return nil
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
