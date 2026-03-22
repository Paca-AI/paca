# Deploy

This directory contains deployment assets for two distinct use cases:

- contributor-friendly local development;
- production-oriented container deployment examples.

Keeping those concerns separate makes the repository easier to understand and avoids presenting a local-only stack as a production recommendation.

## Contents

| File | Description |
|---|---|
| `docker-compose.dev.yml` | Local development stack with PostgreSQL, Valkey, RabbitMQ, and optional app containers |
| `docker-compose.prod.yml` | Production-oriented single-host stack for web, API, PostgreSQL, Valkey, and RabbitMQ |
| `.env.production.example` | Example environment file for `docker-compose.prod.yml` |

Service container definitions live with each service:
- [`services/api/Dockerfile`](../services/api/Dockerfile)
- [`apps/web/Dockerfile`](../apps/web/Dockerfile)

## Development Compose

Use [`docker-compose.dev.yml`](./docker-compose.dev.yml) for local development and contributor onboarding.

Start the full local stack in containers:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

Start only shared dependencies:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres valkey rabbitmq
```

For day-to-day coding, contributors can still run the application services directly on the host and use Docker Compose only for PostgreSQL, Valkey, and RabbitMQ.

The Postgres schema is applied automatically on the first container start from `services/api/migrations/`.

### Development service ports

| Service | Port | Notes |
|---|---|---|
| PostgreSQL | 5432 | Local database for development |
| Valkey | 6379 | Local cache / ephemeral state |
| RabbitMQ (AMQP) | 5672 | Event transport |
| RabbitMQ (management UI) | 15672 | http://localhost:15672, user `paca`, password `paca` |
| API | 8080 | Containerized Go service |
| Web | 3000 | Containerized TanStack Start app |

Stop the development stack:

```bash
docker compose -f deploy/docker-compose.dev.yml down
```

Remove the Postgres volume as well:

```bash
docker compose -f deploy/docker-compose.dev.yml down -v
```

## Production Compose

Use [`docker-compose.prod.yml`](./docker-compose.prod.yml) as a self-hosting baseline for open-source deployments.

The production compose includes PostgreSQL, Valkey, and RabbitMQ because a public repository should offer a runnable end-to-end deployment path. It is still a single-host baseline rather than a universal recommendation. Teams using managed services can keep the same application images and point the runtime configuration at external infrastructure instead.

Create a production environment file from the example:

```bash
cp deploy/.env.production.example deploy/.env.production
```

Then run:

```bash
docker compose --env-file deploy/.env.production -f deploy/docker-compose.prod.yml up -d --build
```

This file is suitable as:

- a self-hosting starting point;
- a CI/CD handoff artifact;
- a reference for container image names and required runtime configuration.

By default, only the web service is published to the host in the production compose. PostgreSQL, Valkey, RabbitMQ, and the API stay on the internal Compose network unless an operator intentionally exposes them.