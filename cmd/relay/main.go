// agentanycast-relay is a lightweight signaling and relay server for AgentAnycast.
//
// It provides:
// - Circuit Relay v2: resource-limited relay for peers that cannot directly connect
// - AutoNAT service: helps peers detect their NAT type
// - Bootstrap node: initial peer discovery for new nodes
// - Skill Registry: capability-based agent discovery (v0.2)
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/event"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
	"google.golang.org/grpc"

	pb "github.com/agentanycast/agentanycast-proto/gen/go/agentanycast/v1"
	"github.com/agentanycast/agentanycast-relay/internal/registry"
)

var version = "dev"

func main() {
	var (
		flagListenAddr      = flag.String("listen", "/ip4/0.0.0.0/tcp/4001", "listen multiaddr")
		flagKeyPath         = flag.String("key", "", "path to persistent identity key")
		flagMaxReservations = flag.Int("max-reservations", 128, "max concurrent relay reservations")
		flagLogLevel        = flag.String("log-level", "info", "log level (debug/info/warn/error)")
		flagRegistryListen  = flag.String("registry-listen", ":50052", "gRPC listen address for Skill Registry")
		flagRegistryTTL     = flag.Duration("registry-ttl", 30*time.Second, "skill registration TTL")
		flagVersion         = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *flagVersion {
		fmt.Println("agentanycast-relay", version)
		os.Exit(0)
	}

	// ── Logger ───────────────────────────────────────────────
	logLevel := slog.LevelInfo
	switch *flagLogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// ── Identity ─────────────────────────────────────────────
	privKey, err := loadOrGenerateKey(*flagKeyPath)
	if err != nil {
		logger.Error("failed to load key", "error", err)
		os.Exit(1)
	}

	// ── Listen Address ───────────────────────────────────────
	listenMA, err := multiaddr.NewMultiaddr(*flagListenAddr)
	if err != nil {
		logger.Error("invalid listen address", "addr", *flagListenAddr, "error", err)
		os.Exit(1)
	}

	// Also listen on QUIC using the same IP and port as TCP.
	quicAddrStr := strings.Replace(*flagListenAddr, "/tcp/", "/udp/", 1) + "/quic-v1"
	quicMA, err := multiaddr.NewMultiaddr(quicAddrStr)
	if err != nil {
		logger.Error("invalid QUIC listen address", "addr", quicAddrStr, "error", err)
		os.Exit(1)
	}

	listenAddrs := []multiaddr.Multiaddr{listenMA, quicMA}

	// ── libp2p Host with Relay v2 ────────────────────────────
	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.EnableAutoNATv2(),
		libp2p.ForceReachabilityPublic(), // Relay server must be public
	)
	if err != nil {
		logger.Error("failed to create host", "error", err)
		os.Exit(1)
	}
	defer h.Close()

	// ── Enable Circuit Relay v2 ──────────────────────────────
	_, err = relay.New(h,
		relay.WithResources(relay.Resources{
			Limit: &relay.RelayLimit{
				Duration: 2 * time.Minute,
				Data:     1 << 17, // 128 KiB
			},
			MaxReservations:        *flagMaxReservations,
			MaxCircuits:            16,
			BufferSize:             4096,
			MaxReservationsPerPeer: 4,
			MaxReservationsPerIP:   8,
		}),
	)
	if err != nil {
		logger.Error("failed to enable relay", "error", err)
		os.Exit(1)
	}

	// ── Skill Registry ──────────────────────────────────────
	reg := registry.New(registry.Config{
		TTL:    *flagRegistryTTL,
		Logger: logger,
	})
	defer reg.Close()

	// Watch for peer disconnections and remove their registrations.
	sub, err := h.EventBus().Subscribe(new(event.EvtPeerConnectednessChanged))
	if err != nil {
		logger.Error("failed to subscribe to peer events", "error", err)
		os.Exit(1)
	}
	go func() {
		defer sub.Close()
		for evt := range sub.Out() {
			e, ok := evt.(event.EvtPeerConnectednessChanged)
			if !ok {
				continue
			}
			if e.Connectedness == network.NotConnected {
				reg.RemovePeer(e.Peer.String())
				logger.Debug("peer disconnected, removed from registry", "peer_id", e.Peer)
			}
		}
	}()

	// ── gRPC Server for Registry ────────────────────────────
	grpcLis, err := net.Listen("tcp", *flagRegistryListen)
	if err != nil {
		logger.Error("failed to listen for gRPC", "addr", *flagRegistryListen, "error", err)
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterRegistryServiceServer(grpcServer, registry.NewGRPCServer(reg, logger))

	go func() {
		logger.Info("registry gRPC server started", "addr", *flagRegistryListen)
		if err := grpcServer.Serve(grpcLis); err != nil {
			logger.Error("registry gRPC server error", "error", err)
		}
	}()

	// ── Print Startup Info ───────────────────────────────────
	logger.Info("agentanycast-relay started",
		"version", version,
		"peer_id", h.ID().String(),
		"addresses", h.Addrs(),
		"max_reservations", *flagMaxReservations,
		"registry_listen", *flagRegistryListen,
		"registry_ttl", flagRegistryTTL.String(),
	)

	for _, addr := range h.Addrs() {
		fmt.Printf("RELAY_ADDR=%s/p2p/%s\n", addr, h.ID())
	}
	fmt.Printf("REGISTRY_ADDR=%s\n", *flagRegistryListen)

	// ── Wait for Shutdown ────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("shutting down")
	grpcServer.GracefulStop()
}

func loadOrGenerateKey(path string) (crypto.PrivKey, error) {
	if path == "" {
		priv, _, err := crypto.GenerateEd25519Key(nil)
		return priv, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			priv, _, err := crypto.GenerateEd25519Key(nil)
			if err != nil {
				return nil, err
			}
			raw, err := crypto.MarshalPrivateKey(priv)
			if err != nil {
				return nil, fmt.Errorf("marshal private key: %w", err)
			}
			dir := path[:len(path)-len("/key")]
			if mkErr := os.MkdirAll(dir, 0700); mkErr != nil {
				slog.Warn("failed to create key directory", "dir", dir, "error", mkErr)
			}
			if wErr := os.WriteFile(path, raw, 0600); wErr != nil {
				slog.Warn("failed to persist identity key", "path", path, "error", wErr)
			}
			return priv, nil
		}
		return nil, err
	}

	return crypto.UnmarshalPrivateKey(data)
}
