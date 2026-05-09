package punch

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	bindingRequest  uint16 = 0x0001
	bindingSuccess  uint16 = 0x0101
	attrMappedAddr  uint16 = 0x0001
	attrXORMapped   uint16 = 0x0020
	stunMagicCookie uint32 = 0x2112A442
)

type Config struct {
	ListenPort        int
	WireGuardHost     string
	WireGuardPort     int
	STUNServers       []string
	ProbeTimeout      time.Duration
	KeepaliveInterval time.Duration
}

type Service struct {
	cfg    Config
	conn   *net.UDPConn
	wgAddr *net.UDPAddr
	logger *slog.Logger

	mu       sync.Mutex
	endpoint string
	pending  map[string]chan probeResult
	sessions map[string]*session
}

type session struct {
	external *net.UDPAddr
	conn     *net.UDPConn
	lastSeen time.Time
}

type probeResult struct {
	endpoint string
	err      error
}

func Start(ctx context.Context, cfg Config, logger *slog.Logger) (*Service, string, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ListenPort <= 0 {
		return nil, "", errors.New("punch listen port is required")
	}
	if cfg.WireGuardHost == "" {
		cfg.WireGuardHost = "127.0.0.1"
	}
	if cfg.WireGuardPort <= 0 {
		return nil, "", errors.New("punch WireGuard upstream port is required")
	}
	if cfg.ListenPort == cfg.WireGuardPort && isLocalHost(cfg.WireGuardHost) {
		return nil, "", errors.New("punch listen port must differ from local WireGuard port")
	}
	if len(cfg.STUNServers) == 0 {
		return nil, "", errors.New("at least one mainland STUN server is required")
	}
	if cfg.ProbeTimeout <= 0 {
		cfg.ProbeTimeout = 5 * time.Second
	}
	if cfg.KeepaliveInterval <= 0 {
		cfg.KeepaliveInterval = 20 * time.Second
	}

	wgAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(cfg.WireGuardHost, fmt.Sprintf("%d", cfg.WireGuardPort)))
	if err != nil {
		return nil, "", fmt.Errorf("resolve WireGuard upstream: %w", err)
	}

	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: cfg.ListenPort})
	if err != nil {
		return nil, "", fmt.Errorf("bind punch UDP port %d: %w", cfg.ListenPort, err)
	}

	s := &Service{
		cfg:      cfg,
		conn:     conn,
		wgAddr:   wgAddr,
		logger:   logger,
		pending:  make(map[string]chan probeResult),
		sessions: make(map[string]*session),
	}

	go s.readLoop(ctx)
	go s.cleanupLoop(ctx)

	endpoint, err := s.Probe(ctx)
	if err != nil {
		_ = s.Close()
		return nil, "", err
	}
	s.setEndpoint(endpoint)

	go s.keepaliveLoop(ctx)
	logger.Info("udp punch proxy ready", "listen_port", cfg.ListenPort, "wg_upstream", wgAddr.String(), "endpoint", endpoint)
	return s, endpoint, nil
}

func (s *Service) Probe(ctx context.Context) (string, error) {
	var lastErr error
	for _, server := range s.cfg.STUNServers {
		endpoint, err := s.probeOne(ctx, server)
		if err == nil {
			return endpoint, nil
		}
		lastErr = err
		s.logger.Warn("stun probe failed", "server", server, "error", err)
	}
	if lastErr == nil {
		lastErr = errors.New("no STUN servers configured")
	}
	return "", lastErr
}

func (s *Service) Close() error {
	s.mu.Lock()
	for key, sess := range s.sessions {
		_ = sess.conn.Close()
		delete(s.sessions, key)
	}
	s.mu.Unlock()
	return s.conn.Close()
}

func (s *Service) Endpoint() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endpoint
}

func (s *Service) setEndpoint(endpoint string) {
	s.mu.Lock()
	s.endpoint = endpoint
	s.mu.Unlock()
}

func (s *Service) probeOne(ctx context.Context, server string) (string, error) {
	addr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return "", fmt.Errorf("resolve STUN server: %w", err)
	}

	transactionID, packet, err := bindingRequestPacket()
	if err != nil {
		return "", err
	}

	resultCh := make(chan probeResult, 1)
	key := string(transactionID[:])
	s.mu.Lock()
	s.pending[key] = resultCh
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, key)
		s.mu.Unlock()
	}()

	if _, err := s.conn.WriteToUDP(packet, addr); err != nil {
		return "", fmt.Errorf("send STUN binding request: %w", err)
	}

	timeout := time.NewTimer(s.cfg.ProbeTimeout)
	defer timeout.Stop()

	select {
	case result := <-resultCh:
		return result.endpoint, result.err
	case <-timeout.C:
		return "", fmt.Errorf("STUN probe timed out")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (s *Service) keepaliveLoop(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.KeepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if endpoint, err := s.Probe(ctx); err != nil {
				s.logger.Warn("stun keepalive failed", "error", err)
			} else {
				s.setEndpoint(endpoint)
				s.logger.Debug("stun keepalive ok", "endpoint", endpoint)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) readLoop(ctx context.Context) {
	buf := make([]byte, 64*1024)
	for {
		n, remote, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() == nil && !strings.Contains(err.Error(), "use of closed network connection") {
				s.logger.Warn("punch UDP read failed", "error", err)
			}
			return
		}

		packet := append([]byte(nil), buf[:n]...)
		if s.handleSTUN(packet) {
			continue
		}
		s.forwardToWireGuard(packet, remote)
	}
}

func (s *Service) handleSTUN(packet []byte) bool {
	transactionID, endpoint, ok := parseBindingResponse(packet)
	if !ok {
		return false
	}

	key := string(transactionID[:])
	s.mu.Lock()
	ch := s.pending[key]
	s.mu.Unlock()
	if ch == nil {
		return true
	}

	select {
	case ch <- probeResult{endpoint: endpoint}:
	default:
	}
	return true
}

func (s *Service) forwardToWireGuard(packet []byte, external *net.UDPAddr) {
	sess, err := s.sessionFor(external)
	if err != nil {
		s.logger.Warn("create punch proxy session failed", "remote", external.String(), "error", err)
		return
	}

	if _, err := sess.conn.WriteToUDP(packet, s.wgAddr); err != nil {
		s.logger.Warn("forward packet to WireGuard failed", "remote", external.String(), "error", err)
	}
}

func (s *Service) sessionFor(external *net.UDPAddr) (*session, error) {
	key := external.String()
	s.mu.Lock()
	if sess := s.sessions[key]; sess != nil {
		sess.lastSeen = time.Now()
		s.mu.Unlock()
		return sess, nil
	}
	s.mu.Unlock()

	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	sess := &session{
		external: external,
		conn:     conn,
		lastSeen: time.Now(),
	}

	s.mu.Lock()
	if existing := s.sessions[key]; existing != nil {
		s.mu.Unlock()
		_ = conn.Close()
		return existing, nil
	}
	s.sessions[key] = sess
	s.mu.Unlock()

	go s.readFromWireGuard(key, sess)
	return sess, nil
}

func (s *Service) readFromWireGuard(key string, sess *session) {
	buf := make([]byte, 64*1024)
	for {
		n, _, err := sess.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if _, err := s.conn.WriteToUDP(buf[:n], sess.external); err != nil {
			s.logger.Warn("forward packet to external peer failed", "remote", sess.external.String(), "error", err)
			return
		}
		s.mu.Lock()
		if current := s.sessions[key]; current == sess {
			current.lastSeen = time.Now()
		}
		s.mu.Unlock()
	}
}

func (s *Service) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.cleanupIdleSessions(5 * time.Minute)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) cleanupIdleSessions(maxIdle time.Duration) {
	cutoff := time.Now().Add(-maxIdle)
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, sess := range s.sessions {
		if sess.lastSeen.Before(cutoff) {
			_ = sess.conn.Close()
			delete(s.sessions, key)
		}
	}
}

func bindingRequestPacket() ([12]byte, []byte, error) {
	var transactionID [12]byte
	if _, err := rand.Read(transactionID[:]); err != nil {
		return transactionID, nil, err
	}

	packet := make([]byte, 20)
	binary.BigEndian.PutUint16(packet[0:2], bindingRequest)
	binary.BigEndian.PutUint16(packet[2:4], 0)
	binary.BigEndian.PutUint32(packet[4:8], stunMagicCookie)
	copy(packet[8:20], transactionID[:])
	return transactionID, packet, nil
}

func parseBindingResponse(packet []byte) ([12]byte, string, bool) {
	var transactionID [12]byte
	if len(packet) < 20 {
		return transactionID, "", false
	}
	if binary.BigEndian.Uint16(packet[0:2]) != bindingSuccess {
		return transactionID, "", false
	}
	if binary.BigEndian.Uint32(packet[4:8]) != stunMagicCookie {
		return transactionID, "", false
	}

	msgLen := int(binary.BigEndian.Uint16(packet[2:4]))
	if msgLen+20 > len(packet) {
		return transactionID, "", false
	}
	copy(transactionID[:], packet[8:20])

	offset := 20
	limit := 20 + msgLen
	for offset+4 <= limit {
		attrType := binary.BigEndian.Uint16(packet[offset : offset+2])
		attrLen := int(binary.BigEndian.Uint16(packet[offset+2 : offset+4]))
		valueStart := offset + 4
		valueEnd := valueStart + attrLen
		if valueEnd > limit {
			return transactionID, "", false
		}

		if endpoint, ok := parseAddressAttribute(attrType, packet[valueStart:valueEnd]); ok {
			return transactionID, endpoint, true
		}

		offset = valueEnd
		if rem := offset % 4; rem != 0 {
			offset += 4 - rem
		}
	}

	return transactionID, "", false
}

func parseAddressAttribute(attrType uint16, value []byte) (string, bool) {
	if len(value) < 8 || value[1] != 0x01 {
		return "", false
	}

	port := binary.BigEndian.Uint16(value[2:4])
	ipRaw := binary.BigEndian.Uint32(value[4:8])
	switch attrType {
	case attrXORMapped:
		port ^= uint16(stunMagicCookie >> 16)
		ipRaw ^= stunMagicCookie
	case attrMappedAddr:
	default:
		return "", false
	}

	ip := net.IPv4(byte(ipRaw>>24), byte(ipRaw>>16), byte(ipRaw>>8), byte(ipRaw))
	return net.JoinHostPort(ip.String(), fmt.Sprintf("%d", port)), true
}

func isLocalHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
