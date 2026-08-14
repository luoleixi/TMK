package model

import "time"

const (
	RoleUser  = "user"
	RoleAdmin = "admin"

	UserStatusActive   = "active"
	UserStatusDisabled = "disabled"
)

type User struct {
	ID                 string     `json:"id"`
	Email              string     `json:"email"`
	DisplayName        string     `json:"display_name"`
	PasswordHash       string     `json:"-"`
	Role               string     `json:"role"`
	Status             string     `json:"status"`
	MustChangePassword bool       `json:"must_change_password"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
}

type AuthToken struct {
	ID        string
	UserID    string
	Kind      string
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type AuditEvent struct {
	ActorUserID  string
	Action       string
	ResourceType string
	ResourceID   string
	IPAddress    string
	UserAgent    string
	Result       string
	DetailsJSON  string
	CreatedAt    time.Time
}
