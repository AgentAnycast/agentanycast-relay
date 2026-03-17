# AgentAnycast Relay

Self-hosted circuit relay server for cross-network agent communication.

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/License-FSL--1.1--ALv2-blue)](LICENSE)

> **Deploy your own relay in one command.** AgentAnycast is fully decentralized -- you own your infrastructure.

## What is this?

When two agents are on different networks (behind NATs or firewalls), they can't connect directly. A relay server bridges them:

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
git clone https://github.com/agentanycast/agentanycast-relay.git
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
| `--log-level` | Log level (`debug`, `info`, `warn`, `error`) | `info` |
| `--version` | Print version and exit | |

## Resource Limits

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
