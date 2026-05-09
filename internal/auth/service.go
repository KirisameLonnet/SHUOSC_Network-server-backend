package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/shuosc/scnet-server/internal/model"
	"github.com/shuosc/scnet-server/internal/store"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrStudentIDTaken     = errors.New("student ID already taken")
	ErrInvalidInvite      = errors.New("invalid or expired invite code")
	ErrTokenExpired       = errors.New("token has expired")
	ErrAccountInactive    = errors.New("account is not active")
)

// Claims represents the JWT claims for a user.
type Claims struct {
	jwt.RegisteredClaims
	StudentID string `json:"student_id"`
	Role      string `json:"role"`
}

// userStore defines the minimal interface for user persistence.
type userStore interface {
	Create(ctx context.Context, user *model.User) error
	RegisterBootstrapAdmin(ctx context.Context, user *model.User) error
	RegisterWithInvite(ctx context.Context, user *model.User, inviteCode string) error
	FindByStudentID(ctx context.Context, studentID string) (*model.User, error)
	Count(ctx context.Context) (int, error)
}

// inviteStore defines the minimal interface for invite code persistence.
type inviteStore interface {
	FindByCode(ctx context.Context, code string) (*model.InviteCode, error)
	MarkUsed(ctx context.Context, inviteID uuid.UUID, usedBy uuid.UUID) error
}

// AuthService defines the authentication operations.
type AuthService interface {
	Register(ctx context.Context, studentID, password, inviteCode string) (*model.User, error)
	Login(ctx context.Context, studentID, password string) (token string, expiresAt time.Time, err error)
	ValidateToken(tokenString string) (*Claims, error)
}

// AuthServiceImpl implements AuthService.
type AuthServiceImpl struct {
	userStore   userStore
	inviteStore inviteStore
	jwtSecret   []byte
	expiryHours int
}

// NewAuthService creates a new AuthServiceImpl.
func NewAuthService(us userStore, is inviteStore, jwtSecret string, expiryHours int) AuthService {
	return &AuthServiceImpl{
		userStore:   us,
		inviteStore: is,
		jwtSecret:   []byte(jwtSecret),
		expiryHours: expiryHours,
	}
}

// Register creates a new user account using an invite code.
func (s *AuthServiceImpl) Register(ctx context.Context, studentID, password, inviteCode string) (*model.User, error) {
	studentID = strings.TrimSpace(studentID)
	inviteCode = strings.TrimSpace(inviteCode)

	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	existing, err := s.userStore.FindByStudentID(ctx, studentID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrStudentIDTaken
	}

	userCount, err := s.userStore.Count(ctx)
	if err != nil {
		return nil, err
	}

	hashedPassword, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	user := &model.User{
		ID:        uuid.New(),
		StudentID: studentID,
		Password:  hashedPassword,
		Role:      "user",
		MaxPeers:  5,
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if userCount == 0 {
		user.Role = "admin"
		if err := s.userStore.RegisterBootstrapAdmin(ctx, user); err != nil {
			switch {
			case errors.Is(err, store.ErrStudentIDTaken):
				return nil, ErrStudentIDTaken
			case errors.Is(err, store.ErrBootstrapClosed):
				return nil, ErrInvalidInvite
			}
			return nil, err
		}
		return user, nil
	}

	if inviteCode == "" {
		return nil, ErrInvalidInvite
	}
	invite, err := s.inviteStore.FindByCode(ctx, inviteCode)
	if err != nil {
		return nil, ErrInvalidInvite
	}
	if invite == nil || !invite.IsAvailable() {
		return nil, ErrInvalidInvite
	}

	if err := s.userStore.RegisterWithInvite(ctx, user, inviteCode); err != nil {
		switch {
		case errors.Is(err, store.ErrStudentIDTaken):
			return nil, ErrStudentIDTaken
		case errors.Is(err, store.ErrInviteUnavailable):
			return nil, ErrInvalidInvite
		}
		return nil, err
	}

	return user, nil
}

// Login authenticates a user and returns a JWT.
func (s *AuthServiceImpl) Login(ctx context.Context, studentID, password string) (string, time.Time, error) {
	user, err := s.userStore.FindByStudentID(ctx, studentID)
	if err != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}
	if user == nil {
		return "", time.Time{}, ErrInvalidCredentials
	}

	if user.Status != "active" {
		return "", time.Time{}, ErrAccountInactive
	}

	if err := CheckPassword(user.Password, password); err != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(s.expiryHours) * time.Hour)

	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		StudentID: user.StudentID,
		Role:      user.Role,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return "", time.Time{}, err
	}

	return tokenString, expiresAt, nil
}

// ValidateToken parses and validates a JWT, returning its claims.
func (s *AuthServiceImpl) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return s.jwtSecret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidCredentials
	}

	return claims, nil
}
