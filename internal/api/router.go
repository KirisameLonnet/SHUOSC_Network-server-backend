package api

import (
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shuosc/scnet-server/internal/account"
	"github.com/shuosc/scnet-server/internal/admin"
	"github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/peer"
	"github.com/shuosc/scnet-server/internal/store"
)

func SetupRouter(
	authService auth.AuthService,
	accountService account.AccountService,
	peerManager peer.PeerManager,
	adminService admin.AdminService,
	userStore store.UserStore,
	dbPool *pgxpool.Pool,
	defaultInviteMaxUses int,
	defaultInviteExpiryDays int,
) *gin.Engine {
	r := gin.Default()

	r.Use(corsMiddleware())

	authHandler := &AuthHandler{authService: authService}
	meHandler := &MeHandler{accountService: accountService}
	peerHandler := &PeerHandler{peerManager: peerManager, authService: authService}
	adminHandler := &AdminHandler{
		adminService:            adminService,
		peerManager:             peerManager,
		defaultInviteMaxUses:    defaultInviteMaxUses,
		defaultInviteExpiryDays: defaultInviteExpiryDays,
	}
	healthHandler := &HealthHandler{peerManager: peerManager, dbPool: dbPool}

	r.GET("/health", healthHandler.Health)
	r.GET("/version", healthHandler.Version)

	v1 := r.Group("/api/v1")

	authGroup := v1.Group("/auth")
	authGroup.POST("/register", RegisterRateLimit(), authHandler.Register)
	authGroup.POST("/login", LoginRateLimit(), authHandler.Login)

	meGroup := v1.Group("/me")
	meGroup.Use(AuthMiddleware(authService, userStore))
	meGroup.GET("", meHandler.GetMe)
	meGroup.GET("/peers", meHandler.ListMyPeers)
	meGroup.PUT("", RequireActiveUser(), meHandler.UpdateMe)
	meGroup.PUT("/password", RequireActiveUser(), meHandler.ChangePassword)

	peerGroup := v1.Group("/peer")
	peerGroup.Use(AuthMiddleware(authService, userStore), PeerRateLimit())
	peerGroup.POST("/register", RequireActiveUser(), peerHandler.RegisterPeer)
	peerGroup.GET("/config", peerHandler.GetPeerConfig)
	peerGroup.DELETE("/disconnect", RequireActiveUser(), peerHandler.DisconnectPeer)
	peerGroup.PUT("/replace-key", RequireActiveUser(), peerHandler.ReplaceKey)

	adminGroup := v1.Group("/admin")
	adminGroup.Use(AuthMiddleware(authService, userStore), RequireAdmin())
	adminGroup.GET("/me", adminHandler.AdminMe)
	adminGroup.GET("/summary", adminHandler.AdminSummary)
	adminGroup.GET("/users", adminHandler.AdminListUsers)
	adminGroup.GET("/user/:id", adminHandler.AdminGetUser)
	adminGroup.PUT("/user/:id", adminHandler.AdminUpdateUser)
	adminGroup.GET("/user/:id/peers", adminHandler.AdminListUserPeers)
	adminGroup.GET("/peers", adminHandler.AdminListPeers)
	adminGroup.POST("/peer/:id/disconnect", adminHandler.AdminDisconnectPeer)
	adminGroup.POST("/peer/:id/revoke", adminHandler.AdminRevokePeer)
	adminGroup.GET("/invites", adminHandler.AdminListInvites)
	adminGroup.POST("/invite", adminHandler.AdminCreateInvite)
	adminGroup.PUT("/invite/:id", adminHandler.AdminUpdateInvite)
	adminGroup.DELETE("/invite/:id", adminHandler.AdminDeleteInvite)

	// WireGuard server management
	adminGroup.GET("/wg", adminHandler.AdminGetWGStatus)
	adminGroup.POST("/wg/rotate-key", adminHandler.AdminRotateWGKey)
	adminGroup.POST("/wg/toggle", adminHandler.AdminToggleWG)

	spaDir := os.Getenv("SCNET_SPA_DIR")
	if spaDir == "" {
		spaDir = "admin-panel/dist/spa"
	}
	var spaContent fs.FS
	if info, err := os.Stat(spaDir); err == nil && info.IsDir() {
		spaContent = os.DirFS(spaDir)
	}
	spaHandler := spaFileServer(spaContent)

	appGroup := r.Group("/app")
	appGroup.Use(spaHandler)
	appGroup.GET("/*path", spaIndexFallback(spaContent))

	adminSPAGroup := r.Group("/admin")
	adminSPAGroup.Use(spaHandler)
	adminSPAGroup.GET("/*path", spaIndexFallback(spaContent))

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	return r
}

func corsMiddleware() gin.HandlerFunc {
	origin := os.Getenv("SCNET_CORS_ORIGIN")
	if origin == "" {
		origin = "*"
	}

	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func spaFileServer(spaFS fs.FS) gin.HandlerFunc {
	fileServer := http.FileServer(http.FS(spaFS))

	return func(c *gin.Context) {
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
			c.Next()
			return
		}

		path := c.Request.URL.Path
		path = strings.TrimPrefix(path, "/app")
		path = strings.TrimPrefix(path, "/admin")
		if path == "" {
			path = "/"
		}

		if spaFS == nil {
			c.Next()
			return
		}

		f, err := spaFS.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			f.Close()
			c.Request.URL.Path = path
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}

		c.Next()
	}
}

func spaIndexFallback(spaFS fs.FS) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Writer.Written() {
			return
		}

		if spaFS == nil {
			c.String(http.StatusServiceUnavailable, "SPA not available")
			return
		}

		data, err := fs.ReadFile(spaFS, "index.html")
		if err != nil {
			c.String(http.StatusNotFound, "SPA index not found")
			return
		}

		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, string(data))
		c.Abort()
	}
}
