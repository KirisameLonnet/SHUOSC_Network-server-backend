package account

import (
	"context"
	"testing"

	"github.com/google/uuid"

	authsvc "github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/model"
	"github.com/shuosc/scnet-server/internal/store"
)

type accountUserStoreStub struct {
	user             *model.User
	lastUpdateFields store.UserUpdateFields
	lastPasswordHash string
}

func (s *accountUserStoreStub) FindByID(context.Context, uuid.UUID) (*model.User, error) {
	return s.user, nil
}

func (s *accountUserStoreStub) Update(_ context.Context, _ uuid.UUID, fields store.UserUpdateFields) error {
	s.lastUpdateFields = fields
	return nil
}

func (s *accountUserStoreStub) UpdatePassword(_ context.Context, _ uuid.UUID, passwordHash string) error {
	s.lastPasswordHash = passwordHash
	return nil
}

type accountPeerStoreStub struct{}

func (accountPeerStoreStub) CountActiveByUserID(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}

func (accountPeerStoreStub) ListByUserID(context.Context, uuid.UUID, store.PeerListByUserParams) (*store.PeerListResult, error) {
	return &store.PeerListResult{}, nil
}

func TestUpdateMeClearsOptionalFields(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	userStore := &accountUserStoreStub{
		user: &model.User{
			ID:       userID,
			Status:   "active",
			MaxPeers: 5,
		},
	}
	svc := NewAccountService(userStore, accountPeerStoreStub{})

	empty := "   "
	if _, err := svc.UpdateMe(context.Background(), userID.String(), SelfUpdateFields{
		Email: &empty,
	}); err != nil {
		t.Fatalf("UpdateMe returned error: %v", err)
	}

	if !userStore.lastUpdateFields.Email.Set {
		t.Fatal("expected email field to be marked as updated")
	}
	if userStore.lastUpdateFields.Email.Value != nil {
		t.Fatalf("expected email to be cleared to nil, got %v", *userStore.lastUpdateFields.Email.Value)
	}
}

func TestChangePasswordRejectsTrimmedReuse(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	hash, err := authsvc.HashPassword(" old-password-123 ")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	userStore := &accountUserStoreStub{
		user: &model.User{
			ID:       userID,
			Status:   "active",
			Password: hash,
		},
	}
	svc := NewAccountService(userStore, accountPeerStoreStub{})

	err = svc.ChangePassword(context.Background(), userID.String(), " old-password-123 ", "old-password-123")
	if err != ErrPasswordReuse {
		t.Fatalf("expected ErrPasswordReuse, got %v", err)
	}
	if userStore.lastPasswordHash != "" {
		t.Fatal("expected password hash not to be updated")
	}
}
