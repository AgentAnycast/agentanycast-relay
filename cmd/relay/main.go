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
	libp2pwebtransport "github.com/libp2p/go-libp2p/p2p/transport/webtransport"
	"github.com/multiformats/go-multiaddr"
	"google.golang.org/grpc"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

	pb "github.com/agentanycast/agentanycast-proto/gen/go/agentanycast/v1"
	"github.com/agentanycast/agentanycast-relay/internal/api"
	"github.com/agentanycast/agentanycast-relay/internal/federation"
	"github.com/agentanycast/agentanycast-relay/internal/health"
	relaymcp "github.com/agentanycast/agentanycast-relay/internal/mcp"
	"github.com/agentanycast/agentanycast-relay/internal/namespace"
	"github.com/agentanycast/agentanycast-relay/internal/policy"
	"github.com/agentanycast/agentanycast-relay/internal/registry"
	"github.com/agentanycast/agentanycast-relay/internal/resilience"
	"github.com/agentanycast/agentanycast-relay/internal/telemetry"
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
		flagEnableWebTransport = flag.Bool("enable-webtransport", false, "enable WebTransport (QUIC-based, browser-compatible)")
		flagMCPListen              = flag.String("mcp-listen", ":8080", "MCP Streamable HTTP listen address (empty to disable)")
		flagMetricsListen          = flag.String("metrics-listen", ":9090", "health/metrics HTTP listen address (empty to disable)")
		flagAPIListen              = flag.String("api-listen", ":8081", "REST API listen address for agent directory (empty to disable)")
		flagAPICORSOrigins         = flag.String("api-cors-origins", "", "comma-separated allowed CORS origins (empty = same-origin only, * = all)")
		flagOTLPEndpoint           = flag.String("otlp-endpoint", "", "OTLP gRPC endpoint for tracing (e.g., localhost:4317)")
		flagFederationPeers        = flag.String("federation-peers", "", "comma-separated peer relay gRPC addresses for federation")
		flagFederationSyncInterval = flag.Duration("federation-sync-interval", 10*time.Second, "federation sync interval")
		flagVersion                = flag.Bool("version", false, "print version and exit")
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

	// ── OpenTelemetry Tracing ────────────────────────────────
	otelShutdown, err := telemetry.Setup(context.Background(), telemetry.Config{
		Enabled:      *flagOTLPEndpoint != "",
		OTLPEndpoint: *flagOTLPEndpoint,
		SampleRate:   1.0,
		Version:      version,
		Logger:       logger,
	})
	if err != nil {
		logger.Error("failed to initialize telemetry", "error", err)
		os.Exit(1)
	}
	defer otelShutdown(context.Background())

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

	// Optionally add WebTransport listener.
	if *flagEnableWebTransport {
		wtAddrStr := strings.Replace(*flagListenAddr, "/tcp/", "/udp/", 1) + "/quic-v1/webtransport"
		wtMA, wtErr := multiaddr.NewMultiaddr(wtAddrStr)
		if wtErr != nil {
			logger.Error("invalid WebTransport listen address", "addr", wtAddrStr, "error", wtErr)
			os.Exit(1)
		}
		listenAddrs = append(listenAddrs, wtMA)
		logger.Info("WebTransport enabled", "addr", wtAddrStr)
	}

	// ── libp2p Host with Relay v2 ────────────────────────────
	opts := []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.EnableAutoNATv2(),
		libp2p.ForceReachabilityPublic(), // Relay server must be public
	}

	if *flagEnableWebTransport {
		opts = append(opts, libp2p.Transport(libp2pwebtransport.New))
	}

	h, err := libp2p.New(opts...)
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

	grpcServer := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	pb.RegisterRegistryServiceServer(grpcServer, registry.NewGRPCServer(reg, logger))

	// ── Federation ──────────────────────────────────────────
	var fed *federation.Federation
	var federationPeers []string
	if *flagFederationPeers != "" {
		for _, p := range strings.Split(*flagFederationPeers, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				federationPeers = append(federationPeers, p)
			}
		}
	}

	if len(federationPeers) > 0 {
		relayID := h.ID().String()
		fed = federation.New(federation.Config{
			PeerRelays:   federationPeers,
			SyncInterval: *flagFederationSyncInterval,
			RelayID:      relayID,
			Logger:       logger,
		}, reg)

		// Register FederationService on the same gRPC server.
		pb.RegisterFederationServiceServer(grpcServer, federation.NewServer(reg, relayID, logger))

		federationCtx, federationCancel := context.WithCancel(context.Background())
		defer federationCancel()
		if err := fed.Start(federationCtx); err != nil {
			logger.Error("failed to start federation", "error", err)
		} else {
			logger.Info("federation enabled",
				"peers", len(federationPeers),
				"sync_interval", flagFederationSyncInterval.String(),
			)
		}
	}

	go func() {
		logger.Info("registry gRPC server started", "addr", *flagRegistryListen)
		if err := grpcServer.Serve(grpcLis); err != nil {
			logger.Error("registry gRPC server error", "error", err)
		}
	}()

	// ── MCP Server (Streamable HTTP) ────────────────────────
	var mcpSrv *relaymcp.Server
	if *flagMCPListen != "" {
		var mcpErr error
		mcpSrv, mcpErr = relaymcp.New(relaymcp.Config{
			ListenAddr: *flagMCPListen,
			Registry:   reg,
			Host:       h,
			Logger:     logger,
		})
		if mcpErr != nil {
			logger.Error("failed to create MCP server", "error", mcpErr)
			os.Exit(1)
		}
		if err := mcpSrv.Start(); err != nil {
			logger.Error("failed to start MCP server", "error", err)
			os.Exit(1)
		}
	}

	// ── Health / Metrics Server ─────────────────────────────
	var healthSrv *health.Server
	if *flagMetricsListen != "" {
		healthSrv = health.New(health.Config{
			ListenAddr: *flagMetricsListen,
			Version:    version,
			Host:       h,
			Registry:   reg,
			Federation: fed,
			Logger:     logger,
		})
		if err := healthSrv.Start(); err != nil {
			logger.Error("failed to start health/metrics server", "error", err)
			os.Exit(1)
		}
		logger.Info("health/metrics server started", "addr", *flagMetricsListen)
	}

	// ── Mesh Control Plane ─────────────────────────────────
	policyEngine := policy.NewEngine(logger)
	namespaceMgr := namespace.NewManager(logger)
	breakerRegistry := resilience.NewBreakerRegistry(resilience.BreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Cooldown:         30 * time.Second,
	})
	logger.Info("mesh control plane initialized",
		"components", []string{"policy-engine", "namespace-manager", "circuit-breaker-registry"},
	)

	// ── REST API Server (Agent Directory) ───────────────────
	var apiSrv *api.Server
	if *flagAPIListen != "" {
		var corsOrigins []string
		if *flagAPICORSOrigins != "" {
			corsOrigins = strings.Split(*flagAPICORSOrigins, ",")
		}
		apiSrv = api.New(api.Config{
			ListenAddr:      *flagAPIListen,
			CORSOrigins:     corsOrigins,
			Registry:        reg,
			Federation:      fed,
			PolicyEngine:    policyEngine,
			NamespaceMgr:    namespaceMgr,
			BreakerRegistry: breakerRegistry,
			Logger:          logger,
		})
		if err := apiSrv.Start(); err != nil {
			logger.Error("failed to start API server", "error", err)
			os.Exit(1)
		}
	}

	// ── Print Startup Info ───────────────────────────────────
	logger.Info("agentanycast-relay started",
		"version", version,
		"peer_id", h.ID().String(),
		"addresses", h.Addrs(),
		"max_reservations", *flagMaxReservations,
		"registry_listen", *flagRegistryListen,
		"registry_ttl", flagRegistryTTL.String(),
		"mcp_listen", *flagMCPListen,
		"metrics_listen", *flagMetricsListen,
		"api_listen", *flagAPIListen,
	)

	for _, addr := range h.Addrs() {
		fmt.Printf("RELAY_ADDR=%s/p2p/%s\n", addr, h.ID())
	}
	fmt.Printf("REGISTRY_ADDR=%s\n", *flagRegistryListen)
	if *flagMCPListen != "" {
		fmt.Printf("MCP_ADDR=%s\n", *flagMCPListen)
	}
	if *flagMetricsListen != "" {
		fmt.Printf("METRICS_ADDR=%s\n", *flagMetricsListen)
	}
	if *flagAPIListen != "" {
		fmt.Printf("API_ADDR=%s\n", *flagAPIListen)
	}

	// ── Wait for Shutdown ────────────────────────────────────
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("shutting down")
	if apiSrv != nil {
		apiSrv.Close()
	}
	if healthSrv != nil {
		healthSrv.Close()
	}
	if fed != nil {
		fed.Stop()
	}
	if mcpSrv != nil {
		mcpSrv.Close()
	}
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
