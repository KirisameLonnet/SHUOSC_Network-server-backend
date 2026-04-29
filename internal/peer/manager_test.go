package peer

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shuosc/scnet-server/internal/model"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type reconcilePeerStore struct {
	activePeers      []*model.Peer
	lastSeenUpdates  map[uuid.UUID]time.Time
	updateLastSeenFn func(peerID uuid.UUID, lastSeen time.Time)
}

func (s *reconcilePeerStore) Create(context.Context, *model.Peer) error { return nil }
func (s *reconcilePeerStore) FindActiveByUserID(context.Context, uuid.UUID) ([]*model.Peer, error) {
	return nil, nil
}
func (s *reconcilePeerStore) FindByID(context.Context, uuid.UUID) (*model.Peer, error) {
	return nil, nil
}
func (s *reconcilePeerStore) ListActive(context.Context) ([]*model.Peer, error) {
	return s.activePeers, nil
}
func (s *reconcilePeerStore) CountActiveByUserID(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (s *reconcilePeerStore) UpdateStatus(context.Context, uuid.UUID, string) error { return nil }
func (s *reconcilePeerStore) UpdatePublicKey(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *reconcilePeerStore) UpdateLastSeen(_ context.Context, peerID uuid.UUID, lastSeen time.Time) error {
	if s.lastSeenUpdates == nil {
		s.lastSeenUpdates = make(map[uuid.UUID]time.Time)
	}
	s.lastSeenUpdates[peerID] = lastSeen
	if s.updateLastSeenFn != nil {
		s.updateLastSeenFn(peerID, lastSeen)
	}
	return nil
}

type fakeWGClient struct {
	device         *wgtypes.Device
	configureCalls []wgtypes.Config
}

func (c *fakeWGClient) ConfigureDevice(_ string, cfg wgtypes.Config) error {
	c.configureCalls = append(c.configureCalls, cfg)
	return nil
}

func (c *fakeWGClient) Device(_ string) (*wgtypes.Device, error) {
	return c.device, nil
}

type addPeerUserStore struct {
	user *model.User
}

func (s *addPeerUserStore) FindByID(context.Context, uuid.UUID) (*model.User, error) {
	return s.user, nil
}

type addPeerStore struct {
	createdPeer *model.Peer
}

func (s *addPeerStore) Create(_ context.Context, peer *model.Peer) error {
	s.createdPeer = peer
	return nil
}

func (s *addPeerStore) FindActiveByUserID(context.Context, uuid.UUID) ([]*model.Peer, error) {
	return nil, nil
}

func (s *addPeerStore) FindByID(context.Context, uuid.UUID) (*model.Peer, error) {
	return nil, nil
}

func (s *addPeerStore) ListActive(context.Context) ([]*model.Peer, error) {
	return nil, nil
}

func (s *addPeerStore) CountActiveByUserID(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}

func (s *addPeerStore) UpdateStatus(context.Context, uuid.UUID, string) error {
	return nil
}

func (s *addPeerStore) UpdatePublicKey(context.Context, uuid.UUID, string) error {
	return nil
}

func (s *addPeerStore) UpdateLastSeen(context.Context, uuid.UUID, time.Time) error {
	return nil
}

func TestPeerManagerReconcileSyncsState(t *testing.T) {
	t.Parallel()

	dbOnlyKey := mustPrivateKey(t).PublicKey()
	dbAndWGKey := mustPrivateKey(t).PublicKey()
	wgOnlyKey := mustPrivateKey(t).PublicKey()

	dbOnlyPeerID := uuid.New()
	dbAndWGPeerID := uuid.New()
	handshakeAt := time.Now().Add(-5 * time.Minute).Round(0)

	ps := &reconcilePeerStore{
		activePeers: []*model.Peer{
			{ID: dbOnlyPeerID, PublicKey: dbOnlyKey.String(), AssignedIP: "10.0.0.1", Status: "active"},
			{ID: dbAndWGPeerID, PublicKey: dbAndWGKey.String(), AssignedIP: "10.0.0.2", Status: "active"},
		},
	}
	wgClient := &fakeWGClient{
		device: &wgtypes.Device{
			Name: "wg_scnet",
			Peers: []wgtypes.Peer{
				{PublicKey: dbAndWGKey, LastHandshakeTime: handshakeAt},
				{PublicKey: wgOnlyKey},
			},
		},
	}
	ipam, err := NewIPAM("10.0.0.0/29")
	if err != nil {
		t.Fatalf("NewIPAM returned error: %v", err)
	}

	manager := NewPeerManager(nil, ps, ipam, wgClient, "wg_scnet", mustPrivateKey(t), 51820, "localhost:51820")

	if err := manager.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	if len(wgClient.configureCalls) != 1 {
		t.Fatalf("expected 1 ConfigureDevice call, got %d", len(wgClient.configureCalls))
	}

	changes := wgClient.configureCalls[0].Peers
	if len(changes) != 2 {
		t.Fatalf("expected 2 peer changes, got %d", len(changes))
	}

	var added, removed bool
	for _, change := range changes {
		switch {
		case change.PublicKey.String() == dbOnlyKey.String():
			added = true
			if len(change.AllowedIPs) != 1 || change.AllowedIPs[0].IP.String() != "10.0.0.1" {
				t.Fatalf("unexpected allowed IPs for added peer: %+v", change.AllowedIPs)
			}
		case change.PublicKey.String() == wgOnlyKey.String():
			removed = change.Remove
		}
	}
	if !added {
		t.Fatal("expected db-only peer to be re-added to WireGuard")
	}
	if !removed {
		t.Fatal("expected wg-only peer to be removed from WireGuard")
	}

	if got := ps.lastSeenUpdates[dbAndWGPeerID]; !got.Equal(handshakeAt) {
		t.Fatalf("expected last_seen %v, got %v", handshakeAt, got)
	}

	nextIP, err := ipam.Allocate(context.Background())
	if err != nil {
		t.Fatalf("Allocate returned error after reconcile: %v", err)
	}
	if nextIP != "10.0.0.3" {
		t.Fatalf("expected next allocated IP to skip reserved peers and be 10.0.0.3, got %s", nextIP)
	}
}

func TestAddPeerReturnsServerPublicKeyInsteadOfPrivateKey(t *testing.T) {
	t.Parallel()

	serverPrivateKey := mustPrivateKey(t)
	userID := uuid.New()
	userStore := &addPeerUserStore{
		user: &model.User{
			ID:       userID,
			Role:     "user",
			Status:   "active",
			MaxPeers: 5,
		},
	}
	peerStore := &addPeerStore{}
	wgClient := &fakeWGClient{device: &wgtypes.Device{Name: "wg_scnet"}}

	ipam, err := NewIPAM("10.0.0.0/29")
	if err != nil {
		t.Fatalf("NewIPAM returned error: %v", err)
	}

	manager := NewPeerManager(userStore, peerStore, ipam, wgClient, "wg_scnet", serverPrivateKey, 51820, "vpn.example.com:51820")
	registration, err := manager.AddPeer(context.Background(), userID.String(), mustPrivateKey(t).PublicKey())
	if err != nil {
		t.Fatalf("AddPeer returned error: %v", err)
	}

	if registration.ServerPublicKey != serverPrivateKey.PublicKey().String() {
		t.Fatalf("expected server public key %q, got %q", serverPrivateKey.PublicKey().String(), registration.ServerPublicKey)
	}
	if registration.ServerPublicKey == serverPrivateKey.String() {
		t.Fatal("registration leaked the server private key")
	}
}

func mustPrivateKey(t *testing.T) wgtypes.Key {
	t.Helper()

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey returned error: %v", err)
	}
	return key
}

var _ net.IP
