# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.7.0] - 2026-03-20

### Added

- OpenTelemetry distributed tracing with configurable exporters
- `/health` and `/metrics` HTTP endpoints for monitoring
- REST API (`/api/v1/agents`, `/api/v1/skills`, `/api/v1/stats`) for agent directory
- Embedded Preact web UI for browsing registered agents and skills
- Grafana dashboard templates for relay monitoring
- Prometheus monitoring stack with docker-compose integration

## [0.6.0] - 2026-03-19

### Added

- MCP server integration with stdio and Streamable HTTP transports
- Three MCP tools for skill discovery and relay management

### Changed

- Bumped MCP server version to 0.6.0

## [0.5.0] - 2026-03-19

### Added

- Inverted skill index for O(1) skill lookups in the registry
- Tag-based filtering for fine-grained skill discovery

### Changed

- Parallel federation sync for faster cross-relay convergence
- Circuit breaker for resilient federation peer connections

## [0.4.0] - 2026-03-19

### Added

- Multi-relay federation with gossip-based registry synchronization
- Peer exchange protocol for automatic federation topology discovery

### Fixed

- Federation safety and correctness issues identified during audit

## [0.3.0] - 2026-03-18

### Added

- MCP server with stdio transport for AI tool integration
- Streamable HTTP transport for remote MCP access
- Three MCP tools: discover skills, list agents, relay status

## [0.2.0] - 2026-03-17

### Added

- Skill registry gRPC service (RegisterSkills, UnregisterSkills, DiscoverBySkill, Heartbeat)
- TTL-based skill registration expiry with automatic cleanup
- Input validation for skill registration requests
- Registry unit tests and integration test suite

## [0.1.0] - 2026-03-17

### Added

- Circuit Relay v2 server with configurable resource limits
- Dual-stack transport: TCP + QUIC
- Persistent Ed25519 identity key with auto-generation
- Docker support with multi-stage build and docker-compose
- Structured JSON logging with configurable log levels
- Graceful shutdown on SIGINT/SIGTERM
- CLI flags for listen address, key path, max reservations, and log level
- CI pipeline with vet, build, and Docker build verification

[0.7.0]: https://github.com/AgentAnycast/agentanycast-relay/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/AgentAnycast/agentanycast-relay/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/AgentAnycast/agentanycast-relay/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/AgentAnycast/agentanycast-relay/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/AgentAnycast/agentanycast-relay/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/AgentAnycast/agentanycast-relay/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/AgentAnycast/agentanycast-relay/releases/tag/v0.1.0
