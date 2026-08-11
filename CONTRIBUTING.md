# Contributing to Paca

Thanks for contributing to Paca.

## Repository Shape

- `apps/web` — React + TanStack Start + shadcn/ui frontend.
- `apps/mcp` — MCP server (`@paca-ai/paca-mcp` npm package).
- `apps/e2e` — Playwright end-to-end test suite.
- `services/api` — Go + Gin application backend.
- `services/realtime` — Node.js + Socket.IO real-time event fan-out.
- `services/agent-runner` — Go AI agent execution service (Goose over ACP; also brokers `acp`-type dispatch to `apps/acp-bridge`).
- `services/agent-server` — Docker image for the Goose sandbox agent-runner spawns per conversation.
- `docs` — architecture, guides, API, deployment, and plugin documentation.
- `deploy` — Docker Compose files and environment templates.
- `plugins` — local plugin store (WASM + frontend bundles).
- `scripts` — install and plugin management scripts.

## Development Setup

See [docs/guides/local-development.md](docs/guides/local-development.md) for a complete walkthrough of the local dev environment.

**Quick start:**

```bash
# Start infrastructure
docker compose -f deploy/docker-compose.dev.yml up -d postgres valkey

# Run the API
cd services/api && make run

# Run the web app
cd apps/web && bun install && bun run dev
```

## Contribution Guidelines

- Keep pull requests focused on one concern.
- Explain the reasoning behind non-obvious changes.
- Update relevant docs when changing behavior or interfaces.
- Avoid premature abstraction — add it when reuse is proven.

## Pull Request Checklist

- The change is scoped to one concern.
- Tests are added or updated for changed behaviour.
- Related documentation is updated.
- New decisions or non-obvious tradeoffs are explained clearly.

## Discussion Areas

- Product workflow and user experience.
- Service responsibilities and system boundaries.
- Plugin system design and extension points.
- AI agent behaviour and collaboration model.
- Open-source governance and contributor experience.