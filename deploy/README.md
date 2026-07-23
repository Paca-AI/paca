# Deploy

This directory contains deployment assets for two distinct use cases:

- contributor-friendly local development;
- production container deployment for self-hosters.

## Contents

| File | Description |
|---|---|
| `docker-compose.dev.yml` | Local development stack: PostgreSQL, Valkey, MinIO, and optional app containers |
| `docker-compose.prod.yml` | Production stack: pulls pre-built images from DockerHub, no source checkout required |
| `docker-compose.e2e.yml` | End-to-end test stack mirroring production topology with fixed test credentials |
| `.env.dev.example` | Optional environment file for `docker-compose.dev.yml` (tunnel / custom domain) |
| `.env.production.example` | Example environment file for manual production deployments |

Service container definitions live with each service:
- [`services/api/Dockerfile`](../services/api/Dockerfile)
- [`apps/web/Dockerfile`](../apps/web/Dockerfile)

## Production Deployment

### Recommended: install script

The easiest way to run Paca without cloning the repository is via the install script
published with each release. It downloads the compose file and Caddyfile, walks you
through configuration interactively (database, storage, networking/HTTPS, AI agent),
generates a `.env` with strong random secrets, and starts the stack.

```bash
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install.sh -o install.sh
bash install.sh
```

The installer supports:

| Option | Description |
|---|---|
| Bundled PostgreSQL | Starts a postgres container (default) |
| External PostgreSQL | Supply a `DATABASE_URL`; postgres container is suppressed |
| Self-hosted MinIO | Starts a MinIO container for S3-compatible file storage (default) |
| AWS S3 | Supply AWS credentials; MinIO container is suppressed |
| HTTPS | Enabled by default — Let's Encrypt for a real domain, Caddy's local CA otherwise; can be disabled for plain HTTP |
| AI Agent | Enabled by default; can be skipped to reduce resource usage. See [docs/ai-agent/api-design.md](../docs/ai-agent/api-design.md) for the agent REST API once it's running. |

**Non-interactive / CI install** — set `PACA_YES=1` to skip every prompt and use
defaults plus freshly auto-generated secrets for all of the above (including
`JWT_SECRET`, `ADMIN_PASSWORD`, `AGENT_API_KEY`, `INTERNAL_API_KEY`, and
`ENCRYPTION_KEY`):

```bash
PACA_YES=1 bash install.sh
# or, without a local copy of the script at all:
PACA_YES=1 bash <(curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/install.sh)
```

`PACA_DIR` (default `./paca`) and `PACA_VERSION` (default `latest`) are also
read from the environment, so a fully unattended install needs no interactive
input at all. See the comment header at the top of
[`scripts/install.sh`](../scripts/install.sh) for the full list of variables.

### Manual setup

Download the two required files from the latest release:

```bash
mkdir -p paca/caddy && cd paca
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/docker-compose.yml -o docker-compose.yml
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/Caddyfile         -o caddy/Caddyfile
```

Download the example environment file and edit it:

```bash
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/docker-compose.yml -o docker-compose.yml
# Or use the .env.production.example from the repo as a reference:
# https://github.com/Paca-AI/paca/blob/master/deploy/.env.production.example
```

Create a `.env` with the required variables:

```bash
# Required: generate with 'openssl rand -hex 32'
JWT_SECRET=<strong-random-secret>
ADMIN_PASSWORD=<strong-password>
POSTGRES_PASSWORD=<strong-random-password>
# Required even if you don't plan to use the AI agent (--scale ai-agent=0) —
# the api service itself refuses to start without this set, since it's also
# used to authenticate the api → ai-agent internal status/control calls.
# Generate with: openssl rand -hex 32
INTERNAL_API_KEY=<strong-random-secret>
# Optional — only used by the AI agent to call back into the api service as
# its own bot user. Leave unset if you're skipping the AI agent.
# Generate with: openssl rand -hex 32
AGENT_API_KEY=<strong-random-secret>
# Required to encrypt LLM-type agents' API keys and plugin secrets at rest.
# Not enforced at startup, but leaving it empty stores those values in
# plaintext instead of failing — set it unless you're certain you don't need
# it (e.g. ACP-only agents, no plugins with stored secrets).
# Generate with: openssl rand -hex 32
ENCRYPTION_KEY=<64-char-hex>
PUBLIC_URL=http://your-domain-or-ip
```

Start the full stack (bundled PostgreSQL + MinIO):

```bash
docker compose --env-file .env up -d
```

**With HTTPS** — set `SITE_ADDRESS` to any concrete domain or IP address and Caddy
handles certificates automatically, choosing the right kind for what you give it:

```bash
# In .env: set SITE_ADDRESS to your domain/IP, and PUBLIC_URL/COOKIE_SECURE to match.
SITE_ADDRESS=paca.example.com
PUBLIC_URL=https://paca.example.com
COOKIE_SECURE=true
```

```bash
docker compose --env-file .env up -d
```

- A real domain name with DNS already pointed here gets a trusted Let's Encrypt
  certificate, renewed automatically. Ports 80 and 443 must both be reachable from the
  internet for the ACME challenge to succeed.
- An IP address, `localhost`, `*.localhost`, or anything else that isn't a publicly
  resolvable domain gets a certificate from Caddy's own local certificate authority
  instead — traffic is still encrypted, but browsers will show a trust warning since
  that CA isn't publicly trusted.

Either way, certificates persist in the `caddy_data` volume across restarts.

Without `SITE_ADDRESS` (or set to a bare port like `:80`), the gateway serves plain
HTTP — the simplest option, and the right one when another proxy or load balancer in
front of this server already terminates TLS.

**With external PostgreSQL** (suppress the bundled container):

```bash
# Set DATABASE_URL in .env to your managed connection string.
docker compose --env-file .env up -d --scale postgres=0
```

**With AWS S3** (suppress MinIO):

```bash
# Set STORAGE_PROVIDER=s3 and real AWS credentials in .env.
docker compose --env-file .env up -d --scale minio=0
```

**Without the AI agent**:

```bash
docker compose --env-file .env up -d --scale ai-agent=0
```

`INTERNAL_API_KEY` must still be set in `.env` even with the AI agent scaled
to 0 — the api service requires it unconditionally at startup regardless of
whether ai-agent is actually running (see above). `AGENT_API_KEY` can be left
unset in this case.

Flags can be combined:

```bash
docker compose --env-file .env up -d --scale postgres=0 --scale minio=0
```

### Upgrading to a new version

**Recommended: upgrade script.** From the directory where your `docker-compose.yml` and
`.env` live, run the same upgrade script published with each release. It backs up
`docker-compose.yml`, `caddy/Caddyfile`, and `.env` before overwriting them, refreshes
the compose file and Caddyfile, re-pins image versions when you request a specific
release, then pulls and restarts the stack:

```bash
curl -fsSL https://github.com/Paca-AI/paca/releases/latest/download/upgrade.sh -o upgrade.sh
bash upgrade.sh
```

Pin to a specific release instead of `latest`:

```bash
PACA_VERSION=v1.2.3 bash upgrade.sh
```

Pass through any `--scale` flags you used originally:

```bash
bash upgrade.sh --scale web=0 --scale minio=0
```

**Manual:** pull the latest images and restart the stack — this is enough when
`docker-compose.yml` and the Caddyfile haven't changed shape since your last upgrade:

```bash
docker compose pull
docker compose --env-file .env up -d
```

Database migrations run automatically on API startup — no manual steps are required.

---

### Upgrading from an earlier installation

The compose project was renamed from `paca-prod` to `paca` in this release.
Docker Compose namespaces volumes by project name, so existing volumes
(`paca-prod_postgres_data`, `paca-prod_minio_data`, etc.) are **not** automatically
attached to the new stack. To migrate:

```bash
# 1. Stop the old stack (volumes are preserved on disk).
docker compose -p paca-prod --env-file .env down

# 2. Rename each volume you want to keep.
docker volume create paca_postgres_data
docker run --rm \
  -v paca-prod_postgres_data:/from \
  -v paca_postgres_data:/to \
  alpine sh -c "cp -av /from/. /to/"
docker volume rm paca-prod_postgres_data

# Repeat for minio_data, valkey_data, and plugin volumes as needed.

# 3. Start the new stack.
docker compose --env-file .env up -d
```

If you are doing a fresh install (no data to keep), no migration is needed.

### Pinning a release version

Set the image variables in `.env` to lock to a specific release:

```bash
PACA_API_IMAGE=pacaai/paca-api:1.2.3
PACA_WEB_IMAGE=pacaai/paca-web:1.2.3
PACA_REALTIME_IMAGE=pacaai/paca-realtime:1.2.3
PACA_AI_AGENT_IMAGE=pacaai/paca-ai-agent:1.2.3
```

### Database backups

A `db-backup` container runs alongside the stack and writes a gzip-compressed
`pg_dump` on a cron schedule you control, pruning dumps older than the
configured retention period. It works against the bundled `postgres` container
or an external `DATABASE_URL`.

Configure it in `.env`:

```bash
BACKUP_ENABLED=true             # set to false to turn off backups permanently
BACKUP_DIR=./backups            # host directory dumps are written to
BACKUP_CRON=0 2 * * *           # standard 5-field cron syntax, default 02:00 daily
BACKUP_RETENTION_DAYS=7         # dumps older than this are deleted
# TZ=America/New_York           # interpret BACKUP_CRON in this zone instead of UTC
```

`BACKUP_DIR` is bind-mounted into the container, so it must be a path (relative
to wherever you run `docker compose`, or absolute) — not a bare name.
`BACKUP_CRON` accepts any standard cron expression, e.g. `*/30 * * * *` (every
30 minutes) or `0 2 * * 0` (weekly, Sunday at 02:00). The install script prompts
for all four; existing installs get them backfilled by `upgrade.sh` with these
same defaults.

`BACKUP_ENABLED` is the persisted record of whether the service should run at
all — `--scale db-backup=0` only suppresses it for a single `up` invocation,
so `upgrade.sh` reads `BACKUP_ENABLED` back on every upgrade to decide whether
to re-apply that scale flag automatically, rather than guessing from
`DATABASE_URL` alone. Set it (not just the scale flag) if you want a "no" to
backups during install to stay a "no" on every future upgrade.

Scheduling is handled by `crond` inside the container, which blocks until the
next due minute rather than polling, and the container is capped at 0.5 CPU /
256MB (see `deploy.resources.limits` on the service) — so it stays effectively
idle (well under 1MB RSS, 0% CPU observed) between runs and can't compete for
host resources during the brief dump window either. Raise the memory limit in
`docker-compose.yml` directly if you have an unusually large database.

Dumps are written by the container's root user, so deleting or moving them
directly on the host may require `sudo`.

**Restore** (bundled PostgreSQL container):

```bash
gunzip -c backups/paca-<timestamp>.sql.gz | docker compose exec -T postgres psql -U ${POSTGRES_USER:-paca} -d ${POSTGRES_DB:-paca}
```

**Restore** (external PostgreSQL, using `DATABASE_URL`):

```bash
gunzip -c backups/paca-<timestamp>.sql.gz | psql "$DATABASE_URL"
```

Disable automated backups (e.g. if a managed database already handles this):

```bash
docker compose --env-file .env up -d --scale db-backup=0
```

To keep it disabled across future `upgrade.sh` runs as well, also set
`BACKUP_ENABLED=false` in `.env` — otherwise `upgrade.sh` has no record of the
choice and may re-enable it on the next upgrade.

## Development Compose

Use [`docker-compose.dev.yml`](./docker-compose.dev.yml) for local development and contributor onboarding.

When exposing the stack through a tunnel or reverse proxy, copy the example env file and set the public host:

```bash
cp deploy/.env.dev.example deploy/.env.dev
# Edit PUBLIC_HOST and VITE_ALLOWED_HOST in deploy/.env.dev
docker compose --env-file deploy/.env.dev -f deploy/docker-compose.dev.yml up -d
```

Start the full local stack in containers (no tunnel, plain localhost):

```bash
docker compose -f deploy/docker-compose.dev.yml up -d
```

Open `http://localhost:3000` once all services are healthy.

Start only shared dependencies:

```bash
docker compose -f deploy/docker-compose.dev.yml up -d postgres valkey
```

For day-to-day coding, contributors can run the application services directly on the host
and use Docker Compose only for PostgreSQL and Valkey.

### Development service ports

| Service | Port | Notes |
|---|---|---|
| Gateway (Caddy) | **3000** | Main entry point — `http://localhost:3000` |
| PostgreSQL | 5432 | Local database for development |
| Valkey | 6379 | Local cache / event streams |
| API | 8080 (internal) | Routed via gateway at `/api/` |
| Web | 3000 (internal) | Routed via gateway at `/` |
| MinIO S3 API | 9000 | Local object store (S3-compatible) |
| MinIO Console | 9001 | MinIO web UI (credentials: `minioadmin` / `minioadmin`) |

### Database backups (dev)

`docker-compose.dev.yml` includes the same `db-backup` service as production
(see [Database backups](#database-backups) above), pointed at the local
`postgres` container by default. It starts automatically with the rest of the
stack and writes dumps to `deploy/backups/`.

To test it without waiting for the cron schedule, trigger a dump on demand:

```bash
docker compose -f deploy/docker-compose.dev.yml exec db-backup run-backup.sh
ls deploy/backups/
```

To test retention pruning, backdate a dummy file past the retention window
and re-run the script:

```bash
docker compose -f deploy/docker-compose.dev.yml exec db-backup \
  sh -c 'touch -t "$(date -d "@$(($(date +%s)-10*86400))" +%Y%m%d%H%M)" /backups/paca-fake.sql.gz'
docker compose -f deploy/docker-compose.dev.yml exec db-backup run-backup.sh
ls deploy/backups/   # paca-fake.sql.gz should be gone
```

To watch the cron schedule itself fire, override `BACKUP_CRON` in
`deploy/.env.dev` (e.g. `*/2 * * * *` for every 2 minutes) and tail the logs:

```bash
docker compose --env-file deploy/.env.dev -f deploy/docker-compose.dev.yml up -d db-backup
docker compose -f deploy/docker-compose.dev.yml logs -f db-backup
```

Restore a dump into the local dev database:

```bash
gunzip -c deploy/backups/paca-<timestamp>.sql.gz | docker compose -f deploy/docker-compose.dev.yml exec -T postgres psql -U paca -d paca
```

Stop the development stack:

```bash
docker compose -f deploy/docker-compose.dev.yml down
```

Remove the Postgres volume as well:

```bash
docker compose -f deploy/docker-compose.dev.yml down -v
```

## E2E Compose

Use [`docker-compose.e2e.yml`](./docker-compose.e2e.yml) to spin up a full production-like
stack with fixed, test-safe credentials for running end-to-end tests:

```bash
docker compose -f deploy/docker-compose.e2e.yml up -d --build --wait
docker compose -f deploy/docker-compose.e2e.yml down -v
```

All secrets are intentionally weak and public — never use them outside a local E2E environment.
