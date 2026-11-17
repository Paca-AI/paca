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
| RustFS S3 API | rustfs/rustfs | 9000 | — |
| RustFS Console | rustfs/rustfs | 9001 | http://localhost:9001 (user: `rustfsadmin`, pass: `rustfsadmin`) |

The Caddy gateway (port 3000) routes `/api/v1/…` to the API, socket traffic to realtime, and `/storage/…` to RustFS. `apps/web` is served at the root.

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

Migrations are plain SQL files under `services/api/migrations/` named in lexicographic order. The API applies them itself on startup and records each applied file in the `schema_migrations` table, so every file runs exactly once. Do not also mount the folder into Postgres's `/docker-entrypoint-initdb.d`: that would run every file a second time, and migrations since 000053 are not written to be re-runnable.

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
| RustFS console | `rustfsadmin` / `rustfsadmin` at http://localhost:9001 |

These are intentionally weak defaults — never use them in production.

---

## Architecture Notes

- `services/api` owns all persistent state changes and publishes domain events to Valkey Streams.
- `services/realtime` consumes those events and fans them out to Socket.IO clients.
- `services/agent-runner` reads agent trigger events from a separate Valkey Stream and manages Docker containers running Goose conversations over ACP.
- `apps/mcp` is stateless; run it with `npx @paca-ai/paca-mcp` pointed at the running API.

### Project prefix uniqueness (000093)

Migration `000093_unique_project_task_id_prefix.sql` makes non-empty task ID
prefixes unique, ignoring case, among projects with `deleted_at IS NULL`.
Empty prefixes and soft-deleted projects are excluded. It keeps the oldest
project in each duplicate group (`created_at`, then UUID `id` to break ties).
Newer duplicates receive the first available numeric suffix starting at 2;
the base is shortened as needed to keep the result within 10 characters.
Existing active prefixes, including already suffixed ones, are reserved.

The migration records each changed project UUID, original prefix, replacement
prefix and timestamp in `project_prefix_changes_000093`. Inspect that table
after upgrading to find the projects whose external references need updating.
The table is retained even if a project is later deleted.

Task UUIDs, project UUIDs and task numbers are unchanged. UUID-based task URLs
keep working. Displayed task keys, CSV labels, newly copied short links and
future git task-key matching use the new prefix. Historical short links and
textual git references are not rewritten or aliased: an old duplicate key
continues to address the project that kept the prefix, or returns not found
if that project has no such task. Previously the lookup could fall through
to another duplicate project with that task number. Permissions are still
checked on the resolved task. Changing a prefix later or reusing one after
soft deletion can likewise change what an old short link addresses; use UUID
URLs for references that must remain stable across those operations.

Deduplication, audit records and the index are committed in one migration
transaction. A table lock blocks project writes during the migration and
index build, so plan an upgrade window for a large projects table. Errors
roll back all three changes. The migration tolerates a retry after the
runner commits it but crashes before recording its filename.

There is no automatic down migration: removing the index does not restore
old prefixes. Keep a backup and the audit table. Restoring old duplicate
prefixes would reintroduce ambiguous links, and blindly applying that history
after subsequent edits could overwrite newer choices. An application-only
rollback can keep the index and new prefixes, but older binaries do not
handle prefix conflicts or allocate collision-free defaults correctly.
