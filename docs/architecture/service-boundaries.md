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
- translating MCP tool calls into REST calls to `services/api`, which enforces permissions per call (user mode and agent mode alike) and returns 403 for anything the caller isn't allowed to do;
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
- authentication and authorization (JWT, API keys, IAM-style role policies evaluated per route and per resource; see [authorization](authorization.md));
- persistence coordination with PostgreSQL and Valkey;
- S3-compatible file attachment handling (RustFS or AWS S3);
- WASM plugin runtime (wazero) — loads backend plugins, registers routes, mediates host function calls;
- publication of domain events to Valkey Streams for downstream consumers;
- agent trigger event publication and conversation summary ingestion.

## services/realtime

Responsible for real-time delivery to connected clients.

Concerns:

- Socket.IO namespaces, rooms, and client connection lifecycle;
- authentication and authorization of socket connections using contracts from `services/api`;
- consumption of the `paca.events` Pub/Sub channel and the API's per-project change log (a Valkey Stream), which gives a client an ordered feed it can resume from its last applied change;
- per-event authorization: the API's IAM engine, not this service, decides whether an account may see an event, and an account with no access to a project gets no feed of it at all;
- transformation of internal domain events into client-safe real-time payloads;
- broadcast of updates for boards, tasks, comments, agent conversation events, and collaboration signals (Yjs relay for docs and task descriptions).

It holds no business rules and no durable state: sessions and collaboration rooms live in memory, so more than one replica needs the collaboration routing described in its README.

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

## Convention: real-time events over polling

If a page needs to reflect state that can change from outside the current tab — another user's edit, an async worker finishing, a background job — wire a `services/realtime` event for it. Do not fall back to `refetchInterval` or a `setInterval` re-fetch loop as the way a page learns data changed; a fixed-interval poll wastes requests on every tick where nothing changed and still lags reality by up to the interval on the one tick where something did.

The established pattern, followed throughout `apps/web`:

1. `services/api` publishes a domain event at the point a state transition is persisted, through `events.Fanout` (events that also belong in the activity log) or `events.PublishRealtime` (realtime only: a view reorder, an environment status change, a doc folder, a member's role change). Both put it on the `paca.events` Valkey Pub/Sub channel and append it to the project's change log with a `seq` — see `internal/events/topics.go` for the topic convention (`<domain>.<verb>`, e.g. `sprint.updated`, `environment.status_changed`). Where a client can patch its cache with it, the call attaches the entity as it is after the change (`events.Event.Snapshot`).
2. `services/realtime` delivers the event to the sockets of a namespace-scoped Socket.IO room whose account the API confirms may read the event's resource, using the `authz` descriptor `events.AuthzFor` derives (`src/subscriber.ts`, `src/access.ts`). A client that joined with `sync:join` gets it from the change log instead, in order and resumable (`src/sync.ts`).
3. `apps/web` handles it in one place: the ordered rule table `EVENT_RULES` in `src/lib/project-event-rules.ts`, driven by `useProjectRealtime` on the shared project socket — no bespoke socket listener per component. A rule patches the cache from the entity (`src/lib/cache-patch.ts`) or invalidates only the queries the event names.

Adding a new event type touches three tables, each owning one question, and each has a test that fails until the new type is placed in it: what IAM must allow (`events.AuthzFor`, enforced by `TestEveryTopicIsClassified`), which room that belongs to (`NAMESPACE_PERMISSIONS` / `eventNamespace` in `services/realtime/src/permissions.ts`), and what the web cache does (`EVENT_RULES`, pinned by `project-event-rules.test.ts`). `environment.status_changed` (replacing a 2-second `refetchInterval` that polled `GET .../environments/:id` while an environment was creating/starting/stopping) is a template to copy.

When the event can carry the new state, prefer patching over invalidating: an invalidation refetches a list for every client for every change, a patch is free. Patch only what is the same for every viewer (a task, a sprint, an environment status), never what is personal to the actor (a view's `config`), and never what another mechanism owns (a task's description belongs to collaborative editing). Lists are filtered on the server, so a patched list is refetched only when the change could move the entity in or out of it.

Narrow exceptions that are *not* polling-for-data and should stay as-is:

- **Heartbeats** — a client telling the server "I'm still here" to keep an idle timer from expiring (`heartbeatConversation`, `environment-terminal.tsx`'s `heartbeatTimer`). This is the client pushing liveness, not the client pulling for fresh data.
- **Dropped-message safety nets** — a long-running, high-stakes view (an in-flight agent conversation) may keep a low-frequency reconciliation tick *in addition to* its real-time listener, purely to self-heal from an occasional missed socket message, not as the primary way it learns about changes (see `conversation-view.tsx`'s `CONVERSATION_RECONCILE_INTERVAL_MS`). If you add one of these, comment it as a safety net explicitly, and make sure a real-time listener already covers the common case first.
