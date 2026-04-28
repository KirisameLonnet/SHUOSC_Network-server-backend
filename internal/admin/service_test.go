package admin

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shuosc/scnet-server/internal/model"
	"github.com/shuosc/scnet-server/internal/store"
)

type stubInviteStore struct {
	invite      *model.InviteCode
	updated     store.InviteUpdateFields
	deleteCalls int
}

func (s *stubInviteStore) Create(_ context.Context, invite *model.InviteCode) error {
	s.invite = invite
	return nil
}

func (s *stubInviteStore) FindByID(_ context.Context, id uuid.UUID) (*model.InviteCode, error) {
	if s.invite == nil || s.invite.ID != id {
		return nil, nil
	}
	return s.invite, nil
}

func (s *stubInviteStore) Update(_ context.Context, inviteID uuid.UUID, fields store.InviteUpdateFields) error {
	s.updated = fields
	if s.invite != nil && s.invite.ID == inviteID {
		if fields.MaxUses != nil {
			s.invite.MaxUses = *fields.MaxUses
		}
		if fields.ClearExpiresAt {
			s.invite.ExpiresAt = nil
		} else if fields.ExpiresAt != nil {
			s.invite.ExpiresAt = fields.ExpiresAt
		}
	}
	return nil
}

func (s *stubInviteStore) Delete(_ context.Context, inviteID uuid.UUID) error {
	if s.invite != nil && s.invite.ID == inviteID {
		s.deleteCalls++
		s.invite = nil
	}
	return nil
}

func (s *stubInviteStore) List(_ context.Context, _ store.InviteListParams) (*store.InviteListResult, error) {
	return &store.InviteListResult{}, nil
}

func (s *stubInviteStore) CountByState(_ context.Context) (map[string]int, error) {
	return map[string]int{"available": 0, "used_up": 0, "expired": 0}, nil
}

type noopUserStore struct{}

func (noopUserStore) FindByID(context.Context, uuid.UUID) (*model.User, error) { return nil, nil }
func (noopUserStore) Update(context.Context, uuid.UUID, store.UserUpdateFields) error {
	return nil
}
func (noopUserStore) List(context.Context, store.UserListParams) (*store.UserListResult, error) {
	return &store.UserListResult{}, nil
}
func (noopUserStore) CountByStatus(context.Context) (map[string]int, error) {
	return map[string]int{"active": 0, "suspended": 0, "deleted": 0}, nil
}

type noopPeerStore struct{}

func (noopPeerStore) FindByID(context.Context, uuid.UUID) (*model.Peer, error) { return nil, nil }
func (noopPeerStore) UpdateStatus(context.Context, uuid.UUID, string) error    { return nil }
func (noopPeerStore) List(context.Context, store.PeerListParams) (*store.PeerListResult, error) {
	return &store.PeerListResult{}, nil
}
func (noopPeerStore) CountByStatus(context.Context) (map[string]int, error) {
	return map[string]int{"active": 0, "disconnected": 0, "revoked": 0}, nil
}
func (noopPeerStore) ListByUserID(context.Context, uuid.UUID, store.PeerListByUserParams) (*store.PeerListResult, error) {
	return &store.PeerListResult{}, nil
}

func TestUpdateInviteRejectsMaxUsesBelowUseCount(t *testing.T) {
	t.Parallel()

	inviteID := uuid.New()
	inviteStore := &stubInviteStore{
		invite: &model.InviteCode{
			ID:       inviteID,
			MaxUses:  3,
			UseCount: 2,
		},
	}
	svc := NewAdminService(noopUserStore{}, noopPeerStore{}, inviteStore)

	maxUses := 1
	_, err := svc.UpdateInvite(context.Background(), inviteID.String(), InviteUpdateFields{
		MaxUses: &maxUses,
	})
	if err != ErrInvalidMaxUses {
		t.Fatalf("expected ErrInvalidMaxUses, got %v", err)
	}
}

func TestUpdateInviteClearsExpiry(t *testing.T) {
	t.Parallel()

	expiresAt := time.Now().Add(24 * time.Hour)
	inviteID := uuid.New()
	inviteStore := &stubInviteStore{
		invite: &model.InviteCode{
			ID:        inviteID,
			MaxUses:   3,
			UseCount:  0,
			ExpiresAt: &expiresAt,
		},
	}
	svc := NewAdminService(noopUserStore{}, noopPeerStore{}, inviteStore)

	invite, err := svc.UpdateInvite(context.Background(), inviteID.String(), InviteUpdateFields{
		ClearExpiresAt: true,
	})
	if err != nil {
		t.Fatalf("UpdateInvite returned error: %v", err)
	}
	if !inviteStore.updated.ClearExpiresAt {
		t.Fatal("expected ClearExpiresAt to be propagated to store")
	}
	if invite.ExpiresAt != nil {
		t.Fatalf("expected expiry to be cleared, got %v", invite.ExpiresAt)
	}
}

func TestDeleteInviteRejectsUsedInvite(t *testing.T) {
	t.Parallel()

	usedBy := uuid.New()
	inviteID := uuid.New()
	inviteStore := &stubInviteStore{
		invite: &model.InviteCode{
			ID:       inviteID,
			UseCount: 1,
			UsedBy:   &usedBy,
		},
	}
	svc := NewAdminService(noopUserStore{}, noopPeerStore{}, inviteStore)

	err := svc.DeleteInvite(context.Background(), inviteID.String())
	if err != ErrInviteUsed {
		t.Fatalf("expected ErrInviteUsed, got %v", err)
	}
	if inviteStore.deleteCalls != 0 {
		t.Fatalf("expected no delete call, got %d", inviteStore.deleteCalls)
	}
}
