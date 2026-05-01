package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shuosc/scnet-server/internal/relayagent"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	agent, err := relayagent.New(cfg, logger)
	if err != nil {
		logger.Error("build relay agent", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := agent.Run(ctx); err != nil {
		logger.Error("relay agent exited", "error", err)
		os.Exit(1)
	}
}

func loadConfig() (relayagent.Config, error) {
	defaultAgentID, _ := os.Hostname()
	if strings.TrimSpace(defaultAgentID) == "" {
		defaultAgentID = "scnet-relay-agent"
	}

	var cfg relayagent.Config

	flag.StringVar(&cfg.AgentURL, "url", envString("SCNET_AGENT_URL", ""), "Cloudflare Worker relay URL, e.g. https://scnet-test.lonnet.uk/_agent/connect")
	flag.StringVar(&cfg.Token, "token", envString("SCNET_AGENT_TOKEN", ""), "Relay bearer token")
	flag.StringVar(&cfg.AgentID, "id", envString("SCNET_AGENT_ID", defaultAgentID), "Agent identifier")
	flag.StringVar(&cfg.BackendURL, "backend", envString("SCNET_AGENT_BACKEND_URL", "http://127.0.0.1:8080"), "Local backend base URL")

	pingInterval := flag.Int("ping-ms", envInt("SCNET_AGENT_PING_INTERVAL_MS", 15000), "Ping interval in milliseconds")
	requestTimeout := flag.Int("timeout-ms", envInt("SCNET_AGENT_REQUEST_TIMEOUT_MS", 20000), "Backend request timeout in milliseconds")
	reconnectDelay := flag.Int("reconnect-ms", envInt("SCNET_AGENT_RECONNECT_DELAY_MS", 3000), "Reconnect delay in milliseconds")

	flag.Parse()

	cfg.PingInterval = time.Duration(*pingInterval) * time.Millisecond
	cfg.RequestTimeout = time.Duration(*requestTimeout) * time.Millisecond
	cfg.ReconnectDelay = time.Duration(*reconnectDelay) * time.Millisecond

	return cfg, nil
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}
