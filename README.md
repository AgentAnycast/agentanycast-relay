# AgentAnycast Relay

Self-hosted circuit relay server, skill registry, and federation hub for cross-network agent communication.

[![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-FSL--1.1--ALv2-blue)](LICENSE)

> **Deploy your own relay in one command.** AgentAnycast is fully decentralized -- you own your infrastructure.

## What is this?

The relay server provides three services:

1. **Circuit Relay** — bridges agents across different networks when direct connections aren't possible
2. **Skill Registry** — lets agents register their skills and discover each other by capability
3. **Federation** — synchronizes registrations across multiple relays for global discovery

```
Agent A (behind NAT)                              Agent B (behind NAT)
        │                                                 │
        └────► Relay Server (public IP) ◄─────────────────┘
                  │
                  ▼
         Hole punch attempt
                  │
        ┌─────────┴──────────┐
        ▼                    ▼
   Agent A ◄─── direct ───► Agent B    (if hole punch succeeds)
```

The relay **cannot read your traffic** -- all communication is end-to-end encrypted with Noise_XX. The relay only forwards opaque encrypted bytes.

**On a local network, you don't need a relay at all.** Agents discover each other via mDNS automatically.

## Deploy Your Own

### Docker Compose (recommended)

```bash
git clone https://github.com/AgentAnycast/agentanycast-relay.git
cd agentanycast-relay
docker-compose up -d
```

### From Source

```bash
go build -o relay ./cmd/relay
./relay --listen /ip4/0.0.0.0/tcp/4001 --key ./relay.key
```

### Get Your Relay Address

After starting, check the logs for `RELAY_ADDR`:

```bash
docker-compose logs relay
# Look for: RELAY_ADDR=/ip4/<YOUR_IP>/tcp/4001/p2p/12D3KooW...
```

Give this address to your agents:

```bash
export AGENTANYCAST_BOOTSTRAP_PEERS="/ip4/<YOUR_IP>/tcp/4001/p2p/12D3KooW..."
```

## CLI Flags

| Flag | Description | Default |
|---|---|---|
| `--listen` | Multiaddr to listen on | `/ip4/0.0.0.0/tcp/4001` |
| `--key` | Path to persistent identity key | (generates in-memory) |
| `--max-reservations` | Max concurrent relay reservations | `128` |
| `--registry-listen` | gRPC address for skill registry | `:50052` |
| `--registry-ttl` | Skill registration TTL | `30s` |
| `--enable-webtransport` | Enable WebTransport (QUIC-based) | `false` |
| `--mcp-listen` | MCP Streamable HTTP listen address | `:8080` |
| `--federation-peers` | Comma-separated peer relay gRPC addresses | (none) |
| `--federation-sync-interval` | Federation gossip sync interval | `10s` |
| `--log-level` | Log level (`debug`, `info`, `warn`, `error`) | `info` |
| `--version` | Print version and exit | |

## Skill Registry

The relay includes a **skill registry** that enables capability-based agent discovery.

### How It Works

1. Agent starts and connects to the relay
2. Agent registers its skills (e.g., `"translate"`, `"summarize"`)
3. Other agents query `discover("translate")` to find providers
4. Registry returns matching agents with their Peer IDs and metadata
5. Agents send heartbeats to keep registrations alive (TTL-based)
6. Peer disconnections automatically remove registrations

### Registry gRPC API (RegistryService)

| RPC | Description |
|---|---|
| `RegisterSkills` | Register skills with the relay. Returns expiration timestamp. |
| `UnregisterSkills` | Remove specific skill registrations. |
| `DiscoverBySkill` | Find agents offering a specific skill. Supports tag-based filtering and federated queries. |
| `Heartbeat` | Renew TTL on existing registrations. |

### Registry Limits

| Limit | Value |
|---|---|
| Max local registrations | 4096 |
| Max federated registrations | 8192 |
| Default discover limit | 100 |
| Hard discover limit | 1000 |
| Default TTL | 30 seconds |

## Multi-Relay Federation

Multiple relays can synchronize their registries using gossip-based federation, enabling global agent discovery across relay clusters.

### How It Works

1. Configure `--federation-peers` with peer relay addresses
2. Each relay periodically pulls updates from peers (`SyncRegistrations`)
3. New local registrations are optionally pushed to peers (`PushRegistrations`)
4. Conflicts are resolved using Last-Writer-Wins (LWW) with version counters
5. Local registrations always take priority over federated ones

### Federation gRPC API (FederationService)

| RPC | Description |
|---|---|
| `SyncRegistrations` | Pull registration updates since a given timestamp |
| `PushRegistrations` | Push local registration updates to a peer relay |

### Example: Two-Relay Federation

```bash
# Relay 1
./relay --key ./relay1.key --federation-peers "relay2.example.com:50052"

# Relay 2
./relay --key ./relay2.key --federation-peers "relay1.example.com:50052"
```

Agents registered on Relay 1 become discoverable via Relay 2, and vice versa.

## MCP Server

The relay exposes an MCP (Model Context Protocol) Streamable HTTP server, allowing AI assistants to query the registry.

| Tool | Description |
|---|---|
| `discover_agents` | Find agents offering a specific skill |
| `list_skills` | List all registered skills with agent counts |
| `get_relay_info` | Get relay status (peer ID, connections, registrations) |

Default endpoint: `http://localhost:8080`

## Relay Resource Limits

| Limit | Value | Description |
|---|---|---|
| Duration | 2 min | Max duration of a single relayed connection |
| Data | 128 KiB | Max data transferred per relayed connection |
| Reservations | 128 | Max concurrent relay reservations |
| Circuits | 16 | Max concurrent active relay circuits |
| Per peer | 4 | Max reservations per peer |
| Per IP | 8 | Max reservations per IP address |

## Hosting Recommendations

| Platform | Cost | Notes |
|---|---|---|
| **Oracle Cloud Free Tier** | $0/forever | 4 ARM cores, 24 GB RAM -- more than enough |
| DigitalOcean | $4/mo | Basic droplet |
| Fly.io | $0 (free tier) | 3 shared VMs |
| Any VPS with public IP | Varies | Ensure ports 4001 TCP+UDP and 50052 TCP are open |

**Important:** Always use `--key` with a persistent path so your relay's Peer ID survives restarts. If the Peer ID changes, all agents need to update their bootstrap configuration.

## Disclaimer

This software is provided "as is", without warranty of any kind. The relay forwards end-to-end encrypted traffic without the ability to inspect, decrypt, or store content.

## License

[FSL-1.1-ALv2](LICENSE) -- Functional Source License, Version 1.1, with Apache License, Version 2.0 as the future license. Each release converts to Apache 2.0 two years after its publication date.
