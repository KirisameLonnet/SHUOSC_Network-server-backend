package relayagent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	AgentURL       string
	Token          string
	AgentID        string
	BackendURL     string
	PingInterval   time.Duration
	RequestTimeout time.Duration
	ReconnectDelay time.Duration
}

type Agent struct {
	agentURL   string
	backendURL *url.URL
	client     *http.Client
	cfg        Config
	logger     *slog.Logger
}

type requestEnvelope struct {
	Type           string      `json:"type"`
	RequestID      string      `json:"request_id"`
	Method         string      `json:"method"`
	URL            string      `json:"url"`
	Headers        headerPairs `json:"headers"`
	BodyBase64     string      `json:"body_base64"`
	ForwardedHost  string      `json:"forwarded_host"`
	ForwardedProto string      `json:"forwarded_proto"`
	ClientIP       string      `json:"client_ip"`
	ClientOrigin   string      `json:"client_origin"`
	CreatedAt      string      `json:"created_at"`
}

type responseEnvelope struct {
	Type       string      `json:"type"`
	RequestID  string      `json:"request_id"`
	Status     int         `json:"status"`
	Headers    headerPairs `json:"headers"`
	BodyBase64 string      `json:"body_base64"`
}

type helloEnvelope struct {
	Type        string `json:"type"`
	AgentID     string `json:"agent_id"`
	ConnectedAt string `json:"connected_at,omitempty"`
}

type pingEnvelope struct {
	Type string `json:"type"`
	At   string `json:"at,omitempty"`
}

type errorEnvelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id,omitempty"`
	Code      string `json:"code"`
	Error     string `json:"error"`
}

type headerPair [2]string
type headerPairs []headerPair

var hopByHopHeaders = map[string]struct{}{
	"connection":          {},
	"content-length":      {},
	"host":                {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"x-forwarded-for":     {},
	"x-forwarded-host":    {},
	"x-forwarded-proto":   {},
}

func New(cfg Config, logger *slog.Logger) (*Agent, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("SCNET_AGENT_TOKEN is required")
	}

	agentURL, err := normalizeAgentURL(cfg.AgentURL)
	if err != nil {
		return nil, err
	}

	backendURL, err := normalizeBackendURL(cfg.BackendURL)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(cfg.AgentID) == "" {
		cfg.AgentID = "scnet-relay-agent"
	}

	if cfg.PingInterval <= 0 {
		cfg.PingInterval = 15 * time.Second
	}

	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 20 * time.Second
	}

	if cfg.ReconnectDelay <= 0 {
		cfg.ReconnectDelay = 3 * time.Second
	}

	return &Agent{
		agentURL:   agentURL,
		backendURL: backendURL,
		cfg:        cfg,
		client: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
		logger: logger,
	}, nil
}

func (a *Agent) Run(ctx context.Context) error {
	a.logger.Info("starting relay agent",
		"agent_url", a.agentURL,
		"backend_url", a.backendURL.String(),
		"agent_id", a.cfg.AgentID,
	)

	for {
		err := a.runSession(ctx)
		if ctx.Err() != nil {
			return nil
		}

		a.logger.Error("relay session ended", "error", err)

		select {
		case <-time.After(a.cfg.ReconnectDelay):
		case <-ctx.Done():
			return nil
		}
	}
}

func (a *Agent) runSession(ctx context.Context) error {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+a.cfg.Token)
	headers.Set("X-SCNET-Agent-ID", a.cfg.AgentID)

	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, a.agentURL, headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return fmt.Errorf("dial relay: %w (status=%s body=%s)", err, resp.Status, strings.TrimSpace(string(body)))
		}
		return fmt.Errorf("dial relay: %w", err)
	}
	defer conn.Close()

	conn.SetReadLimit(8 << 20)

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var writeMu sync.Mutex

	if err := writeJSON(conn, &writeMu, helloEnvelope{
		Type:        "hello",
		AgentID:     a.cfg.AgentID,
		ConnectedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	go a.pingLoop(sessionCtx, conn, &writeMu)

	a.logger.Info("relay connected", "agent_url", a.agentURL)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		if err := a.handleMessage(sessionCtx, conn, &writeMu, data); err != nil {
			a.logger.Error("handle relay message", "error", err)
		}
	}
}

func (a *Agent) pingLoop(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex) {
	ticker := time.NewTicker(a.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := writeJSON(conn, writeMu, pingEnvelope{
				Type: "ping",
				At:   time.Now().UTC().Format(time.RFC3339),
			}); err != nil {
				a.logger.Warn("send ping", "error", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (a *Agent) handleMessage(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, data []byte) error {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode relay envelope: %w", err)
	}

	switch envelope.Type {
	case "hello":
		a.logger.Info("relay hello received")
		return nil
	case "ping":
		return writeJSON(conn, writeMu, pingEnvelope{
			Type: "pong",
			At:   time.Now().UTC().Format(time.RFC3339),
		})
	case "pong":
		return nil
	case "request":
		var request requestEnvelope
		if err := json.Unmarshal(data, &request); err != nil {
			return fmt.Errorf("decode relay request: %w", err)
		}

		go a.handleRequest(ctx, conn, writeMu, request)
		return nil
	case "error":
		var relayErr errorEnvelope
		if err := json.Unmarshal(data, &relayErr); err != nil {
			return fmt.Errorf("decode relay error: %w", err)
		}
		a.logger.Warn("relay returned error",
			"code", relayErr.Code,
			"request_id", relayErr.RequestID,
			"error", relayErr.Error,
		)
		return nil
	default:
		return writeJSON(conn, writeMu, errorEnvelope{
			Type:  "error",
			Code:  "UNSUPPORTED_MESSAGE",
			Error: "unsupported relay message type",
		})
	}
}

func (a *Agent) handleRequest(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, request requestEnvelope) {
	requestCtx, cancel := context.WithTimeout(ctx, a.cfg.RequestTimeout)
	defer cancel()

	response, err := a.proxyRequest(requestCtx, request)
	if err != nil {
		a.logger.Error("proxy backend request",
			"request_id", request.RequestID,
			"url", request.URL,
			"error", err,
		)
		_ = writeJSON(conn, writeMu, errorEnvelope{
			Type:      "error",
			RequestID: request.RequestID,
			Code:      "UPSTREAM_REQUEST_FAILED",
			Error:     err.Error(),
		})
		return
	}

	if err := writeJSON(conn, writeMu, response); err != nil {
		a.logger.Error("send relay response", "request_id", request.RequestID, "error", err)
	}
}

func (a *Agent) proxyRequest(ctx context.Context, request requestEnvelope) (responseEnvelope, error) {
	targetURL, err := a.resolveTargetURL(request.URL)
	if err != nil {
		return responseEnvelope{}, fmt.Errorf("resolve target url: %w", err)
	}

	body, err := base64.StdEncoding.DecodeString(request.BodyBase64)
	if err != nil {
		return responseEnvelope{}, fmt.Errorf("decode request body: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, request.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return responseEnvelope{}, fmt.Errorf("create upstream request: %w", err)
	}

	copyRequestHeaders(httpRequest.Header, request.Headers)
	if request.ForwardedHost != "" {
		httpRequest.Header.Set("X-Forwarded-Host", request.ForwardedHost)
	}
	if request.ForwardedProto != "" {
		httpRequest.Header.Set("X-Forwarded-Proto", request.ForwardedProto)
	}
	if request.ClientIP != "" {
		httpRequest.Header.Set("X-Forwarded-For", request.ClientIP)
	}
	if request.ClientOrigin != "" {
		httpRequest.Header.Set("X-SCNET-Client-Origin", request.ClientOrigin)
	}

	resp, err := a.client.Do(httpRequest)
	if err != nil {
		return responseEnvelope{}, fmt.Errorf("perform upstream request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return responseEnvelope{}, fmt.Errorf("read upstream response: %w", err)
	}

	return responseEnvelope{
		Type:       "response",
		RequestID:  request.RequestID,
		Status:     resp.StatusCode,
		Headers:    collectResponseHeaders(resp.Header),
		BodyBase64: base64.StdEncoding.EncodeToString(respBody),
	}, nil
}

func (a *Agent) resolveTargetURL(raw string) (string, error) {
	relative, err := url.Parse(raw)
	if err != nil {
		return "", err
	}

	return a.backendURL.ResolveReference(relative).String(), nil
}

func normalizeAgentURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("SCNET_AGENT_URL is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse SCNET_AGENT_URL: %w", err)
	}

	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", errors.New("SCNET_AGENT_URL must start with https://, http://, wss://, or ws://")
	}

	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = "/_agent/connect"
	}

	return parsed.String(), nil
}

func normalizeBackendURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		trimmed = "http://127.0.0.1:8080"
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse SCNET_AGENT_BACKEND_URL: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("SCNET_AGENT_BACKEND_URL must start with http:// or https://")
	}

	if parsed.Host == "" {
		return nil, errors.New("SCNET_AGENT_BACKEND_URL must include host")
	}

	if parsed.Path == "" {
		parsed.Path = "/"
	}

	return parsed, nil
}

func copyRequestHeaders(dst http.Header, src headerPairs) {
	for _, pair := range src {
		name := pair[0]
		value := pair[1]
		if name == "" {
			continue
		}

		if _, blocked := hopByHopHeaders[strings.ToLower(name)]; blocked {
			continue
		}

		dst.Add(name, value)
	}
}

func collectResponseHeaders(src http.Header) headerPairs {
	pairs := make(headerPairs, 0, len(src))
	for name, values := range src {
		if _, blocked := hopByHopHeaders[strings.ToLower(name)]; blocked {
			continue
		}

		for _, value := range values {
			pairs = append(pairs, headerPair{name, value})
		}
	}

	return pairs
}

func writeJSON(conn *websocket.Conn, writeMu *sync.Mutex, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	writeMu.Lock()
	defer writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}
