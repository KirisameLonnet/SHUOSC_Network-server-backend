package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/shuosc/scnet-server/config"
	"github.com/shuosc/scnet-server/internal/account"
	"github.com/shuosc/scnet-server/internal/admin"
	"github.com/shuosc/scnet-server/internal/api"
	"github.com/shuosc/scnet-server/internal/auth"
	"github.com/shuosc/scnet-server/internal/discovery"
	"github.com/shuosc/scnet-server/internal/peer"
	"github.com/shuosc/scnet-server/internal/punch"
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

	authService := auth.NewAuthService(userStore, inviteStore, cfg.JWT.Secret, cfg.JWT.ExpiryHours)
	accountService := account.NewAccountService(userStore, peerStore)
	ipamInstance, err := peer.NewIPAM(cfg.WireGuard.Subnet)
	if err != nil {
		slog.Error("failed to create IPAM", "error", err)
		os.Exit(1)
	}
	wgAddress, wgHostIP, err := resolveWireGuardAddress(cfg.WireGuard.Subnet, cfg.WireGuard.Address)
	if err != nil {
		slog.Error("failed to resolve wireguard server address", "error", err)
		os.Exit(1)
	}
	if err := ipamInstance.Reserve(wgHostIP); err != nil {
		slog.Error("failed to reserve wireguard server address", "address", wgHostIP, "error", err)
		os.Exit(1)
	}

	endpoint, endpointSource, punchProxy, err := resolveWireGuardEndpoint(ctx, cfg)
	if err != nil {
		slog.Error("failed to resolve wireguard endpoint", "error", err)
		os.Exit(1)
	}
	if punchProxy != nil {
		defer punchProxy.Close()
	}
	if cfg.Discovery.Enabled && endpointSource == "fallback" {
		slog.Error("discovery enabled but no publishable WireGuard endpoint is configured")
		os.Exit(1)
	}
	slog.Info("wireguard endpoint resolved", "endpoint", endpoint, "source", endpointSource)

	var endpointValue func() string
	if punchProxy != nil {
		endpointValue = punchProxy.Endpoint
	} else {
		endpointValue = func() string { return endpoint }
	}
	peerManager := peer.NewPeerManagerWithEndpointFunc(
		userStore,
		peerStore,
		ipamInstance,
		wgClient,
		cfg.WireGuard.Interface,
		serverKey,
		cfg.WireGuard.ListenPort,
		endpointValue,
	)
	if err := ensureWireGuardReady(ctx, cfg.WireGuard.Interface, wgAddress, peerManager); err != nil {
		slog.Error("failed to initialize wireguard interface", "error", err, "interface", cfg.WireGuard.Interface)
		os.Exit(1)
	}
	slog.Info("wireguard interface ready", "interface", cfg.WireGuard.Interface, "address", wgAddress, "listen_port", cfg.WireGuard.ListenPort)

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
	reporter := discovery.NewReporterFunc(reporterCfg, cfg.Discovery.ApiAddr, endpointValue)
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

func resolveWireGuardEndpoint(ctx context.Context, cfg *config.Config) (string, string, *punch.Service, error) {
	if strings.TrimSpace(cfg.Discovery.WgEndpoint) != "" {
		return strings.TrimSpace(cfg.Discovery.WgEndpoint), "manual", nil, nil
	}

	if cfg.Punch.Enabled {
		wgPort := cfg.Punch.WireGuardPort
		if wgPort <= 0 {
			wgPort = cfg.WireGuard.ListenPort
		}
		probeTimeout := time.Duration(cfg.Punch.ProbeTimeoutSeconds) * time.Second
		keepalive := time.Duration(cfg.Punch.KeepaliveIntervalSeconds) * time.Second
		proxy, endpoint, err := punch.Start(ctx, punch.Config{
			ListenPort:        cfg.Punch.ListenPort,
			WireGuardHost:     cfg.Punch.WireGuardHost,
			WireGuardPort:     wgPort,
			STUNServers:       cfg.Punch.STUNServers,
			ProbeTimeout:      probeTimeout,
			KeepaliveInterval: keepalive,
		}, slog.Default())
		if err != nil {
			return "", "", nil, err
		}
		return endpoint, "punch", proxy, nil
	}

	return fmt.Sprintf("%s:%d", "localhost", cfg.WireGuard.ListenPort), "fallback", nil, nil
}

func ensureWireGuardReady(ctx context.Context, iface string, address string, pm peer.PeerManager) error {
	if err := ensureWireGuardInterface(ctx, iface, address); err != nil {
		return err
	}
	if err := pm.SetWGEnabled(true); err != nil {
		return fmt.Errorf("configure interface %q: %w", iface, err)
	}
	return nil
}

func ensureWireGuardInterface(ctx context.Context, iface string, address string) error {
	if strings.TrimSpace(iface) == "" {
		return fmt.Errorf("wireguard interface is required")
	}
	if strings.TrimSpace(address) == "" {
		return fmt.Errorf("wireguard interface address is required")
	}

	if _, err := exec.LookPath("ip"); err != nil {
		return fmt.Errorf("find ip command: %w", err)
	}

	if err := runIPCommand(ctx, "link", "add", "dev", iface, "type", "wireguard"); err != nil && !strings.Contains(err.Error(), "File exists") {
		return fmt.Errorf("create wireguard interface %q: %w", iface, err)
	}
	if err := runIPCommand(ctx, "addr", "replace", address, "dev", iface); err != nil {
		return fmt.Errorf("set wireguard interface %q address %q: %w", iface, address, err)
	}

	if err := runIPCommand(ctx, "link", "set", "up", "dev", iface); err != nil {
		return fmt.Errorf("bring wireguard interface %q up: %w", iface, err)
	}

	return nil
}

func resolveWireGuardAddress(subnetCIDR string, configuredAddress string) (string, string, error) {
	_, subnet, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return "", "", fmt.Errorf("parse wireguard subnet: %w", err)
	}
	ones, bits := subnet.Mask.Size()
	if bits != 32 {
		return "", "", fmt.Errorf("wireguard subnet must be IPv4")
	}

	value := strings.TrimSpace(configuredAddress)
	if value == "" {
		ip := firstUsableIPv4(subnet)
		if ip == nil {
			return "", "", fmt.Errorf("wireguard subnet %s has no usable server address", subnetCIDR)
		}
		value = fmt.Sprintf("%s/%d", ip.String(), ones)
	}

	ip, parsedNet, err := net.ParseCIDR(value)
	if err != nil {
		plainIP := net.ParseIP(value)
		if plainIP == nil || plainIP.To4() == nil {
			return "", "", fmt.Errorf("parse wireguard address %q: %w", configuredAddress, err)
		}
		ip = plainIP
		value = fmt.Sprintf("%s/%d", ip.String(), ones)
	} else if parsedNet == nil {
		return "", "", fmt.Errorf("parse wireguard address %q", configuredAddress)
	}

	ip4 := ip.To4()
	if ip4 == nil {
		return "", "", fmt.Errorf("wireguard address %q must be IPv4", configuredAddress)
	}
	if !subnet.Contains(ip4) {
		return "", "", fmt.Errorf("wireguard address %s is outside subnet %s", ip4.String(), subnet.String())
	}

	if _, _, err := net.ParseCIDR(value); err != nil {
		return "", "", fmt.Errorf("wireguard address %q must include a valid prefix: %w", value, err)
	}

	return value, ip4.String(), nil
}

func firstUsableIPv4(subnet *net.IPNet) net.IP {
	networkIP := subnet.IP.To4()
	if networkIP == nil {
		return nil
	}
	ones, bits := subnet.Mask.Size()
	hostBits := bits - ones
	if hostBits < 2 {
		return nil
	}
	networkNum := uint32(networkIP[0])<<24 | uint32(networkIP[1])<<16 | uint32(networkIP[2])<<8 | uint32(networkIP[3])
	ipNum := networkNum + 1
	return net.IP{byte(ipNum >> 24), byte(ipNum >> 16), byte(ipNum >> 8), byte(ipNum)}
}

func runIPCommand(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ip", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
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
