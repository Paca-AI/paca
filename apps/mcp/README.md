# Paca MCP Server

Model Context Protocol (MCP) server for Paca — an open-source, AI-native project management platform.

Connect your AI assistant (Claude, Cursor, VS Code Copilot, etc.) to your Paca workspace and manage projects, tasks, sprints, and documents using natural language.

## Getting Started

### Prerequisites

- Node.js 18+
- A running Paca instance (local or deployed)
- A Paca API key (generate one in your Paca user settings)

## Setup

No installation or build step required. Configure your AI agent client to use the MCP server directly via `npx`.

### Environment Variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `PACA_API_KEY` | ✅ | — | API key for authentication (see below) |
| `PACA_API_URL` | ❌ | `http://localhost:8080` | URL of your Paca API instance |
| `PACA_AGENT_ID` | ❌ | — | Agent UUID — set to connect as a specific ACP agent instead of yourself (see Agent Mode below) |
| `PACA_PROJECT_ID` | ❌ | — | Project UUID to pin every tool call to a single project. Optional even when `PACA_AGENT_ID` is set — a global agent left unset here runs "unpinned" across every project it's invited into |

### Claude Desktop

Add the following to your Claude Desktop config file:

- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "your-api-key-here",
        "PACA_API_URL": "http://localhost:8080"
      }
    }
  }
}
```

Restart Claude Desktop after saving. Claude will automatically have access to all Paca tools.

### VS Code (GitHub Copilot)

Add to your VS Code `settings.json` or `.vscode/mcp.json`:

```json
{
  "mcp": {
    "servers": {
      "paca": {
        "command": "npx",
        "args": ["-y", "@paca-ai/paca-mcp"],
        "env": {
          "PACA_API_KEY": "your-api-key-here",
          "PACA_API_URL": "http://localhost:8080"
        }
      }
    }
  }
}
```

### Other MCP-Compatible Clients

Any MCP-compatible client can use the server with:

```json
{
  "name": "paca",
  "command": "npx",
  "args": ["-y", "@paca-ai/paca-mcp"],
  "env": {
    "PACA_API_KEY": "your-api-key-here",
    "PACA_API_URL": "http://localhost:8080"
  }
}
```

For a full setup walkthrough, see the [MCP Server Setup Guide](../../docs/guides/mcp-server-setup.md).

## Features

- **API Key Authentication**: Secure access using Paca API keys
- **Comprehensive Project Management**: Full project lifecycle with member and role management
- **Advanced Task Management**: Tasks with types, statuses, custom fields, and attachments
- **Sprint Management**: Complete sprint lifecycle management
- **Document Management**: Documents with folder hierarchy, version history, and file support
- **View Management**: Multiple view types (sprint, backlog, timeline) with task positioning
- **GitHub Integration**: Repository linking, PR management, and branch creation
- **Collaboration Tools**: Comments and activities for team collaboration
- **BlockNote Integration**: Automatic conversion between BlockNote JSON and Markdown
- **Plugin MCP Tools**: Plugins can contribute additional MCP tools, loaded automatically at startup

## Agent Mode vs. User Mode

The MCP server always lists every tool it knows about — it doesn't fetch or check permissions itself. Each tool *call* is authorized by the Paca API, which returns `403` for anything the caller isn't allowed to do; the MCP server turns that into a "Permission denied: ..." error for the calling agent rather than hiding the tool up front. So whether a call succeeds depends on the caller's actual role, not on which mode below you're running in — these modes only control *identity* (who a call is made as) and *scope* (which project a call may target).

| Mode | Trigger | API Key Source | Scope |
|---|---|---|---|
| **Agent Single-Project** | `PACA_AGENT_ID` + `PACA_PROJECT_ID` | That agent's own `PACA_API_KEY` | Every tool call is pinned to the specified project |
| **Agent Global** | `PACA_AGENT_ID` only (no `PACA_PROJECT_ID`) | That agent's own `PACA_API_KEY` | Each tool call may target any project the agent is invited into |
| **User Single-Project** | `PACA_PROJECT_ID` only (no `PACA_AGENT_ID`) | User's personal API key | Every tool call is pinned to the specified project |
| **User Global** | Neither set | User's personal API key | Each tool call may target any project you're a member of |

**Note**: Each ACP agent has its own `PACA_API_KEY`, generated per-agent — not a key shared across every agent in the deployment. Generate (or regenerate) it from the agent's setup page in the Paca UI; regenerating immediately invalidates whatever key was live before, since only one key is ever active per agent.

### Agent Mode

Set `PACA_AGENT_ID` to connect as a specific ACP agent instead of as yourself. Add `PACA_PROJECT_ID` too if you want every tool call pinned to one project (recommended for a project-scoped agent); leave it unset for a global agent, which can then work across any project it's been invited into.

1. **Authentication**: Uses that agent's own `PACA_API_KEY` — the key itself identifies the agent, so no separate impersonation header is needed or honored
2. **Project Validation**: With `PACA_PROJECT_ID` set, enforces that all tool calls use that project ID

**How to Get the Agent's API Key:**

Generate it from the agent's setup page in the Paca UI (Project → Agents → the agent → "Generate key" under the local bridge setup steps). The plaintext is shown once — copy it immediately, since only its SHA-256 hash is stored server-side. It's also available via the API directly: `POST /projects/:projectId/agents/:agentId/mcp-agent-key` for a project agent, or `POST /admin/agents/:agentId/mcp-agent-key` for a global agent.

**Configuration (single-project):**
```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "agent-mcp-key-from-above",
        "PACA_API_URL": "http://localhost:8080",
        "PACA_AGENT_ID": "your-agent-uuid-here",
        "PACA_PROJECT_ID": "your-project-uuid-here"
      }
    }
  }
}
```

**Configuration (global agent, unpinned across every invited project):**
```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "agent-mcp-key-from-above",
        "PACA_API_URL": "http://localhost:8080",
        "PACA_AGENT_ID": "your-agent-uuid-here"
      }
    }
  }
}
```

**User configuration** (optional single-project mode):
```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "your-api-key-here",
        "PACA_API_URL": "http://localhost:8080",
        "PACA_PROJECT_ID": "your-project-uuid-here"
      }
    }
  }
}
```

### User Mode (No Agent ID)

When `PACA_AGENT_ID` is not set:

1. **Authentication**: Uses user's personal API key (from Settings → API Keys)
2. **No Impersonation**: Acts as the authenticated user directly

**Configuration:**
```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "your-personal-api-key-here",
        "PACA_API_URL": "http://localhost:8080"
      }
    }
  }
}
```

**To add project scope:**
```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "your-personal-api-key-here",
        "PACA_API_URL": "http://localhost:8080",
        "PACA_PROJECT_ID": "your-project-uuid-here"
      }
    }
  }
}
```

**Note**: Without `PACA_PROJECT_ID`, project-scoped tool calls (like `list_tasks`, `create_task`) must pass a `projectId` argument explicitly — the tool is listed either way, but the call still needs a project to act on, and still needs a role attached to the caller in that project that allows the underlying action (e.g. `tasks:read`).

### Configuring Permissions

Permissions live entirely on the Paca API side, so configuring them is the same regardless of the MCP server:

**For Agents:**
1. **Add the agent to the project** with one or more roles (`add_project_member` / the Team page). A project agent holds only the roles attached inside its project; a global agent holds platform roles attached by an administrator.
2. **Check the roles' policies**: the attached roles must allow the actions the tools need (for example `tasks:write` for `create_task`).

**Important**: When using agent mode, `PACA_API_KEY` must be that agent's own key (see "How to Get the Agent's API Key" above) — the key itself is what identifies the agent to the server, so `PACA_AGENT_ID` alone is not a claim the server will trust from any other key.

**For Users:**
1. **Attach platform roles**: an administrator attaches one or more roles to the user (Administration, Users). A platform role applies across the workspace.
2. **Add to projects**: add the user to projects with one or more roles (`roleIds`).
3. **Check the policies**: roles are IAM policy documents (default deny, an explicit `Deny` wins). See [IAM authorization](../../docs/guides/iam-authorization.md) and [Roles and policies](../../docs/guides/roles-and-policies.md).

If a tool call fails with `403` / "Permission denied: ...", none of the caller's roles allows the action the underlying endpoint requires. Use `get_my_project_permissions` to list the actions you hold in a project, adjust the role, and retry; no MCP server restart is needed.

### Example Configuration

```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "your-api-key-here",
        "PACA_API_URL": "http://localhost:8080",
        "PACA_AGENT_ID": "your-agent-uuid-here"
      }
    }
  }
}
```

## Available Tools

The MCP server provides **81 tools** across **16 categories** for comprehensive project management.


> **Paged lists.** List tools (`list_projects`, `list_sprints`, `list_project_members`, `list_project_roles`, `list_task_links`, `list_task_attachments`, `list_task_activities`, `list_doc_activities`, `list_docs`, `get_automation` without `automationId`, `list_tasks`, `search_tasks`) return one page per call and never read a list to the end on their own. When more results exist, the reply ends with a line giving a `cursor`; call the same tool again with that `cursor` (and optionally `pageSize`, 1-100) for the next page. `get_task` loads the first 20 items of each of its lists (subtasks, attachments, activities, links) and says how to read the rest.

### 📁 Project Management (5 tools)
- `list_projects` - List accessible projects (paged)
- `get_project` - Get details of a specific project
- `create_project` - Create a new project
- `update_project` - Update an existing project
- `delete_project` - Delete a project

### ✅ Task Management (6 tools)
- `list_tasks` - List all tasks in a project
- `get_task` - Get details of a specific task
- `get_task_by_number` - Get a task by its number
- `create_task` - Create a new task
- `update_task` - Update an existing task
- `delete_task` - Delete a task

### 🏃 Sprint Management (6 tools)
- `list_sprints` - List the sprints in a project (paged)
- `get_sprint` - Get details of a specific sprint
- `create_sprint` - Create a new sprint
- `update_sprint` - Update an existing sprint
- `delete_sprint` - Delete a sprint
- `complete_sprint` - Mark a sprint as completed

### 📄 Document Management (5 tools)
- `list_documents` - List all documents in a project
- `get_document` - Get details of a specific document
- `create_document` - Create a new document
- `update_document` - Update an existing document
- `delete_document` - Delete a document

### 👥 Project Members (5 tools)
- `list_project_members` - List the members of a project (paged)
- `add_project_member` - Add a member to a project
- `get_my_project_permissions` - List the IAM actions (e.g. `tasks:write`) the caller may perform in a project
- `update_project_member_role` - Replace the set of roles (`roleIds`) a member holds in the project
- `remove_project_member` - Remove a member from a project

### 🎭 Project Roles (4 tools)
- `list_project_roles` - List the roles available in a project (its own roles plus attachable workspace roles)
- `create_project_role` - Create a project role from an IAM `policy` document (`name`, `description`, `policy`)
- `update_project_role` - Update a project role's name, description or policy
- `delete_project_role` - Delete a project role (it is detached from its holders)

### 🏷️ Task Types (5 tools)
- `list_task_types` - List all task types in a project
- `create_task_type` - Create a new task type
- `update_task_type` - Update an existing task type
- `delete_task_type` - Delete a task type
- `set_default_task_type` - Set a task type as default

### 📊 Task Statuses (4 tools)
- `list_task_statuses` - List all task statuses in a project
- `create_task_status` - Create a new task status
- `update_task_status` - Update an existing task status
- `delete_task_status` - Delete a task status

### 🎯 Views (9 tools)
- `list_views` - List all views in a project
- `create_view` - Create a new view (sprint/backlog/timeline)
- `reorder_views` - Reorder views in a project
- `get_view` - Get details of a specific view
- `update_view` - Update an existing view
- `delete_view` - Delete a view
- `list_task_positions` - List task positions in a view
- `bulk_move_tasks` - Set several tasks' positions within a view
- `move_task` - Set a task's position within a view

### 🔧 Custom Fields (5 tools)
- `list_custom_fields` - List all custom field definitions
- `create_custom_field` - Create a new custom field definition
- `get_custom_field` - Get details of a custom field
- `update_custom_field` - Update a custom field definition
- `delete_custom_field` - Delete a custom field definition

### 📎 Attachments (3 tools)
- `list_task_attachments` - List all attachments for a task
- `get_attachment_download_url` - Get a download URL for an attachment
- `delete_task_attachment` - Delete an attachment

### 📁 Document Folders (4 tools)
- `list_doc_folders` - List all folders in a project
- `create_doc_folder` - Create a new document folder
- `update_doc_folder` - Update a document folder
- `delete_doc_folder` - Delete a document folder

### 📸 Document Snapshots (2 tools)
- `list_doc_snapshots` - List all snapshots of a document
- `get_doc_snapshot` - Get a specific document snapshot

### 🔗 Git (27 tools)
- `list_repositories` - List the repositories linked to a project
- `clone_repository` - Clone a linked repository into the workspace
- `push_branch` - Push a branch of a linked repository
- `workspace_inspect` - Survey the working directory and the checkouts in it
- `workspace_prepare` - Prepare (reuse or clone, fetch, branch) a checkout
- `git_commit` - Commit changes in a checkout
- `git_push` - Push a branch of a linked repository
- `change_request_get_context` - Pull/merge request with files, review threads and CI in one call
- `change_request_get_diff` - Unified diff, whole or per file
- `review_list_threads` - Review threads with comments and resolved state
- `issue_list` / `issue_get` / `comment_list` - List issues, read an issue, and read comments on an issue or pull request
- `checks_get` / `checks_get_logs` - CI results and the tail of a failing log
- `repo_get_file` / `repo_search_code` - Read a file or search code without a clone
- `change_request_open` / `change_request_update` - Open or edit a pull/merge request
- `comment_post` / `review_reply_thread` / `review_resolve_thread` - Comment and work threads
- `review_submit` - Review with a verdict and inline comments
- `labels_add` / `reviewers_request` / `assignees_set` - Metadata
- `checks_rerun` - Re-run failed CI (capped)
- `change_request_merge` - Merge (needs `git:merge` and the expected head SHA)


### 💬 Task Activities (4 tools)
- `list_task_activities` - List activities for a task (newest first, one page per call)
- `add_task_comment` - Add a comment to a task
- `update_task_comment` - Update a task comment
- `delete_task_comment` - Delete a task comment

### 🔀 Task GitHub (5 tools)
- `list_task_prs` - List pull requests linked to a task
- `link_pr_to_task` - Link a pull request to a task
- `unlink_pr_from_task` - Unlink a pull request
- `create_branch_for_task` - Create a branch for a task
- `list_task_branches` - List branches for a task

For a complete list of all tools with detailed descriptions, see [ALL_TOOLS.md](./ALL_TOOLS.md).

### 🔌 Plugin Tools

Installed Paca plugins can contribute additional MCP tools. When the server starts it fetches `GET /api/v1/plugins`, and for each enabled plugin that declares an `mcp.remoteEntryUrl` in its manifest, dynamically loads the plugin's tool module and merges its tools into the list above.

Plugin tools appear alongside core tools — there is no distinction from the AI client's perspective.

To add MCP tools to your own plugin, see the [MCP Plugin System](../../docs/plugins/mcp-plugin-system.md) docs and the [`@paca-ai/plugin-sdk-mcp`](../../plugin-sdk-mcp/README.md) SDK.

## Command line: `pull` and `push`

The package also runs two commands for working with documents as files. They
read the same environment as the server: `PACA_API_KEY`, `PACA_API_URL` and
`PACA_PROJECT_ID` (or `--project <id>`). `PACA_CLIENT` sets the client label
sent with writes (default `paca-cli`). They need a Paca server with the
document change feed and by-path routes; an older server answers "this Paca
server is too old for pull/push".

```bash
npx @paca-ai/paca-mcp pull [--project <id>] [--dir .paca/docs] [--force]
npx @paca-ai/paca-mcp push <file> [--path "Folder/Title"] [--project <id>] [--note "…"] [--task PACA-88] [--lock] [--force] [--dry-run]
```

### `pull`: mirror documents to Markdown files

`pull` writes every document you can read to `<dir>/<folder path>/<title>.md`.
The first run downloads everything; later runs ask the server what changed
since the last one, so an up-to-date pull is one request. Edited documents are
fetched again, moved or renamed ones (including folder renames) are moved
without downloading them, and deleted ones are removed. When you lose access
to a document, its file is removed on the next pull.

- File names come from titles and folder names. Characters that are not safe
  in file names become `_`, and two documents that would get the same name
  (compared without case and Unicode normalization) are told apart by
  `~<first 8 characters of the id>`. Names Windows reserves (`CON`, `NUL`, …)
  get a `_` prefix. Nothing is written outside `--dir`, and files in `--dir`
  that `pull` did not write are never overwritten; a folder whose path is such
  a file gets `~<first 8 characters of the folder id>`. Sibling folders whose
  names are the same when compared that way get one directory each: a folder
  keeps the directory it had at the last pull, and a new clash gives the
  suffix to the newcomer (lowest id first when neither had one). A folder
  keeps its suffix after the clash ends, so its files do not move back.
- `pull` refuses to run (exit 1) when the mirror folder contains a symlink, so
  it never writes, moves or deletes through one.
- If a pull fails part way, files it moved are put back where they were
  (an unedited one may already hold the newer server copy) and files it
  created are removed; a file left mid-move by an interrupted pull is put back
  by the next one. Before any file is moved into place or written, the state
  records every document's new file name (and reserves the names of new
  files), so a pull stopped part way (Ctrl-C, out of memory) leaves a state
  that matches the files, and the next pull finishes the job without
  duplicates, and it records what it is about to write so a file it wrote is
  not reported as edited locally later. A file written at a reserved name
  (a new document's) is overwritten by the next pull, even if you edited it
  in between.
- One pull at a time per mirror: a pull holds `<dir>/.paca-pull.lock`, and a
  second one exits 1. A lock left by a process that is no longer running is
  taken over.
- A mirror file you edited is never overwritten. If its document did not
  change in Paca, `pull` reports it (`modified locally <file>`) and leaves it
  for `push`. If the document changed too, `pull` merges the two (see
  "Editing documents as files"). `--force` replaces an edited file with the
  Paca version only where `pull` would otherwise keep, merge or mark it (its
  document changed in Paca, or the file still has the conflict markers of an
  earlier merge); a file whose document did not change is left alone. When a document is deleted (or you lose access) while its file
  was edited, the file is kept and every pull reports it and exits 2 until you
  delete the file or run `pull --force`, which removes it. If the document
  comes back (restored in Paca), the kept file is its file again: it moves
  with the document like any edited file and is reported as edited locally,
  never overwritten without `--force`.
- State: `<dir>/.paca-sync.json` (cursor, folders and their directories, and
  per document its file, content hash and file hash) and
  `<dir>/.paca-base/<id>.md` (what the server had at the last pull). During a
  pull the state file is written up to twice: once with the new file names
  (keeping the old cursor and content hashes), and once at the end with the
  new cursor and content hashes, after every file and base copy was written.
  The state never claims content pull did not write; base copies are
  written as each document is.
- Removing a mirrored file does not revoke copies already made of it: anything
  that read the mirror before keeps what it read.

### Editing documents as files

A file `pull` wrote can be edited and pushed back: `push <file>` on a file in a
mirror folder edits the document it was pulled from instead of writing a
repository file.

```bash
npx @paca-ai/paca-mcp pull
$EDITOR .paca/docs/Plans/Roadmap.md
npx @paca-ai/paca-mcp push .paca/docs/Plans/Roadmap.md --note "move the launch to May"
```

- Only the blocks your edit reaches are rewritten. Every other block is sent
  back exactly as Paca stores it, with formatting Markdown cannot show
  (colours, alignment, mentions, underline, table cell colours and widths)
  intact. A block with such formatting can be edited in place; an edit that
  would move or rebuild it is refused with the line it is on, and nothing is
  sent. Make that change in smaller steps, or in the web editor.
- The push sends `If-Match` with the version you pulled. If the document
  changed in Paca since, the push stops (exit 2) with who changed it and when:
  run `pull`, which merges, then push again. Most such conflicts are seen
  before the write, from the document Paca returns, which carries no note;
  only when the change lands between that read and the write does Paca's
  refusal also give the writer's note, client and task.
- Refused before anything is sent (exit 1): a file `pull` did not write (`not
  a pulled document`), including another file whose name only differs by case
  or Unicode form; a managed copy (`this document is a copy of <path> in
  <repository>; edit that file and push it`); a document that was deleted or
  that `pull` has not finished recording; an empty file (a document is deleted
  in Paca, not by emptying its file); a file with conflict markers
  that its pulled version does not have: `<<<<<<<`, `|||||||` or `>>>>>>>`
  lines outside fenced code, and the lines `pull`'s merge writes
  (`<<<<<<< local`, `||||||| base`, `>>>>>>> Paca …`) anywhere, fenced code
  included); a file an interrupted pull left in the middle of a merge (run
  `pull` first); `--path`, `--lock` and `--force`. An unchanged file prints
  `unchanged`.
- An edited file whose base copy (`.paca-base/<id>.md`) is gone or is not
  what the last pull or push wrote is refused (exit 2): the version it was
  edited from is not known. Save your edits elsewhere, then delete the file
  and pull, or run `pull --force`. A file that already says what Paca holds
  is recorded again, which writes the base copy back.
- Whether the document changed in Paca is asked of Paca on every push, never
  taken from what an earlier pull saw.
- `--dry-run` prints how many blocks would change and sends nothing. The
  count is the same the push prints: blocks edited or replaced, plus blocks
  added or removed, plus blocks moved (one each), so one edited paragraph is
  `1 block changed`.
- A push holds the mirror's lock (`.paca-pull.lock`), so a push and a pull of
  the same mirror never run at once.

**How the file is read.** The file is compared with Paca's Markdown line by
line, and these differences are not edits:

- a byte order mark, CRLF line ends, and the newlines at the end of the file;
- outside fenced code, spaces and tabs at the end of a line (other white
  space, such as a non-breaking space, is text);
- a run of blank lines outside fenced code (read as one blank line);
- outside fenced code, a `-` or `+` bullet (read as `*`), but not on a
  thematic break line (`- - -` is a divider, not a bullet);
- outside fenced code, blank lines between two list items (so `* three`
  added straight under `* two` is one new item, checklist items included);
- outside fenced code, how a list item is indented, as long as it nests the
  same: a nested item written with a tab or four spaces is read as Paca
  writes it (two spaces under `* `, three under `1. `), and one space under
  `* ` is a sibling, read as such. Where a line that is not an item would
  belong somewhere else under the item's own indent than under Paca's (text
  or code inside an item, or a line that continues an item without its
  indent), that list's indents are read as written;
- outside fenced code, the number of a numbered item after the first in its
  list: Markdown numbers a list from its first item, so `1.` on every item
  reads as Paca's `1.`, `2.`, `3.` (the first item's number is the list's
  start, and counts);
- outside fenced code, the padding of table cells, and a table's delimiter
  row (its second row, `| --- | :-: |`) apart from its alignment, for a row
  indented three spaces at most (four make it code).

A `-` or `+` bullet, an indent and a number are read this way only on a line
that starts a list item: not on a line that continues a paragraph, such as
`10. ten` straight after text (only a bullet or `1.` can start a list
there).

Inside fenced code only line ends are read loosely: trailing spaces there are
part of the code. These differences are read as no edit although they are
one; Paca's own Markdown never has them, so they only matter in a file
written by hand:

- a run of blank lines inside indented code (code indented four spaces, not
  fenced) is read as one blank line, like any other run; Paca writes code
  fenced, so a file with indented code is refused at that line anyway;
- a line that starts with `|` is read as a table row even outside a table, so
  `| a` and `| a |` on a line of text read the same, and changing one into
  the other is not sent;
- blank lines at the end of a fenced code block that is never closed are
  dropped, like the blank lines at the end of any file.

A refusal says what to write: for example that Paca writes a blank line
before a line, or that Paca keeps a paragraph on one line (join the line
with the one after it).

Anything else must be written as Paca writes it, or the push is refused with
the line and what Paca would save there (for example `__bold__` where Paca
writes `**bold**`). After a push that differed only by these rules (or an
`unchanged` one), the file is rewritten to Paca's Markdown and the push prints
`normalised <file>`, so the next `pull` finds nothing to do.

**Merging on pull.** When a file was edited here and its document changed in
Paca, `pull` runs `git merge-file --diff3` with the version you pulled as the
base (`.paca-base/<id>.md`, or the snapshot of that version when the base copy
is gone):

- A file that already says what Paca holds, but for the rules above (a push
  that stopped after its write, say), takes Paca's version; nothing is
  reported for it.
- A clean merge writes the merged file (`merged <file>`), and `push` then
  sends your part.
- A conflict writes the file with markers (`<<<<<<< local`, `||||||| base`,
  `=======`, `>>>>>>> Paca (<writer>, <time>)`), lists the file as conflicted
  and exits 2. Every line you wrote is in the file. Resolve the marked lines,
  then push; push refuses the file while the markers are there, and every
  pull reports it as conflicted (exit 2) and never merges it again until they
  are gone. `pull --force` replaces it with Paca's version, discarding your
  edits in it; deleting the file and pulling does the same for that file
  alone.
- The file is kept as it is (exit 2, `kept <file>: edited locally …`), with
  what to do next. Two kinds, never mixed:
  - The version it was edited from is known, but the merge cannot run now:
    `git` is not installed or fails (pull again once it works), or the file
    is a managed copy (edit it in its repository; `pull --force` replaces
    it). If the document goes back to the version you pulled, the file is
    simply edited locally again and `push` sends it.
  - The version it was edited from is not known: neither the base copy nor a
    snapshot of the pulled version is left (the server keeps one snapshot
    per 3 minutes of edits), or an interrupted pull may have written into the
    file before you edited it (a stopped merge included; see below). Paca's version is written next to the file
    as `<file>.remote.md` and follows Paca on later pulls (a copy you edited
    is left alone). Nothing settles this file but you: every pull keeps it,
    even when the document goes back to an earlier version, and `push`
    refuses it (exit 1), naming the `.remote.md` copy once a pull has
    written it. Compare the two and
    save your edits elsewhere, then delete the file and pull, or run
    `pull --force`; either replaces the file with Paca's version, and the
    `.remote.md` copy is then removed (unless you edited it).

  Instead of `pull --force`, which replaces every such file, you can delete
  one file and pull: that file alone is written again from Paca.
- A document that was moved or renamed while you edited its file moves first,
  then merges under its new name.
- A merge is journalled: Paca's version is written as
  `.paca-base/<id>.next.md` and recorded in the state, with the hashes of
  your file before the merge and of the merged file, before the merged file
  is written; then it becomes the base copy. A pull that stops anywhere is
  settled by the next one from what is on disk, in this order:
  - `.next.md` is gone and the base copy is Paca's version: the merge had
    finished; its record is completed;
  - `.next.md` is there and the file is as it was before the merge: the
    merge starts over;
  - the file is the merged file, or still has the merge's conflict markers:
    the merge had written it; it is completed (and reported as conflicted
    while markers are left);
  - anything else (you changed the file after the merge wrote it and
    before the next pull, or `.next.md` or the base copy was deleted or
    changed): which version the file was edited from cannot be told, so the
    file is kept with its base unknown, as above (Paca's version next to it
    as `<file>.remote.md`; delete the file and pull, or `pull --force`).

  A file is never merged twice, and markers never nest.

  The same holds for a pull that stops or fails after writing a file it was
  not merging: the next pull knows that file may hold Paca's newer text. If
  you edited the file in between, it is kept with its base unknown, even if
  the document has gone back to the version you pulled, so a push can never
  send a change that was taken back in Paca.

**Limits.** Formatting Markdown cannot show must never move to another block,
so these edits are refused, or can be, even when the text would be right
(make them in smaller steps or in the web editor):

- Moving blocks, when the document holds any block with formatting Markdown
  cannot show: a reorder anywhere in it is refused, not only next to the
  formatted block. That includes moving a section that holds formatted
  blocks. In a document without such blocks, a move is sent.
- Ids: a plain block keeps its id where it stays in place (edited or not). A
  moved plain block may get a new id: a move is often read as removing the
  block and adding it again elsewhere.
- Changes in several places that each pass alone but together could move
  formatting: deleting a formatted paragraph and adding one elsewhere,
  rewording a formatted paragraph and another one, or editing a formatted
  paragraph and adding one that reads like it.
- Two identical paragraphs where one is formatted: deleting or rewording one
  of them can be refused, since it cannot be told which one you meant.
- Two structural edits (an item added, removed or moved) in one list that has
  a formatted item can be refused.
- A numbered list item after one you edited can lose a start number it does
  not show (only a list's first item is numbered from it).

### `push`: write a repository file to a document

`push docs/plan.md` writes the file to a document by path (default
`Plans/<file name>`, or `Specifications/<file name>` when the file is under a
`specs` folder; later pushes reuse the recorded path; `--path` sets another).
The document records where it comes from: the repository (`owner/name` from the
`origin` remote, else the folder name), the file's path in it, and the commit
when the file has no uncommitted changes.

- An unchanged file is not sent (`unchanged`), unless `--path`, `--lock` or
  `--force` asks for something new. A push the server finds identical is also
  `unchanged`.
- Each push after the first sends `If-Match` with what the last push stored.
  If someone changed the document in Paca since, the push stops (exit 2) and
  prints who changed it, when, their note, client and task, and a diff from the
  last pushed version to the Paca copy. Merge that change into the file and
  push again, or use `--force` to overwrite it.
- `--note` and `--task` are stored with the change; without `--note`, the note
  is the subject of the file's last commit (when the file is committed).
- `--lock` makes the document read-only in Paca: only a push from the same
  repository can change it. A push to a document locked to another repository
  fails and names that repository.
- Two files never share a document: a push to a document path that
  `.paca/push.json` records for another file (same project and server) stops
  with exit 1 and names that file; pass `--path` to push to another document.
  `--force` does not override this, and `--dry-run` reports it too. If the
  other file is gone (it was moved, as with `git mv`, including a move that
  changes only case on a case-insensitive file system), the push takes over its
  entry instead and says so: `If-Match` and the conflict diff still use the
  last push from the old name, and the document records the new source path
  even when the content is unchanged. If several gone files claim the
  document, all their entries are dropped. Paths are compared as Paca
  resolves them: without case, with spaces around each segment and empty
  segments ignored (`Plans/plan`, ` plans / PLAN ` and `Plans//plan` are one
  document), and recorded as you typed them. If Paca still resolves a push to
  a document another file records, the push says so, records nothing and
  exits 1.
- `--dry-run` prints what would be sent and sends nothing.
- State: `.paca/push.json` at the repository root (per file: document path and
  id, file hash, ETag, and the project and server URL it was pushed to; an
  entry for another project or server is ignored) and `.paca/push-base/` (the
  last pushed text of each file as Paca stores it, used for the conflict
  diff). Outside a git repository, the working
  folder takes the place of the repository root. You may want `.paca/` in
  `.gitignore`.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | Done, or nothing to do |
| 1 | Error (bad arguments, server error, server too old) |
| 2 | Conflict: the Paca copy changed since the last push or pull, a merge left conflict markers, or an edited mirror file was kept |
| 3 | The server could not be reached; nothing was recorded |

## Markdown/BlockNote Conversion

The MCP server automatically handles conversion between Markdown and BlockNote JSON format:

- **Reading**: Fetches content as BlockNote JSON and converts to Markdown for readability
- **Writing**: Accepts Markdown input and converts to BlockNote JSON for storage

This allows AI assistants to work with familiar Markdown format while the API stores content in BlockNote's rich text format.

## API Key Authentication

All tools authenticate via the `X-API-Key` header. Generate an API key in your Paca user settings and set it as `PACA_API_KEY` in your MCP client configuration.

## Examples

### Create a Task with Markdown Description

```
Tool: create_task
Arguments:
- projectId: "project-uuid"
- title: "Implement user authentication"
- description: "# Implementation Plan\n\n## Steps\n1. Create auth service\n2. Add login endpoint\n3. Implement JWT tokens"
- statusId: "status-uuid"
- importance: 5
- tags: ["auth", "backend"]
```

### Update Document Content

```
Tool: update_document
Arguments:
- projectId: "project-uuid"
- docId: "doc-uuid"
- content: "# System Design\n\n## Architecture\nThis document describes the..."
```

## Notes

- The server requires a running Paca API instance
- API keys can be created through the Paca web interface under user settings
- All descriptions and document contents are automatically converted between Markdown and BlockNote format
- Date fields should be provided in ISO 8601 format (e.g., `2024-01-01T00:00:00Z`)

## Contributing

Interested in contributing to the MCP server? Clone the repository and follow the steps below:

```bash
git clone https://github.com/paca-ai/paca.git
cd paca/apps/mcp
npm install
npm run build
```

For development with auto-rebuild:

```bash
npm run watch
```

To test with the MCP Inspector:

```bash
npm run inspector
```

For detailed information about the codebase structure, how to add new tools, and code style guidelines, see [DEVELOPMENT.md](./DEVELOPMENT.md).

## License

Apache License 2.0

