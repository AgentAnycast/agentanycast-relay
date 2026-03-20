package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/agentanycast/agentanycast-relay/internal/registry"
)

// stubNetwork is a minimal network.Network for testing.
type stubNetwork struct{ network.Network }

func (n *stubNetwork) Peers() []peer.ID { return nil }

// stubHost is a minimal host.Host for testing the health handler.
type stubHost struct {
	id  peer.ID
	net *stubNetwork
}

func (h *stubHost) ID() peer.ID                        { return h.id }
func (h *stubHost) Network() network.Network            { return h.net }
func (h *stubHost) Peerstore() peerstore.Peerstore      { return nil }
func (h *stubHost) Addrs() []ma.Multiaddr               { return nil }
func (h *stubHost) ConnManager() connmgr.ConnManager    { return nil }
func (h *stubHost) EventBus() event.Bus                 { return nil }
func (h *stubHost) Close() error                        { return nil }
func (h *stubHost) Mux() protocol.Switch                { return nil }
func (h *stubHost) Connect(_ context.Context, _ peer.AddrInfo) error { return nil }
func (h *stubHost) SetStreamHandler(_ protocol.ID, _ network.StreamHandler) {}
func (h *stubHost) SetStreamHandlerMatch(_ protocol.ID, _ func(protocol.ID) bool, _ network.StreamHandler) {
}
func (h *stubHost) RemoveStreamHandler(_ protocol.ID) {}
func (h *stubHost) NewStream(_ context.Context, _ peer.ID, _ ...protocol.ID) (network.Stream, error) {
	return nil, nil
}

func TestHealthEndpoint(t *testing.T) {
	reg := registry.New(registry.Config{TTL: 30 * time.Second})
	defer reg.Close()

	// Register an agent so stats are non-zero.
	_, err := reg.Register("peer-1", []registry.SkillInfo{
		{SkillID: "echo", Description: "Echo"},
	}, "Test Agent", "")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	s := &Server{
		startTime: time.Now().Add(-10 * time.Second),
		version:   "test-1.0",
		registry:  reg,
		host: &stubHost{
			id:  peer.ID("test-peer-id"),
			net: &stubNetwork{},
		},
	}

	// Call handleHealth directly — exercises the real handler, not a copy.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Status != StatusHealthy {
		t.Fatalf("expected healthy, got %s", resp.Status)
	}
	if resp.Version != "test-1.0" {
		t.Fatalf("expected test-1.0, got %s", resp.Version)
	}
	if resp.Registry.LocalCount != 1 {
		t.Fatalf("expected 1 local registration, got %d", resp.Registry.LocalCount)
	}
	if resp.Registry.SkillCount != 1 {
		t.Fatalf("expected 1 skill, got %d", resp.Registry.SkillCount)
	}
	if resp.UptimeSeconds < 10 {
		t.Fatalf("expected uptime >= 10s, got %f", resp.UptimeSeconds)
	}
}
