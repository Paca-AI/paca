# MCP Plugin System

## Overview

The Paca MCP server supports **plugin-contributed tools**. Each installed Paca plugin can ship a small Node.js-compatible ESM module that declares MCP tool definitions and handles tool calls. When the MCP server starts, it fetches the list of enabled plugins from the API and dynamically loads any plugin that declares an `mcp.remoteEntryUrl` in its manifest.

From the AI client's perspective, plugin tools appear alongside core Paca tools in a single flat list — there is no visible distinction.

## Architecture

```
MCP Client (Claude, Copilot, Cursor, …)
    │
    │ stdio
    ▼
Paca MCP Server (apps/mcp)
    │
    ├── Startup: GET /api/v1/plugins
    │       │
    │       └── For each plugin with mcp.remoteEntryUrl:
    │               import(remoteEntryUrl)   ← dynamic ESM import
    │               validate PluginMCPEntry
    │               collect tools + register handler
    │
    ├── ListTools → [core tools] + [plugin tools]
    │
    └── CallTool → route to plugin registry OR core handlers
                        │
                        └── plugin handler calls Paca API
                              /api/v1/plugins/{pluginId}/…
```

### Key Components

| Component | Location | Purpose |
|---|---|---|
| `plugin-loader.ts` | `apps/mcp/src/` | Fetches plugin list, imports modules, builds `PluginRegistry` |
| `PluginRegistry` | `apps/mcp/src/plugin-loader.ts` | Holds loaded plugins, merges tools, routes calls |
| `server.ts` | `apps/mcp/src/` | Async server factory; integrates plugin registry |
| `@paca-ai/plugin-sdk-mcp` | `plugin-sdk-mcp/` | SDK for plugin developers |

## Plugin Manifest

Add an `mcp` section to your `plugin.json`:

```json
{
  "id": "com.example.my-plugin",
  "displayName": "My Plugin",
  "version": "1.0.0",
  "mcp": {
    "remoteEntryUrl": "https://cdn.example.com/my-plugin/1.0.0/mcp.js"
  }
}
```

`remoteEntryUrl` points to the plugin's MCP entry module — a Node.js-compatible ESM bundle built from your plugin source. The MCP server dynamically imports it via `import(url)`.

> **Local development:** `http://` URLs are supported. The server fetches the source over HTTP and re-evaluates it internally. Use `https://` or `file://` in production.

## Plugin MCP Entry Module

The module must export a `PluginMCPEntry` object as its **default export**:

```ts
import type { PluginMCPEntry } from "@paca-ai/plugin-sdk-mcp";
import { PluginAPIClient, textResult, errorResult } from "@paca-ai/plugin-sdk-mcp";

const entry: PluginMCPEntry = {
  tools: [
    {
      name: "checklist_list_items",
      description: "List checklist items attached to a task.",
      inputSchema: {
        type: "object",
        properties: {
          project_id: { type: "string", description: "Project ID" },
          task_id:    { type: "string", description: "Task ID" },
        },
        required: ["project_id", "task_id"],
      },
    },
  ],

  async handleToolCall(name, args, context) {
    const api = new PluginAPIClient(context);
    const { project_id, task_id } = args as { project_id: string; task_id: string };

    try {
      if (name === "checklist_list_items") {
        const items = await api.pluginGet(`projects/${project_id}/tasks/${task_id}/items`);
        return textResult(JSON.stringify(items, null, 2));
      }
      return errorResult(`Unknown tool: ${name}`);
    } catch (err) {
      return errorResult(err instanceof Error ? err.message : String(err));
    }
  },
};

export default entry;
```

## Contributing to any core tool's response (`getToolContext`)

A plugin can optionally implement `getToolContext` on its `PluginMCPEntry` to
attach additional text to the response of **any** core Paca tool call — not
just `get_task`. This lets an AI client see a plugin's data (linked
branches, checklist items, BDD scenarios, …) inline in whatever core tool it
already called, without separately discovering and calling the plugin's own
tools.

```ts
const entry: PluginMCPEntry = {
  tools: [ /* ... */ ],
  async handleToolCall(name, args, context) { /* ... */ },

  async getToolContext(toolId, args, context) {
    if (toolId !== "get_task") return null; // only enrich get_task
    const { projectId, taskId } = args as { projectId: string; taskId: string };

    const api = new PluginAPIClient(context);
    const items = await api.pluginGet<Item[]>(
      `projects/${projectId}/tasks/${taskId}/items`,
    );
    if (items.length === 0) return null; // nothing to add — omit the section
    return `## My Plugin\n\n${items.map((i) => `- ${i.title}`).join("\n")}`;
  },
};
```

Notes:

- `toolId` is the core tool's name (`"get_task"`, `"list_tasks"`,
  `"get_project"`, …) — switch on it to decide what, if anything, to add.
  See `ALL_TOOLS.md` in `apps/mcp` for the full list of core tools and their
  argument shapes.
- `args` is exactly what the AI client passed for that call — the same
  shape the core tool itself receives, nothing more. It may not contain
  every ID your plugin needs: e.g. `get_task_by_number` has no `taskId`,
  only `taskNumber`, so a hook scoped to `"get_task"` won't fire for it.
- Return `null` (or `undefined`) when the plugin has nothing to contribute
  for this call. The host omits the section entirely rather than rendering
  empty boilerplate on every call — most calls won't touch every plugin.
- The host calls `getToolContext` for every loaded plugin that implements
  it, in parallel, after every successful core tool call (skipped when the
  core call itself returned an error). Keep it fast and read-only.
- Errors are caught and logged by the host (`[plugin-loader] Plugin "<id>"
  getToolContext("<toolId>") failed: ...`) — a throwing plugin contributes
  nothing but cannot break the rest of the response. You don't need your
  own try/catch purely for that; add one if you want a specific failure
  (e.g. "not configured for this project") to resolve to `null` instead of
  logging.
- All plugins' returned text is joined and appended as one additional
  content block on the tool result — prefix your text with a heading (e.g.
  `## GitHub`) so it reads clearly alongside other plugins' sections.

## Plugin SDK (`@paca-ai/plugin-sdk-mcp`)

The `@paca-ai/plugin-sdk-mcp` package provides:

- **`PluginMCPEntry`** — interface your default export must implement.
- **`PluginMCPContext`** — runtime context injected by the host (`pluginId`, `baseURL`, `apiKey`).
- **`PluginAPIClient`** — scoped HTTP client for calling your plugin's backend routes.
- **`textResult(text)`** / **`errorResult(message)`** — helpers for building tool results.
- **`Tool`** — re-exported MCP tool definition type.

See the [SDK README](../../plugin-sdk-mcp/README.md) and [sdk-reference.md](sdk-reference.md) for full API documentation.

## Tool Naming

Tool names must be unique across all enabled plugins. Use a short prefix derived from your plugin ID:

| Plugin ID | Prefix | Example tool name |
|---|---|---|
| `com.paca.checklist` | `checklist_` | `checklist_list_items` |
| `com.paca.bdd` | `bdd_` | `bdd_list_scenarios` |
| `com.example.timetracking` | `timetracking_` | `timetracking_log_hours` |

Tool names must match `[a-z][a-z0-9_]*`.

## Loading Behaviour

1. The MCP server fetches `GET /api/v1/plugins` using the configured `PACA_API_KEY`.
2. Plugins where `enabled: false` are skipped.
3. Plugins without `manifest.mcp.remoteEntryUrl` are skipped (they may still have frontend or backend extensions).
4. For each qualifying plugin, the server calls `import(remoteEntryUrl)` and validates the default export.
5. If a plugin fails to load (network error, invalid module, etc.), a warning is logged to stderr and the server continues with the remaining plugins.
6. Core tools are always available regardless of plugin load failures.

## Security Considerations

- Plugin MCP modules run in the **same Node.js process** as the MCP server with no sandboxing (v1). Only install plugins from trusted sources.
- The `PluginAPIClient` authenticates using the MCP server's API key. Plugin access is scoped by Paca's existing authorization model (routes under `/api/v1/plugins/{pluginId}/`).
- The server fetches `remoteEntryUrl` at startup — not at every tool call — so the module is cached for the server's lifetime.
- `http://` URLs are permitted for local development only. In production, all `remoteEntryUrl` values should use `https://`.

## Error Handling

| Scenario | Behaviour |
|---|---|
| API unreachable at startup | Warning logged; server starts with no plugins |
| Plugin module fetch fails | Warning logged; that plugin's tools are unavailable |
| Plugin module has invalid shape | Warning logged; that plugin's tools are unavailable |
| Plugin `handleToolCall` throws | Error returned to AI client as an `isError: true` result |

## Comparison with the Frontend Plugin System

| Aspect | Frontend (Module Federation) | MCP (Dynamic Import) |
|---|---|---|
| Load time | Lazy — on first navigation | Eager — at server startup |
| Runtime | Browser (ES modules) | Node.js 18+ |
| SDK | `@paca-ai/plugin-sdk-react` | `@paca-ai/plugin-sdk-mcp` |
| Entry field | `frontend.remoteEntryUrl` | `mcp.remoteEntryUrl` |
| Sandboxing | Browser origin isolation | Same process (v1) |
| Lifecycle | Loaded per browser session | Loaded once per server process |
