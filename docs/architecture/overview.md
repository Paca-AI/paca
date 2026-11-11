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
- `services/api` authorizes every request with IAM-style role policies (default deny, explicit `Deny` wins) evaluated per route and per resource; see [authorization](authorization.md).
- `services/api` remains the system of record for product state. For every change it publishes a domain event on the `paca.events` Valkey Pub/Sub channel and appends it to a capped, per-project change log (a Valkey Stream with a gapless sequence number), after the activity and plugin streams.
- `services/realtime` fans those events out to connected Socket.IO rooms and users after the API confirms the account may read each one. A client that joins with the sync protocol reads the change log from its own cursor, so it resumes after any absence without refetching; see [the realtime service](../../services/realtime/README.md#sync-protocol-resumable-change-feed).
- `apps/web` applies a change to its React Query cache (patching the entity where the change carries it, otherwise refetching only what the change names) instead of reloading lists.
- `services/agent-runner` reads agent trigger events from a Valkey Stream, spawns a Docker container per `llm`-type conversation and drives it over ACP, and publishes conversation events back to Valkey.

## Architectural Intent

- Keep service boundaries explicit.
- Keep state-changing business logic in `services/api`, not in the real-time edge service.
- Use Valkey (Pub/Sub for the immediate nudge, a per-project stream for replay) to decouple event production from Socket.IO delivery.
- Avoid adding shared layers before reuse is proven.
- Separate product-facing documentation from implementation-facing documentation.
- Keep the repository easy to read in public from the root.

For the shared sprint, backlog, and timeline view model, see [interaction-views.md](interaction-views.md).

For the automation-workflow dependency graph and execution engine, see [automation-workflows.md](automation-workflows.md).
