# Contributing

Thank you for your interest in contributing to AgentAnycast!

Please see the [Contributing Guide](https://github.com/AgentAnycast/agentanycast/blob/main/CONTRIBUTING.md) in the main repository for guidelines on:

- Development workflow (fork → branch → PR → squash merge)
- Coding standards and commit message conventions
- CLA requirements

## Relay-Specific Guidelines

- This repository is licensed under FSL-1.1-ALv2, so a [CLA](https://github.com/AgentAnycast/agentanycast/blob/main/CLA.md) signature is required
- Run `go vet ./...` and `go test ./...` before submitting
- Test Docker builds locally: `docker build -t agentanycast/relay .`

## Required CI Checks

All of the following must pass before a PR can be merged:

- **vet** — `go vet ./...`
- **test** — Unit tests
- **build** — Binary build verification
- **docker** — Docker image build
