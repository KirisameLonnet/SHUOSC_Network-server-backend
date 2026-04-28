package admin

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/shuosc/scnet-server/internal/model"
	"github.com/shuosc/scnet-server/internal/store"
)

var (
	ErrUserNotFound    = errors.New("user not found")
	ErrPeerNotFound    = errors.New("peer not found")
	ErrInviteNotFound  = errors.New("invite not found")
	ErrNotAdmin        = errors.New("user is not an admin")
	ErrInvalidStatus   = errors.New("invalid status: must be 'active' or 'suspended'")
	ErrInvalidMaxPeers = errors.New("invalid max_peers: must be >= 0")
	ErrInvalidMaxUses  = errors.New("invalid max_uses: must be >= 1 and not below current use_count")
	ErrInvalidExpiry   = errors.New("invalid expires_days: must be >= 1")
	ErrInviteUsed      = errors.New("invite has already been used and cannot be deleted")
)

type adminUserStore interface {
	FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	Update(ctx context.Context, userID uuid.UUID, fields store.UserUpdateFields) error
	List(ctx context.Context, params store.UserListParams) (*store.UserListResult, error)
	CountByStatus(ctx context.Context) (map[string]int, error)
}

type adminPeerStore interface {
	FindByID(ctx context.Context, id uuid.UUID) (*model.Peer, error)
	UpdateStatus(ctx context.Context, peerID uuid.UUID, status string) error
	List(ctx context.Context, params store.PeerListParams) (*store.PeerListResult, error)
	CountByStatus(ctx context.Context) (map[string]int, error)
	ListByUserID(ctx context.Context, userID uuid.UUID, params store.PeerListByUserParams) (*store.PeerListResult, error)
}

type adminInviteStore interface {
	Create(ctx context.Context, invite *model.InviteCode) error
	FindByID(ctx context.Context, id uuid.UUID) (*model.InviteCode, error)
	Update(ctx context.Context, inviteID uuid.UUID, fields store.InviteUpdateFields) error
	Delete(ctx context.Context, inviteID uuid.UUID) error
	List(ctx context.Context, params store.InviteListParams) (*store.InviteListResult, error)
	CountByState(ctx context.Context) (map[string]int, error)
}

type AdminService interface {
	GetSummary(ctx context.Context) (*Summary, error)
	ListUsers(ctx context.Context, params store.UserListParams) (*store.UserListResult, error)
	GetUser(ctx context.Context, userID string) (*model.User, error)
	UpdateUser(ctx context.Context, userID string, fields AdminUserUpdateFields) (*model.User, error)
	ListUserPeers(ctx context.Context, userID string, params store.PeerListByUserParams) (*store.PeerListResult, error)
	ListPeers(ctx context.Context, params store.PeerListParams) (*store.PeerListResult, error)
	DisconnectPeer(ctx context.Context, peerID string) error
	RevokePeer(ctx context.Context, peerID string) error
	ListInvites(ctx context.Context, params store.InviteListParams) (*store.InviteListResult, error)
	CreateInvite(ctx context.Context, createdBy string, maxUses int, expiresDays int) (*model.InviteCode, error)
	UpdateInvite(ctx context.Context, inviteID string, fields InviteUpdateFields) (*model.InviteCode, error)
	DeleteInvite(ctx context.Context, inviteID string) error
	VerifyAdmin(ctx context.Context, userID string) (*model.User, error)
	GetPeer(ctx context.Context, peerID string) (*model.Peer, error)
}

type Summary struct {
	UsersTotal        int `json:"users_total"`
	UsersActive       int `json:"users_active"`
	UsersSuspended    int `json:"users_suspended"`
	PeersActive       int `json:"peers_active"`
	PeersDisconnected int `json:"peers_disconnected"`
	PeersRevoked      int `json:"peers_revoked"`
	InvitesTotal      int `json:"invites_total"`
	InvitesAvailable  int `json:"invites_available"`
	InvitesExpired    int `json:"invites_expired"`
}

type AdminUserUpdateFields struct {
	MaxPeers *int
	Status   *string
}

type InviteUpdateFields struct {
	MaxUses        *int
	ExpiresAt      *time.Time
	ClearExpiresAt bool
}

type adminService struct {
	userStore   adminUserStore
	peerStore   adminPeerStore
	inviteStore adminInviteStore
}

func NewAdminService(userStore adminUserStore, peerStore adminPeerStore, inviteStore adminInviteStore) AdminService {
	return &adminService{
		userStore:   userStore,
		peerStore:   peerStore,
		inviteStore: inviteStore,
	}
}

func (s *adminService) GetSummary(ctx context.Context) (*Summary, error) {
	userCounts, err := s.userStore.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	peerCounts, err := s.peerStore.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	inviteCounts, err := s.inviteStore.CountByState(ctx)
	if err != nil {
		return nil, err
	}

	summary := &Summary{
		UsersTotal:        userCounts["active"] + userCounts["suspended"] + userCounts["deleted"],
		UsersActive:       userCounts["active"],
		UsersSuspended:    userCounts["suspended"],
		PeersActive:       peerCounts["active"],
		PeersDisconnected: peerCounts["disconnected"],
		PeersRevoked:      peerCounts["revoked"],
		InvitesTotal:      inviteCounts["available"] + inviteCounts["used_up"] + inviteCounts["expired"],
		InvitesAvailable:  inviteCounts["available"],
		InvitesExpired:    inviteCounts["expired"],
	}
	return summary, nil
}

func (s *adminService) ListUsers(ctx context.Context, params store.UserListParams) (*store.UserListResult, error) {
	return s.userStore.List(ctx, params)
}

func (s *adminService) GetUser(ctx context.Context, userID string) (*model.User, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}
	user, err := s.userStore.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *adminService) UpdateUser(ctx context.Context, userID string, fields AdminUserUpdateFields) (*model.User, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}

	updateFields := store.UserUpdateFields{}

	if fields.MaxPeers != nil {
		if *fields.MaxPeers < 0 {
			return nil, ErrInvalidMaxPeers
		}
		updateFields.MaxPeers = fields.MaxPeers
	}

	if fields.Status != nil {
		if *fields.Status != "active" && *fields.Status != "suspended" {
			return nil, ErrInvalidStatus
		}
		updateFields.Status = fields.Status
	}

	err = s.userStore.Update(ctx, id, updateFields)
	if err != nil {
		return nil, err
	}

	user, err := s.userStore.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *adminService) ListUserPeers(ctx context.Context, userID string, params store.PeerListByUserParams) (*store.PeerListResult, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}
	return s.peerStore.ListByUserID(ctx, id, params)
}

func (s *adminService) ListPeers(ctx context.Context, params store.PeerListParams) (*store.PeerListResult, error) {
	return s.peerStore.List(ctx, params)
}

func (s *adminService) DisconnectPeer(ctx context.Context, peerID string) error {
	id, err := uuid.Parse(peerID)
	if err != nil {
		return err
	}
	peer, err := s.peerStore.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if peer == nil {
		return ErrPeerNotFound
	}
	return s.peerStore.UpdateStatus(ctx, id, "disconnected")
}

func (s *adminService) RevokePeer(ctx context.Context, peerID string) error {
	id, err := uuid.Parse(peerID)
	if err != nil {
		return err
	}
	peer, err := s.peerStore.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if peer == nil {
		return ErrPeerNotFound
	}
	return s.peerStore.UpdateStatus(ctx, id, "revoked")
}

func (s *adminService) ListInvites(ctx context.Context, params store.InviteListParams) (*store.InviteListResult, error) {
	return s.inviteStore.List(ctx, params)
}

func (s *adminService) CreateInvite(ctx context.Context, createdBy string, maxUses int, expiresDays int) (*model.InviteCode, error) {
	creatorID, err := uuid.Parse(createdBy)
	if err != nil {
		return nil, err
	}
	if maxUses < 1 {
		return nil, ErrInvalidMaxUses
	}
	if expiresDays < 1 {
		return nil, ErrInvalidExpiry
	}

	code, err := generateInviteCode()
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().AddDate(0, 0, expiresDays)

	invite := &model.InviteCode{
		ID:        uuid.New(),
		Code:      code,
		CreatedBy: creatorID,
		MaxUses:   maxUses,
		ExpiresAt: &expiresAt,
		CreatedAt: time.Now(),
	}

	err = s.inviteStore.Create(ctx, invite)
	if err != nil {
		return nil, err
	}
	return invite, nil
}

func (s *adminService) UpdateInvite(ctx context.Context, inviteID string, fields InviteUpdateFields) (*model.InviteCode, error) {
	id, err := uuid.Parse(inviteID)
	if err != nil {
		return nil, err
	}

	invite, err := s.inviteStore.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if invite == nil {
		return nil, ErrInviteNotFound
	}

	if fields.MaxUses != nil {
		if *fields.MaxUses < 1 || *fields.MaxUses < invite.UseCount {
			return nil, ErrInvalidMaxUses
		}
	}

	err = s.inviteStore.Update(ctx, id, store.InviteUpdateFields{
		MaxUses:        fields.MaxUses,
		ExpiresAt:      fields.ExpiresAt,
		ClearExpiresAt: fields.ClearExpiresAt,
	})
	if err != nil {
		return nil, err
	}

	return s.inviteStore.FindByID(ctx, id)
}

func (s *adminService) DeleteInvite(ctx context.Context, inviteID string) error {
	id, err := uuid.Parse(inviteID)
	if err != nil {
		return err
	}

	invite, err := s.inviteStore.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if invite == nil {
		return ErrInviteNotFound
	}
	if invite.UseCount > 0 || invite.UsedBy != nil {
		return ErrInviteUsed
	}

	return s.inviteStore.Delete(ctx, id)
}

func (s *adminService) VerifyAdmin(ctx context.Context, userID string) (*model.User, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}
	user, err := s.userStore.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrUserNotFound
	}
	if user.Role != "admin" {
		return nil, ErrNotAdmin
	}
	return user, nil
}

func (s *adminService) GetPeer(ctx context.Context, peerID string) (*model.Peer, error) {
	id, err := uuid.Parse(peerID)
	if err != nil {
		return nil, err
	}
	peer, err := s.peerStore.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if peer == nil {
		return nil, ErrPeerNotFound
	}
	return peer, nil
}

const inviteCodeCharset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateInviteCode() (string, error) {
	part := func() (string, error) {
		b := make([]byte, 4)
		for i := range b {
			idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(inviteCodeCharset))))
			if err != nil {
				return "", err
			}
			b[i] = inviteCodeCharset[idx.Int64()]
		}
		return string(b), nil
	}
	p1, err := part()
	if err != nil {
		return "", err
	}
	p2, err := part()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SCNET-%s-%s", p1, p2), nil
}
