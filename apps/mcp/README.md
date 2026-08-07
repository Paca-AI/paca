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
- **Agent-Specific Permissions**: MCP tools are filtered based on agent's project permissions at startup
- **Comprehensive Project Management**: Full project lifecycle with member and role management
- **Advanced Task Management**: Tasks with types, statuses, custom fields, and attachments
- **Sprint Management**: Complete sprint lifecycle management
- **Document Management**: Documents with folder hierarchy, version history, and file support
- **View Management**: Multiple view types (sprint, backlog, timeline) with task positioning
- **GitHub Integration**: Repository linking, PR management, and branch creation
- **Collaboration Tools**: Comments and activities for team collaboration
- **BlockNote Integration**: Automatic conversion between BlockNote JSON and Markdown
- **Plugin MCP Tools**: Plugins can contribute additional MCP tools, loaded automatically at startup

## Agent & User Permissions

The MCP server automatically filters available tools based on permissions, whether you're using it as an agent or as a regular user.

### Agent Mode vs. User Mode

| Mode | Trigger | API Key Source | Permission Source | Scope |
|---|---|---|---|---|
| **Agent Single-Project** | `PACA_AGENT_ID` + `PACA_PROJECT_ID` | That agent's own `PACA_API_KEY` | Agent's permissions in the specified project | Single project only |
| **Agent Global** | `PACA_AGENT_ID` only (no `PACA_PROJECT_ID`) | That agent's own `PACA_API_KEY` | Agent's global role, plus its per-project permissions resolved as each tool call needs them | Every project the agent is invited into |
| **User Single-Project** | `PACA_PROJECT_ID` only (no `PACA_AGENT_ID`) | User's personal API key | User's global + project permissions | Single project |
| **User Global** | Neither set | User's personal API key | User's global permissions only | All projects (no project-scoped tools) |

**Note**: Each ACP agent has its own `PACA_API_KEY`, generated per-agent — not a key shared across every agent in the deployment. Generate (or regenerate) it from the agent's setup page in the Paca UI; regenerating immediately invalidates whatever key was live before, since only one key is ever active per agent.

### Agent Mode

Set `PACA_AGENT_ID` to connect as a specific ACP agent instead of as yourself. Add `PACA_PROJECT_ID` too if you want every tool call pinned to one project (recommended for a project-scoped agent); leave it unset for a global agent, which can then work across any project it's been invited into.

1. **Authentication**: Uses that agent's own `PACA_API_KEY` — the key itself identifies the agent, so no separate impersonation header is needed or honored
2. **Permission Fetch**: With `PACA_PROJECT_ID` set, only that project's permissions are fetched; without it, the agent's global role and per-project permissions are resolved as each tool call needs them
3. **Tool Filtering**: Shows only tools the agent has permission to use
4. **Project Validation**: With `PACA_PROJECT_ID` set, enforces that all tool calls use that project ID
5. **Performance**: Single-project mode is optimized for that common case (one API call at startup)

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
3. **Global Permissions**: Fetches global permissions via `GET /api/v1/users/me/global-permissions`
4. **Tool Filtering**: Shows only globally permitted tools (no project-scoped tools)
5. **Best Practice**: Set `PACA_PROJECT_ID` to filter tools at the MCP level and reduce API errors

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

**Note**: Without `PACA_PROJECT_ID`, project-scoped tools (like `list_tasks`, `create_task`) will not be available.

### Supported Permissions

| Permission | Tools Requiring It |
|---|---|
| `projects.read` | `list_projects`, `get_project` |
| `projects.write` | `update_project`, `delete_project` |
| `projects.create` | `create_project` |
| `tasks.read` | `list_tasks`, `get_task`, `get_task_by_number`, `list_task_types`, `list_task_statuses` |
| `tasks.write` | `create_task`, `update_task`, `delete_task`, `create_task_type`, `update_task_type`, `delete_task_type`, `set_default_task_type`, `create_task_status`, `update_task_status`, `delete_task_status`, `set_default_task_status` |
| `sprints.read` | `list_sprints`, `get_sprint` |
| `sprints.write` | `create_sprint`, `update_sprint`, `delete_sprint`, `complete_sprint` |
| `docs.read` | `list_documents`, `get_document`, `list_doc_folders`, `list_doc_snapshots`, `get_doc_snapshot` |
| `docs.write` | `create_document`, `update_document`, `delete_document`, `create_doc_folder`, `update_doc_folder`, `delete_doc_folder` |
| `project.members.read` | `list_project_members`, `get_my_project_permissions` |
| `project.members.write` | `add_project_member`, `update_project_member_role`, `remove_project_member` |
| `project.roles.read` | `list_project_roles` |
| `project.roles.write` | `create_project_role`, `update_project_role`, `delete_project_role` |

### Configuring Permissions

**For Agents:**
1. **Add Agent as Project Member**: Add the agent to the desired projects with appropriate roles
2. **Configure Role Permissions**: Ensure the assigned roles have the necessary permissions
3. **Restart MCP Server**: Restart the MCP server to refresh the permission cache

**Important**: When using agent mode, `PACA_API_KEY` must be that agent's own key (see "How to Get the Agent's API Key" above) — the key itself is what identifies the agent to the server, so `PACA_AGENT_ID` alone is not a claim the server will trust from any other key.

**For Users:**
1. **Assign Global Roles**: Grant users global permissions through their global roles
2. **Add to Projects**: Add users to projects with appropriate project roles
3. **Configure Project Roles**: Ensure project roles have the necessary permissions
4. **Restart MCP Server**: Restart the MCP server to refresh the permission cache

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

**Note**: The MCP server automatically filters tools based on your permissions:
- **With `PACA_AGENT_ID`**: Filters tools based on the agent's project permissions
- **Without `PACA_AGENT_ID`**: Filters tools based on your personal user permissions (including global permissions)

If permission fetching fails, all tools will be shown to maintain backward compatibility.

## Available Tools

The MCP server provides **81 tools** across **16 categories** for comprehensive project management.

### 📁 Project Management (5 tools)
- `list_projects` - List all accessible projects
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
- `list_sprints` - List all sprints in a project
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
- `list_project_members` - List all members of a project
- `add_project_member` - Add a member to a project
- `get_my_project_permissions` - Get the current user's permissions
- `update_project_member_role` - Update a member's role
- `remove_project_member` - Remove a member from a project

### 🎭 Project Roles (4 tools)
- `list_project_roles` - List all roles in a project
- `create_project_role` - Create a new project role
- `update_project_role` - Update an existing project role
- `delete_project_role` - Delete a project role

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
- `bulk_move_tasks` - Bulk move tasks in a view
- `move_task` - Move a task within a view

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

### 🔗 GitHub Integration (7 tools)
- `get_github_integration` - Get GitHub integration status
- `set_github_token` - Set GitHub token for a project
- `delete_github_token` - Delete GitHub token
- `list_github_repositories` - List available GitHub repositories
- `list_linked_github_repos` - List linked repositories
- `link_github_repository` - Link a GitHub repository
- `unlink_github_repository` - Unlink a GitHub repository

### 💬 Task Activities (4 tools)
- `list_task_activities` - List all activities for a task
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

