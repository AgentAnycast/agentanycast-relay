# AgentAnycast Relay

**Production-ready relay server for AgentAnycast P2P networks.**

[![CI](https://github.com/AgentAnycast/agentanycast-relay/actions/workflows/ci.yml/badge.svg)](https://github.com/AgentAnycast/agentanycast-relay/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)](https://hub.docker.com/r/agentanycast/relay)
[![License](https://img.shields.io/badge/License-FSL--1.1--ALv2-blue)](LICENSE)

The relay bridges agents across different networks when direct connections are not possible. It provides circuit relay, skill-based discovery, multi-relay federation, and a web dashboard -- all deployable in 30 seconds.

> **Privacy by design.** The relay cannot read your traffic -- all communication is end-to-end encrypted with NaCl box. It only forwards opaque encrypted bytes.

> **On a local network, you don't need a relay at all.** Agents discover each other via mDNS automatically.

## Deploy in 30 Seconds

```bash
git clone https://github.com/AgentAnycast/agentanycast-relay.git
cd agentanycast-relay
docker-compose up -d
```

Then give your agents the relay address from the logs:

```bash
docker-compose logs relay
# Look for: RELAY_ADDR=/ip4/<YOUR_IP>/tcp/4001/p2p/12D3KooW...

export AGENTANYCAST_BOOTSTRAP_PEERS="/ip4/<YOUR_IP>/tcp/4001/p2p/12D3KooW..."
```

### From Source

```bash
go build -o relay ./cmd/relay
./relay --listen /ip4/0.0.0.0/tcp/4001 --key ./relay.key
```

## Features

| Feature | Description |
|---|---|
| **Circuit Relay v2** | Bridges agents across NATs; facilitates hole punching for direct connections |
| **Skill Registry** | Agents register skills and discover each other by capability (gRPC API) |
| **Federation** | Gossip-based sync across multiple relays for global discovery |
| **Agent Directory** | REST API + embedded web UI for browsing registered agents |
| **MCP Server** | Streamable HTTP endpoint for AI assistant integration |
| **Health & Metrics** | `/health` JSON endpoint, `/metrics` Prometheus endpoint |
| **OpenTelemetry** | Distributed tracing with OTLP exporter |

```
Agent A (behind NAT)                              Agent B (behind NAT)
        |                                                 |
        +-------> Relay Server (public IP) <--------------+
                  |
                  v
         Hole punch attempt
                  |
        +---------+-----------+
        v                     v
   Agent A <--- direct ---> Agent B    (if hole punch succeeds)
```

## Hosting Recommendations

| Platform | Cost | Notes |
|---|---|---|
| **Oracle Cloud Free Tier** | **$0/forever** | 4 ARM cores, 24 GB RAM -- more than enough |
| Fly.io | $0 (free tier) | 3 shared VMs |
| DigitalOcean | $4/mo | Basic droplet |
| Any VPS with public IP | Varies | Ensure ports 4001, 50052, 8081, 9090 are open |

> **Important:** Always use `--key` with a persistent path so your relay's Peer ID survives restarts. If the Peer ID changes, all agents need to update their bootstrap configuration.

## CLI Flags

| Flag | Default | Description |
|---|---|---|
| `--listen` | `/ip4/0.0.0.0/tcp/4001` | Multiaddr to listen on |
| `--key` | (in-memory) | Path to persistent identity key |
| `--max-reservations` | `128` | Max concurrent relay reservations |
| `--registry-listen` | `:50052` | gRPC address for skill registry |
| `--registry-ttl` | `30s` | Skill registration TTL |
| `--enable-webtransport` | `false` | Enable WebTransport (QUIC-based) |
| `--mcp-listen` | `:8080` | MCP Streamable HTTP listen address |
| `--federation-peers` | (none) | Comma-separated peer relay gRPC addresses |
| `--federation-sync-interval` | `10s` | Federation gossip sync interval |
| `--metrics-listen` | `:9090` | Health/metrics HTTP address |
| `--api-listen` | `:8081` | Agent directory API address |
| `--otlp-endpoint` | (disabled) | OTLP collector endpoint |
| `--log-level` | `info` | Log level (`debug`, `info`, `warn`, `error`) |
| `--version` | | Print version and exit |

## Skill Registry

The registry enables capability-based agent discovery:

1. Agent connects to the relay and registers skills (e.g., `"translate"`, `"summarize"`)
2. Other agents query `discover("translate")` to find providers
3. Registry returns matching agents with Peer IDs and metadata
4. Heartbeats keep registrations alive; peer disconnections auto-remove them

### Registry gRPC API

| RPC | Description |
|---|---|
| `RegisterSkills` | Register skills with the relay. Returns expiration timestamp. |
| `UnregisterSkills` | Remove specific skill registrations. |
| `DiscoverBySkill` | Find agents by skill. Supports tag filtering and federated queries. |
| `Heartbeat` | Renew TTL on existing registrations. |

### Registry Limits

| Limit | Value |
|---|---|
| Max local registrations | 4,096 |
| Max federated registrations | 8,192 |
| Default discover limit | 100 |
| Hard discover limit | 1,000 |
| Default TTL | 30 seconds |

## Multi-Relay Federation

Multiple relays synchronize registries via gossip, enabling global agent discovery across relay clusters.

```bash
# Relay 1
./relay --key ./relay1.key --federation-peers "relay2.example.com:50052"

# Relay 2
./relay --key ./relay2.key --federation-peers "relay1.example.com:50052"
```

Agents registered on Relay 1 become discoverable via Relay 2, and vice versa.

### Federation gRPC API

| RPC | Description |
|---|---|
| `SyncRegistrations` | Pull registration updates since a given timestamp |
| `PushRegistrations` | Push local registration updates to a peer relay |

**Conflict resolution:** Last-Writer-Wins (LWW) with version counters. Local registrations always take priority.

## Agent Directory

REST API and embedded web UI for browsing registered agents. Default address: `:8081`.

| Endpoint | Description |
|---|---|
| `GET /api/v1/agents` | List all registered agents with skills and metadata |
| `GET /api/v1/skills` | List all registered skills with agent counts |
| `GET /api/v1/stats` | Relay statistics (connections, registrations, uptime) |

Open `http://localhost:8081` in a browser to access the searchable agent directory dashboard.

## MCP Server

AI assistants can query the registry via the MCP Streamable HTTP endpoint (default: `http://localhost:8080`).

| Tool | Description |
|---|---|
| `discover_agents` | Find agents offering a specific skill |
| `list_skills` | List all registered skills with agent counts |
| `get_relay_info` | Get relay status (peer ID, connections, registrations) |

## Health & Monitoring

Health and metrics endpoints are available on a configurable HTTP port (default `:9090`):

```bash
curl http://localhost:9090/health    # JSON: uptime, peer count, registrations
curl http://localhost:9090/metrics   # Prometheus metrics
```

### OpenTelemetry

```bash
./relay --otlp-endpoint localhost:4317
```

A pre-built Grafana dashboard is included in `deploy/grafana/` for visualizing connection counts, registration churn, federation sync latency, and resource utilization.

## Relay Resource Limits

| Limit | Value | Description |
|---|---|---|
| Duration | 2 min | Max duration of a single relayed connection |
| Data | 128 KiB | Max data transferred per relayed connection |
| Reservations | 128 | Max concurrent relay reservations |
| Circuits | 16 | Max concurrent active relay circuits |
| Per peer | 4 | Max reservations per peer |
| Per IP | 8 | Max reservations per IP address |

## Disclaimer

This software is provided "as is", without warranty of any kind. The relay forwards end-to-end encrypted traffic without the ability to inspect, decrypt, or store content.

## License

[FSL-1.1-ALv2](LICENSE) -- Functional Source License, Version 1.1, with Apache License, Version 2.0 as the future license. Each release converts to Apache 2.0 two years after its publication date.
