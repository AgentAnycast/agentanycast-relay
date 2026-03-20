// Package federation implements relay-to-relay skill registry synchronization.
//
// It coordinates periodic gossip-based sync between peer relays, allowing
// agents registered at one relay to be discovered through any federated peer.
// Conflict resolution uses Last-Writer-Wins (LWW) based on a version counter.
package federation

import (
	"context"
	"log/slog"
	"sync"
	"time"

	pb "github.com/agentanycast/agentanycast-proto/gen/go/agentanycast/v1"
	"github.com/agentanycast/agentanycast-relay/internal/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// DefaultSyncInterval is the default interval between sync rounds.
const DefaultSyncInterval = 10 * time.Second

// Config holds federation configuration.
type Config struct {
	// PeerRelays is a list of gRPC addresses of peer relay servers.
	PeerRelays []string

	// SyncInterval is the interval between sync rounds. Defaults to 10s.
	SyncInterval time.Duration

	// RelayID is this relay's unique identifier.
	RelayID string

	// Logger is the structured logger.
	Logger *slog.Logger
}

// Federation manages relay-to-relay skill registry synchronization.
type Federation struct {
	cfg        Config
	registry   *registry.Registry
	clients    map[string]pb.FederationServiceClient
	conns      map[string]*grpc.ClientConn
	lastSync   map[string]time.Time // per-peer sync timestamps
	peerStates map[string]peerState // per-peer circuit breaker state
	mu         sync.RWMutex
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	logger     *slog.Logger
	startOnce  sync.Once
	started    bool
}

// New creates a new Federation manager.
func New(cfg Config, reg *registry.Registry) *Federation {
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = DefaultSyncInterval
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Federation{
		cfg:        cfg,
		registry:   reg,
		clients:    make(map[string]pb.FederationServiceClient),
		conns:      make(map[string]*grpc.ClientConn),
		lastSync:   make(map[string]time.Time),
		peerStates: make(map[string]peerState),
		logger:     cfg.Logger,
	}
}

// Start initializes connections to peer relays and begins the sync loop.
// It is safe to call multiple times; only the first call has any effect.
func (f *Federation) Start(ctx context.Context) error {
	var startErr error
	f.startOnce.Do(func() {
		ctx, f.cancel = context.WithCancel(ctx)

		// Build connection maps before starting any goroutines to avoid races.
		clients := make(map[string]pb.FederationServiceClient)
		conns := make(map[string]*grpc.ClientConn)

		for _, addr := range f.cfg.PeerRelays {
			conn, err := grpc.NewClient(addr,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				f.logger.Warn("failed to connect to peer relay",
					"addr", addr,
					"error", err,
				)
				continue
			}
			conns[addr] = conn
			clients[addr] = pb.NewFederationServiceClient(conn)
			f.logger.Info("connected to peer relay", "addr", addr)
		}

		// Assign maps under write lock before starting goroutines.
		f.mu.Lock()
		f.clients = clients
		f.conns = conns
		f.started = true
		f.mu.Unlock()

		if len(clients) == 0 {
			f.logger.Info("no peer relays configured, federation disabled")
			return
		}

		f.wg.Add(1)
		go f.syncLoop(ctx)

		f.logger.Info("federation started",
			"relay_id", f.cfg.RelayID,
			"peers", len(clients),
			"sync_interval", f.cfg.SyncInterval,
		)
	})

	return startErr
}

// Stop gracefully shuts down the federation sync loop and closes connections.
func (f *Federation) Stop() error {
	if f.cancel != nil {
		f.cancel()
	}
	f.wg.Wait()

	for addr, conn := range f.conns {
		if err := conn.Close(); err != nil {
			f.logger.Warn("error closing peer relay connection",
				"addr", addr,
				"error", err,
			)
		}
	}

	f.logger.Info("federation stopped")
	return nil
}

// PeerCount returns the number of connected peer relays.
func (f *Federation) PeerCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.clients)
}
