package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/shuosc/scnet-server/internal/account"
	"github.com/shuosc/scnet-server/internal/auth"
)

type MeHandler struct {
	accountService account.AccountService
}

type updateMeRequest struct {
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email"`
	Phone       *string `json:"phone"`
	Wechat      *string `json:"wechat"`
	Telegram    *string `json:"telegram"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

func (h *MeHandler) GetMe(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	profile, err := h.accountService.GetMe(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, account.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found", "code": "USER_NOT_FOUND"})
			return
		}
		slog.Error("get me failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"user":   profile.User,
		"peer_usage": gin.H{
			"active":    profile.ActivePeers,
			"max_peers": profile.MaxPeers,
		},
		"peer_limit": profile.MaxPeers,
	})
}

func (h *MeHandler) UpdateMe(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	var req updateMeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "INVALID_REQUEST"})
		return
	}

	fields := account.SelfUpdateFields{
		DisplayName: req.DisplayName,
		Email:       req.Email,
		Phone:       req.Phone,
		Wechat:      req.Wechat,
		Telegram:    req.Telegram,
	}

	user, err := h.accountService.UpdateMe(c.Request.Context(), userID, fields)
	if err != nil {
		switch {
		case errors.Is(err, account.ErrDisplayNameTooLong):
			c.JSON(http.StatusBadRequest, gin.H{"error": "display name too long", "code": "INVALID_CONTACT"})
		case errors.Is(err, account.ErrInvalidEmail):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email format", "code": "INVALID_CONTACT"})
		case errors.Is(err, account.ErrInvalidPhone):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid phone format", "code": "INVALID_CONTACT"})
		case errors.Is(err, account.ErrInvalidWechat):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid wechat format", "code": "INVALID_CONTACT"})
		case errors.Is(err, account.ErrInvalidTelegram):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid telegram format", "code": "INVALID_CONTACT"})
		case errors.Is(err, auth.ErrAccountInactive):
			c.JSON(http.StatusForbidden, gin.H{"error": "account is not active", "code": "FORBIDDEN"})
		default:
			slog.Error("update me failed", "error", err, "user_id", userID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"user":   user,
	})
}

func (h *MeHandler) ChangePassword(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "INVALID_REQUEST"})
		return
	}

	err := h.accountService.ChangePassword(c.Request.Context(), userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, account.ErrInvalidPassword):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect", "code": "AUTH_FAILED"})
		case errors.Is(err, account.ErrPasswordTooWeak):
			c.JSON(http.StatusBadRequest, gin.H{"error": "new password does not meet policy", "code": "INVALID_PASSWORD"})
		case errors.Is(err, account.ErrPasswordReuse):
			c.JSON(http.StatusBadRequest, gin.H{"error": "new password does not meet policy", "code": "INVALID_PASSWORD"})
		case errors.Is(err, auth.ErrAccountInactive):
			c.JSON(http.StatusForbidden, gin.H{"error": "account is not active", "code": "FORBIDDEN"})
		default:
			slog.Error("change password failed", "error", err, "user_id", userID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "password updated"})
}

func (h *MeHandler) ListMyPeers(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	result, err := h.accountService.ListMyPeers(c.Request.Context(), userID, account.SelfPeerListParams{
		Page:     page,
		PageSize: pageSize,
		Status:   status,
	})
	if err != nil {
		slog.Error("list my peers failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"items":     result.Items,
		"total":     result.Total,
		"page":      page,
		"page_size": pageSize,
		"pagination": gin.H{
			"page":      page,
			"page_size": pageSize,
			"total":     result.Total,
		},
	})
}
