package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shuosc/scnet-server/internal/admin"
	"github.com/shuosc/scnet-server/internal/model"
	"github.com/shuosc/scnet-server/internal/peer"
	"github.com/shuosc/scnet-server/internal/store"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type adminHandlerAdminServiceStub struct {
	peer      *model.Peer
	callOrder *[]string
}

func (s *adminHandlerAdminServiceStub) GetSummary(context.Context) (*admin.Summary, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) ListUsers(context.Context, store.UserListParams) (*store.UserListResult, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) GetUser(context.Context, string) (*model.User, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) UpdateUser(context.Context, string, admin.AdminUserUpdateFields) (*model.User, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) ListUserPeers(context.Context, string, store.PeerListByUserParams) (*store.PeerListResult, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) ListPeers(context.Context, store.PeerListParams) (*store.PeerListResult, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) DisconnectPeer(context.Context, string) error {
	*s.callOrder = append(*s.callOrder, "disconnect")
	return nil
}

func (s *adminHandlerAdminServiceStub) RevokePeer(context.Context, string) error {
	return nil
}

func (s *adminHandlerAdminServiceStub) ListInvites(context.Context, store.InviteListParams) (*store.InviteListResult, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) CreateInvite(context.Context, string, int, int) (*model.InviteCode, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) UpdateInvite(context.Context, string, admin.InviteUpdateFields) (*model.InviteCode, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) DeleteInvite(context.Context, string) error {
	return nil
}

func (s *adminHandlerAdminServiceStub) VerifyAdmin(context.Context, string) (*model.User, error) {
	return nil, nil
}

func (s *adminHandlerAdminServiceStub) GetPeer(context.Context, string) (*model.Peer, error) {
	*s.callOrder = append(*s.callOrder, "get")
	return s.peer, nil
}

type adminHandlerPeerManagerStub struct {
	callOrder *[]string
}

func (s *adminHandlerPeerManagerStub) AddPeer(context.Context, string, wgtypes.Key) (*peer.PeerRegistration, error) {
	return nil, nil
}

func (s *adminHandlerPeerManagerStub) RemovePeer(context.Context, string, string) error {
	*s.callOrder = append(*s.callOrder, "remove")
	return nil
}

func (s *adminHandlerPeerManagerStub) GetPeerConfig(context.Context, string, string) (*peer.PeerRegistration, error) {
	return nil, nil
}

func (s *adminHandlerPeerManagerStub) ReplaceKey(context.Context, string, wgtypes.Key, wgtypes.Key) error {
	return nil
}

func (s *adminHandlerPeerManagerStub) Reconcile(context.Context) error {
	return nil
}

func (s *adminHandlerPeerManagerStub) ActivePeerCount(context.Context) int {
	return 0
}

func (s *adminHandlerPeerManagerStub) ServerPublicKey() wgtypes.Key {
	return wgtypes.Key{}
}

func (s *adminHandlerPeerManagerStub) RotateServerKey(newKey wgtypes.Key) (wgtypes.Key, error) {
	return newKey.PublicKey(), nil
}

func (s *adminHandlerPeerManagerStub) WGEnabled() (bool, error) {
	return true, nil
}

func (s *adminHandlerPeerManagerStub) SetWGEnabled(enabled bool) error {
	return nil
}

func (s *adminHandlerPeerManagerStub) ListenPort() int {
	return 51820
}

func TestAdminDisconnectPeerRemovesWireGuardPeerBeforeStatusUpdate(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	callOrder := []string{}
	peerInfo := &model.Peer{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		PublicKey: mustPeerKey(t).String(),
		Status:    "active",
	}

	handler := &AdminHandler{
		adminService: &adminHandlerAdminServiceStub{
			peer:      peerInfo,
			callOrder: &callOrder,
		},
		peerManager: &adminHandlerPeerManagerStub{callOrder: &callOrder},
	}

	router := gin.New()
	router.POST("/admin/peer/:id/disconnect", handler.AdminDisconnectPeer)

	req := httptest.NewRequest(http.MethodPost, "/admin/peer/"+peerInfo.ID.String()+"/disconnect", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	got := []string{"get", "remove", "disconnect"}
	if len(callOrder) != len(got) {
		t.Fatalf("unexpected call order length: got %v", callOrder)
	}
	for i := range got {
		if callOrder[i] != got[i] {
			t.Fatalf("expected call order %v, got %v", got, callOrder)
		}
	}
}

func TestAdminGetWGStatusReturns200WithWGData(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	pubKey := mustPeerKey(t)
	pm := &wgStatusStub{enabled: true, pubKey: pubKey, listenPort: 51820, peersCount: 3}

	handler := &AdminHandler{peerManager: pm}
	router := gin.New()
	router.GET("/admin/wg", handler.AdminGetWGStatus)

	req := httptest.NewRequest(http.MethodGet, "/admin/wg", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{`"enabled":true`, `"listen_port":51820`, `"peers_count":3`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %s: %s", want, body)
		}
	}
}

func TestAdminGetWGStatusReturnsDisabledState(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	pm := &wgStatusStub{enabled: false, pubKey: wgtypes.Key{}, listenPort: 0, peersCount: 0}

	handler := &AdminHandler{peerManager: pm}
	router := gin.New()
	router.GET("/admin/wg", handler.AdminGetWGStatus)

	req := httptest.NewRequest(http.MethodGet, "/admin/wg", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	for _, want := range []string{`"enabled":false`, `"listen_port":0`, `"peers_count":0`} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %s: %s", want, body)
		}
	}
}

func TestAdminRotateWGKeySucceeds(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	privKey := mustPrivateKey(t)
	pubKey := privKey.PublicKey()
	pm := &wgKeyRotationStub{expectedKey: privKey, resultKey: pubKey}

	handler := &AdminHandler{peerManager: pm}
	router := gin.New()
	router.POST("/admin/wg/rotate-key", handler.AdminRotateWGKey)

	reqBody := `{"private_key":"` + privKey.String() + `"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/wg/rotate-key", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("expected success response: %s", body)
	}
	if !strings.Contains(body, pubKey.String()) {
		t.Errorf("expected response to contain new public key: %s", body)
	}
}

func TestAdminRotateWGKeyRejectsMissingBody(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	handler := &AdminHandler{peerManager: &adminHandlerPeerManagerStub{}}
	router := gin.New()
	router.POST("/admin/wg/rotate-key", handler.AdminRotateWGKey)

	req := httptest.NewRequest(http.MethodPost, "/admin/wg/rotate-key", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestAdminToggleWGEnablesAndDisables(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	pm := &wgToggleStub{}
	handler := &AdminHandler{peerManager: pm}
	router := gin.New()
	router.POST("/admin/wg/toggle", handler.AdminToggleWG)

	// enable
	reqBody := `{"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/admin/wg/toggle", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 on enable, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Errorf("expected enabled:true in response: %s", rec.Body.String())
	}

	// disable
	reqBody = `{"enabled":false}`
	req = httptest.NewRequest(http.MethodPost, "/admin/wg/toggle", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 on disable, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Errorf("expected enabled:false in response: %s", rec.Body.String())
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

func mustPeerKey(t *testing.T) wgtypes.Key {
	t.Helper()

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey returned error: %v", err)
	}
	return key.PublicKey()
}

type wgStatusStub struct {
	enabled    bool
	pubKey     wgtypes.Key
	listenPort int
	peersCount int
}

func (s *wgStatusStub) AddPeer(context.Context, string, wgtypes.Key) (*peer.PeerRegistration, error) {
	return nil, nil
}
func (s *wgStatusStub) RemovePeer(context.Context, string, string) error { return nil }
func (s *wgStatusStub) GetPeerConfig(context.Context, string, string) (*peer.PeerRegistration, error) {
	return nil, nil
}
func (s *wgStatusStub) ReplaceKey(context.Context, string, wgtypes.Key, wgtypes.Key) error {
	return nil
}
func (s *wgStatusStub) Reconcile(context.Context) error                  { return nil }
func (s *wgStatusStub) ActivePeerCount(context.Context) int              { return s.peersCount }
func (s *wgStatusStub) ServerPublicKey() wgtypes.Key                     { return s.pubKey }
func (s *wgStatusStub) RotateServerKey(wgtypes.Key) (wgtypes.Key, error) { return wgtypes.Key{}, nil }
func (s *wgStatusStub) WGEnabled() (bool, error)                         { return s.enabled, nil }
func (s *wgStatusStub) SetWGEnabled(bool) error                          { return nil }
func (s *wgStatusStub) ListenPort() int                                  { return s.listenPort }

type wgKeyRotationStub struct {
	expectedKey wgtypes.Key
	resultKey   wgtypes.Key
}

func (s *wgKeyRotationStub) AddPeer(context.Context, string, wgtypes.Key) (*peer.PeerRegistration, error) {
	return nil, nil
}
func (s *wgKeyRotationStub) RemovePeer(context.Context, string, string) error { return nil }
func (s *wgKeyRotationStub) GetPeerConfig(context.Context, string, string) (*peer.PeerRegistration, error) {
	return nil, nil
}
func (s *wgKeyRotationStub) ReplaceKey(context.Context, string, wgtypes.Key, wgtypes.Key) error {
	return nil
}
func (s *wgKeyRotationStub) Reconcile(context.Context) error     { return nil }
func (s *wgKeyRotationStub) ActivePeerCount(context.Context) int { return 0 }
func (s *wgKeyRotationStub) ServerPublicKey() wgtypes.Key        { return wgtypes.Key{} }
func (s *wgKeyRotationStub) RotateServerKey(newKey wgtypes.Key) (wgtypes.Key, error) {
	return s.resultKey, nil
}
func (s *wgKeyRotationStub) WGEnabled() (bool, error) { return true, nil }
func (s *wgKeyRotationStub) SetWGEnabled(bool) error  { return nil }
func (s *wgKeyRotationStub) ListenPort() int          { return 51820 }

type wgToggleStub struct {
	enabled bool
}

func (s *wgToggleStub) AddPeer(context.Context, string, wgtypes.Key) (*peer.PeerRegistration, error) {
	return nil, nil
}
func (s *wgToggleStub) RemovePeer(context.Context, string, string) error { return nil }
func (s *wgToggleStub) GetPeerConfig(context.Context, string, string) (*peer.PeerRegistration, error) {
	return nil, nil
}
func (s *wgToggleStub) ReplaceKey(context.Context, string, wgtypes.Key, wgtypes.Key) error {
	return nil
}
func (s *wgToggleStub) Reconcile(context.Context) error                  { return nil }
func (s *wgToggleStub) ActivePeerCount(context.Context) int              { return 0 }
func (s *wgToggleStub) ServerPublicKey() wgtypes.Key                     { return wgtypes.Key{} }
func (s *wgToggleStub) RotateServerKey(wgtypes.Key) (wgtypes.Key, error) { return wgtypes.Key{}, nil }
func (s *wgToggleStub) WGEnabled() (bool, error)                         { return s.enabled, nil }
func (s *wgToggleStub) SetWGEnabled(enabled bool) error                  { s.enabled = enabled; return nil }
func (s *wgToggleStub) ListenPort() int                                  { return 51820 }

var _ peer.PeerManager = (*adminHandlerPeerManagerStub)(nil)
var _ peer.PeerManager = (*wgStatusStub)(nil)
var _ peer.PeerManager = (*wgKeyRotationStub)(nil)
var _ peer.PeerManager = (*wgToggleStub)(nil)
