// Package mcp implements a Streamable HTTP MCP server for the relay.
// It exposes the relay's skill registry and P2P connectivity as MCP tools,
// enabling AI assistants (Claude, ChatGPT, Cursor, etc.) to discover and
// communicate with agents on the P2P network.
package mcp

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/agentanycast/agentanycast-relay/internal/registry"
)

// Server wraps the MCP server and its HTTP transport.
type Server struct {
	mcpServer  *server.MCPServer
	httpServer *server.StreamableHTTPServer
	listener   net.Listener
	listenAddr string
	registry   *registry.Registry
	host       host.Host
	logger     *slog.Logger
}

// Config holds MCP server configuration.
type Config struct {
	// ListenAddr is the HTTP address to listen on (default ":8080").
	ListenAddr string
	// Registry is the skill registry to query for discovery.
	Registry *registry.Registry
	// Host is the libp2p host for connecting to peers.
	Host host.Host
	// Logger is the structured logger.
	Logger *slog.Logger
}

// New creates a new MCP server with all tools registered.
func New(cfg Config) (*Server, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	mcpSrv := server.NewMCPServer(
		"AgentAnycast Relay",
		"0.6.0",
		server.WithToolCapabilities(true),
	)

	s := &Server{
		mcpServer:  mcpSrv,
		listenAddr: cfg.ListenAddr,
		registry:   cfg.Registry,
		host:       cfg.Host,
		logger:     cfg.Logger,
	}

	s.registerTools()

	s.httpServer = server.NewStreamableHTTPServer(mcpSrv)

	return s, nil
}

// Start begins listening for MCP requests over Streamable HTTP.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("MCP server listen: %w", err)
	}
	s.listener = ln

	s.logger.Info("MCP server started",
		"addr", ln.Addr().String(),
		"transport", "streamable-http",
	)

	go func() {
		if err := http.Serve(ln, s.httpServer); err != nil && err != http.ErrServerClosed {
			s.logger.Error("MCP server error", "error", err)
		}
	}()

	return nil
}

// Addr returns the listener's address, or empty if not started.
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// Close shuts down the MCP server.
func (s *Server) Close() error {
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// registerTools adds all MCP tools to the server.
func (s *Server) registerTools() {
	s.mcpServer.AddTool(
		mcp.NewTool("discover_agents",
			mcp.WithDescription("Find AI agents on the P2P network that offer a specific skill. Returns a list of agents with their PeerID, name, and skill details."),
			mcp.WithString("skill_id",
				mcp.Required(),
				mcp.Description("The skill identifier to search for (e.g. 'translate', 'summarize', 'code-review')"),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of results to return (default: 20, max: 100)"),
			),
		),
		s.handleDiscoverAgents,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("list_skills",
			mcp.WithDescription("List all skills currently registered on the P2P network. Shows unique skill IDs and how many agents offer each one."),
		),
		s.handleListSkills,
	)

	s.mcpServer.AddTool(
		mcp.NewTool("get_relay_info",
			mcp.WithDescription("Get information about this relay node: PeerID, listen addresses, connected peers count, and registered agent count."),
		),
		s.handleGetRelayInfo,
	)
}
