# Plugin System Overview

Paca's plugin system lets developers extend the product without forking the core. Plugins can add UI surfaces (views, sidebar sections, task detail panels, project settings tabs), register backend HTTP routes, and run server-side logic with scoped access to the database and event bus.

## Goals

- Let the community ship features independently of the core release cycle.
- Allow teams to install only the capabilities they need.
- Provide a safe, sandboxed execution model on both frontend and backend.
- Migrate first-party features (BDD scenarios, checklists, GitHub integration, time tracking) into plugins as the proof-of-concept for the system.

## Non-Goals

- Hot-reload of backend plugins at runtime without a process restart is deferred.
- Cross-plugin communication (plugins calling each other) is out of scope for v1.

## Marketplace

Paca includes an admin marketplace UI backed by a public GitHub catalog.

- Catalog source: `Paca-AI/paca-plugins` (`catalog/plugins.json`)
- Publish model: plugin developers contribute via pull requests
- Install flow: API downloads artifact tarballs, installs assets, runs migrations, and loads the plugin runtime module

See [marketplace.md](marketplace.md) for schema and operational details.

## Key Concepts

| Concept | Description |
|---|---|
| **Plugin** | A versioned bundle of frontend and/or backend code that declares its extension points in a manifest. |
| **Extension Point** | A named slot in the Paca UI or backend where a plugin can inject behaviour. |
| **Plugin Manifest** | A `plugin.json` file that declares the plugin's ID, version, minimum required Paca version (`minCoreVersion`), permissions, and extension point registrations. |
| **Plugin Registry** | The per-installation record of which plugins are installed, enabled, and at what version. |
| **Plugin SDK** | The TypeScript (`@paca-ai/plugin-sdk-react`), Go (`github.com/Paca-AI/plugin-sdk-go`), and MCP (`@paca-ai/plugin-sdk-mcp`) packages that provide typed APIs against the Paca host. |

## Architecture at a Glance

```
┌─────────────────────────────────────────────────────────────────┐
│  Browser                                                        │
│                                                                 │
│  ┌──────────────────┐     Module Federation / ES modules        │
│  │   apps/web        │◄───────────────────────────────────────┐ │
│  │  (host app)       │                                         │ │
│  └──────────────────┘                                         │ │
│                                                                │ │
│  ┌──────────────────┐  ┌──────────────────┐                   │ │
│  │ Plugin A (JS/CSS)│  │ Plugin B (JS/CSS)│  ... (remote entry│ │
│  │  micro-frontend  │  │  micro-frontend  │       served by   │ │
│  └──────────────────┘  └──────────────────┘       plugin CDN) │ │
└────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│  services/api  (Go)                                            │
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Plugin Runtime (wazero)                                 │  │
│  │                                                          │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌───────────────┐  │  │
│  │  │ plugin-bdd   │  │ plugin-gh    │  │ plugin-time   │  │  │
│  │  │   .wasm      │  │   .wasm      │  │   .wasm       │  │  │
│  │  └──────────────┘  └──────────────┘  └───────────────┘  │  │
│  │          │                │                  │           │  │
│  │          └────────────────┴──────────────────┘           │  │
│  │                           │                              │  │
│  │              Host Function Bridge                        │  │
│  │      (db_query, db_exec, http_register, event_emit)      │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                │
│  Core API (Gin router, domain services, PostgreSQL, Valkey)    │
└────────────────────────────────────────────────────────────────┘
```

## Extension Points

| ID | Surface | Description |
|---|---|---|
| `sidebar.general.section` | General sidebar | Adds a collapsible section to the global left navigation. |
| `sidebar.project.section` | Project sidebar | Adds a collapsible section inside a project's sidebar navigation. |
| `task.detail.section` | Task detail panel | Adds a panel below the description in the task drawer/page. |
| `project.settings.tab` | Project settings | Adds a tab to the project settings page. |
| `view` | Main content area | Registers a full view (e.g., Gantt, Roadmap, Calendar) as a selectable board view. |
| `api.route` | Backend | Registers one or more HTTP routes under `/api/v1/plugins/{pluginId}/`. |
| `event.handler` | Backend | Subscribes to core domain events (task created, sprint closed, etc.). |
| `mcp.tools` | MCP server | Exposes AI-callable MCP tools through the Paca MCP server. |

## Plugin Lifecycle

```
Uploaded → Validated (manifest + WASM signature check) → Installed
         → Enabled per project (or globally)
         → Routes registered at startup / plugin enable
         → UI loaded lazily when user navigates to extension point
         → Disabled → Uninstalled (data retained unless plugin opts-in to cleanup)
```

## Security Model

### Frontend
- Plugin JS bundles are loaded from a configurable plugin CDN origin.
- The host app enforces a strict Content Security Policy; plugin origins must be allowlisted in the server config.
- Plugins receive only the context object the host explicitly passes to each extension point — they cannot access the host's React tree or internal stores directly.
- Plugins should be sandboxed in an iframe for untrusted/third-party plugins (v2 consideration).

### Backend
- Each WASM plugin runs in an isolated `wazero` module with no access to the host filesystem.
- The host function bridge enforces row-level scoping: every DB call is implicitly filtered to the plugin's authorised project scope.
- Plugins cannot execute arbitrary SQL; they call typed host functions (`db.QueryTasks`, `db.CreateCustomRecord`, etc.).
- A per-plugin permission list in `plugin.json` gates which host functions are available.
- WASM execution is CPU/memory-limited via `wazero`'s resource controls.

## Directory Structure

```
plugins/                          ← local plugin store
  local/
    backend/                      ← Backend WASM binaries, migrations, manifests
      <plugin-id>/
        plugin.json
        backend.wasm
        migrations/
    frontend/                     ← Frontend JS/CSS bundles
      <plugin-id>/
        assets/
          remoteEntry.js
          ...
  README.md                       ← This file
```

The Plugin SDKs are now maintained in separate repositories:

- **Backend SDK (Go)**: [github.com/Paca-AI/plugin-sdk-go](https://github.com/Paca-AI/plugin-sdk-go)
- **Frontend SDK (React/TypeScript)**: [github.com/Paca-AI/plugin-sdk-react](https://github.com/Paca-AI/plugin-sdk-react)

## Related Documents

- [Frontend Plugin System](frontend-plugin-system.md) — module federation, extension point registry, SDK API for UI.
- [Backend Plugin System](backend-plugin-system.md) — WASM runtime, host function bridge, route registration.
- [SDK Reference](sdk-reference.md) — full API reference for both SDKs.
- [First-Party Plugins](first-party-plugins.md) — migration plan for BDD, Checklist, GitHub, and Time Tracking.
- [Developer Guide](developer-guide.md) — step-by-step guide to writing your first plugin.
