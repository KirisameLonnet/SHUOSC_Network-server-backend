package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shuosc/scnet-server/internal/auth"
)

type AuthHandler struct {
	authService auth.AuthService
}

type registerRequest struct {
	StudentID  string `json:"student_id" binding:"required"`
	Password   string `json:"password" binding:"required"`
	InviteCode string `json:"invite_code"`
}

type loginRequest struct {
	StudentID string `json:"student_id" binding:"required"`
	Password  string `json:"password" binding:"required"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
			"code":  "INVALID_REQUEST",
		})
		return
	}

	user, err := h.authService.Register(c.Request.Context(), req.StudentID, req.Password, req.InviteCode)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials", "code": "AUTH_FAILED"})
		case errors.Is(err, auth.ErrStudentIDTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "student_id already registered", "code": "ALREADY_EXISTS"})
		case errors.Is(err, auth.ErrInvalidInvite):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid or expired invite code", "code": "INVALID_INVITE"})
		case auth.IsPasswordPolicyError(err):
			c.JSON(http.StatusBadRequest, gin.H{"error": "new password does not meet policy", "code": "INVALID_PASSWORD"})
		case errors.Is(err, auth.ErrAccountInactive):
			c.JSON(http.StatusForbidden, gin.H{"error": "account is not active", "code": "FORBIDDEN"})
		default:
			slog.Error("register failed", "error", err, "student_id", req.StudentID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"user_id": user.ID.String(),
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
			"code":  "INVALID_REQUEST",
		})
		return
	}

	token, expiresAt, err := h.authService.Login(c.Request.Context(), req.StudentID, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid student_id or password", "code": "AUTH_FAILED"})
		case errors.Is(err, auth.ErrAccountInactive):
			c.JSON(http.StatusForbidden, gin.H{"error": "account is not active", "code": "FORBIDDEN"})
		default:
			slog.Error("login failed", "error", err, "student_id", req.StudentID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"token":      token,
		"expires_at": expiresAt,
	})
}
