package model

import (
	"time"

	"github.com/google/uuid"
)

// User represents a registered user account.
type User struct {
	ID          uuid.UUID `json:"id"`
	StudentID   string    `json:"student_id"`
	Password    string    `json:"-"`    // bcrypt hash, never serialized to JSON
	Role        string    `json:"role"` // "user" | "admin"
	InviteID    uuid.UUID `json:"invite_id"`
	DisplayName *string   `json:"display_name"`
	Email       *string   `json:"email"`
	Phone       *string   `json:"phone"`
	Wechat      *string   `json:"wechat"`
	Telegram    *string   `json:"telegram"`
	MaxPeers    int       `json:"max_peers"`
	Status      string    `json:"status"` // "active" | "suspended" | "deleted"
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// IsActive returns true if the user is in "active" status.
func (u *User) IsActive() bool {
	return u.Status == "active"
}

// IsAdmin returns true if the user has the "admin" role.
func (u *User) IsAdmin() bool {
	return u.Role == "admin"
}

// ValidRoles is the set of allowed user roles.
var ValidRoles = map[string]bool{
	"user":  true,
	"admin": true,
}

// ValidStatuses is the set of allowed user statuses.
var ValidStatuses = map[string]bool{
	"active":    true,
	"suspended": true,
	"deleted":   true,
}
