// Package health provides HTTP endpoints for relay health monitoring.
//
// It exposes /health (JSON health status) and /metrics (Prometheus) on a
// configurable listen address, enabling integration with monitoring stacks
// like Prometheus + Grafana.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/agentanycast/agentanycast-relay/internal/federation"
	"github.com/agentanycast/agentanycast-relay/internal/registry"
)

// Status represents the overall health status of the relay.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
)

// Response is the JSON structure returned by the /health endpoint.
type Response struct {
	Status        Status                      `json:"status"`
	Version       string                      `json:"version"`
	PeerID        string                      `json:"peer_id"`
	UptimeSeconds float64                     `json:"uptime_seconds"`
	Registry      registry.RegistryStats      `json:"registry"`
	Federation    *federation.FederationHealth `json:"federation,omitempty"`
	Libp2p        Libp2pHealth                `json:"libp2p"`
}

// Libp2pHealth holds libp2p host statistics.
type Libp2pHealth struct {
	ConnectedPeers int `json:"connected_peers"`
}

// Server serves /health and /metrics HTTP endpoints.
type Server struct {
	httpServer *http.Server
	startTime  time.Time
	version    string
	host       host.Host
	registry   *registry.Registry
	federation *federation.Federation // may be nil
	logger     *slog.Logger
}

// Config holds the configuration for the health server.
type Config struct {
	ListenAddr string
	Version    string
	Host       host.Host
	Registry   *registry.Registry
	Federation *federation.Federation // optional
	Logger     *slog.Logger
}

// New creates a new health/metrics server.
func New(cfg Config) *Server {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		startTime:  time.Now(),
		version:    cfg.Version,
		host:       cfg.Host,
		registry:   cfg.Registry,
		federation: cfg.Federation,
		logger:     logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.Handle("GET /metrics", promhttp.Handler())

	s.httpServer = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start begins serving health and metrics endpoints.
// ListenAndServe errors (other than ErrServerClosed) are logged as fatal.
func (s *Server) Start() error {
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("health server exited with error", "error", err)
		}
	}()
	return nil
}

// Close gracefully shuts down the server.
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	resp := Response{
		Status:        StatusHealthy,
		Version:       s.version,
		PeerID:        s.host.ID().String(),
		UptimeSeconds: time.Since(s.startTime).Seconds(),
		Registry:      s.registry.Stats(),
		Libp2p: Libp2pHealth{
			ConnectedPeers: len(s.host.Network().Peers()),
		},
	}

	if s.federation != nil {
		fh := s.federation.HealthStatus()
		resp.Federation = &fh

		// Degraded when any federation peer is unhealthy.
		if fh.Peers > 0 && fh.HealthyPeers < fh.Peers {
			resp.Status = StatusDegraded
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp) //nolint:errcheck
}
