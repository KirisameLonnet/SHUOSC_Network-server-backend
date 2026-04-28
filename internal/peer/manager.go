package peer

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/shuosc/scnet-server/internal/model"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

var (
	ErrTooManyPeers    = errors.New("peer limit reached")
	ErrPeerExists      = errors.New("peer with this key already exists")
	ErrNoActivePeer    = errors.New("no active peer found")
	ErrAccountInactive = errors.New("user account is not active")
)

type userStore interface {
	FindByID(ctx context.Context, id uuid.UUID) (*model.User, error)
}

type peerStore interface {
	Create(ctx context.Context, peer *model.Peer) error
	FindActiveByUserID(ctx context.Context, userID uuid.UUID) ([]*model.Peer, error)
	FindByID(ctx context.Context, id uuid.UUID) (*model.Peer, error)
	ListActive(ctx context.Context) ([]*model.Peer, error)
	CountActiveByUserID(ctx context.Context, userID uuid.UUID) (int, error)
	UpdateStatus(ctx context.Context, peerID uuid.UUID, status string) error
	UpdatePublicKey(ctx context.Context, peerID uuid.UUID, newPubKey string) error
	UpdateLastSeen(ctx context.Context, peerID uuid.UUID, lastSeen time.Time) error
}

type wgClient interface {
	ConfigureDevice(name string, cfg wgtypes.Config) error
	Device(name string) (*wgtypes.Device, error)
}

type PeerManager interface {
	AddPeer(ctx context.Context, userID string, pubKey wgtypes.Key) (*PeerRegistration, error)
	RemovePeer(ctx context.Context, userID string, pubKey string) error
	GetPeerConfig(ctx context.Context, userID string, pubKey string) (*PeerRegistration, error)
	ReplaceKey(ctx context.Context, userID string, oldPubKey, newPubKey wgtypes.Key) error
	Reconcile(ctx context.Context) error
	ActivePeerCount(ctx context.Context) int
	ServerPublicKey() wgtypes.Key
}

type PeerRegistration struct {
	PublicKey           string   `json:"public_key"`
	AssignedIP          string   `json:"assigned_ip"`
	AllowedIPs          []string `json:"allowed_ips"`
	Endpoint            string   `json:"endpoint"`
	ServerPublicKey     string   `json:"server_pubkey"`
	PersistentKeepalive int      `json:"persistent_keepalive"`
	Connected           bool     `json:"connected,omitempty"`
}

type PeerManagerImpl struct {
	userStore   userStore
	peerStore   peerStore
	ipam        *IPAMImpl
	wgClient    wgClient
	wgInterface string
	serverKey   wgtypes.Key
	listenPort  int
	endpoint    string
}

func NewPeerManager(
	userStore userStore,
	peerStore peerStore,
	ipam *IPAMImpl,
	wgClient wgClient,
	wgInterface string,
	serverKey wgtypes.Key,
	listenPort int,
	endpoint string,
) PeerManager {
	return &PeerManagerImpl{
		userStore:   userStore,
		peerStore:   peerStore,
		ipam:        ipam,
		wgClient:    wgClient,
		wgInterface: wgInterface,
		serverKey:   serverKey,
		listenPort:  listenPort,
		endpoint:    endpoint,
	}
}

func (m *PeerManagerImpl) AddPeer(ctx context.Context, userID string, pubKey wgtypes.Key) (*PeerRegistration, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}

	user, err := m.userStore.FindByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive() {
		return nil, ErrAccountInactive
	}

	count, err := m.peerStore.CountActiveByUserID(ctx, uid)
	if err != nil {
		return nil, err
	}
	if count >= user.MaxPeers {
		return nil, ErrTooManyPeers
	}

	peers, err := m.peerStore.FindActiveByUserID(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, p := range peers {
		if p.PublicKey == pubKey.String() {
			return nil, ErrPeerExists
		}
	}

	assignedIP, err := m.ipam.Allocate(ctx)
	if err != nil {
		return nil, err
	}

	peerConfig := wgtypes.PeerConfig{
		PublicKey: pubKey,
		AllowedIPs: []net.IPNet{
			{IP: net.ParseIP(assignedIP), Mask: net.CIDRMask(32, 32)},
		},
	}
	if err := m.wgClient.ConfigureDevice(m.wgInterface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peerConfig},
	}); err != nil {
		return nil, err
	}

	peer := &model.Peer{
		ID:         uuid.New(),
		UserID:     uid,
		PublicKey:  pubKey.String(),
		AssignedIP: assignedIP,
		Status:     "active",
	}
	if err := m.peerStore.Create(ctx, peer); err != nil {
		m.wgClient.ConfigureDevice(m.wgInterface, wgtypes.Config{
			Peers: []wgtypes.PeerConfig{{PublicKey: pubKey, Remove: true}},
		})
		m.ipam.Release(ctx, assignedIP)
		return nil, err
	}

	return &PeerRegistration{
		PublicKey:           pubKey.String(),
		AssignedIP:          assignedIP,
		AllowedIPs:          []string{m.ipam.subnet.String()},
		Endpoint:            m.endpoint,
		ServerPublicKey:     m.serverKey.String(),
		PersistentKeepalive: 25,
		Connected:           false,
	}, nil
}

func (m *PeerManagerImpl) RemovePeer(ctx context.Context, userID string, pubKey string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return err
	}

	peers, err := m.peerStore.FindActiveByUserID(ctx, uid)
	if err != nil {
		return err
	}

	var targetPeer *model.Peer
	for _, p := range peers {
		if p.PublicKey == pubKey {
			targetPeer = p
			break
		}
	}
	if targetPeer == nil {
		return ErrNoActivePeer
	}

	key, err := wgtypes.ParseKey(pubKey)
	if err != nil {
		return err
	}

	if err := m.wgClient.ConfigureDevice(m.wgInterface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{{PublicKey: key, Remove: true}},
	}); err != nil {
		return err
	}

	if err := m.peerStore.UpdateStatus(ctx, targetPeer.ID, "disconnected"); err != nil {
		return err
	}

	return m.ipam.Release(ctx, targetPeer.AssignedIP)
}

func (m *PeerManagerImpl) GetPeerConfig(ctx context.Context, userID string, pubKey string) (*PeerRegistration, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return nil, err
	}

	peers, err := m.peerStore.FindActiveByUserID(ctx, uid)
	if err != nil {
		return nil, err
	}

	var targetPeer *model.Peer
	for _, p := range peers {
		if p.PublicKey == pubKey {
			targetPeer = p
			break
		}
	}
	if targetPeer == nil {
		return nil, ErrNoActivePeer
	}

	connected := false
	dev, err := m.wgClient.Device(m.wgInterface)
	if err == nil {
		for _, p := range dev.Peers {
			if p.PublicKey.String() == pubKey {
				connected = true
				break
			}
		}
	}

	return &PeerRegistration{
		PublicKey:           targetPeer.PublicKey,
		AssignedIP:          targetPeer.AssignedIP,
		AllowedIPs:          []string{m.ipam.subnet.String()},
		Endpoint:            m.endpoint,
		ServerPublicKey:     m.serverKey.String(),
		PersistentKeepalive: 25,
		Connected:           connected,
	}, nil
}

func (m *PeerManagerImpl) ReplaceKey(ctx context.Context, userID string, oldPubKey, newPubKey wgtypes.Key) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return err
	}

	peers, err := m.peerStore.FindActiveByUserID(ctx, uid)
	if err != nil {
		return err
	}

	var targetPeer *model.Peer
	for _, p := range peers {
		if p.PublicKey == oldPubKey.String() {
			targetPeer = p
			break
		}
	}
	if targetPeer == nil {
		return ErrNoActivePeer
	}

	if err := m.wgClient.ConfigureDevice(m.wgInterface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{PublicKey: oldPubKey, Remove: true},
			{PublicKey: newPubKey, AllowedIPs: []net.IPNet{{IP: net.ParseIP(targetPeer.AssignedIP), Mask: net.CIDRMask(32, 32)}}},
		},
	}); err != nil {
		return err
	}

	return m.peerStore.UpdatePublicKey(ctx, targetPeer.ID, newPubKey.String())
}

func (m *PeerManagerImpl) Reconcile(ctx context.Context) error {
	activePeers, err := m.peerStore.ListActive(ctx)
	if err != nil {
		return err
	}

	dbPeersByKey := make(map[string]*model.Peer, len(activePeers))
	for _, p := range activePeers {
		dbPeersByKey[p.PublicKey] = p
		if err := m.ipam.Reserve(p.AssignedIP); err != nil {
			return err
		}
	}

	device, err := m.wgClient.Device(m.wgInterface)
	if err != nil {
		return err
	}

	wgPeersByKey := make(map[string]wgtypes.Peer, len(device.Peers))
	for _, p := range device.Peers {
		wgPeersByKey[p.PublicKey.String()] = p
	}

	var changes []wgtypes.PeerConfig

	for _, p := range activePeers {
		wgPeer, ok := wgPeersByKey[p.PublicKey]
		if !ok {
			key, err := wgtypes.ParseKey(p.PublicKey)
			if err != nil {
				slog.Warn("reconciliation skipped invalid db peer key", "peer_id", p.ID.String(), "error", err)
				continue
			}

			allowedIP := net.ParseIP(p.AssignedIP)
			if allowedIP == nil {
				slog.Warn("reconciliation skipped invalid db peer ip", "peer_id", p.ID.String(), "assigned_ip", p.AssignedIP)
				continue
			}

			slog.Warn("db peer missing from wireguard device, re-adding", "peer_id", p.ID.String(), "public_key", p.PublicKey)
			changes = append(changes, wgtypes.PeerConfig{
				PublicKey: key,
				AllowedIPs: []net.IPNet{
					{IP: allowedIP, Mask: net.CIDRMask(32, 32)},
				},
			})
			continue
		}

		if !wgPeer.LastHandshakeTime.IsZero() {
			if err := m.peerStore.UpdateLastSeen(ctx, p.ID, wgPeer.LastHandshakeTime); err != nil {
				return err
			}
		}
	}

	for key, wgPeer := range wgPeersByKey {
		if _, ok := dbPeersByKey[key]; ok {
			continue
		}

		slog.Warn("wireguard peer missing from database, removing", "public_key", key)
		changes = append(changes, wgtypes.PeerConfig{
			PublicKey: wgPeer.PublicKey,
			Remove:    true,
		})
	}

	if len(changes) == 0 {
		slog.Info("reconciliation complete", "db_active_peers", len(activePeers), "wg_peers", len(device.Peers), "changes", 0)
		return nil
	}

	if err := m.wgClient.ConfigureDevice(m.wgInterface, wgtypes.Config{Peers: changes}); err != nil {
		return err
	}

	slog.Info("reconciliation complete", "db_active_peers", len(activePeers), "wg_peers", len(device.Peers), "changes", len(changes))
	return nil
}

func (m *PeerManagerImpl) ActivePeerCount(ctx context.Context) int {
	dev, err := m.wgClient.Device(m.wgInterface)
	if err != nil {
		slog.Warn("failed to query wireguard device for peer count", "error", err)
		return 0
	}
	return len(dev.Peers)
}

func (m *PeerManagerImpl) ServerPublicKey() wgtypes.Key {
	return m.serverKey
}

var _ PeerManager = (*PeerManagerImpl)(nil)
