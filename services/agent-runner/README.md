# Agent Runner Service

Go service that executes `llm`-type agent conversations using [Goose](https://github.com/block/goose)
(driven over [ACP](https://agentclientprotocol.com/), the Agent Client Protocol) as the execution
engine, and brokers `acp`-type agent conversations to a user's own local coding CLI via
`apps/acp-bridge`. It consumes trigger events from a Valkey Stream, manages a dedicated Docker
container per `llm`-type conversation, and streams conversation events back through Postgres and a
`paca.events` Pub/Sub channel for `services/realtime`.

It replaced `services/ai-agent` (Python, OpenHands SDK), which has been fully removed from the
repository.

For the deeper architecture write-up (Valkey stream protocol, conversation execution flow, Docker
container strategy, skills/MCP server injection, pause/resume/stop semantics), see
[`docs/ai-agent/agent-runner-service.md`](../../docs/ai-agent/agent-runner-service.md). This README
covers day-to-day orientation and local development instead.

## Responsibilities

- Consume new-conversation triggers **and** control messages (`agent.stop` / `agent.pause` /
  `agent.heartbeat`) from the `paca:agent:triggers` Valkey Stream.
- Start a dedicated Docker sandbox per `llm`-type conversation (running `services/agent-server`'s
  image), drive it over ACP, and persist/publish every `session/update` event.
- Keep a paused chat conversation's sandbox alive between turns (`internal/chatsandbox`) instead of
  cold-starting a new container on every reply, reaped after an idle timeout.
- Broker `acp`-type conversations to a user's locally-running `apps/acp-bridge` daemon over a
  WebSocket connection (`internal/acpbridge`).
- Never clone a repository itself — all git operations happen inside the sandbox via the agent's own
  tool calls against the Paca MCP server (`apps/mcp`).
- Encrypt/decrypt LLM API keys and per-agent env vars with AES-256-GCM, matching `services/api`'s own
  `ENCRYPTION_KEY`.

## Technology Stack

| Component | Choice |
|---|---|
| Language | Go 1.26 |
| Execution engine | [Goose](https://github.com/block/goose) `goose serve`, driven over ACP (HTTP + SSE) |
| Container orchestration | Docker Engine API (`github.com/moby/moby/client`) — direct control, no framework wrapper |
| Stream consumer/producer | `github.com/redis/go-redis/v9` (Valkey-compatible) |
| DB client | `github.com/jmoiron/sqlx` + `github.com/jackc/pgx/v5` |
| HTTP | Go stdlib `net/http` — a small internal surface (ACP bridge WebSocket + a couple of internal endpoints), no framework needed |
| Hot reload (dev) | [`air`](https://github.com/air-verse/air) |
| Linting | [`golangci-lint`](https://golangci-lint.run) v2 |

## Source Layout

```text
services/agent-runner/
├── go.mod / go.sum
├── Dockerfile / Dockerfile.dev
├── .air.toml                      # hot-reload config for local dev
├── .golangci.yml
├── data/llm_models.json           # static provider/model catalog served at GET /llm/models
├── cmd/agent-runner/
│   └── main.go                    # wiring: config, repos, executor, consumer, HTTP server, idle reaper
├── internal/
│   ├── acp/                       # ACP-over-HTTP+SSE client (initialize, session/new, session/prompt)
│   ├── acpbridge/                 # acp-type dispatch: WebSocket registry + HTTP server
│   ├── agent/                     # domain types: Config, Trigger, Skill, MCPServer, EnvVar
│   ├── bundledskills/              # fetches Paca's bundled skill set (paca, paca-do, ...)
│   ├── chatsandbox/                # process-local registry of paused chat sandboxes (turn continuity)
│   ├── config/                     # env var settings + the per-agent rollout Gate
│   ├── executor/                   # ties sandbox lifecycle + acp.Client into one conversation turn
│   ├── handler/                    # per-trigger dispatch: Gate check, executor.Run, event persistence
│   ├── messaging/                  # Valkey stream consumer, control-message routing, event/status publisher
│   ├── registry/                   # process-local map of conversation_id → in-flight turn's cancel func
│   ├── repository/postgres/        # agent config + conversation status/event repositories
│   ├── sandbox/                    # Docker container lifecycle for the Goose sandbox
│   └── secret/                     # AES-256-GCM encrypt/decrypt for LLM API keys and agent env vars
└── test/e2e/                       # real Docker/Postgres/Valkey e2e suite, gated on PACA_E2E=1
```

## Local Development

Unlike `services/realtime`, this service needs a real Docker daemon (to start sandbox containers), a
real Postgres, and a real Valkey — there's no meaningful standalone dev loop, so the documented path
is the full Docker Compose stack.

```sh
# From the repo root.

# 1. Build the sandbox image once (agent-runner refuses to start conversations
#    without it — see AGENT_SERVER_IMAGE below).
docker build -f services/agent-server/Dockerfile \
  -t paca-agent-server-goose:dev services/agent-server

# 2. (Optional) build apps/mcp locally if you want sandbox conversations to run
#    the Paca MCP server from your own checkout instead of the image's globally
#    npm-installed @paca-ai/paca-mcp — see PACA_MCP_DEV_SOURCE_DIR below.
(cd apps/mcp && bun install && bun run build)

# 3. Start the stack — postgres, valkey, api, web, agent-runner, and gateway.
docker compose -f deploy/docker-compose.dev.yml up -d
```

`agent-runner` runs via `air` inside its dev container (see `.air.toml`), so editing any `.go` file
under this directory triggers an automatic rebuild and restart — no manual restart needed.

### Building/running directly (no Docker Compose)

Only useful for compiling or running non-sandbox unit tests without the full stack — you still need a
real Docker daemon, Postgres, and Valkey reachable for anything that actually starts a conversation:

```sh
go build ./...
go run ./cmd/agent-runner
```

## Environment Variables

The required ones, to get the service to actually start:

| Variable | Description |
|---|---|
| `VALKEY_URL` | e.g. `redis://valkey:6379/0` |
| `DATABASE_URL` | PostgreSQL connection string |
| `ENCRYPTION_KEY` | 64-char hex; must equal `services/api`'s own `ENCRYPTION_KEY` |
| `AGENT_SERVER_IMAGE` | Deliberately no hardcoded default — a digest- or tag-pinned reference to `services/agent-server`'s image |
| `INTERNAL_API_KEY` | Shared secret for `services/api`'s calls into this service's internal HTTP endpoints; must equal `services/api`'s `AI_AGENT_INTERNAL_KEY` |
| `AGENT_RUNNER_ALLOWED_AGENT_IDS` | Comma-separated agent UUIDs, or `*` for every agent — see `internal/config.Gate`'s doc comment |

See [`docs/ai-agent/agent-runner-service.md#environment-variables`](../../docs/ai-agent/agent-runner-service.md#environment-variables)
for the full table, including optional ones (`PACA_API_KEY`, `PACA_MCP_DEV_SOURCE_DIR`,
`PORT_POOL_START`/`PORT_POOL_SIZE`, `WORKER_CONCURRENCY`, `CHAT_SANDBOX_IDLE_TIMEOUT_MINUTES`,
`HTTP_ADDR`, `LLM_MODELS_PATH`, `LOG_LEVEL`) — kept in one place so the two docs don't drift out of
sync.

## Testing

```sh
# Unit tests (no external dependencies) — this is what CI runs on every PR.
go test -race -timeout 60s ./...

# Real-infra e2e suite (Docker/Postgres/Valkey via testcontainers-go, plus a
# real goose serve container) — gated behind PACA_E2E=1 since it's slow and
# needs Docker. Requires AGENT_SERVER_IMAGE to point at a built sandbox image
# (see Local Development above).
PACA_E2E=1 go test -v -timeout 600s -parallel 4 ./test/e2e/...
```

Every real-infra behavior this service has is covered by `test/e2e/` — see that directory's
`common_env_test.go` doc comment and each test file's own doc comment for what it exercises.

## Linting

```sh
golangci-lint run --timeout=5m
```

## Related Documentation

- [`docs/ai-agent/agent-runner-service.md`](../../docs/ai-agent/agent-runner-service.md) — full
  architecture: Valkey stream protocol, conversation execution flow, Docker container strategy,
  skills/MCP server injection, pause/resume/stop/heartbeat semantics, HTTP surface.
- [`docs/ai-agent/repository-plugin-adapter.md`](../../docs/ai-agent/repository-plugin-adapter.md) —
  the short-lived VCS token fetch protocol `apps/mcp`'s repo tools use.
- [`docs/architecture/service-boundaries.md`](../../docs/architecture/service-boundaries.md) — where
  this service's responsibilities start and end relative to `services/api`.
