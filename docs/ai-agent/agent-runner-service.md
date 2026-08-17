# Agent Runner — `services/agent-runner` Implementation

The `services/agent-runner` service is a Go service responsible for executing `llm`-type agent conversations using [Goose](https://github.com/block/goose) (via [ACP](https://agentclientprotocol.com/), the Agent Client Protocol) as the execution engine, and for brokering `acp`-type agent conversations to a user's own local coding CLI via `apps/acp-bridge`. It consumes trigger events from a Valkey Stream, manages Docker container lifecycles for `llm`-type agents, and streams conversation events back.

It replaced `services/ai-agent` (Python, OpenHands SDK), which has been fully removed from the repository.

## Table of Contents

- [Technology Stack](#technology-stack)
- [Project Structure](#project-structure)
- [Valkey Stream Protocol](#valkey-stream-protocol)
- [Conversation Execution](#conversation-execution)
- [Docker Container Strategy](#docker-container-strategy)
- [Repository Access](#repository-access)
- [Skills & MCP Server Injection](#skills--mcp-server-injection)
- [Pause / Resume / Stop / Heartbeat](#pause--resume--stop--heartbeat)
- [HTTP Surface](#http-surface)
- [Environment Variables](#environment-variables)

---

## Technology Stack

| Component | Choice | Reason |
|---|---|---|
| Execution engine | [Goose](https://github.com/block/goose) `goose serve`, driven over ACP (HTTP + SSE) | No Go SDK equivalent to OpenHands exists; ACP is a stable, documented wire protocol Goose implements natively |
| Container orchestration | Docker Engine API (`github.com/moby/moby/client`) | Direct control over container lifecycle — no framework wrapper equivalent to OpenHands' `DockerWorkspace` exists for Go |
| Stream consumer/producer | `github.com/redis/go-redis/v9` (Valkey-compatible) | Consume `paca:agent:triggers`, publish `paca:agent:events` |
| DB client | `github.com/jmoiron/sqlx` + `github.com/jackc/pgx/v5` | Read agent config, write conversation status/events |
| HTTP | Go stdlib `net/http` | Serves the ACP bridge WebSocket + two internal endpoints + `/llm/models`; no framework needed for this small a surface |

---

## Project Structure

```
services/agent-runner/
├── go.mod / go.sum
├── Dockerfile
├── cmd/agent-runner/
│   └── main.go                    # wiring: config, repos, executor, consumer, HTTP server
├── internal/
│   ├── acp/                       # ACP-over-HTTP+SSE client (initialize, session/new, session/prompt)
│   ├── acpbridge/                 # acp-type dispatch: WebSocket registry + HTTP server
│   ├── agent/                     # domain types: Config, Trigger, Skill, MCPServer, EnvVar
│   ├── chatsandbox/                # process-local registry of paused chat sandboxes (continuity across turns)
│   ├── config/                    # env var settings + the per-agent rollout Gate
│   ├── executor/                  # ties sandbox lifecycle + acp.Client into one conversation turn
│   ├── handler/                   # per-trigger dispatch: Gate check, executor.Run, event persistence/publishing
│   ├── messaging/                 # Valkey stream consumer, control-message routing, event/status publisher
│   ├── registry/                  # process-local map of conversation_id → in-flight turn's cancel func
│   ├── repository/postgres/       # agent config + conversation status/event repositories
│   ├── sandbox/                   # Docker container lifecycle for the Goose sandbox
│   └── secret/                    # AES-256-GCM encrypt/decrypt for LLM API keys and agent env vars
└── test/e2e/                      # automated E2E suite: real Docker/Postgres/Valkey via testcontainers-go +
                                    # sandbox.Manager, gated on PACA_E2E=1, run by the test-e2e CI job
```

Every real-infra behavior this service has (Docker/Postgres/Valkey/a real `goose serve` container) is covered by `test/e2e/` — see that directory's `common_env_test.go` doc comment and each test file's own doc comment for what it exercises. This used to be a set of manually-run `internal/*/livecheck*` programs, not wired into CI; they were replaced by this automated suite (see `.github/workflows/agent-runner-pr-ci.yml`'s `test-e2e` job).

---

## Valkey Stream Protocol

### Inbound stream: `paca:agent:triggers`

`services/api` publishes to this stream. `agent-runner` consumes it via a single consumer group (`agent-runner-workers`) — it is the only consumer of this stream (see [config.Gate's doc comment](../../services/agent-runner/internal/config/gate.go) for why that wasn't always true, back when `services/ai-agent` also read it independently).

Each new-conversation trigger is handled in its own goroutine, up to `WORKER_CONCURRENCY` at a time — but **two triggers for the same `conversation_id` never run concurrently**, regardless of how close together they arrive (e.g. a user sending two chat messages back to back). `internal/messaging.Consumer` acquires a per-`conversation_id` lock before dispatching a trigger to `handler.Handle`, and holds it for that trigger's entire handling — a second trigger for the same conversation queues behind the first rather than racing it. This matters because a turn's `event_index` allocation (`ConversationRepository.NextEventIndex`, read once per turn and then incremented purely in-memory) and the in-flight-turn registry (`internal/registry.Conversations`, below) are both only safe under that guarantee.

The same stream also carries **control messages** (`agent.stop` / `agent.pause` / `agent.heartbeat`) for an already-running conversation, distinguished from a new-conversation trigger by the entry's own `type` field — there is no separate REST endpoint for pause/resume/stop the way `services/ai-agent` exposed one.

**New-conversation trigger fields:**

| Field | Type | Description |
|---|---|---|
| `trigger_type` | string | `task_assigned` \| `comment_mention` \| `chat_message` \| `description_write` \| `automation_message` |
| `conversation_id` | UUID | Pre-allocated by the API |
| `agent_id` | UUID | |
| `project_id` | UUID | |
| `task_id` / `comment_id` / `chat_session_id` | UUID? | Present depending on `trigger_type` |
| `message` | string | User message text |
| `actor_member_id` | UUID? | Project member who triggered the agent; absent for automation-originated triggers |
| `repo_plugin_ids` | string | Comma-separated repo-plugin IDs available to this trigger |

### Outbound stream: `paca:agent:events`

`agent-runner` appends every conversation event here as a durable log. Nothing in `services/api` currently consumes this stream directly — conversation history is read from Postgres (`agent_conversation_events`, written by `agent-runner` itself via `ConversationRepository.InsertEvent`/`NextEventIndex`, seeded per-conversation so a resumed chat turn's events continue the same index sequence rather than restarting at 0); live UI updates go through the separate `paca.events` Pub/Sub channel below.

### `paca.events` (Pub/Sub)

Every individual conversation event *and* every status transition is published here for `services/realtime`'s WebSocket fan-out — not just terminal events.

### `paca:agent:conversation_status` (stream)

A terminal status (`finished` / `failed` / `stopped` — never `paused`) is appended here once a conversation ends. `services/api`'s automation engine consumes this to resume a graph walk paused at a `trigger_ai_agent` node.

For a **task-linked** conversation that reaches `finished`, `agent-runner` first persists a durable task-level handoff — the final agent reply, keyed idempotently by `conversation_id` in `agent_task_handoffs` (migration `000039`) — *before* appending `finished` here. That ordering is load-bearing: `services/api` resolves the handoff synchronously when it consumes the `finished` status to record an `agent.session.finished` task activity, so persisting after the publish would race the read. Later task-linked conversations also inject up to three prior handoffs into their prompt (`internal/handler`, `internal/executor/prompt.go`) for cross-conversation continuity (#392).

---

## Conversation Execution

The core flow, in `internal/executor/executor.go`'s `Run`:

1. Decrypt the agent's LLM API key and any per-agent environment variables (`internal/secret.Encryptor`).
2. Resolve the LLM provider to Goose's own env var shape (`GOOSE_PROVIDER`, `GOOSE_MODEL`, and a provider-specific API key env var — see `resolveProviderEnv`). A handful of Paca's `llm_provider` values don't match the provider id Goose actually registers them under (verified directly against `block/goose`'s source, not its docs site — see `gooseProviderID`'s doc comment): `"gemini"` is Goose's `"google"` provider, and `"deepseek"` is Goose's `"custom_deepseek"`. An unmapped mismatch here means every conversation for that provider silently fails to initialize at all, with no ACP-level error to surface — this is why `gooseProviderID` exists as an explicit alias table rather than passing `llm_provider` straight through.
3. Start a dedicated sandbox container (`sandbox.Manager.Start`) — or, for a resumed chat turn, reuse an existing one (see [Pause / Resume / Stop / Heartbeat](#pause--resume--stop--heartbeat)).
4. Complete the ACP handshake: `initialize`, then `session/new` with the working directory and the agent's MCP server list (see [Skills & MCP Server Injection](#skills--mcp-server-injection)).
5. Build the turn's message. For a cold start this folds together the agent's system prompt, its enabled skills' content, project/trigger context, and the user's message into one string — Goose's `session/new` has no system-message-suffix channel the way OpenHands SDK's `AgentContext` did, confirmed empirically against a real `goose serve`, so everything rides in the turn's own message instead (`internal/executor/prompt.go`). A resumed turn sends just the new message — the agent already has full context from earlier turns.
6. `session/prompt` the message, capped at `MaxIterations` tool calls (default 50) and `TimeoutMinutes` (default 30) — Goose enforces no turn cap of its own, confirmed in the protocol spike where a non-converging reply produced 600+ tool-call cycles with no backoff.
7. Every `session/update` notification is forwarded to `onEvent`, supplied by the caller (`handler.Handle`) — `Run` itself has no Valkey/Postgres dependency, so it stays testable without either.

`Run` does **not** tear the sandbox down itself, on success or on error — that decision belongs to the caller, since whether to keep a sandbox alive depends on whether the trigger is chat-type and whether an interruption was a pause or a full stop, information `Run` doesn't have. See `Result`'s doc comment.

---

## Docker Container Strategy

- Each conversation gets a **dedicated Docker container**, started via the Docker Engine API directly (`internal/sandbox.Manager`).
- The sandbox image is `services/agent-server/Dockerfile` — Goose plus Node.js and the Paca MCP package pre-installed. **Must** be this image, not raw upstream Goose: the raw image has no Node.js, so the built-in Paca MCP server can never start, and Goose does not surface that failure as an ACP error — `session/new` just hangs forever instead. This is why `executor.Run` derives a bounded `context.WithTimeout` from `cfg.TimeoutMinutes` (falling back to 30 minutes) for the whole ACP handshake/prompt sequence, rather than trusting the call to fail fast on its own.
- The container is stopped and removed by `sandbox.Manager.Stop`, called explicitly by the caller once it decides teardown is appropriate (see [Conversation Execution](#conversation-execution) above) — not automatically on every turn's end, since a paused chat conversation's container stays alive between turns.
- Containers are placed on this process's own Docker network, isolated from other Paca service containers.
- Port allocation: a configurable port pool (`PORT_POOL_START`/`PORT_POOL_SIZE`, default `10000`–`10099`) tracked in-process; a port is claimed on `Start` and released on `Stop`.
- **Container resource limits**, matching `services/ai-agent`'s own defaults:
  - CPU: 2 cores
  - Memory: 4 GB

---

## Repository Access

Unlike `services/ai-agent`, `agent-runner` **never clones a repository itself**. Cloning, committing, and PR creation happen entirely inside the sandbox, driven by the agent's own tool calls against the built-in Paca MCP server (`apps/mcp`, running as `npx @paca-ai/paca-mcp` inside the container — see [Skills & MCP Server Injection](#skills--mcp-server-injection)). `apps/mcp/src/tools/repo-tools.ts` fetches short-lived VCS tokens from `services/api` on demand, runs `git` as a subprocess, and scrubs the token from any output before it can reach the model's context or a log line. `clone_repository` recursively deletes its target directory before cloning into it (`targetDir` is agent-chosen, defaulting to `/home/goose/repo`); `assertSafeDeleteTarget` refuses to run that delete against a handful of top-level system directories (`/`, `/etc`, `/home`, ...) regardless of what the agent passes.

See [repository-plugin-adapter.md](repository-plugin-adapter.md) for the full token-fetch protocol.

---

## Skills & MCP Server Injection

### Skills

`internal/repository/postgres/agent_repository.go` reads an agent's skills directly from the `agent_skills` table. **Unlike `services/ai-agent`, this is not merged with Paca's default skill set or any plugin-contributed skills** — `services/ai-agent`'s `builder.load_default_skills()` fetched and merged those in at conversation-run time (`merge_skills_by_name`); `agent-runner` has no equivalent step. Every row with `is_enabled = true` gets its content folded unconditionally into the turn's initial message (`internal/executor/prompt.go`) — trigger-based (keyword-activated) skill selection has no Goose analog yet either, so this is simpler than `services/ai-agent`'s behavior in two ways at once, not just one. See `agent.Skill`'s doc comment.

### MCP Servers

`internal/executor/executor.go`'s `buildMCPServers` sends the agent's own configured `agent_mcp_servers` rows first, then always appends the built-in Paca MCP server last (so it can't be shadowed by a same-named user entry) whenever `PACA_API_KEY` is configured. The wire shape is ACP's real schema (a `type`-discriminated stdio/http/sse enum, `env` as an array of `{name, value}` pairs) — verified against the spec directly after a hand-guessed shape omitting both made `session/new` hang. `oauth`-transport servers (a value the DB schema allows) have no ACP equivalent yet and are skipped.

---

## Pause / Resume / Stop / Heartbeat

There is no REST equivalent to `services/ai-agent`'s `/conversations/:id/pause` etc. — control is entirely message-driven, via `agent.stop` / `agent.pause` / `agent.heartbeat` entries on `paca:agent:triggers`, handled by `internal/messaging.Consumer`'s `ControlHandler`:

- **In-flight turn**: `internal/registry.Conversations` is a process-local map of `conversation_id` → the running turn's `context.CancelFunc`, plus which of `stop`/`pause` was requested — read back by the goroutine actually running that turn once its context is cancelled, to decide whether to tear the sandbox down (`stop`) or hand it to `chatsandbox.Registry` to keep alive (`pause`). `Register` returns an ownership token that `Unregister` must present back; this exists specifically so a turn only ever clears the registry entry it itself created, not a newer turn's — belt-and-suspenders alongside the per-`conversation_id` serialization described above.
- **Paused chat sandbox**: `internal/chatsandbox.Registry` holds live `*acp.Client`s (already past `initialize`) for chat conversations between turns. A `chat_message` trigger for a conversation already in this registry resumes the same container instead of cold-starting a new one. `agent.heartbeat` extends its idle deadline; a background reaper (`cmd/agent-runner/main.go`) tears down any entry that's gone idle past `CHAT_SANDBOX_IDLE_TIMEOUT_MINUTES` and isn't currently mid-turn. `agent.stop` for a conversation with no in-flight turn checks this registry directly and tears the sandbox down if found there. `Handler.TeardownPausedChatSandbox` — the shared teardown path both the reaper and `agent.stop` call — re-checks `InFlight.IsRegistered` immediately before popping the sandbox, so a turn that started resuming the same conversation in the meantime (registered before it ever reads the sandbox — see `Handle`'s own comment on that ordering) isn't torn down out from under it.
- **Multi-replica note** (same limitation `services/ai-agent` had): both registries above are process-local. In a multi-replica deployment, a control message is only actionable on whichever replica actually holds that conversation's turn/sandbox — nothing here routes it to the right one.

---

## HTTP Surface

`internal/acpbridge.Server` (default `:8080`, `HTTP_ADDR`) — this is a much smaller surface than `services/ai-agent`'s REST API, because conversation status/events are read directly from Postgres by `services/api` rather than proxied through this service at all:

| Method | Path | Description |
|---|---|---|
| `GET` | `/agent-bridge/ws` | WebSocket endpoint an `apps/acp-bridge` daemon connects to for `acp`-type agent dispatch |
| `GET` | `/agent-bridge/status/{agentId}` | Internal — is this agent's bridge daemon currently connected? Proxied by `services/api` |
| `POST` | `/agent-bridge/disconnect/{agentId}` | Internal — force-disconnect an agent's bridge daemon. Proxied by `services/api` |
| `GET` | `/llm/models` | Static provider/model catalog (`data/llm_models.json`), proxied by `services/api`'s `GetLLMModels` |

The three internal endpoints require `X-Internal-Token` matching `INTERNAL_API_KEY` — `services/api`'s `AI_AGENT_INTERNAL_KEY` must equal this value.

---

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `VALKEY_URL` | ✅ | e.g. `redis://valkey:6379/0` |
| `DATABASE_URL` | ✅ | PostgreSQL connection string |
| `ENCRYPTION_KEY` | ✅ | 64-char hex; must equal `services/api`'s own `ENCRYPTION_KEY` |
| `AGENT_SERVER_IMAGE` | ✅ | Deliberately no hardcoded default — must be a digest- or tag-pinned reference to `services/agent-server/Dockerfile`, chosen deliberately |
| `INTERNAL_API_KEY` | ✅ | Shared secret for `services/api`'s calls into the internal HTTP endpoints above; must equal `services/api`'s `AI_AGENT_INTERNAL_KEY` |
| `AGENT_RUNNER_ALLOWED_AGENT_IDS` | ✅ | Comma-separated agent UUIDs, or `*` for every agent — see `config.Gate`'s doc comment |
| `PACA_API_KEY` | | Credential for the built-in Paca MCP server; omit to disable it entirely |
| `PACA_API_URL` / `PACA_GATEWAY_URL` | | Internal URLs the Paca MCP server calls out to |
| `PORT_POOL_START` / `PORT_POOL_SIZE` | | Container port pool. Default: `10000` / `100` |
| `WORKER_CONCURRENCY` | | Trigger goroutines in flight at once. Default: `5` |
| `CHAT_SANDBOX_IDLE_TIMEOUT_MINUTES` | | Idle timeout before the reaper tears down a paused chat sandbox. Default: `3` |
| `HTTP_ADDR` | | Address `internal/acpbridge.Server` listens on. Default: `:8080` |
| `LLM_MODELS_PATH` | | Path to the static model catalog. Default: `./data/llm_models.json` |
| `LOG_LEVEL` | | Default: `info` |
| `PACA_MCP_DEV_SOURCE_DIR` | | Dev-only: a host path to a local `apps/mcp` checkout. When set, every sandbox runs the built-in Paca MCP server from this mount instead of the image's globally npm-installed `@paca-ai/paca-mcp` — a local `apps/mcp` change is live on the next conversation with no npm publish or image rebuild. See `sandbox.Config.MCPDevSourceDir`'s doc comment for the sibling-container path-resolution gotcha (this is a *host* path, resolved by the Docker daemon, not a path inside this container). |
