# Service Boundaries

Paca consists of one frontend application, an MCP server, and three backend services.

## apps/web

Responsible for the user-facing product experience.

Concerns:

- authentication and session-driven UI flow;
- board, backlog, and sprint management interfaces;
- human and AI agent collaboration views;
- real-time board updates via Socket.IO;
- product-facing components built with React, TanStack Start, and shadcn/ui.

## apps/mcp

Responsible for AI agent integration via the Model Context Protocol.

Concerns:

- MCP server implementation (`@paca-ai/paca-mcp` npm package);
- translating MCP tool calls into REST calls to `services/api`;
- permission-based tool filtering (user mode and agent mode);
- dynamic loading of plugin-contributed MCP tools at startup;
- BlockNote ↔ Markdown format conversion for descriptions and documents.

## apps/e2e

Responsible for end-to-end validation of the full running stack from a real browser.

Concerns:

- Playwright test suites exercising cross-cutting flows spanning `apps/web`, `services/api`, and the Caddy gateway;
- test categories: auth flows, form validation, security (injection/XSS rejection), session management, and UX correctness;
- Page Object Models and shared fixtures to keep test logic stable as the UI evolves;
- global setup that logs in once and persists browser auth state.

Not deployed. Runs against a live environment (local or CI stack) and produces an HTML report with traces and screenshots on failure.

## services/api

Responsible for the core application backend.

Concerns:

- business workflows (tasks, sprints, boards, members, documents, custom fields);
- authentication and authorization (JWT, API keys, role-based permissions);
- persistence coordination with PostgreSQL and Valkey;
- S3-compatible file attachment handling (MinIO or AWS S3);
- WASM plugin runtime (wazero) — loads backend plugins, registers routes, mediates host function calls;
- publication of domain events to Valkey Streams for downstream consumers;
- agent trigger event publication and conversation summary ingestion.

## services/realtime

Responsible for real-time delivery to connected clients.

Concerns:

- Socket.IO namespaces, rooms, and client connection lifecycle;
- authentication and authorization of socket connections using contracts from `services/api`;
- consumption of Valkey Stream messages emitted by `services/api`;
- transformation of internal domain events into client-safe real-time payloads;
- broadcast of updates for boards, tasks, comments, agent conversation events, and collaboration signals.

## services/agent-runner

Responsible for AI agent execution. Replaced `services/ai-agent` (Python/OpenHands), which has been fully removed from the repository — see [agent-runner-service.md](../ai-agent/agent-runner-service.md) for its implementation.

Concerns:

- consumption of agent trigger events from the `paca:agent:triggers` Valkey Stream;
- spawning and managing Docker containers running Goose (one container per active `llm`-type conversation), driven over ACP;
- brokering `acp`-type agent dispatch to a user's own `apps/acp-bridge` daemon over WebSocket;
- writing conversation status and events directly to Postgres (`agent_conversations`, `agent_conversation_events`) — see the Boundary Rule below;
- publishing conversation events to the `paca:agent:events` Valkey Stream and the `paca.events` Pub/Sub channel for real-time delivery;
- pause/resume/stop/heartbeat via control messages on `paca:agent:triggers`, not REST endpoints;
- repository access via the repository plugin adapter, mediated by tool calls the agent itself makes against the built-in Paca MCP server (short-lived tokens, no persistent credential storage).

## Boundary Rule

Keep ownership clear. `services/api` owns business rules and most durable state transitions. `services/realtime` only delivers live updates derived from API-owned events. `agent_conversations`/`agent_conversation_events` are a deliberate exception to "only `services/api` writes to the database": `services/api` creates the initial conversation row and publishes the trigger, then `services/agent-runner` writes status transitions and every event directly for the rest of that conversation's lifetime, rather than proxying each one through `services/api` — a round-trip per event would add latency with no ownership benefit, since `agent-runner` is the only thing that knows a given event happened as it happens. Shared code stays inside the owning runtime until duplication is real and proven.
