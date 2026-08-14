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
	ID           int64     `json:"id"`
	ActorUserID  string    `json:"actor_user_id,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	Result       string    `json:"result"`
	DetailsJSON  string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}
