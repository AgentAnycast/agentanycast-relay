// Package api provides a REST API for querying the relay's agent registry.
//
// Endpoints:
//
//	GET /api/v1/agents          — list all registered agents
//	GET /api/v1/agents/:peer_id — agent details
//	GET /api/v1/skills          — list all skills with agent counts
//	GET /api/v1/stats           — registry statistics
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/agentanycast/agentanycast-relay/internal/federation"
	"github.com/agentanycast/agentanycast-relay/internal/registry"
)

// Config holds API server configuration.
type Config struct {
	ListenAddr  string
	CORSOrigins []string // Allowed CORS origins; empty defaults to same-origin only.
	Registry    *registry.Registry
	Federation  *federation.Federation // optional
	Logger      *slog.Logger
}

// Server serves the REST API.
type Server struct {
	httpServer  *http.Server
	registry    *registry.Registry
	federation  *federation.Federation
	logger      *slog.Logger
	corsOrigins []string
}

// New creates a new API server.
func New(cfg Config) *Server {
	corsOrigins := cfg.CORSOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{} // no CORS headers → same-origin only
	}

	s := &Server{
		registry:    cfg.Registry,
		federation:  cfg.Federation,
		logger:      cfg.Logger,
		corsOrigins: corsOrigins,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/v1/agents/{peer_id}", s.handleGetAgent)
	mux.HandleFunc("GET /api/v1/skills", s.handleListSkills)
	mux.HandleFunc("GET /api/v1/stats", s.handleStats)

	// Serve embedded SPA for all non-API paths.
	mux.Handle("/", WebHandler())

	// Wrap with CORS middleware.
	handler := s.corsMiddleware(mux)

	s.httpServer = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start begins serving the API.
func (s *Server) Start() error {
	s.logger.Info("API server started", "addr", s.httpServer.Addr)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logger.Error("API server exited with error", "error", err)
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

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) isAllowedOrigin(origin string) bool {
	for _, allowed := range s.corsOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}
