# Services

This directory contains backend runtime services.

## Services

- `api` — Go + Gin application backend (business logic, REST API, WASM plugin runtime).
- `realtime` — Node.js + Socket.IO real-time event fan-out.
- `agent-runner` — Go AI agent execution service (Goose over ACP; also brokers `acp`-type dispatch to `apps/acp-bridge`).
- `agent-server` — Docker image for the Goose sandbox `agent-runner` spawns per conversation.

Service boundaries are documented in [../docs/architecture/service-boundaries.md](../docs/architecture/service-boundaries.md).
