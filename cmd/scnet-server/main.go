package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/shuosc/scnet-server/config"
	"github.com/shuosc/scnet-server/internal/account"
	"github.com/shuosc/scnet-server/internal/admin"
	"github.com/shuosc/scnet-server/internal/api"
	"github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/discovery"
	"github.com/shuosc/scnet-server/internal/model"
	"github.com/shuosc/scnet-server/internal/peer"
	"github.com/shuosc/scnet-server/internal/store"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	slog.Info("config loaded",
		"server_port", cfg.Server.Port,
		"db_host", cfg.Database.Host,
		"wg_interface", cfg.WireGuard.Interface,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := store.NewPool(cfg.Database)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("database connected")

	if err := store.RunMigrations(pool, "migrations"); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations complete")

	wgClient, err := wgctrl.New()
	if err != nil {
		slog.Error("failed to create wireguard client", "error", err)
		os.Exit(1)
	}
	defer wgClient.Close()

	serverKey, err := wgtypes.ParseKey(cfg.WireGuard.PrivateKey)
	if err != nil {
		slog.Error("failed to parse wireguard private key", "error", err)
		os.Exit(1)
	}

	userStore := store.NewUserStore(pool)
	peerStore := store.NewPeerStore(pool)
	inviteStore := store.NewInviteStore(pool)

	if err := bootstrapAdmin(ctx, userStore, cfg.Admin); err != nil {
		slog.Error("failed to bootstrap admin user", "error", err)
		os.Exit(1)
	}

	authService := auth.NewAuthService(userStore, inviteStore, cfg.JWT.Secret, cfg.JWT.ExpiryHours)
	accountService := account.NewAccountService(userStore, peerStore)
	ipamInstance, err := peer.NewIPAM(cfg.WireGuard.Subnet)
	if err != nil {
		slog.Error("failed to create IPAM", "error", err)
		os.Exit(1)
	}

	endpoint := cfg.Discovery.WgEndpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("%s:%d", "localhost", cfg.WireGuard.ListenPort)
	}
	peerManager := peer.NewPeerManager(
		userStore,
		peerStore,
		ipamInstance,
		wgClient,
		cfg.WireGuard.Interface,
		serverKey,
		cfg.WireGuard.ListenPort,
		endpoint,
	)
	adminService := admin.NewAdminService(userStore, peerStore, inviteStore)

	router := api.SetupRouter(
		authService,
		accountService,
		peerManager,
		adminService,
		userStore,
		pool,
		cfg.Invite.DefaultMaxUses,
		cfg.Invite.DefaultExpiryDays,
	)

	go reconciliationLoop(ctx, peerManager, 5*time.Minute)

	reporterCfg := discovery.ReporterConfig{
		Enabled: cfg.Discovery.Enabled,
		URL:     cfg.Discovery.DiscoveryURL,
		Secret:  cfg.Discovery.DiscoverySecret,
	}
	reporter := discovery.NewReporter(reporterCfg, cfg.Discovery.ApiAddr, cfg.Discovery.WgEndpoint)
	go reporter.Start(ctx)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		slog.Info("shutdown signal received, stopping server...")
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
	}()

	slog.Info("starting server", "addr", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}

func bootstrapAdmin(ctx context.Context, userStore store.UserStore, cfg config.AdminConfig) error {
	count, err := userStore.Count(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.Password), 12)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	admin := &model.User{
		StudentID: cfg.StudentID,
		Password:  string(hash),
		Role:      "admin",
		MaxPeers:  1,
		Status:    "active",
	}
	if err := userStore.Create(ctx, admin); err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}

	slog.Info("admin user bootstrapped", "student_id", admin.StudentID)
	return nil
}

func reconciliationLoop(ctx context.Context, pm peer.PeerManager, interval time.Duration) {
	reconcile(ctx, pm)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile(ctx, pm)
		}
	}
}

func reconcile(ctx context.Context, pm peer.PeerManager) {
	if err := pm.Reconcile(ctx); err != nil {
		slog.Error("reconciliation failed", "error", err)
	}
}
