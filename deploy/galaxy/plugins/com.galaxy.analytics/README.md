# com.galaxy.analytics — Agile analytics plugin (ADR-038 P3.4)

Paca plugin that renders **native analytics views** for a project — sprint
progress, velocity, status distribution and a per-sprint report — as real
Module Federation components with dependency-free inline SVG charts.

**No iframe, no chart library, no secrets, no backend behaviour:**

- The views call the **same-origin REST API** (`/api/v1/projects/{id}/...`)
  from the browser with `credentials: "include"`, i.e. the caller's own Paca
  session cookie. The plugin sees exactly what the signed-in user can see
  (`tasks.read`/`sprints.read` are enforced server-side) and holds no keys.
- All aggregation is computed **client-side** (`frontend/src/analytics.ts`).
- Charts are hand-rolled inline SVG (`frontend/src/charts.tsx`) to keep the
  remote small — the built `remoteEntry.js` is ~14 KB (~5 KB gzip).
- The WASM backend is the same **inert stub** pattern as `com.galaxy.sdd`
  (the host requires a loadable `backend.wasm` for every enabled plugin).

## Surfaces

| Extension point | Component | What you get |
|---|---|---|
| `project.page` + `navItems` | `AnalyticsView` | Routed full page `/projects/:projectId/plugins/com.galaxy.analytics/analytics` with an "Analytics" entry (ChartNoAxesCombined icon) in the project sidebar. |
| `view` | `AnalyticsView` | "+ Add view" on any board offers an Analytics plugin layout that fills the view area with the same dashboard. |

At the `view` surface the host passes `{projectId, tasks, statuses, ...}`,
but that task list is the current view's **filtered page** — the component
deliberately ignores it and fetches the full project snapshot itself so all
four panels see every sprint plus the backlog.

## The four panels (and their honest approximations)

Paca v1 keeps **no task history**, so anything historical is approximated
from the current snapshot — each approximation is documented in the UI
footnote and here:

1. **Sprint Progress** (honest burndown v1) — for the active sprint (selector
   appears when several are active): hero completion %, meter, done/total
   story points and tasks, and a burndown plot showing the **ideal line**
   (start_date → end_date) plus **today's actual remaining** as a single
   marker. Past days are NOT reconstructed. Sprints without story points fall
   back to task counts (labeled). Sprints without dates get the meter but an
   empty-state instead of the plot.
2. **Velocity** — story points on done-category tasks per **completed**
   sprint, bar chart with an average annotation. This one is exact, not
   approximate: completing a sprint moves unfinished tasks out and keeps done
   tasks "for record-keeping" (see `docs/api/http-design.md`), so the done
   points still attached to a completed sprint ARE its delivered points.
3. **Status Distribution** (CFD v1) — current-snapshot stacked counts by
   status **category** (`backlog refinement ready todo inprogress done`, plus
   a "No status" bucket) for the backlog row and every active/planned sprint.
   Completed sprints are excluded (they only retain done tasks — their
   distribution is a tautology). A real cumulative-flow diagram needs daily
   history; this is the v1 stand-in.
4. **Sprint Report** — table per sprint: tasks done/attached, still-open
   count, points done, points total. For **completed** sprints the original
   commitment is not reconstructable (unfinished tasks were moved out at
   completion), so "Open" is shown as "—" and the counts reflect *delivery*.

"Done" always means: the task's status **category** is `done`.

## Data access

One shared fetch module (`frontend/src/paca-api.ts`):

- `GET /projects/{id}/sprints`, `GET /projects/{id}/task-statuses` — single
  envelope `{success, data: {items}, request_id}` calls.
- `GET /projects/{id}/tasks?page_size=100[&cursor=...]` — looped until
  `next_cursor` is null. NOTE: the tasks endpoint is **cursor-paginated** in
  the implementation (`task_handler.go`, page_size clamped 1..200) even
  though http-design.md's generic recommendation says page/page_size.
- **60s in-memory cache** keyed by project, storing the in-flight promise so
  concurrent surfaces share one round trip; failures self-evict so Retry
  works immediately.
- Errors render a graceful panel; 401/403 gets "Session expired — reload the
  app" (the plugin cannot drive the SPA's token refresh).

## Theme

`frontend/src/theme.ts` injects one `<style>` tag. Ink/border colors ride the
host's own tokens (`--foreground`, `--muted-foreground`, `--border`) with
mode-correct fallbacks; series colors are fixed hexes per mode keyed off the
`light`/`dark` class the SPA stamps on `<html>` (plus a `prefers-color-scheme`
fallback). The palette is the dataviz reference palette, validator-passed for
both surfaces: categorical slots 1-6 map FIXED to the six status categories
(color follows the category, never the row), the single-series accent is
slot-1 blue, and the legend always shows visible per-category counts (three
light-mode hues sit below 3:1 contrast — the counts are the required relief;
the 2px surface gaps between stacked segments are the CVD secondary encoding).

## Layout

```
com.galaxy.analytics/
  plugin.json          manifest (backend MUST stay a non-null {} — see below)
  build.sh             validate manifest + build frontend & stub backend
  install-prod.sh      copy stores into the galaxy-paca volumes + register via API
  backend/             INERT stub (go.mod + main.go, no deps, no SDK)
  frontend/            Vite + @module-federation/vite remote
    src/config.ts      API base, page size, cache TTL
    src/paca-api.ts    envelope + cursor pagination + 60s cache (the ONLY fetcher)
    src/analytics.ts   pure computations (no React, no IO)
    src/theme.ts       injected CSS, validated palette, light/dark
    src/charts.tsx     SVG primitives: burndown, velocity bars, stacked rows
    src/AnalyticsView.tsx  the single exposed component (all four panels)
    smoke.mjs          replays the host loader contract against dist/
```

## Build

Requirements: `jq`, `bun` (or npm/node >= 20), optionally `go` >= 1.24.

```sh
cd deploy/galaxy/plugins/com.galaxy.analytics
./build.sh
```

This validates `plugin.json` (mirrors
`services/api/internal/domain/plugin/entity.go` +
`apps/web/src/lib/plugin-api.ts`), produces
`frontend/dist/assets/remoteEntry.js`, runs `frontend/smoke.mjs` (imports the
built remote, drives the exact `init`/`get` container contract
`apps/web/src/lib/plugins/loader.tsx` uses, then renders the exposed
component twice — bare "loading" frame AND seeded with a fixture via the
test-only `__testData` prop so every panel, chart and the honesty footnote
are asserted, and `<iframe` is asserted absent), and builds
`backend/backend.wasm` (Go wasip1 `c-shared`; 8-byte empty-module fallback
when Go is absent — both load under wazero the same way `Runtime.Load` does).

## Install — dev / bind-mount stacks

```sh
./scripts/install-local-plugin.sh deploy/galaxy/plugins/com.galaxy.analytics \
  --api-url http://localhost --api-key <your-api-key>
```

## Install — prod (galaxy-paca stack)

Prod stores plugins in **named volumes**, so on the prod host:

```sh
cd ~/Nexus/Galaxy-Paca && git pull
cd deploy/galaxy/plugins/com.galaxy.analytics
./build.sh
API_KEY=<paca-api-key> ./install-prod.sh
```

`install-prod.sh` stages the two store layouts, `docker cp`s them into the
running `galaxy-paca-api-1` container, registers/updates the plugin via the
admin API after the files are in place, and verifies the gateway serves
`/plugins/com.galaxy.analytics/assets/remoteEntry.js`. Overrides: `API_URL`,
`API_CONTAINER`, and the required `API_KEY`.

### Verify after install

1. `curl -fsS https://tasks.skyplatform.net/plugins/com.galaxy.analytics/assets/remoteEntry.js | head -c 60` → starts with `import`.
2. `curl -fsS -H "X-API-Key: $API_KEY" https://tasks.skyplatform.net/api/v1/plugins | jq '.data.plugins[] | select(.name=="com.galaxy.analytics") | {version, enabled}'`
3. In the SPA (plugins list cached ~5 min — hard-reload): open any project →
   sidebar shows the **Analytics** nav item; the page renders the four
   panels from live data; "+ Add view" offers the Analytics layout.
4. `docker logs galaxy-paca-api-1 | grep 'plugin loaded'` → `name=com.galaxy.analytics`.

### Uninstall

```sh
# UUID from GET /api/v1/plugins
curl -X DELETE -H "X-API-Key: $API_KEY" https://tasks.skyplatform.net/api/v1/admin/plugins/<uuid>
docker exec -u 0 galaxy-paca-api-1 rm -rf /plugins/com.galaxy.analytics /plugins-frontend/com.galaxy.analytics
```

## Manifest gotchas (host behaviour — same list as com.galaxy.sdd)

- `"backend": {}` must stay in `plugin.json` even though the plugin is
  frontend-only: `Runtime.EmitEvent` dereferences
  `Manifest.Backend.EventSubscriptions` without a nil check.
- The API's typed manifest struct has **no `label` field** on
  `extensionPoints[]` — the "+ Add view" chip falls back to the component
  name (`AnalyticsView`). `navItems[].label` IS honoured ("Analytics").
- Component render props come from the host per surface; no SDK object is
  injected — the frontend uses plain `fetch` and the classic JSX runtime so
  `react` is its only shared import.

## TODO — v2

- Real burndown/CFD history: either a small WASM backend that snapshots
  per-day sprint state into plugin storage, or (better) upstream task status
  history and read it here.
- Per-chart drill-down (click a bar → filtered task list) once the host
  exposes router/navigation to plugins.
- Scope filters (assignee, task type) in one filter row above the panels.
- AI companion: the deploy/galaxy/skills/galaxy-sprint-health skill reads the
  same numbers through the agent's MCP tools — link the two surfaces when
  plugins can deep-link agent chats.
