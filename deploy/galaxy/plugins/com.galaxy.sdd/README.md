# com.galaxy.sdd — SDD Sensor plugin (ADR-038 T6)

Paca plugin that embeds the existing **SDD sensor fleet dashboard**
(`https://nexus.8verse.games/sdd-server`) inside Galaxy-Paca
(`tasks.skyplatform.net`).

**v1 is deliberately a thin, frontend-only plugin (locked design):**

- The views are `<iframe>` embeds of the sensor UI, which already enforces its
  own Vortex OIDC browser auth — the plugin carries **no secrets** and calls
  **no sensor API**.
- Task-level SDD signals already reach Paca through the server-side bridge as
  task comments; this plugin only adds the fleet-level dashboard surface.
- There is **no real WASM backend** — see "The stub backend" below for why a
  1.8 MB inert `backend.wasm` still ships.

## Surfaces

| Extension point | Component | What you get |
|---|---|---|
| `view` | `SddFleetView` | "+ Add view" on any board offers an SDD Fleet plugin layout that fills the view area with the sensor dashboard. |
| `project.page` + `navItems` | `SddFleetView` | Routed full page `/projects/:projectId/plugins/com.galaxy.sdd/sdd-fleet` with an "SDD Fleet" entry (Radar icon) in the project sidebar. |
| `sidebar.project.section` | `SddSidebarCard` | Small card in the project sidebar linking the full page and opening the raw sensor in a new tab. |

`project.page` is registered in addition to the two originally-planned points
because a `view` has no stable URL (users add it per-board with a generated
view id) — the routed page is the only host mechanism the sidebar card can
deep-link. Same component, zero extra code.

## Layout

```
com.galaxy.sdd/
  plugin.json          manifest (see notes below)
  build.sh             validate manifest + build frontend & stub backend
  install-prod.sh      copy stores into the galaxy-paca volumes + register via API
  backend/             INERT stub (go.mod + main.go, no deps, no SDK)
  frontend/            Vite + @module-federation/vite remote
    src/config.ts      THE sensor URL lives here (hard-coded, see below)
    src/SddFrame.tsx   iframe + onload-timer fallback overlay
    src/SddFleetView.tsx
    src/SddSidebarCard.tsx
    smoke.mjs          replays the host loader contract against dist/
```

## Build

Requirements: `jq`, `bun` (or npm/node >= 20), optionally `go` >= 1.24.

```sh
cd deploy/galaxy/plugins/com.galaxy.sdd
./build.sh
```

This validates `plugin.json` (no formal schema exists in the repo — the checks
mirror `services/api/internal/domain/plugin/entity.go` and
`apps/web/src/lib/plugin-api.ts`), produces
`frontend/dist/assets/remoteEntry.js`, runs `frontend/smoke.mjs` (imports the
built remote and drives the exact `init`/`get` container contract
`apps/web/src/lib/plugins/loader.tsx` uses, then renders both components), and
builds `backend/backend.wasm` (Go wasip1 `c-shared`; falls back to an 8-byte
empty WASM module when Go is absent — both variants verified to load under
wazero v1.11.0 the same way `Runtime.Load` does).

## Install — dev / bind-mount stacks

The stock installer works unchanged (it needs Go, bun and jq; run from the
repo root):

```sh
./scripts/install-local-plugin.sh deploy/galaxy/plugins/com.galaxy.sdd \
  --api-url http://localhost --api-key <your-api-key>
```

It builds both halves, copies them into `plugins/local/{backend,frontend}/com.galaxy.sdd/`
(the dev compose bind mounts) and registers the plugin via
`POST /api/v1/admin/plugins`. API keys: Paca web → Settings → API Keys
(`scripts/QUICK_START.md`).

## Install — prod (galaxy-paca stack)

Prod (`deploy/docker-compose.prod.yml` + `deploy/galaxy/docker-compose.galaxy.yml`)
stores plugins in **named volumes**, not bind mounts, so the stock script's
copy step does not apply. On the prod host:

```sh
cd ~/Nexus/Galaxy-Paca && git pull
cd deploy/galaxy/plugins/com.galaxy.sdd
./build.sh
API_KEY=<paca-api-key> ./install-prod.sh
```

`install-prod.sh` stages the two store layouts, `docker cp`s them into the
running `galaxy-paca-api-1` container (`/plugins/com.galaxy.sdd` +
`/plugins-frontend/com.galaxy.sdd` — the latter is the same volume the Caddy
gateway serves read-only at `/plugins/*`), registers/updates the plugin via
the admin API **after** the files are in place (registration with
`enabled:true` loads the stub WASM immediately), and verifies that
`https://tasks.skyplatform.net/plugins/com.galaxy.sdd/assets/remoteEntry.js`
is served. Overrides: `API_URL`, `API_CONTAINER`, and the required `API_KEY`.

### Verify after install

1. `curl -fsS https://tasks.skyplatform.net/plugins/com.galaxy.sdd/assets/remoteEntry.js | head -c 60`
   → starts with `import`.
2. `curl -fsS -H "X-API-Key: $API_KEY" https://tasks.skyplatform.net/api/v1/plugins | jq '.data.plugins[] | select(.name=="com.galaxy.sdd") | {version, enabled}'`
3. In the SPA (plugins list is cached ~5 min — hard-reload): open any project →
   sidebar shows the **SDD Sensor** card and the **SDD Fleet** nav item; the
   nav item renders the embedded dashboard; "+ Add view" on a board offers the
   SDD Fleet layout.
4. `docker logs galaxy-paca-api-1 | grep 'plugin loaded'` → `name=com.galaxy.sdd`.

### Uninstall

```sh
# UUID from GET /api/v1/plugins
curl -X DELETE -H "X-API-Key: $API_KEY" https://tasks.skyplatform.net/api/v1/admin/plugins/<uuid>
docker exec -u 0 galaxy-paca-api-1 rm -rf /plugins/com.galaxy.sdd /plugins-frontend/com.galaxy.sdd
```

## Framing / CSP notes (read before debugging a blank frame)

Two independent policies decide whether the embed renders:

1. **Paca gateway side (embedding page):** `deploy/caddy/Caddyfile` sets **no
   `Content-Security-Policy` header at all** (checked at ADR-038 T6 time), so
   there is no `frame-src` restriction to allowlist — nothing to change today.
   The gateway's global `X-Frame-Options: DENY` protects *Paca* from being
   framed and is irrelevant to Paca framing the sensor. **If a CSP is ever
   added to the gateway, it must include
   `frame-src https://nexus.8verse.games` (plus `default-src` fallout) or this
   plugin's views will go blank.**
2. **Sensor side (embedded page):** the sensor's own responses must NOT forbid
   framing. If the frame stays blank and the overlay appears, on the sensor
   deployment (`nexus.8verse.games/sdd-server`) **unset `X-Frame-Options`**
   (or set `Content-Security-Policy: frame-ancestors 'self'
   https://tasks.skyplatform.net`). The Vortex OIDC login hop can add the same
   requirement on the IdP for first-time logins — opening the sensor once in a
   normal tab (the fallback link) sidesteps that.

Detection honesty: cross-origin JS cannot reliably distinguish "blocked by
XFO/frame-ancestors" from "slow" (Chrome even fires `load` for its own error
page), so the overlay is a timer heuristic (8 s) and every surface keeps a
permanent "Open in new tab" escape hatch.

## Changing the sensor URL

`frontend/src/config.ts` → `SDD_URL`, then rebuild + reinstall. Hard-coded on
purpose: the platform's `GET /api/v1/plugins` returns only the typed manifest
(no per-install config object reaches frontend components), so a config-driven
URL would be dead weight in v1.

## Manifest gotchas (host behaviour, learned the hard way)

- `"backend": {}` must stay in `plugin.json` even though the plugin is
  frontend-only: `Runtime.EmitEvent` dereferences
  `Manifest.Backend.EventSubscriptions` without a nil check.
- The API's typed manifest struct has **no `label` field** on
  `extensionPoints[]` — the labels in `plugin.json` document intent but the
  UI's "+ Add view" chip falls back to the component name (`SddFleetView`).
  `navItems[].label` IS honoured, so the sidebar says "SDD Fleet" correctly.
- Component render props come from the host per surface (`{projectId, ...}`);
  the SDK object described in `docs/plugins/` is not injected today — the
  components therefore use plain anchors and no `@paca-ai/plugin-sdk-react`
  import (which also isn't in the host share scope: only `react`, `react-dom`,
  `@tanstack/react-query` are shared — the frontend uses the classic JSX
  runtime so `react` is its only shared import).

## The stub backend

`Runtime.Load` unconditionally reads `/plugins/<id>/backend.wasm` for every
enabled plugin and install/enable fail when it is missing, so `backend/` ships
a dependency-free Go module whose build is an inert WASI reactor: no exports
the runtime calls, no routes, no events, no SDK. Full rationale in
`backend/main.go`.

## TODO — v2

- Real module-federation views (fleet table, per-sensor drill-down) rendering
  native Paca UI instead of an iframe, reading the sensor API through a real
  WASM backend proxy (`paca.fetch` + `backend.allowedOutboundDomains:
  ["nexus.8verse.games"]`) so browser CORS/cookies stop mattering.
- Per-install sensor URL once the host grows a config surface that reaches
  frontend components.
- Upstream a frontend-only escape hatch (skip `LoadWASM` when
  `manifest.backend` declares nothing) and drop the stub.
