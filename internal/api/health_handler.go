package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shuosc/scnet-server/internal/peer"
)

type HealthHandler struct {
	peerManager peer.PeerManager
	dbPool      *pgxpool.Pool
}

func (h *HealthHandler) Health(c *gin.Context) {
	dbStatus := "disconnected"
	if h.dbPool != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := h.dbPool.Ping(ctx); err != nil {
			slog.Warn("health check db ping failed", "error", err)
			dbStatus = "disconnected"
		} else {
			dbStatus = "connected"
		}
	}

	wgStatus := "inactive"
	peersCount := 0
	if h.peerManager != nil {
		peersCount = h.peerManager.ActivePeerCount(c.Request.Context())
		if peersCount >= 0 {
			wgStatus = "active"
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"db":     dbStatus,
		"wg":     wgStatus,
		"peers":  peersCount,
	})
}

func (h *HealthHandler) Version(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version": "0.1.0-dev",
		"commit":  "dev",
	})
}
