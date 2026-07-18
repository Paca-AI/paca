# Galaxy-Paca runbook

Operational notes for the Galaxy deployment of Paca (`tasks.skyplatform.net`),
the fork's prod overlay `deploy/galaxy/docker-compose.galaxy.yml` (ADR-038, T8).

## Stack invocation (prod)

```bash
cd ~/Nexus/Galaxy-Paca
docker compose \
  --env-file deploy/galaxy/.env.galaxy \
  -f deploy/docker-compose.prod.yml \
  -f deploy/galaxy/docker-compose.galaxy.yml \
  up -d --scale ai-agent=0
```

No host ports: the Caddy gateway (`paca-edge`) joins `galaxy_network` (alias
`paca-gateway`); the Cloudflare tunnel terminates TLS and routes
`tasks.skyplatform.net → paca-gateway:80`. Container names are
`galaxy-paca-<service>-1` (the mcp one is `galaxy-paca-mcp`).

> **Bind-mounted `Caddyfile` inode trap:** `deploy/caddy/Caddyfile` is bind
> mounted into `paca-edge`. After editing/pulling it, recreate the container
> (`up -d --force-recreate paca-edge`) — a reload keeps the old inode.

---

## SDD Fleet (native) — ADR-038 T6

The SDD fleet dashboard is a **native** Paca plugin (`com.galaxy.sdd`), not an
iframe. It reads the SDD Coordination Server same-origin through the `/sdd-api`
proxy. The standalone `ai.skyplatform.net/sdd-server` dashboard is retired.

```
browser (Paca session cookie)
  GET /sdd-api/team/overview   (same origin, credentials: include)
        │
  paca-edge (Caddy)  handle_path /sdd-api/*  →  sdd-proxy:8791
        │  1. gate: Cookie → GET api:8080/api/v1/users/me (else 401)
        │  2. mint: POST nexus-identity:8086/internal/mint-service-token
        │           (X-Service-Secret, iss=galaxy-nexus, aud=sdd-server, RS256)
        └  3. GET-only reverse proxy → sdd-server:4830/api/*   → JSON
```

- Proxy sidecar: `deploy/galaxy/sdd-proxy/` (service `sdd-proxy`, galaxy_network
  alias `paca-sdd-proxy`). Zero-dependency Node. Secrets never logged.
- Plugin: `deploy/galaxy/plugins/com.galaxy.sdd/` (Module Federation remote,
  single exposed `SddFleetView` with an 8-view sub-rail). See its README.

### Deploy / update the plugin

Prod has **no build tooling** (no bun/npm/node/go) — build locally, ship the
`dist`:

```bash
# local
cd deploy/galaxy/plugins/com.galaxy.sdd && ./build.sh    # → frontend/dist + smoke
git commit … && git push                                  # source only (dist is gitignored)

# ship the built artifacts to prod (dist + wasm are gitignored)
rsync -az --delete frontend/dist/  galaxy-ubuntu-remote:~/Nexus/Galaxy-Paca/deploy/galaxy/plugins/com.galaxy.sdd/frontend/dist/
rsync -az        backend/backend.wasm galaxy-ubuntu-remote:~/Nexus/Galaxy-Paca/deploy/galaxy/plugins/com.galaxy.sdd/backend/backend.wasm
```

On prod, with an admin **API key**: `API_KEY=… ./install-prod.sh`. Without one,
do the same two steps directly (no api restart needed — `ListPlugins` reads the
DB per request):

```bash
cd ~/Nexus/Galaxy-Paca/deploy/galaxy/plugins/com.galaxy.sdd
ID=com.galaxy.sdd; API=galaxy-paca-api-1; PG=galaxy-paca-postgres-1
# frontend into the volume (strip html/ssr)
STAGE=$(mktemp -d); mkdir -p "$STAGE/$ID"; cp -R frontend/dist/. "$STAGE/$ID/"
rm -f "$STAGE/$ID/index.html" "$STAGE/$ID/assets/remoteEntry.ssr.js"
docker exec -u 0 "$API" rm -rf "/plugins-frontend/$ID"; docker cp "$STAGE/$ID" "$API:/plugins-frontend/"
# backend + manifest
mkdir -p "$STAGE/b/$ID"; cp backend/backend.wasm plugin.json "$STAGE/b/$ID/"
docker exec -u 0 "$API" rm -rf "/plugins/$ID"; docker cp "$STAGE/b/$ID" "$API:/plugins/"
# upsert the DB row (dollar-quoted manifest, no escaping)
M=$(jq -c . plugin.json)
printf "UPDATE plugins SET version='%s', manifest=\$M\$%s\$M\$::jsonb, updated_at=now() WHERE name='%s';\n" \
  "$(jq -r .version plugin.json)" "$M" "$ID" | docker exec -i "$PG" psql -U paca -d paca
```

> **CF cache-bust:** the CF edge caches assets ~4 h. **Bump `?v=N`** in
> `plugin.json` `frontend.remoteEntryUrl` on every deploy so browsers fetch the
> new bundle.

### Verify

```bash
curl -s -o /dev/null -w '%{http_code}\n' https://tasks.skyplatform.net/plugins/com.galaxy.sdd/assets/remoteEntry.js?v=3   # 200
docker exec galaxy-paca-api-1 sh -c 'grep -rl iframe /plugins-frontend/com.galaxy.sdd/ | wc -l'                          # 0
# /sdd-api needs a Paca session cookie → JSON; anon → 401 (session gate):
curl -s https://tasks.skyplatform.net/sdd-api/team/overview                                                              # {"error":{"code":"UNAUTHENTICATED"…}}
```

### Rollback

- Plugin: re-deploy the previous `dist` + set `plugin.json` version/`?v` back,
  re-run the DB upsert. Or disable: `UPDATE plugins SET enabled=false WHERE
  name='com.galaxy.sdd';`.
- Proxy: `docker compose … up -d --force-recreate sdd-proxy` (or scale to 0).

---

## SDD standalone decommission

- **UI off:** `SDD_SERVE_SPA=off` on the `sdd-server` service
  (`Galaxy-AI-SDD-Server/central/docker-compose.galaxy.yml`). Non-API routes
  return a `410` "moved to Galaxy Tasks" page; `/api/*`, `/ws`, `/api/ingest`
  keep working. `central/index.js` is **baked** into the image → rebuild:
  `cd central && docker compose -p central -f docker-compose.galaxy.yml -f docker-compose.paca-bridge.yml up -d --build sdd-server`.
  Roll back by setting `SDD_SERVE_SPA=on` and rebuilding.
- **Launcher off:** identity `applications.sdd-server` → `status=inactive`,
  `launcher_order/rail_order = NULL` (DB `nexus_identity`, container
  `vortex-identity-postgres`, user `identity_user`). Restore: `status='active'`,
  `launcher_order=86`, `rail_order=76`.
- **Kept:** sensor ingest, the `sdd-agent` relay (identity app untouched), the
  `central` read API (consumed via `/sdd-api`), and the paca-bridge.
