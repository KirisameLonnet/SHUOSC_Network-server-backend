package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shuosc/scnet-server/internal/admin"
	"github.com/shuosc/scnet-server/internal/peer"
	"github.com/shuosc/scnet-server/internal/store"
)

type AdminHandler struct {
	adminService            admin.AdminService
	peerManager             peer.PeerManager
	defaultInviteMaxUses    int
	defaultInviteExpiryDays int
}

func (h *AdminHandler) AdminMe(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	user, err := h.adminService.VerifyAdmin(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, admin.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found", "code": "USER_NOT_FOUND"})
			return
		}
		if errors.Is(err, admin.ErrNotAdmin) {
			c.JSON(http.StatusForbidden, gin.H{"error": "admin access required", "code": "FORBIDDEN"})
			return
		}
		slog.Error("admin me failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"admin": gin.H{
			"id":         user.ID.String(),
			"student_id": user.StudentID,
			"role":       user.Role,
			"status":     user.Status,
		},
	})
}

func (h *AdminHandler) AdminSummary(c *gin.Context) {
	summary, err := h.adminService.GetSummary(c.Request.Context())
	if err != nil {
		slog.Error("admin summary failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"summary": summary,
	})
}

func (h *AdminHandler) AdminListUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	studentID := c.Query("student_id")
	status := c.Query("status")
	role := c.Query("role")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	result, err := h.adminService.ListUsers(c.Request.Context(), store.UserListParams{
		Page:      page,
		PageSize:  pageSize,
		StudentID: studentID,
		Status:    status,
		Role:      role,
		SortBy:    sortBy,
		SortOrder: sortOrder,
	})
	if err != nil {
		slog.Error("admin list users failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"items":     result.Items,
		"total":     result.Total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *AdminHandler) AdminGetUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user id is required", "code": "INVALID_REQUEST"})
		return
	}

	user, err := h.adminService.GetUser(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, admin.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found", "code": "USER_NOT_FOUND"})
			return
		}
		slog.Error("admin get user failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"user":   user,
	})
}

type adminUpdateUserRequest struct {
	MaxPeers *int    `json:"max_peers"`
	Status   *string `json:"status"`
}

func (h *AdminHandler) AdminUpdateUser(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user id is required", "code": "INVALID_REQUEST"})
		return
	}

	var req adminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "INVALID_REQUEST"})
		return
	}

	user, err := h.adminService.UpdateUser(c.Request.Context(), userID, admin.AdminUserUpdateFields{
		MaxPeers: req.MaxPeers,
		Status:   req.Status,
	})
	if err != nil {
		switch {
		case errors.Is(err, admin.ErrUserNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found", "code": "USER_NOT_FOUND"})
		case errors.Is(err, admin.ErrInvalidStatus):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status: must be 'active' or 'suspended'", "code": "INVALID_STATUS"})
		case errors.Is(err, admin.ErrInvalidMaxPeers):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid max_peers: must be >= 0", "code": "INVALID_MAX_PEERS"})
		default:
			slog.Error("admin update user failed", "error", err, "user_id", userID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"user":   user,
	})
}

func (h *AdminHandler) AdminListUserPeers(c *gin.Context) {
	userID := c.Param("id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user id is required", "code": "INVALID_REQUEST"})
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

	result, err := h.adminService.ListUserPeers(c.Request.Context(), userID, store.PeerListByUserParams{
		Page:     page,
		PageSize: pageSize,
		Status:   status,
	})
	if err != nil {
		slog.Error("admin list user peers failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"items":     result.Items,
		"total":     result.Total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *AdminHandler) AdminListPeers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	studentID := c.Query("student_id")
	publicKey := c.Query("public_key")
	sortBy := c.DefaultQuery("sort_by", "created_at")
	sortOrder := c.DefaultQuery("sort_order", "desc")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	result, err := h.adminService.ListPeers(c.Request.Context(), store.PeerListParams{
		Page:      page,
		PageSize:  pageSize,
		Status:    status,
		StudentID: studentID,
		PublicKey: publicKey,
		SortBy:    sortBy,
		SortOrder: sortOrder,
	})
	if err != nil {
		slog.Error("admin list peers failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"items":     result.Items,
		"total":     result.Total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (h *AdminHandler) AdminDisconnectPeer(c *gin.Context) {
	peerID := c.Param("id")
	if peerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "peer id is required", "code": "INVALID_REQUEST"})
		return
	}

	peerInfo, err := h.adminService.GetPeer(c.Request.Context(), peerID)
	if err != nil {
		if errors.Is(err, admin.ErrPeerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "peer not found", "code": "PEER_NOT_FOUND"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	if peerInfo != nil && peerInfo.Status == "active" && peerInfo.PublicKey != "" {
		if wgErr := h.peerManager.RemovePeer(c.Request.Context(), peerInfo.UserID.String(), peerInfo.PublicKey); wgErr != nil && !errors.Is(wgErr, peer.ErrNoActivePeer) {
			slog.Warn("admin disconnect: wg remove failed", "error", wgErr, "peer_id", peerID)
		}
	}

	if err := h.adminService.DisconnectPeer(c.Request.Context(), peerID); err != nil {
		if errors.Is(err, admin.ErrPeerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "peer not found", "code": "PEER_NOT_FOUND"})
			return
		}
		slog.Error("admin disconnect peer failed", "error", err, "peer_id", peerID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *AdminHandler) AdminRevokePeer(c *gin.Context) {
	peerID := c.Param("id")
	if peerID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "peer id is required", "code": "INVALID_REQUEST"})
		return
	}

	peerInfo, err := h.adminService.GetPeer(c.Request.Context(), peerID)
	if err != nil {
		if errors.Is(err, admin.ErrPeerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "peer not found", "code": "PEER_NOT_FOUND"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	if peerInfo != nil && peerInfo.Status == "active" && peerInfo.PublicKey != "" {
		if wgErr := h.peerManager.RemovePeer(c.Request.Context(), peerInfo.UserID.String(), peerInfo.PublicKey); wgErr != nil && !errors.Is(wgErr, peer.ErrNoActivePeer) {
			slog.Warn("admin revoke: wg remove failed", "error", wgErr, "peer_id", peerID)
		}
	}

	if err := h.adminService.RevokePeer(c.Request.Context(), peerID); err != nil {
		if errors.Is(err, admin.ErrPeerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "peer not found", "code": "PEER_NOT_FOUND"})
			return
		}
		slog.Error("admin revoke peer failed", "error", err, "peer_id", peerID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *AdminHandler) AdminListInvites(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	code := c.Query("code")
	state := c.Query("state")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	result, err := h.adminService.ListInvites(c.Request.Context(), store.InviteListParams{
		Page:     page,
		PageSize: pageSize,
		Code:     code,
		State:    state,
	})
	if err != nil {
		slog.Error("admin list invites failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "ok",
		"items":     result.Items,
		"total":     result.Total,
		"page":      page,
		"page_size": pageSize,
	})
}

type createInviteRequest struct {
	MaxUses     int `json:"max_uses"`
	ExpiresDays int `json:"expires_days"`
}

type updateInviteRequest struct {
	MaxUses     *int `json:"max_uses"`
	ExpiresDays *int `json:"expires_days"`
}

func (h *AdminHandler) AdminCreateInvite(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	var req createInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "INVALID_REQUEST"})
		return
	}

	if req.MaxUses <= 0 {
		req.MaxUses = h.defaultInviteMaxUses
	}
	if req.ExpiresDays <= 0 {
		req.ExpiresDays = h.defaultInviteExpiryDays
	}

	invite, err := h.adminService.CreateInvite(c.Request.Context(), userID, req.MaxUses, req.ExpiresDays)
	if err != nil {
		switch {
		case errors.Is(err, admin.ErrInvalidMaxUses):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid max_uses", "code": "INVALID_REQUEST"})
		case errors.Is(err, admin.ErrInvalidExpiry):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid expires_days", "code": "INVALID_REQUEST"})
		default:
			slog.Error("admin create invite failed", "error", err, "created_by", userID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"invite": invite,
	})
}

func (h *AdminHandler) AdminUpdateInvite(c *gin.Context) {
	inviteID := c.Param("id")
	if inviteID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invite id is required", "code": "INVALID_REQUEST"})
		return
	}

	var req updateInviteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "INVALID_REQUEST"})
		return
	}

	fields := admin.InviteUpdateFields{
		MaxUses: req.MaxUses,
	}
	if req.ExpiresDays != nil {
		if *req.ExpiresDays < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "expires_days must be >= 0", "code": "INVALID_REQUEST"})
			return
		}
		if *req.ExpiresDays == 0 {
			fields.ClearExpiresAt = true
		} else {
			expiresAt := time.Now().AddDate(0, 0, *req.ExpiresDays)
			fields.ExpiresAt = &expiresAt
		}
	}

	invite, err := h.adminService.UpdateInvite(c.Request.Context(), inviteID, fields)
	if err != nil {
		switch {
		case errors.Is(err, admin.ErrInviteNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "invite not found", "code": "INVITE_NOT_FOUND"})
		case errors.Is(err, admin.ErrInvalidMaxUses):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid max_uses", "code": "INVALID_REQUEST"})
		default:
			slog.Error("admin update invite failed", "error", err, "invite_id", inviteID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"invite": invite,
	})
}

func (h *AdminHandler) AdminDeleteInvite(c *gin.Context) {
	inviteID := c.Param("id")
	if inviteID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invite id is required", "code": "INVALID_REQUEST"})
		return
	}

	if err := h.adminService.DeleteInvite(c.Request.Context(), inviteID); err != nil {
		switch {
		case errors.Is(err, admin.ErrInviteNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "invite not found", "code": "INVITE_NOT_FOUND"})
		case errors.Is(err, admin.ErrInviteUsed):
			c.JSON(http.StatusConflict, gin.H{"error": "invite has already been used", "code": "INVITE_IN_USE"})
		default:
			slog.Error("admin delete invite failed", "error", err, "invite_id", inviteID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
