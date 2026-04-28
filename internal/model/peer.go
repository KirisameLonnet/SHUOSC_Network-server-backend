package model

import (
	"time"

	"github.com/google/uuid"
)

// Peer represents a registered WireGuard peer.
type Peer struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	PublicKey  string     `json:"public_key"`
	AssignedIP string     `json:"assigned_ip"`
	Status     string     `json:"status"` // "active" | "disconnected" | "revoked"
	LastSeen   *time.Time `json:"last_seen"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// PeerWithStudent extends Peer with the owner's student_id (used in admin peer lists).
type PeerWithStudent struct {
	*Peer
	StudentID string `json:"student_id"`
}

// ValidPeerStatuses is the set of allowed peer statuses.
var ValidPeerStatuses = map[string]bool{
	"active":       true,
	"disconnected": true,
	"revoked":      true,
}
