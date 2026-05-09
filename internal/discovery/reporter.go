package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ReporterConfig struct {
	Enabled bool
	URL     string
	Secret  string
}

type Reporter struct {
	cfg             ReporterConfig
	apiAddr         string
	wgEndpointValue func() string
	httpClient      *http.Client
}

type reportPayload struct {
	ApiURL     string `json:"api_url"`
	WgEndpoint string `json:"wg_endpoint"`
}

func NewReporter(cfg ReporterConfig, apiAddr, wgEndpoint string) *Reporter {
	return NewReporterFunc(cfg, apiAddr, func() string {
		return wgEndpoint
	})
}

func NewReporterFunc(cfg ReporterConfig, apiAddr string, wgEndpointValue func() string) *Reporter {
	if wgEndpointValue == nil {
		wgEndpointValue = func() string { return "" }
	}
	return &Reporter{
		cfg:             cfg,
		apiAddr:         APIURL(apiAddr),
		wgEndpointValue: wgEndpointValue,
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
		WgEndpoint: r.wgEndpointValue(),
	}
	if payload.ApiURL == "" {
		slog.Warn("discovery reporter skipped empty api_url")
		return
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

	slog.Debug("discovery reporter success", "api_url", payload.ApiURL, "wg_endpoint", payload.WgEndpoint)
}

func APIURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}

	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}

	parsed, err := url.Parse(addr)
	if err != nil || parsed.Host == "" {
		return ""
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if parsed.Path == "" {
		parsed.Path = "/api/v1"
	} else if !strings.HasSuffix(parsed.Path, "/api/v1") {
		parsed.Path += "/api/v1"
	}

	return parsed.String()
}
