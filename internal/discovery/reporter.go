package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type ReporterConfig struct {
	Enabled bool
	URL     string
	Secret  string
}

type Reporter struct {
	cfg        ReporterConfig
	apiAddr    string
	wgEndpoint string
	httpClient *http.Client
}

type reportPayload struct {
	ApiURL     string `json:"api_url"`
	WgEndpoint string `json:"wg_endpoint"`
}

func NewReporter(cfg ReporterConfig, apiAddr, wgEndpoint string) *Reporter {
	return &Reporter{
		cfg:        cfg,
		apiAddr:    apiAddr,
		wgEndpoint: wgEndpoint,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (r *Reporter) Start(ctx context.Context) {
	if !r.cfg.Enabled {
		slog.Info("discovery reporting disabled")
		return
	}

	slog.Info("discovery reporter started", "url", r.cfg.URL)
	r.report(ctx)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("discovery reporter stopped")
			return
		case <-ticker.C:
			r.report(ctx)
		}
	}
}

func (r *Reporter) report(ctx context.Context) {
	payload := reportPayload{
		ApiURL:     r.apiAddr,
		WgEndpoint: r.wgEndpoint,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("discovery reporter marshal error", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.cfg.URL, bytes.NewReader(body))
	if err != nil {
		slog.Warn("discovery reporter create request error", "error", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.Secret)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		slog.Warn("discovery reporter request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Warn("discovery reporter unexpected status", "status", resp.StatusCode)
		return
	}

	slog.Debug("discovery reporter success", "api_url", r.apiAddr, "wg_endpoint", r.wgEndpoint)
}

func APIURL(addr string) string {
	if addr == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/api/v1", addr)
}
