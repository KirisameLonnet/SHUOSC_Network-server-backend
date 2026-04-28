package api

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func mustPeerKey(t *testing.T) wgtypes.Key {
	t.Helper()

	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey returned error: %v", err)
	}
	return key.PublicKey()
}
