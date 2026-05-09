package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shuosc/scnet-server/internal/model"
)

type authUserStoreStub struct {
	count          int
	existing       *model.User
	bootstrapUser  *model.User
	invitedUser    *model.User
	bootstrapErr   error
	registerErr    error
	bootstrapCalls int
	registerCalls  int
}

func (s *authUserStoreStub) Create(context.Context, *model.User) error {
	return nil
}

func (s *authUserStoreStub) RegisterBootstrapAdmin(_ context.Context, user *model.User) error {
	s.bootstrapCalls++
	if s.bootstrapErr != nil {
		return s.bootstrapErr
	}
	s.bootstrapUser = user
	return nil
}

func (s *authUserStoreStub) RegisterWithInvite(_ context.Context, user *model.User, _ string) error {
	s.registerCalls++
	if s.registerErr != nil {
		return s.registerErr
	}
	s.invitedUser = user
	return nil
}

func (s *authUserStoreStub) FindByStudentID(context.Context, string) (*model.User, error) {
	return s.existing, nil
}

func (s *authUserStoreStub) Count(context.Context) (int, error) {
	return s.count, nil
}

type authInviteStoreStub struct {
	invite *model.InviteCode
	err    error
	calls  int
}

func (s *authInviteStoreStub) FindByCode(context.Context, string) (*model.InviteCode, error) {
	s.calls++
	return s.invite, s.err
}

func (s *authInviteStoreStub) MarkUsed(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func TestRegisterBootstrapsFirstUserAsAdminWithoutInvite(t *testing.T) {
	userStore := &authUserStoreStub{count: 0}
	inviteStore := &authInviteStoreStub{}
	service := NewAuthService(userStore, inviteStore, "test-secret", 24)

	user, err := service.Register(context.Background(), " 2024001 ", "Password1234", "")
	if err != nil {
		t.Fatalf("register first user: %v", err)
	}

	if user.Role != "admin" {
		t.Fatalf("expected first user role admin, got %q", user.Role)
	}
	if user.StudentID != "2024001" {
		t.Fatalf("expected trimmed student id, got %q", user.StudentID)
	}
	if userStore.bootstrapCalls != 1 || userStore.registerCalls != 0 {
		t.Fatalf("expected bootstrap path only, bootstrap=%d register=%d", userStore.bootstrapCalls, userStore.registerCalls)
	}
	if inviteStore.calls != 0 {
		t.Fatalf("expected no invite lookup for first user, got %d calls", inviteStore.calls)
	}
}

func TestRegisterRequiresInviteAfterBootstrap(t *testing.T) {
	userStore := &authUserStoreStub{count: 1}
	inviteStore := &authInviteStoreStub{}
	service := NewAuthService(userStore, inviteStore, "test-secret", 24)

	_, err := service.Register(context.Background(), "2024002", "Password1234", "")
	if !errors.Is(err, ErrInvalidInvite) {
		t.Fatalf("expected ErrInvalidInvite, got %v", err)
	}
	if userStore.bootstrapCalls != 0 || userStore.registerCalls != 0 {
		t.Fatalf("expected no registration write, bootstrap=%d register=%d", userStore.bootstrapCalls, userStore.registerCalls)
	}
	if inviteStore.calls != 0 {
		t.Fatalf("expected empty invite to fail before lookup, got %d calls", inviteStore.calls)
	}
}
