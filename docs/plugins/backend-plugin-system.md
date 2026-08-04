# Backend Plugin System

## Overview

Backend plugins run as **WebAssembly (WASM) modules** inside the `services/api` Go process. The host uses [wazero](https://github.com/tetratelabs/wazero) — a pure-Go, zero-CGo WASM runtime — to load and execute plugin modules in a sandboxed environment.

Each plugin:
- Is a single `.wasm` binary compiled from Go (via TinyGo), Rust, AssemblyScript, or any other language with a WASM target.
- Declares the host functions it needs in its `plugin.json` manifest (capability-based permission model).
- Communicates with the host exclusively through a well-defined **host function bridge** — it cannot read host memory, access the filesystem, or make raw network calls.

## Host Function Bridge

The host function bridge exposes a set of typed functions that WASM modules can import from the `env` module. All host functions use a **linear memory protocol**: the plugin writes a request payload (Protocol Buffers) into shared linear memory and calls the host function with a pointer and length. The host reads the request, executes it, writes the response back into a plugin-provided buffer, and returns the response length.

### Protobuf Message Transport

```
Plugin memory layout for a host call:
  [req_ptr, req_len] → plugin writes serialised proto request
  host function called → host reads request, executes, writes response
  [resp_ptr, resp_len] → plugin reads serialised proto response
```

The SDK (see [sdk-reference.md](sdk-reference.md)) hides this transport entirely behind idiomatic Go or Rust function calls.

### Available Host Functions

All host functions are imported from the `paca` module namespace.

#### Database Access

| Function | Description |
|---|---|
| `paca.db_query(req_ptr, req_len, resp_ptr, max_len) → resp_len` | Execute a typed read query. Results are filtered to the plugin's authorised scope. |
| `paca.db_exec(req_ptr, req_len, resp_ptr, max_len) → resp_len` | Execute a write (insert/update/delete) against plugin-owned tables or allowed shared tables. |
| `paca.db_tx_begin() → tx_id` | Begin a database transaction. |
| `paca.db_tx_commit(tx_id)` | Commit a transaction. |
| `paca.db_tx_rollback(tx_id)` | Roll back a transaction. |

Plugins **cannot** issue arbitrary SQL. They use typed query builders exposed by the SDK, which map to pre-approved query templates validated by the host bridge.

#### Plugin-Owned Storage

Each plugin gets a dedicated namespace in the database (`plugin_data_{pluginId}`) for its own tables. Schema migrations for plugin-owned tables are declared in the plugin manifest and run by the host during plugin installation/upgrade using a restricted migration runner.

| Function | Description |
|---|---|
| `paca.storage_get(key_ptr, key_len, resp_ptr, max_len) → resp_len` | Get a value from the plugin's key-value store (backed by a PostgreSQL JSONB column). |
| `paca.storage_set(key_ptr, key_len, val_ptr, val_len) → ok` | Set a value in the plugin's key-value store. |
| `paca.storage_delete(key_ptr, key_len) → ok` | Delete a key from the plugin's key-value store. |

#### Plugin Cache (TTL, Valkey/Redis-backed)

Backed by the host's shared Valkey/Redis instance, keyed under a per-plugin namespace so plugins can't read or overwrite each other's entries. Unlike Plugin-Owned Storage above, entries can expire and there is no durability guarantee — treat the cache as recomputable data only. If the host has no cache backend configured, `cache_get` always misses and `cache_set`/`cache_delete` are no-ops, so plugins should always be able to fall back to recomputing a value on a miss.

| Function | Description |
|---|---|
| `paca.cache_get(key_ptr, key_len, value_ptr_ptr, value_len_ptr)` | Get a value from the plugin's cache. A zero-length response means a miss (never cached, expired, or no cache backend configured). |
| `paca.cache_set(key_ptr, key_len, val_ptr, val_len, ttl_seconds) → ok` | Set a value in the plugin's cache with the given TTL in seconds. `ttl_seconds = 0` stores the value with no expiry; negative values are rejected (`ok = 0`). |
| `paca.cache_delete(key_ptr, key_len) → ok` | Delete a key from the plugin's cache. |

#### Core Data Access (Read-Only, Scoped)

| Function | Description |
|---|---|
| `paca.tasks_list(req_ptr, req_len, resp_ptr, max_len) → resp_len` | List tasks for the authorised project(s) with filter options. |
| `paca.task_get(req_ptr, req_len, resp_ptr, max_len) → resp_len` | Get a single task by ID (project scope enforced). |
| `paca.project_get(req_ptr, req_len, resp_ptr, max_len) → resp_len` | Get project metadata. |
| `paca.members_list(req_ptr, req_len, resp_ptr, max_len) → resp_len` | List project members. |

#### Event System

| Function | Description |
|---|---|
| `paca.event_subscribe(event_ptr, event_len) → ok` | Subscribe to a named core domain event (e.g., `task.created`, `sprint.closed`). Events are delivered at plugin startup based on manifest declarations. |
| `paca.event_emit(req_ptr, req_len) → ok` | Emit a plugin-namespaced event (e.g., `com.paca.bdd.scenario_created`) to the Valkey Stream for consumption by the realtime service or other listeners. |

#### HTTP Response (used inside route handlers)

| Function | Description |
|---|---|
| `paca.http_respond(req_ptr, req_len)` | Write an HTTP response from within a registered route handler. Includes status code, headers, and body. |
| `paca.http_request_body(resp_ptr, max_len) → resp_len` | Read the incoming HTTP request body inside a route handler. |
| `paca.http_request_headers(resp_ptr, max_len) → resp_len` | Read incoming request headers. |
| `paca.http_caller_identity(resp_ptr, max_len) → resp_len` | Read the authenticated caller's user ID and project membership (JWT claims, validated by the host). |

#### Logging

| Function | Description |
|---|---|
| `paca.log(level, msg_ptr, msg_len)` | Write a structured log entry at the given level. Entries are tagged with the plugin ID. |

### Capability Permissions

Each host function group maps to a permission in `plugin.json`:

```json
{
  "permissions": [
    "db:read:tasks",
    "db:read:members",
    "db:write:plugin_data",
    "http:register_routes",
    "events:subscribe:task.*",
    "events:emit"
  ]
}
```

The host validates the requested permissions at install time against the allowlist for the installation tier (self-hosted installations can grant all permissions; future SaaS tiers may restrict).

## Plugin Manifest (`plugin.json`)

```json
{
  "id": "com.paca.bdd",
  "name": "BDD Scenarios",
  "version": "1.0.0",
  "description": "Adds Given/When/Then acceptance criteria to tasks.",
  "author": "Paca Core Team",
  "license": "MIT",
  "minCoreVersion": "0.5.0",

  "frontend": {
    "remoteEntryUrl": "https://plugins.paca.app/bdd/1.0.0/remoteEntry.js",
    "extensionPoints": [
      {
        "point": "task.detail.section",
        "component": "TaskDetailSection",
        "label": "BDD Scenarios",
        "order": 10
      },
      {
        "point": "project.settings.tab",
        "component": "ProjectSettingsTab",
        "label": "BDD",
        "order": 20
      }
    ]
  },

  "backend": {
    "wasm": "bdd.wasm",
    "permissions": [
      "db:read:tasks",
      "db:write:plugin_data",
      "http:register_routes",
      "events:subscribe:task.deleted",
      "events:emit"
    ],
    "routes": [
      {
        "method": "GET",
        "path": "/tasks/:taskId/bdd-scenarios"
      },
      {
        "method": "POST",
        "path": "/tasks/:taskId/bdd-scenarios",
        "middlewares": [
          { "name": "authn" },
          { "name": "requireFreshPassword" },
          {
            "name": "requirePermissions",
            "scope": "project",
            "permissions": ["tasks.write"]
          }
        ]
      },
      {
        "method": "POST",
        "path": "/webhook",
        "middlewares": [
          { "name": "optionalAuthn" }
        ]
      }
    ],
    "migrations": [
      "0001_create_bdd_scenarios.sql"
    ],
    "eventSubscriptions": [
      "task.deleted"
    ]
  }
}
```

All routes declared under `backend.routes` are automatically mounted at `/api/v1/plugins/{pluginId}/projects/:projectId/{path}`.

### Minimum Host Version (`minCoreVersion`)

A manifest may declare `minCoreVersion` as a strict `X.Y.Z` (or `vX.Y.Z`) semver string — the lowest Paca (host) build the plugin is compatible with. The host enforces this on every path that persists a manifest:

- `POST /api/v1/admin/plugins` (direct install)
- `POST /api/v1/admin/plugins/marketplace/install` (marketplace install)
- `PATCH /api/v1/admin/plugins/:pluginId` and `POST /api/v1/admin/plugins/:pluginId/upgrade` (manifest updates/upgrades)

If the running build's version is older than `minCoreVersion`, the request fails with `PLUGIN_INCOMPATIBLE_HOST_VERSION` before any migrations run or the new manifest is loaded into the runtime, and nothing is persisted. Omitting the field means the plugin has no minimum. A host not built from a tagged release (`PACA_VERSION` unset, reporting `"dev"`) is treated as unconstrained, since there's no release version to compare against.

The error response's `error` field is an English-only message meant for logs/CLI use. Clients that need to render a localized message should instead use `error_details`, an `{"error_details": {...}}` object included alongside `error_code`/`error` for this code, carrying `plugin_id`, `required_version`, and `host_version` as plain (non-localized) strings to interpolate into their own translated copy.

### Route Middleware Policy

Each backend route can declare a host-enforced middleware chain in `backend.routes[].middlewares`.

Supported middleware names:
- `authn`
- `optionalAuthn`
- `requireFreshPassword`
- `requireJWTAuth`
- `requirePermissions`

`requirePermissions` options:
- `scope`: `global` or `project` (default: `global`)
- `projectParam`: route param name for project scope (default: `projectId`)
- `permissions`: required permission keys (for example `projects.read`, `tasks.write`)

If `middlewares` is omitted, the host applies the backward-compatible default policy:
- `optionalAuthn`
- `requireFreshPassword`
- `requirePermissions` with project scope and `projects.read`

For legacy manifests, `backend.routes[].public: true` is still supported and means "no host auth middleware" for that route.

## Plugin Runtime Lifecycle

### Startup

1. At `services/api` startup, the host reads the list of enabled plugins from the `plugins` table.
2. For each plugin, it loads the `.wasm` binary from the plugin store (local disk path or object storage URL).
3. A `wazero` module instance is created per plugin with its declared host function imports.
4. The host calls the plugin's exported `Init()` function, passing a `PluginContext` proto with the plugin's ID, granted permissions, and config.
5. The plugin registers its route handlers by calling `paca.http_register_route` (or the SDK equivalent) during `Init()`.
6. Route registrations are applied to the Gin router under the plugin's namespace.

### Request Handling

When an HTTP request matches a plugin-registered route:
1. Gin invokes the host's plugin dispatch handler.
2. The host serialises the request context (method, path params, body) into a proto and writes it to shared memory.
3. The host calls the plugin's exported `HandleRequest(route_id)` function.
4. The plugin reads the request, executes its logic (calling host functions as needed), and calls `paca.http_respond` with the response.
5. The host reads the response from shared memory and writes it to the `gin.Context`.

### Event Delivery

1. When a subscribed core event fires (e.g., `task.deleted`), the host serialises the event payload.
2. The host calls the plugin's exported `HandleEvent(event_id)` function in the plugin's WASM instance.
3. The plugin processes the event (e.g., deleting orphan BDD scenarios when a task is deleted).

### Shutdown / Cleanup

The host calls the plugin's `Shutdown()` export before unloading. Plugins should flush any buffered state and release resources.

## Plugin-Owned Database Migrations

Plugins declare SQL migration files in their bundle. Migrations are run by the host using a restricted migration runner that only allows DDL within the `plugin_data_{pluginId}` schema. The runner uses the same sequential migration pattern as the core (`000001_name.sql`, `000002_name.sql`, ...).

Migrations are run:
- On first plugin installation.
- On plugin upgrade, running only new migration files.
- On plugin uninstall (optional `down` migrations if provided).

## Resource Limits

Each WASM module instance is constrained by `wazero`'s resource controls:

| Resource | Default Limit |
|---|---|
| Memory | 64 MB per module instance |
| CPU (via instruction counting) | Configurable; default prevents runaway loops |
| Concurrent goroutines | N/A — WASM is single-threaded per instance |
| DB connections | Plugins use the host's connection pool; max 5 concurrent queries per plugin |
| Inbound HTTP request body | 10 MB per request to a plugin route; larger requests are rejected with `413` before reaching the plugin |

Limits are configurable via environment variables (see `services/api/.env.example`):

| Variable | Default | Controls |
|---|---|---|
| `PLUGINS_MAX_CALL_DURATION` | `5s` | Max time for a single plugin function call |
| `PLUGINS_MAX_MEMORY_PAGES` | `1024` (64 MiB) | Max WASM linear-memory pages per module instance |
| `PLUGINS_MAX_REQUEST_BODY_BYTES` | `10485760` (10 MiB) | Max inbound HTTP request body / event payload size |

## Plugin Storage Location

The host reads plugin binaries from a configured path:

- **Local (development/self-hosted):** `./plugins/dist/{pluginId}/` directory.
- **Object storage (production):** S3-compatible bucket, same credentials as attachment storage.

The server config entry:
```yaml
plugins:
  store: local          # or "s3"
  local_path: ./plugins/dist
  s3_bucket: paca-plugins
  cdn_allowlist:
    - https://plugins.paca.app
    - https://cdn.example.com
```

## Security Considerations

- WASM modules have **no filesystem access** (wazero's WASI filesystem is not mounted).
- WASM modules have **no raw network access**; all external communication must go through host functions.
- All DB host functions enforce **project-scope isolation** — a plugin enabled for project A cannot query project B data.
- Plugin WASM binaries should be **signed** by the publisher. The host verifies the signature against a public key stored in the plugin manifest before loading. (v1: signature check is enforced for third-party plugins; first-party plugins bypass in dev mode.)
- WASM execution errors are caught by wazero and converted to 500 responses; the host never panics due to a plugin crash.
- Inbound request bodies are capped (see Resource Limits) before being handed to a plugin. Plugin SDK allocators are simple bump allocators with no bounds checking of their own, so an oversized payload would otherwise advance a plugin's allocator cursor past the end of its memory; the host always resets a plugin's allocator after each call, including on failure, so a single bad request cannot leave an instance unable to serve later ones.
- Secrets (e.g., API keys the plugin needs) are stored encrypted in the host's secrets store and passed to the plugin through `paca.config_get` — never baked into the WASM binary.
