# Plugin SDK Reference

The Paca Plugin SDK consists of three packages:

- **`@paca-ai/plugin-sdk-react`** — TypeScript/React SDK for frontend plugin components.
  - Repository: [github.com/Paca-AI/plugin-sdk-react](https://github.com/Paca-AI/plugin-sdk-react)
- **`github.com/Paca-AI/plugin-sdk-go`** — Go SDK for backend WASM plugins.
  - Repository: [github.com/Paca-AI/plugin-sdk-go](https://github.com/Paca-AI/plugin-sdk-go)
- **`@paca-ai/plugin-sdk-mcp`** — TypeScript SDK for MCP tool extensions.
  - Repository: [github.com/Paca-AI/plugin-sdk-mcp](https://github.com/Paca-AI/plugin-sdk-mcp)

All packages are maintained in separate repositories for better modularity and easier dependency management.

See [paca-plugin-example](https://github.com/Paca-AI/paca-plugin-example) for a working reference implementation that exercises every API documented here.

---

## TypeScript SDK (`@paca-ai/plugin-sdk-react`)

### Installation

```sh
bun add @paca-ai/plugin-sdk-react
```

The package is also declared as a `shared` dependency in Module Federation config so the host's singleton instance is used (see [Frontend Plugin System](frontend-plugin-system.md)).

---

### `PluginSDK`

The main SDK object passed to every plugin component via its context prop.

```ts
interface PluginSDK {
  /** HTTP client scoped to /api/v1/plugins/{pluginId}/ */
  api: PluginApiClient;

  /** UI utilities */
  ui: PluginUI;

  /** Metadata about this plugin and the host */
  meta: PluginMeta;
}
```

---

### `PluginApiClient`

The API client provides typed wrappers for common Paca API calls and plugin route helpers. The host creates and injects the instance — plugins must not construct their own.

```ts
class PluginApiClient {
  constructor(opts: PluginApiClientOptions)

  // Core read-only helpers (scoped to the current project)
  listTasks(filters?: TaskFilters): Promise<TaskSummary[]>
  getTask(taskId: string): Promise<Task>
  getProject(): Promise<ProjectSummary>
  listMembers(): Promise<ProjectMember[]>

  // Plugin route helpers (path is relative to /plugins/{pluginId}/)
  // For project-scoped routes, include projects/{projectId}/ in the path.
  pluginGet<T>(pluginId: string, path: string): Promise<T>
  pluginPost<T>(pluginId: string, path: string, body: unknown): Promise<T>
  pluginPatch<T>(pluginId: string, path: string, body: unknown): Promise<T>
  pluginDelete(pluginId: string, path: string): Promise<void>
}

interface PluginApiClientOptions {
  baseUrl: string;    // e.g. "https://app.paca.dev/api/v1"
  projectId: string;  // current project ID, injected by the host
  fetch: (url: string, init?: RequestInit) => Promise<Response>;
}
```

**Example:**
```ts
// Call a backend route registered by this plugin
const result = await api.pluginGet<{ data: MyItem[] }>(meta.pluginId, `tasks/${taskId}/items`);

// Fetch core platform data
const members = await api.listMembers();
const tasks = await api.listTasks({ status_ids: ["done"] });
```

---

### `PluginUI`

Utilities for showing UI feedback without depending on host internals.

```ts
interface PluginUI {
  /** Show a toast notification */
  toast(opts: ToastOptions): void;

  /** Show a confirmation dialog; resolves true if confirmed */
  confirm(opts: ConfirmOptions): Promise<boolean>;

  /** Navigate within the host application using its router */
  navigate(path: string): void;
}

interface ToastOptions {
  title: string;
  description?: string;
  variant?: "default" | "success" | "destructive";
  duration?: number;
}

interface ConfirmOptions {
  title: string;         // required — shown as the dialog heading
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: "default" | "destructive";
}
```

---

### `PluginMeta`

```ts
interface PluginMeta {
  pluginId: string;     // e.g. "com.paca.example"
  displayName: string;  // human-readable plugin name
  version: string;      // semver version string
}
```

---

### Extension Point Component Contracts

Each extension point has a typed React component interface exported from `@paca-ai/plugin-sdk-react`. All interfaces extend `BaseExtensionProps`, which injects `api`, `ui`, and `meta` as top-level props.

```ts
interface BaseExtensionProps {
  api: PluginApiClient;
  ui: PluginUI;
  meta: PluginMeta;
}
```

Your exported component **must** match the prop signature for its extension point.

#### `task.detail.section`

```ts
import type { TaskDetailSectionProps } from "@paca-ai/plugin-sdk-react";

export default function MyTaskDetailSection(props: TaskDetailSectionProps) { ... }

interface TaskDetailSectionProps extends BaseExtensionProps {
  taskId: string;
  projectId: string;
}
```

#### `sidebar.general.section`

```ts
import type { SidebarGeneralSectionProps } from "@paca-ai/plugin-sdk-react";

export default function MyGeneralSection(props: SidebarGeneralSectionProps) { ... }

interface SidebarGeneralSectionProps extends BaseExtensionProps {
  isCollapsed: boolean;
}
```

#### `sidebar.project.section`

```ts
import type { SidebarProjectSectionProps } from "@paca-ai/plugin-sdk-react";

export default function MyProjectSection(props: SidebarProjectSectionProps) { ... }

interface SidebarProjectSectionProps extends BaseExtensionProps {
  projectId: string;
  isCollapsed: boolean;
}
```

#### `project.settings.tab`

```ts
import type { ProjectSettingsTabProps } from "@paca-ai/plugin-sdk-react";

export default function MySettingsTab(props: ProjectSettingsTabProps) { ... }

interface ProjectSettingsTabProps extends BaseExtensionProps {
  projectId: string;
}
```

#### `view`

```ts
import type { ViewExtensionProps } from "@paca-ai/plugin-sdk-react";

export default function MyView(props: ViewExtensionProps) { ... }

interface ViewExtensionProps extends BaseExtensionProps {
  projectId: string;
  viewConfig?: Record<string, unknown>;
}
```

---

### Shared Types

```ts
// Task
interface TaskSummary {
  id: string;
  title: string;
  task_number: number;      // snake_case matching the API JSON
  status_id: string | null;
  assignee_ids: string[];
}

interface Task extends TaskSummary {
  project_id: string;
}

interface TaskFilters {
  status_ids?: string[];
  assignee_ids?: string[];
  sprint_id?: string;
  parent_task_id?: string;
  page?: number;
  page_size?: number;
}

// Project
interface ProjectSummary {
  id: string;
  name: string;
  description: string;
  task_id_prefix: string;
}

// Members
interface ProjectMember {
  id: string;
  username: string;
  full_name: string;
  role_name: string;
}

interface ProjectPermissions {
  canManageProject: boolean;
  canManageMembers: boolean;
  canWriteTasks: boolean;
  canReadTasks: boolean;
}
```

---

### React Query Integration

Plugins may use TanStack Query. The SDK exports `PluginQueryClientProvider` and `usePluginQuery` to namespace cache entries under the plugin ID so they cannot collide with the host or sibling plugins.

```ts
import {
  PluginQueryClientProvider,
  usePluginQuery,
  usePluginQueryClient,
} from "@paca-ai/plugin-sdk-react";

// Wrap your root component
export default function Root(props: TaskDetailSectionProps) {
  return (
    <PluginQueryClientProvider>
      <MyComponent {...props} />
    </PluginQueryClientProvider>
  );
}

// Use inside the provider — query key is prefixed with ["plugin", pluginId, ...]
function MyComponent({ api, meta, taskId }: TaskDetailSectionProps) {
  const { data, isLoading } = usePluginQuery(
    meta.pluginId,
    ["my-items", taskId],
    () => api.pluginGet<MyItem[]>(meta.pluginId, `tasks/${taskId}/items`),
  );
}

// Manual cache invalidation
function afterMutation(pluginId: string) {
  const qc = usePluginQueryClient();
  qc.invalidateQueries({ queryKey: ["plugin", pluginId, "my-items"] });
}
```

`PluginQueryClientProvider` accepts an optional `queryClient` prop to reuse the host's `QueryClient` instance. When running inside the host's Module Federation shell the host injects its client automatically.

---

## Go SDK (`github.com/Paca-AI/plugin-sdk-go`)

### Installation

```sh
go get github.com/Paca-AI/plugin-sdk-go
```

Build with TinyGo, targeting `wasip1`:

```sh
tinygo build -target=wasip1 -buildmode=c-shared -o plugin.wasm .
```

`-buildmode=c-shared` is required — without it, TinyGo skips the WASI
reactor entry point (`_initialize`) that the runtime depends on, and every
call into the plugin panics at runtime with "//go:wasmexport function
called before runtime initialization". TinyGo produces a much smaller
binary than the standard Go compiler, because it doesn't statically link
Go's full runtime (GC, scheduler, reflection) — every loaded plugin gets
its own independent copy of that runtime on the host, so this is a real
difference in production memory use, not just download size.

Standard Go 1.24+ (`GOARCH=wasm GOOS=wasip1 go build -buildmode=c-shared`)
is also supported, and is the right fallback if your plugin hits a TinyGo
compatibility limitation (TinyGo implements a subset of the stdlib, notably
around `reflect`). If your plugin declares its own `//go:wasmimport` host
function, call it directly by name rather than passing it as a value (e.g.
into a generic helper) — TinyGo doesn't support taking the address of a
`go:wasmimport` function, only calling it.

---

### Entry Point

Every plugin has a single Go entry file. The SDK exports all required WASM symbols internally — you only need to implement the `Plugin` interface and call `plugin.Run` from `init()`.

```go
//go:build wasip1

package main

import plugin "github.com/Paca-AI/plugin-sdk-go"

type examplePlugin struct {
    db  *plugin.DB
    kv  *plugin.KV
    log *plugin.Logger
    cfg *plugin.Config
}

func (p *examplePlugin) Init(ctx *plugin.Context) error {
    p.db  = ctx.DB()
    p.kv  = ctx.KV()
    p.log = ctx.Log()
    p.cfg = ctx.Config()

    ctx.Route("GET",    "/tasks/:taskId/messages",     p.listMessages)
    ctx.Route("POST",   "/tasks/:taskId/messages",     p.createMessage)
    ctx.Route("DELETE", "/tasks/:taskId/messages/:id", p.deleteMessage)
    ctx.On("task.deleted", p.onTaskDeleted)
    return nil
}

func (p *examplePlugin) Shutdown() {}

func (p *examplePlugin) listMessages(req *plugin.Request, resp *plugin.Response) {
    taskID := req.PathParam("taskId")
    result, err := p.db.Query(
        `SELECT id, name, message FROM hello_messages WHERE task_id = $1`,
        taskID,
    )
    if err != nil {
        p.log.Error("listMessages query failed")
        resp.Error(500, "query failed")
        return
    }
    resp.JSON(200, result)
}

func (p *examplePlugin) createMessage(req *plugin.Request, resp *plugin.Response) {
    body, err := plugin.JSONBody[struct {
        Name    string `json:"name"`
        Message string `json:"message"`
    }](req)
    if err != nil || body.Name == "" {
        resp.Error(400, "name is required")
        return
    }

    prefix, _ := p.cfg.Get("greeting.prefix")
    if prefix == "" {
        prefix = "Hello"
    }
    greeting := fmt.Sprintf("%s, %s! %s", prefix, body.Name, body.Message)

    result, err := p.db.Query(
        `INSERT INTO hello_messages (task_id, name, message)
         VALUES ($1, $2, $3) RETURNING id, name, message`,
        req.PathParam("taskId"), body.Name, greeting,
    )
    if err != nil {
        resp.Error(500, "insert failed")
        return
    }
    resp.JSON(201, result)
}

func (p *examplePlugin) deleteMessage(req *plugin.Request, resp *plugin.Response) {
    _, err := p.db.Exec(
        `DELETE FROM hello_messages WHERE id = $1`,
        req.PathParam("id"),
    )
    if err != nil {
        resp.Error(500, "delete failed")
        return
    }
    resp.NoContent()
}

func (p *examplePlugin) onTaskDeleted(evt *plugin.Event) {
    payload, err := plugin.JSONPayload[struct {
        TaskID string `json:"task_id"`
    }](evt)
    if err != nil {
        p.log.Error("bad task.deleted payload")
        return
    }
    p.db.Exec(`DELETE FROM hello_messages WHERE task_id = $1`, payload.TaskID)
}

func init() { plugin.Run(&examplePlugin{}) }
func main() {}
```

---

### Unit Testing with `plugintest`

The `plugintest` package (part of the SDK) provides in-memory backends so you can test route and event handlers without a live database or WASM runtime.

```go
package main_test

import (
    "encoding/json"
    "testing"

    plugin "github.com/Paca-AI/plugin-sdk-go"
    "github.com/Paca-AI/plugin-sdk-go/plugintest"
)

func TestListMessages(t *testing.T) {
    tc := plugintest.NewContext(t)

    // Seed initial data
    tc.DB.SeedRows("hello_messages",
        []string{"id", "task_id", "name", "message"},
        [][]any{
            {"id-1", "task-a", "Alice", "Hello, Alice!"},
        },
    )

    // Set config values the plugin reads during Init
    tc.Config.Set("greeting.prefix", "Hi")

    // Init the plugin
    var p examplePlugin
    if err := p.Init(tc.PluginContext()); err != nil {
        t.Fatal(err)
    }

    // Call a route
    res := tc.Call("GET", "/tasks/:taskId/messages", plugintest.Request{
        PathParams: map[string]string{"taskId": "task-a"},
        Caller:     plugin.CallerIdentity{ProjectID: "proj-1"},
    })
    if res.StatusCode != 200 {
        t.Fatalf("expected 200, got %d: %s", res.StatusCode, res.BodyString())
    }

    // Dispatch a platform event
    payload, _ := json.Marshal(map[string]string{"task_id": "task-a"})
    plugin.DispatchEvent(tc.PluginContext(), "task.deleted", payload)

    if rows := tc.DB.AllRows("hello_messages"); len(rows) != 0 {
        t.Fatalf("expected rows deleted, got %d", len(rows))
    }
}
```

Key `plugintest` API:

| Symbol | Description |
|---|---|
| `plugintest.NewContext(t)` | Create a fresh test harness; cleanup registered automatically. |
| `tc.DB.SeedRows(table, cols, rows)` | Pre-populate an in-memory table. |
| `tc.DB.AllRows(table)` | Read all rows after mutations. |
| `tc.KV.Set(key, value)` | Pre-seed KV entries. |
| `tc.Config.Set(key, value)` | Pre-seed config values. |
| `tc.PluginContext()` | Return `*plugin.Context` to pass to `Plugin.Init`. |
| `tc.Call(method, path, req)` | Dispatch a test request; returns `*plugin.Response`. |
| `plugin.DispatchEvent(ctx, topic, payload)` | Fire a platform event directly to the plugin's handler. |

---

## MCP SDK (`@paca-ai/plugin-sdk-mcp`)

Use this SDK to add MCP tools to your plugin. The Paca MCP server loads your plugin's entry module at startup and merges the exported tools into its tool list.

See [mcp-plugin-system.md](mcp-plugin-system.md) for the full architecture.

### Installation

```sh
npm install @paca-ai/plugin-sdk-mcp
# or
bun add @paca-ai/plugin-sdk-mcp
```

### `PluginMCPEntry`

The default export of your plugin's MCP entry module must satisfy this interface.

```ts
interface PluginMCPEntry {
  /** Full MCP tool definitions contributed by this plugin. */
  tools: Tool[];

  /**
   * Handle a tool call routed to this plugin.
   * Called only when `name` matches one of the declared tools.
   */
  handleToolCall(
    name: string,
    args: Record<string, unknown>,
    context: PluginMCPContext,
  ): Promise<PluginToolResult>;
}
```

### `PluginMCPContext`

Runtime context injected by the host into every `handleToolCall` invocation.

```ts
interface PluginMCPContext {
  pluginId: string;  // e.g. "com.paca.checklist"
  baseURL:  string;  // e.g. "http://localhost:8080"
  apiKey:   string;  // Paca API key for authentication
}
```

### `PluginAPIClient`

Scoped HTTP client. Construct one instance per tool call using the injected context.

```ts
class PluginAPIClient {
  constructor(context: PluginMCPContext)

  // Plugin route helpers (prefix: /api/v1/plugins/{pluginId}/)
  pluginGet<T>(path: string): Promise<T>
  pluginPost<T>(path: string, body: unknown): Promise<T>
  pluginPatch<T>(path: string, body: unknown): Promise<T>
  pluginDelete(path: string): Promise<void>

  // Core Paca API (prefix: /api/v1/)
  coreGet<T>(path: string): Promise<T>
}
```

### `PluginToolResult`

The value returned by `handleToolCall`.

```ts
interface PluginToolResult {
  content: ToolResultContent[];
  isError?: boolean;  // set to true to signal an error to the AI client
}

type ToolResultContent =
  | { type: "text"; text: string }
  | { type: "image"; data: string; mimeType: string }
  | { type: "resource"; resource: { uri: string; text?: string; mimeType?: string } };
```

### Helper functions

```ts
/** Build a successful text tool result. */
function textResult(text: string): PluginToolResult

/** Build an error tool result (isError: true). */
function errorResult(message: string): PluginToolResult
```

### Minimal example

```ts
// src/mcp.ts
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
          project_id: { type: "string" },
          task_id:    { type: "string" },
        },
        required: ["project_id", "task_id"],
      },
    },
  ],

  async handleToolCall(name, args, context) {
    const api = new PluginAPIClient(context);
    const { project_id, task_id } = args as { project_id: string; task_id: string };

    try {
      const items = await api.pluginGet(
        `projects/${project_id}/tasks/${task_id}/items`,
      );
      return textResult(JSON.stringify(items, null, 2));
    } catch (err) {
      return errorResult(err instanceof Error ? err.message : String(err));
    }
  },
};

export default entry;
```
