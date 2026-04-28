package account

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"regexp"
	"strings"

	authsvc "github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/model"
	"github.com/shuosc/scnet-server/internal/store"
)

var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrPasswordTooWeak    = errors.New("password must be at least 10 characters, max 128, with at least 1 letter and 1 digit")
	ErrPasswordReuse      = errors.New("new password must differ from current password")
	ErrInvalidEmail       = errors.New("invalid email format")
	ErrInvalidPhone       = errors.New("invalid phone format")
	ErrInvalidWechat      = errors.New("invalid wechat format: no whitespace allowed")
	ErrInvalidTelegram    = errors.New("invalid telegram format")
	ErrDisplayNameTooLong = errors.New("display name must be at most 64 characters")
)

type userStore interface {
	FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	Update(ctx context.Context, userID uuid.UUID, fields store.UserUpdateFields) error
	UpdatePassword(ctx context.Context, userID uuid.UUID, passwordHash string) error
}

type peerStore interface {
	CountActiveByUserID(ctx context.Context, userID uuid.UUID) (int, error)
	ListByUserID(ctx context.Context, userID uuid.UUID, params store.PeerListByUserParams) (*store.PeerListResult, error)
}

type AccountService interface {
	GetMe(ctx context.Context, userID string) (*SelfProfile, error)
	UpdateMe(ctx context.Context, userID string, fields SelfUpdateFields) (*model.User, error)
	ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error
	ListMyPeers(ctx context.Context, userID string, params SelfPeerListParams) (*store.PeerListResult, error)
}

type SelfProfile struct {
	User        *model.User
	ActivePeers int
	MaxPeers    int
}

type SelfUpdateFields struct {
	DisplayName *string
	Email       *string
	Phone       *string
	Wechat      *string
	Telegram    *string
}

type SelfPeerListParams struct {
	Page     int
	PageSize int
	Status   string
}

type accountService struct {
	userStore userStore
	peerStore peerStore
}

func NewAccountService(userStore userStore, peerStore peerStore) AccountService {
	return &accountService{
		userStore: userStore,
		peerStore: peerStore,
	}
}

func (s *accountService) GetMe(ctx context.Context, userID string) (*SelfProfile, error) {
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
	activePeers, err := s.peerStore.CountActiveByUserID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &SelfProfile{
		User:        user,
		ActivePeers: activePeers,
		MaxPeers:    user.MaxPeers,
	}, nil
}

func (s *accountService) UpdateMe(ctx context.Context, userID string, fields SelfUpdateFields) (*model.User, error) {
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
	if !user.IsActive() {
		return nil, authsvc.ErrAccountInactive
	}

	updateFields := store.UserUpdateFields{}

	if fields.DisplayName != nil {
		trimmed := strings.TrimSpace(*fields.DisplayName)
		if trimmed == "" {
			updateFields.DisplayName = nullableUpdate(nil)
		} else {
			if len(trimmed) > 64 {
				return nil, ErrDisplayNameTooLong
			}
			updateFields.DisplayName = nullableUpdate(&trimmed)
		}
	}

	if fields.Email != nil {
		trimmed := strings.TrimSpace(*fields.Email)
		if trimmed == "" {
			updateFields.Email = nullableUpdate(nil)
		} else {
			trimmed = strings.ToLower(trimmed)
			if len(trimmed) > 255 || !strings.Contains(trimmed, "@") {
				return nil, ErrInvalidEmail
			}
			updateFields.Email = nullableUpdate(&trimmed)
		}
	}

	if fields.Phone != nil {
		trimmed := strings.TrimSpace(*fields.Phone)
		if trimmed == "" {
			updateFields.Phone = nullableUpdate(nil)
		} else {
			if len(trimmed) > 32 {
				return nil, ErrInvalidPhone
			}
			matched, _ := regexp.MatchString(`^[\d+\-()\s]+$`, trimmed)
			if !matched {
				return nil, ErrInvalidPhone
			}
			updateFields.Phone = nullableUpdate(&trimmed)
		}
	}

	if fields.Wechat != nil {
		trimmed := strings.TrimSpace(*fields.Wechat)
		if trimmed == "" {
			updateFields.Wechat = nullableUpdate(nil)
		} else {
			if len(trimmed) > 64 {
				return nil, ErrInvalidWechat
			}
			matched, _ := regexp.MatchString(`\s`, trimmed)
			if matched {
				return nil, ErrInvalidWechat
			}
			updateFields.Wechat = nullableUpdate(&trimmed)
		}
	}

	if fields.Telegram != nil {
		trimmed := strings.TrimSpace(*fields.Telegram)
		if trimmed == "" {
			updateFields.Telegram = nullableUpdate(nil)
		} else {
			if len(trimmed) > 64 {
				return nil, ErrInvalidTelegram
			}
			updateFields.Telegram = nullableUpdate(&trimmed)
		}
	}

	err = s.userStore.Update(ctx, id, updateFields)
	if err != nil {
		return nil, err
	}

	user, err = s.userStore.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (s *accountService) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	id, err := uuid.Parse(userID)
	if err != nil {
		return err
	}
	user, err := s.userStore.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if user == nil {
		return ErrUserNotFound
	}
	if !user.IsActive() {
		return authsvc.ErrAccountInactive
	}

	if err := authsvc.CheckPassword(user.Password, currentPassword); err != nil {
		return ErrInvalidPassword
	}

	if strings.TrimSpace(currentPassword) == strings.TrimSpace(newPassword) {
		return ErrPasswordReuse
	}
	if err := authsvc.ValidatePassword(newPassword); err != nil {
		return ErrPasswordTooWeak
	}

	hash, err := authsvc.HashPassword(newPassword)
	if err != nil {
		return err
	}

	return s.userStore.UpdatePassword(ctx, id, hash)
}

func (s *accountService) ListMyPeers(ctx context.Context, userID string, params SelfPeerListParams) (*store.PeerListResult, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}
	return s.peerStore.ListByUserID(ctx, id, store.PeerListByUserParams{
		Page:     params.Page,
		PageSize: params.PageSize,
		Status:   params.Status,
	})
}

func nullableUpdate(value *string) store.OptionalStringUpdate {
	return store.OptionalStringUpdate{
		Set:   true,
		Value: value,
	}
}
