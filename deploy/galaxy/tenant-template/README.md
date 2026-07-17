# Instance-per-tenant Paca (ADR-038 T7)

Paca upstream has no tenant column — every table assumes one organization.
Rather than retrofitting RLS across the whole schema (the ADR-036 path for
multi-tenant-aware apps), ADR-038 T7 isolates tenants at the *stack* level:
**one full Paca stack per tenant, same repo, same images, same compose
files** — only the env file and the Docker Compose *project name* differ.
Hard isolation (separate Postgres, Valkey, MinIO, gateway) with zero fork
drift per tenant.

## What provides the isolation

Verified against `deploy/docker-compose.prod.yml` +
`deploy/galaxy/docker-compose.galaxy.yml`:

| Concern | Mechanism |
| --- | --- |
| Volumes | All 8 named volumes (`postgres_data`, `valkey_data`, `minio_data`, `backend_plugins`, `frontend_plugins`, `mcp_plugins`, `caddy_data`, `caddy_config`) declare no `name:`/`external:`, so `-p paca-<tenant>` yields `paca-<tenant>_postgres_data`, … |
| Containers | No service sets `container_name` → names derive from the project name; no collisions. |
| Stack network | The private `default` network becomes `paca-<tenant>_default`. |
| Host ports | The galaxy overlay strips ALL host port bindings (`ports: !override []`); traffic enters via the Cloudflare tunnel on `galaxy_network` only, so any number of tenants coexist on one host. |
| Ingress | Only the gateway joins the shared external `galaxy_network`, under the per-tenant alias `GATEWAY_NETWORK_ALIAS=paca-<tenant>-gateway` (env-parameterized in `docker-compose.galaxy.yml`; default `paca-gateway` keeps the existing shared instance untouched). |
| Backups | `BACKUP_DIR=/backup/paca-<tenant>-postgres` — one dump directory per tenant on the host HDD. |
| Identity | Each tenant gets its own Vortex OIDC client `paca-<tenant>` and its own local `users` table; JWT/encryption secrets are freshly generated per tenant. |

Project name precedence note: `docker-compose.galaxy.yml` carries
`name: galaxy-paca` for the shared instance; both the `-p` flag and
`COMPOSE_PROJECT_NAME` (set inside `.env.tenant`) override it — the
provision script wires both so they can never disagree.

## Provisioning a tenant

```bash
cd deploy/galaxy/tenant-template
./provision-tenant-paca.sh <tenant_code> <domain>
# e.g.
./provision-tenant-paca.sh vietjet tasks-vietjet.skyplatform.net
```

The script:

1. Validates `tenant_code` (`[a-z0-9][a-z0-9-]{1,29}`) and the domain.
2. Creates `~/Nexus/paca-tenants/<tenant_code>/` (override root with
   `PACA_TENANTS_DIR`) and renders `.env.tenant` (mode 600) from
   `env.tenant.template`:
   - `__TENANT__` / `__DOMAIN__` placeholders replaced;
   - fresh `openssl rand -hex 32` for every secret (Postgres, JWT, admin
     break-glass, 64-hex `ENCRYPTION_KEY`, MinIO, agent pre-shared keys —
     each one distinct);
   - `COMPOSE_PROJECT_NAME=paca-<tenant>`,
     `GATEWAY_NETWORK_ALIAS=paca-<tenant>-gateway`,
     `OIDC_CLIENT_ID=paca-<tenant>`,
     `BACKUP_DIR=/backup/paca-<tenant>-postgres`.
3. Refuses to overwrite an existing `.env.tenant` (those are the live
   stack's credentials).
4. Prints the deploy command and the manual checklist (below).

It never touches Docker, Cloudflare, or Vortex itself.

## Deploying

From the repo root (`~/Nexus/Galaxy-Paca` on the prod host):

```bash
docker compose -p paca-<tenant> \
  --env-file ~/Nexus/paca-tenants/<tenant>/.env.tenant \
  -f deploy/docker-compose.prod.yml \
  -f deploy/galaxy/docker-compose.galaxy.yml \
  up -d --scale ai-agent=0
```

Same two compose files as the shared instance — never copy or fork them per
tenant. Upgrades = bump the image pins in each tenant's `.env.tenant`
(deliberately, per tenant) and re-run the same command.

## Manual checklist per tenant (printed by the script)

1. **Vortex identity** — register OAuth client `paca-<tenant>` with redirect
   URL `https://<domain>/api/v1/auth/oidc/callback`; paste the issued secret
   into `OIDC_CLIENT_SECRET`.
2. **Cloudflare tunnel ingress** — hostname `<domain>` → service
   `http://paca-<tenant>-gateway:80` (the tunnel container shares
   `galaxy_network`, so the alias resolves).
3. **DNS** — CNAME `<domain>` → the tunnel's `cfargotunnel.com` target
   (proxied).
4. **Login test** — OIDC SSO end-to-end at `https://<domain>`.
5. **First backup verify** — a dump appears under
   `/backup/paca-<tenant>-postgres` after the 02:00 cron; test-restore one.

## Galaxy integrations per tenant

- **Chat dock (P3.2)**: enabled by default via `GALAXY_DOCK_SRC`; the
  tenant's Caddy bridges `/dock.js` + `/api/agentops*` + `/api/identity*`
  to `http://vortex-gateway` over `galaxy_network`.
- **notify-bridge (P3.1)**: ships **disabled** (`BRIDGE_ENABLED=false`).
  Each tenant stack has its own Valkey/Postgres, so flipping it on per
  tenant is safe — consumer groups never cross stacks; notifications land
  in the recipient's Galaxy inbox via their Vortex sub.

## Removing a tenant

```bash
docker compose -p paca-<tenant> ... down        # keeps volumes
docker compose -p paca-<tenant> ... down -v     # DANGER: wipes data
```

Also remove the Cloudflare ingress rule + DNS record, deactivate the
`paca-<tenant>` OIDC client in Vortex, and archive
`/backup/paca-<tenant>-postgres`.
