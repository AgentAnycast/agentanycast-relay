# AgentAnycast Relay

Self-hosted circuit relay server and skill registry for cross-network agent communication.

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-FSL--1.1--ALv2-blue)](LICENSE)

> **Deploy your own relay in one command.** AgentAnycast is fully decentralized -- you own your infrastructure.

## What is this?

The relay server provides two services:

1. **Circuit Relay** — bridges agents across different networks when direct connections aren't possible
2. **Skill Registry** — lets agents register their skills and discover each other by capability

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
# On each agent node:
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
| `--log-level` | Log level (`debug`, `info`, `warn`, `error`) | `info` |
| `--version` | Print version and exit | |

## Skill Registry

The relay includes a **skill registry** that enables capability-based agent discovery. Agents register their skills with the relay, and other agents can query by skill ID to find providers.

### How It Works

1. Agent starts and connects to the relay
2. Agent registers its skills (e.g., `"translate"`, `"summarize"`)
3. Other agents query `discover("translate")` to find providers
4. Registry returns matching agents with their Peer IDs and metadata
5. Agents send heartbeats to keep registrations alive (TTL-based)

### Registry gRPC API

| RPC | Description |
|---|---|
| `RegisterSkills` | Register skills with the relay. Returns expiration timestamp. |
| `UnregisterSkills` | Remove specific skill registrations. |
| `DiscoverBySkill` | Find agents offering a specific skill. Supports tag-based filtering. |
| `Heartbeat` | Renew TTL on existing registrations. |

### Registry Limits

| Limit | Value |
|---|---|
| Max registrations | 4096 |
| Default discover limit | 100 |
| Hard discover limit | 1000 |
| Default TTL | 30 seconds |

### Tag-Based Filtering

Skills can include tags for fine-grained matching:

```python
# Agent registers with tags
Skill(id="translate", description="...", tags={"lang": "en,fr,de"})

# Another agent discovers with tag filter
agents = await node.discover("translate", tags={"lang": "fr"})
```

## Relay Resource Limits

The relay enforces per-peer and global limits to prevent abuse:

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
| Any VPS with public IP | Varies | Ensure ports 4001 TCP+UDP are open |

**Important:** Always use `--key` with a persistent path so your relay's Peer ID survives restarts. If the Peer ID changes, all agents need to update their bootstrap configuration.

## Disclaimer

This software is provided "as is", without warranty of any kind. The relay forwards end-to-end encrypted traffic without the ability to inspect, decrypt, or store content.

## License

[FSL-1.1-ALv2](LICENSE) -- Functional Source License, Version 1.1, with Apache License, Version 2.0 as the future license. Each release converts to Apache 2.0 two years after its publication date.
