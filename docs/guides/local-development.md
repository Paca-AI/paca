# Local Development

## Quick Start

One command starts the full stack with hot-reload from your local source files:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

Open `http://localhost:3000`. All services watch the source tree and reload automatically — no manual rebuild needed.

To stop:
```bash
docker compose -f deploy/docker-compose.dev.yml down
```

To also remove data volumes:
```bash
docker compose -f deploy/docker-compose.dev.yml down -v
```

---

## Runtime Stack

| Service | Technology | Port | Hot-reload |
|---|---|---|---|
| Gateway (Caddy) | caddy:2-alpine | **3000** (host) | — |
| `apps/web` | React + TanStack Start + shadcn/ui | 3000 (internal) | Vite HMR |
| `services/api` | Go + Gin | 8080 (internal) | [air](https://github.com/air-verse/air) |
| `services/realtime` | Node.js + Socket.IO | 3001 (internal) | `bun --watch` |
| `services/agent-runner` | Go, driving Goose over ACP | 8080 (internal) | [air](https://github.com/air-verse/air) |
| PostgreSQL | postgres:16-alpine | 5432 | — |
| Valkey | valkey/valkey:8-alpine | 6379 | — |
| MinIO S3 API | minio/minio | 9000 | — |
| MinIO Console | minio/minio | 9001 | http://localhost:9001 (user: `minioadmin`, pass: `minioadmin`) |

The Caddy gateway (port 3000) routes `/api/v1/…` to the API, socket traffic to realtime, and `/storage/…` to MinIO. `apps/web` is served at the root.

---

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) with the Compose plugin

That is the only hard requirement for the full containerized stack. The services build their own images from `Dockerfile.dev` in each service directory.

---

## Infra Only

If you only want PostgreSQL and Valkey (to run services on the host yourself):

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres valkey
```

---

## Running Services on the Host

For faster feedback loops or IDE-integrated debugging, you can run individual services directly on the host and point them at the containerized infra.

**Prerequisites for host-side development:**
- Go 1.23+ (for `services/api` and `services/agent-runner`)
- Bun (for `apps/web` and `services/realtime`)

### API (`services/api`)

```bash
cd services/api
cp .env.example .env   # first time — credentials match docker-compose defaults
make run               # uses air for hot-reload
```

### Web (`apps/web`)

```bash
cd apps/web
bun install            # first time only
bun run dev            # Vite dev server at http://localhost:3000
```

### Realtime (`services/realtime`)

```bash
cd services/realtime
bun install            # first time only
bun run dev
```

### Agent Runner (`services/agent-runner`)

Needs more than `go run` alone: Docker socket access (to spawn Goose sandbox containers) and a locally-built sandbox image first (see [agent-runner-service.md](../ai-agent/agent-runner-service.md#docker-container-strategy)):

```bash
docker build -f services/agent-server/Dockerfile \
  -t paca-agent-server-goose:dev services/agent-server

cd services/agent-runner
DATABASE_URL=postgres://paca:paca@localhost:5432/paca?sslmode=disable \
VALKEY_URL=redis://localhost:6379/0 \
ENCRYPTION_KEY=3b4a93a4f2ffa732a5f7cf44b9a37050e49f5f9c3f105c3995432b9dd49e28ac \
AGENT_SERVER_IMAGE=paca-agent-server-goose:dev \
INTERNAL_API_KEY=dev-internal-key-change-in-production \
AGENT_RUNNER_ALLOWED_AGENT_IDS='*' \
go run ./cmd/agent-runner
```

See [agent-runner-service.md](../ai-agent/agent-runner-service.md#environment-variables) for the full environment variable reference.

---

## Migrations

Migrations are plain SQL files under `services/api/migrations/` named in lexicographic order. They run automatically when the Postgres container first starts (mounted at `/docker-entrypoint-initdb.d`).

To apply migrations manually against a running instance:

```bash
cd services/api
make migrate-up   # requires DATABASE_URL to be set
```

---

## Default Dev Credentials

| Resource | Value |
|---|---|
| App login | `admin` / `adminpassword` |
| PostgreSQL | `paca:paca@localhost:5432/paca` |
| MinIO console | `minioadmin` / `minioadmin` at http://localhost:9001 |
| Agent API key | `dev-agent-api-key-change-in-production` |

These are intentionally weak defaults — never use them in production.

---

## Architecture Notes

- `services/api` owns all persistent state changes and publishes domain events to Valkey Streams.
- `services/realtime` consumes those events and fans them out to Socket.IO clients.
- `services/agent-runner` reads agent trigger events from a separate Valkey Stream and manages Docker containers running Goose conversations over ACP.
- `apps/mcp` is stateless; run it with `npx @paca-ai/paca-mcp` pointed at the running API.
