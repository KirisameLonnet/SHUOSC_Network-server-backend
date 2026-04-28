package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/model"
)

type authMiddlewareAuthServiceStub struct {
	claims *auth.Claims
	err    error
}

func (s authMiddlewareAuthServiceStub) Register(context.Context, string, string, string) (*model.User, error) {
	return nil, nil
}

func (s authMiddlewareAuthServiceStub) Login(context.Context, string, string) (string, time.Time, error) {
	return "", time.Time{}, nil
}

func (s authMiddlewareAuthServiceStub) ValidateToken(string) (*auth.Claims, error) {
	return s.claims, s.err
}

type authMiddlewareUserStoreStub struct {
	user *model.User
	err  error
}

func (s authMiddlewareUserStoreStub) FindByID(context.Context, uuid.UUID) (*model.User, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.user, nil
}

func TestRequireActiveUserBlocksSuspendedAccount(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	userID := uuid.New()
	router := gin.New()
	router.PUT("/me",
		AuthMiddleware(
			authMiddlewareAuthServiceStub{
				claims: &auth.Claims{
					RegisteredClaims: jwt.RegisteredClaims{Subject: userID.String()},
					StudentID:        "2024001",
				},
			},
			authMiddlewareUserStoreStub{
				user: &model.User{
					ID:        userID,
					StudentID: "2024001",
					Role:      "user",
					Status:    "suspended",
				},
			},
		),
		RequireActiveUser(),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)

	req := httptest.NewRequest(http.MethodPut, "/me", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
}

func TestAuthMiddlewareRejectsMissingCurrentUser(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/me",
		AuthMiddleware(
			authMiddlewareAuthServiceStub{
				claims: &auth.Claims{
					RegisteredClaims: jwt.RegisteredClaims{Subject: uuid.New().String()},
					StudentID:        "2024001",
				},
			},
			authMiddlewareUserStoreStub{err: errors.New("db down")},
		),
		func(c *gin.Context) { c.Status(http.StatusNoContent) },
	)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", rec.Code)
	}
}
