# Architecture Overview

Paca is a single open-source monorepo with a small set of clearly separated runtime surfaces.

## Runtime Areas

- `apps/web` — the user-facing application built with React, TanStack Start, and shadcn/ui.
- `apps/mcp` — the `@paca-ai/paca-mcp` MCP server; connects AI agents to the Paca data layer.
- `apps/e2e` — the end-to-end test suite built with Playwright; not a deployed runtime, but an external verifier of the full stack.
- `services/api` — the main application backend built with Go, Chi, and sqlx.
- `services/realtime` — the real-time delivery service built with Node.js and Socket.IO.
- `services/agent-runner` — the AI agent execution runtime built with Go, driving Goose over ACP for `llm`-type agents and brokering `acp`-type dispatch to `apps/acp-bridge`.

## Platform Dependencies

- PostgreSQL stores core transactional product data. See [database-schema.md](database-schema.md) for the full schema.
- Valkey carries cache, short-lived coordination state, and asynchronous event streams between backend runtimes.

## Interaction Model

- `apps/web` uses HTTP APIs exposed by `services/api` for request-response workflows.
- `apps/web` connects to `services/realtime` over Socket.IO for live updates.
- `apps/mcp` calls `services/api` over HTTP using an API key; plugin tools route to `/api/v1/plugins/{pluginId}/…`.
- `apps/e2e` drives a real browser against the full running stack and validates cross-cutting flows that span multiple runtime surfaces.
- `services/api` remains the system of record for product state and publishes real-time relevant domain events to a Valkey Stream.
- `services/realtime` consumes Valkey Stream messages from `services/api` and fans out client-safe events to connected Socket.IO rooms and users.
- `services/agent-runner` reads agent trigger events from a Valkey Stream, spawns a Docker container per `llm`-type conversation and drives it over ACP, and publishes conversation events back to Valkey.

## Architectural Intent

- Keep service boundaries explicit.
- Keep state-changing business logic in `services/api`, not in the real-time edge service.
- Use Valkey Streams to decouple event production from Socket.IO delivery.
- Avoid adding shared layers before reuse is proven.
- Separate product-facing documentation from implementation-facing documentation.
- Keep the repository easy to read in public from the root.

For the shared sprint, backlog, and timeline view model, see [interaction-views.md](interaction-views.md).

For the automation-workflow dependency graph and execution engine, see [automation-workflows.md](automation-workflows.md).
