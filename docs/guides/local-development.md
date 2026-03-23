# Local Development

## Runtime Stack

| Service | Technology | Port |
|---|---|---|
| `apps/web` | React + Vite + shadcn/ui | 3000 |
| `services/api` | Go + Gin | 8080 |
| `services/realtime` | Node.js + Socket.IO | — (not scaffolded) |
| `services/ai-agent` | FastAPI + LangGraph | — (not scaffolded) |
| PostgreSQL | postgres:16-alpine | 5432 |
| Valkey | valkey/valkey:8-alpine | 6379 |
| RabbitMQ | rabbitmq:3.13-management | 5672 / 15672 |

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) with the Compose plugin
- Go 1.26+ (for `services/api`)
- Node.js / Bun (for `apps/web`)

## Start infrastructure

All infra (PostgreSQL, Valkey, RabbitMQ) is defined in `deploy/docker-compose.dev.yml`. Run from the repository root:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres valkey rabbitmq
```

The Postgres schema is seeded automatically from `services/api/migrations/` on first start.

## Start services

### API (`services/api`)

```bash
cd services/api
cp .env.example .env   # first time only — credentials match docker-compose defaults
make run
```

### Web (`apps/web`)

```bash
cd apps/web
bun install            # first time only
bun run dev            # http://localhost:3000
```

Or run API + web in Docker alongside infra:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

## Migrations

Migrations are plain SQL files under `services/api/migrations/` and run in lexicographic order. To apply them manually against a running Postgres instance:

```bash
cd services/api
make migrate-up        # requires DATABASE_URL to be set
```

## Stop infra

```bash
docker compose -f deploy/docker-compose.dev.yml down
```

Use `down -v` to also remove the Postgres data volume.

## Architecture notes

- `services/api` owns all persistent state changes and publishes domain events to RabbitMQ.
- `services/realtime` will consume those events and fan them out to Socket.IO clients.
- RabbitMQ management UI is available at http://localhost:15672 (user `paca` / `paca`).