# com.galaxy.sdd — SDD Fleet plugin (ADR-038)

**Native** Spec-Driven Development fleet dashboard rendered *inside* Galaxy-Paca
(`tasks.skyplatform.net`). No iframe, no chart libraries, no secrets in the
browser — a Module Federation remote that fetches the SDD Coordination Server
**same-origin** through the `/sdd-api` gateway proxy.

> Supersedes the v0.1 iframe wrapper (which embedded
> `ai.skyplatform.net/sdd-server`). The standalone dashboard is retired; the
> sensor keeps ingesting and the paca-bridge keeps flowing comments.

## What it renders

One project page ("SDD Fleet", Radar icon) with a **left sub-rail** of eight
team-wide views and client-side sub-routing (remembered in `localStorage`):

| View | Endpoint (`/sdd-api/*`) | Ported from |
|---|---|---|
| Overview | `/team/overview` | `TeamDashboard.tsx` |
| Task board (read-only) | `/tasks` | `TeamKanban.tsx` |
| Sessions | `/sessions` | `Sessions` |
| Activity | `/events` | `ActivityFeed` |
| Analytics | `/team/analytics` | `TeamAnalytics.tsx` |
| Coordination | `/team/coordination` | `TeamCoordination.tsx` |
| SDD phases | `/sdd` + `/sdd/spec-versions` + `/sdd/flags` | `Sdd.tsx` |
| Fleet | `/team/fleet` | `TeamFleet.tsx` |

i18n: Vietnamese (default), English, 中文 — toggle in the header. Cards, tables,
bars, spark and timeline are dependency-free inline SVG / CSS, theme-aware
(light + dark, blending with the host's `--foreground`/`--border` tokens).

## Data path (no secret in the browser)

```
browser (Paca session cookie)
  fetch("/sdd-api/team/overview", { credentials: "include" })   ← sdd-api.ts
        │ same origin
  paca-edge (Caddy)  handle_path /sdd-api/*  →  sdd-proxy sidecar
        │  gate on Paca session · mint RS256 service token (identity)
  sdd-server:4830 /api/team/overview  →  JSON
```

See `deploy/galaxy/sdd-proxy/` for the proxy. The plugin holds no credential;
it reads exactly what a logged-in Paca member may read (team-wide telemetry).
Only GET reads are proxied — the human task board lives in Paca (ADR-038 T6),
so the Task board view is **read-only**.

## Architecture notes

- **Class components + classic JSX** (`React.createElement`, no hooks) — the
  host share scope provides `react` but not `react/jsx-runtime`, and class
  components survive a federation fallback to the bundled React copy where hooks
  would crash. Stateless presentational helpers are hook-free functions.
- **Single exposed component** `SddFleetView` (federation `exposes`); the eight
  views live behind it and switch in-component.
- **60 s shared cache** in `sdd-api.ts` (per endpoint) — switching views is
  instant; the header Refresh clears it and forces a refetch.
- **Inert stub backend** (`backend/backend.wasm`) — the plugin declares no
  routes/events; the API's runtime still `Load`s a WASM module, so a minimal
  one must exist. Not tracked in git (built by `build.sh`).

## Source layout (`frontend/src`)

```
config.ts      constants + the 8 ViewKeys
types.ts       API response shapes (mirror central/index.js JSON)
sdd-api.ts     the ONLY fetch module — same-origin /sdd-api, 60 s cache
i18n.ts        vi / en / zh dictionaries + t()
icons.tsx      inline-SVG icon set (no lucide dependency)
theme.ts       injected <style> (namespaced gxsd-, light + dark)
base.tsx       DataView base class + presentational primitives
views.tsx      the 8 view components + the VIEWS registry
SddFleetView.tsx  the shell: sub-rail + header + active view
```

## Build & deploy

```bash
./build.sh                     # validate manifest → vite federation build → smoke
                               #   (needs bun or npm + node; jq; Go optional)
API_KEY=<paca-api-key> ./install-prod.sh
```

`install-prod.sh` copies `frontend/dist` + `plugin.json` + `backend.wasm` into
the prod volumes (via `galaxy-paca-api-1`) and registers/updates the plugin
through the admin API. Where no API key is available, the same two steps can be
done directly: `docker cp` the dist into `galaxy-paca-api-1:/plugins-frontend/…`
and `/plugins/…`, then upsert the `plugins` row (`ListPlugins` reads the DB
per-request, so no API restart is needed).

### Cache busting

The CF edge caches assets ~4 h. `frontend.remoteEntryUrl` carries a `?v=N`
query — **bump N** on every deploy so browsers fetch the new bundle.
`build.sh` allows the optional query in its manifest lint.
