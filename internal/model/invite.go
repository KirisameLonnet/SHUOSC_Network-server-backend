package model

import (
	"time"

	"github.com/google/uuid"
)

// InviteCode represents an invite code for user registration.
type InviteCode struct {
	ID        uuid.UUID  `json:"id"`
	Code      string     `json:"code"`
	CreatedBy uuid.UUID  `json:"created_by"`
	UsedBy    *uuid.UUID `json:"used_by"`
	MaxUses   int        `json:"max_uses"`
	UseCount  int        `json:"use_count"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

// IsAvailable returns true if the invite code can still be used.
func (i *InviteCode) IsAvailable() bool {
	if i.UseCount >= i.MaxUses {
		return false
	}
	if i.ExpiresAt != nil && time.Now().After(*i.ExpiresAt) {
		return false
	}
	return true
}

// State returns a derived state string for the invite code.
// Possible values: "available", "used_up", "expired"
func (i *InviteCode) State() string {
	if i.UseCount >= i.MaxUses {
		return "used_up"
	}
	if i.ExpiresAt != nil && time.Now().After(*i.ExpiresAt) {
		return "expired"
	}
	return "available"
}
