# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/), and this project adheres to [Semantic Versioning](https://semver.org/).

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

[0.1.0]: https://github.com/AgentAnycast/agentanycast-relay/releases/tag/v0.1.0
