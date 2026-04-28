package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/model"
)

type authServiceStub struct {
	registerErr error
}

func (s authServiceStub) Register(context.Context, string, string, string) (*model.User, error) {
	return nil, s.registerErr
}

func (s authServiceStub) Login(context.Context, string, string) (string, time.Time, error) {
	return "", time.Time{}, nil
}

func (s authServiceStub) ValidateToken(string) (*auth.Claims, error) {
	return nil, nil
}

func TestRegisterReturnsInvalidPassword(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	handler := &AuthHandler{authService: authServiceStub{registerErr: auth.ErrPasswordTooShort}}
	router := gin.New()
	router.POST("/auth/register", handler.Register)

	req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"student_id":"2024001","password":"short","invite_code":"SCNET-TEST-0001"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if payload["code"] != "INVALID_PASSWORD" {
		t.Fatalf("expected INVALID_PASSWORD, got %v", payload["code"])
	}
}
