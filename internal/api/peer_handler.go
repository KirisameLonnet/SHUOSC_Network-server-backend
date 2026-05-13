package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/peer"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type PeerHandler struct {
	peerManager peer.PeerManager
	authService auth.AuthService
}

type registerPeerRequest struct {
	PublicKey string `json:"public_key" binding:"required"`
}

type replaceKeyRequest struct {
	PublicKey string `json:"public_key" binding:"required"`
}

func (h *PeerHandler) RegisterPeer(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	var req registerPeerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "INVALID_REQUEST"})
		return
	}

	pubKey, err := wgtypes.ParseKey(req.PublicKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid public key", "code": "INVALID_KEY"})
		return
	}

	registration, err := h.peerManager.AddPeer(c.Request.Context(), userID, pubKey)
	if err != nil {
		switch {
		case errors.Is(err, peer.ErrTooManyPeers):
			c.JSON(http.StatusConflict, gin.H{"error": "peer limit reached (max peers for this account)", "code": "TOO_MANY_PEERS"})
		case errors.Is(err, peer.ErrPeerExists):
			c.JSON(http.StatusConflict, gin.H{"error": "user already has an active peer with this key", "code": "PEER_EXISTS"})
		case errors.Is(err, peer.ErrPeerRevoked):
			c.JSON(http.StatusConflict, gin.H{"error": "peer key was revoked and cannot be reused", "code": "PEER_REVOKED"})
		case errors.Is(err, peer.ErrAccountInactive):
			c.JSON(http.StatusForbidden, gin.H{"error": "account is not active", "code": "FORBIDDEN"})
		default:
			slog.Error("register peer failed", "error", err, "user_id", userID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"peer_config": registration,
	})
}

func (h *PeerHandler) GetPeerConfig(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	publicKey := c.Query("public_key")
	if publicKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "public_key query parameter is required", "code": "INVALID_REQUEST"})
		return
	}

	registration, err := h.peerManager.GetPeerConfig(c.Request.Context(), userID, publicKey)
	if err != nil {
		if errors.Is(err, peer.ErrNoActivePeer) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no active peer found for this user", "code": "NO_ACTIVE_PEER"})
			return
		}
		slog.Error("get peer config failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"peer_config": registration,
	})
}

func (h *PeerHandler) DisconnectPeer(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	publicKey := c.Query("public_key")
	if publicKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "public_key query parameter is required", "code": "INVALID_REQUEST"})
		return
	}

	err := h.peerManager.RemovePeer(c.Request.Context(), userID, publicKey)
	if err != nil {
		if errors.Is(err, peer.ErrNoActivePeer) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no active peer found", "code": "NO_ACTIVE_PEER"})
			return
		}
		slog.Error("disconnect peer failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "peer removed from server"})
}

func (h *PeerHandler) ReplaceKey(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized", "code": "AUTH_FAILED"})
		return
	}

	oldPublicKey := c.Query("public_key")
	if oldPublicKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "public_key query parameter is required", "code": "INVALID_REQUEST"})
		return
	}

	var req replaceKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body", "code": "INVALID_REQUEST"})
		return
	}

	newPubKey, err := wgtypes.ParseKey(req.PublicKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid new public key", "code": "INVALID_KEY"})
		return
	}

	oldKey, err := wgtypes.ParseKey(oldPublicKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid old public key", "code": "INVALID_KEY"})
		return
	}

	err = h.peerManager.ReplaceKey(c.Request.Context(), userID, oldKey, newPubKey)
	if err != nil {
		if errors.Is(err, peer.ErrNoActivePeer) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no active peer found", "code": "NO_ACTIVE_PEER"})
			return
		}
		slog.Error("replace peer key failed", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error", "code": "INTERNAL_ERROR"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "peer public key updated"})
}
