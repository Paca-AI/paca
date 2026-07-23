# AI Agent — REST API Design

This document describes the public REST endpoints added to `services/api` for AI Agent management.

All endpoints follow the existing Paca API conventions: JWT authentication, project-scoped authorization, and standard error envelope `{"error": "..."}`.

Every agent has an `agent_type` of either `llm` or `acp` (default `llm` when omitted). The two types are mutually exclusive in both configuration and execution:

- **`llm`** — Paca runs the conversation itself, in an isolated Docker container, using the `llm_provider`/`llm_model`/`llm_api_key`/`llm_base_url` fields. See [overview.md](overview.md) for the sandboxed execution model.
- **`acp`** — the conversation runs on the user's own machine via the [`paca-acp-bridge`](../../apps/acp-bridge/README.md) daemon, connecting to a coding CLI (Claude Code, Codex, Gemini CLI, or a custom [ACP](https://docs.openhands.dev/sdk/guides/agent-acp) server) the user already has installed and authenticated there. Paca never sees, stores, or requests an LLM credential for `acp` agents — `llm_api_key` is neither required nor read for this type. The only credential Paca manages is the bridge connection token described below, which authenticates the WebSocket link, not the LLM.

---

## Agent Management

### `GET /api/v1/projects/:projectId/agents`

List all agents in a project.

**Response:**
```json
{
  "items": [
    {
      "id": "uuid",
      "project_id": "uuid",
      "member_id": "uuid",
      "name": "Dev Bot",
      "handle": "dev-bot",
      "avatar_url": null,
      "agent_type": "llm",
      "llm_provider": "anthropic",
      "llm_model": "claude-sonnet-4-6",
      "llm_base_url": "",
      "acp_provider": null,
      "acp_command": [],
      "has_acp_bridge_token": false,
      "max_iterations": 50,
      "timeout_minutes": 30,
      "created_at": "2026-05-19T00:00:00Z"
    }
  ]
}
```

---

### `POST /api/v1/projects/:projectId/agents`

Create a new agent. This also creates the corresponding `project_members` row with `member_type = 'agent'`.

**Request body (`agent_type: "llm"`, the default):**
```json
{
  "name": "Dev Bot",
  "handle": "dev-bot",
  "agent_type": "llm",
  "llm_provider": "anthropic",
  "llm_model": "claude-sonnet-4-6",
  "llm_api_key": "sk-ant-...",
  "llm_base_url": null,
  "system_prompt": "You are a senior software engineer...",
  "max_iterations": 50,
  "timeout_minutes": 30,
  "project_role_id": "uuid"
}
```

`llm_provider`, `llm_model`, and `llm_api_key` are required when `agent_type` is `llm`. `llm_api_key` is stored in the secrets store; only a reference (`llm_api_key_secret`) is kept on the `agents` row.

**Request body (`agent_type: "acp"`):**
```json
{
  "name": "Local Claude Code",
  "handle": "local-claude",
  "agent_type": "acp",
  "acp_provider": "claude-code",
  "system_prompt": "You are a senior software engineer...",
  "project_role_id": "uuid"
}
```

`acp_provider` (one of `claude-code`, `codex`, `gemini-cli`, `custom`) is required when `agent_type` is `acp`. `acp_command` is required only when `acp_provider` is `custom` — built-in providers resolve a default launch command via the OpenHands SDK's own provider registry. None of the `llm_*` fields apply to `acp` agents.

**Response:** `201 Created` with the created agent object.

---

### `GET /api/v1/projects/:projectId/agents/:agentId`

Get a single agent, including its MCP servers and skills.

**Response:**
```json
{
  "id": "uuid",
  "name": "Dev Bot",
  "handle": "dev-bot",
  "agent_type": "llm",
  "system_prompt": "...",
  "mcp_servers": [
    {
      "id": "uuid",
      "server_name": "fetch",
      "transport": "stdio",
      "command": "uvx",
      "args": ["mcp-server-fetch"],
      "is_enabled": true
    }
  ],
  "skills": [
    {
      "id": "uuid",
      "skill_name": "developer",
      "skill_source": "inline",
      "triggers": ["implement", "fix", "refactor"],
      "is_enabled": true
    }
  ]
}
```

---

### `PATCH /api/v1/projects/:projectId/agents/:agentId`

Update agent configuration. `agent_type` itself is immutable after creation — only the fields belonging to the agent's existing type (`llm_*` or `acp_*`) can be updated.

---

### `DELETE /api/v1/projects/:projectId/agents/:agentId`

Soft-delete the agent and its corresponding project member. Stops any running conversations for this agent.

---

## ACP Local Bridge

Endpoints for `acp`-type agents only; return an error for `llm`-type agents.

### `POST /api/v1/projects/:projectId/agents/:agentId/acp-bridge-token`

Issue a new local-bridge connection token, invalidating any previous one. The plaintext token is returned once — only its hash is persisted, so it cannot be retrieved again after this response.

**Response:**
```json
{
  "token": "<plaintext-token>",
  "run_command": "uvx paca-acp-bridge run --agent-id <agent-id> --token <plaintext-token> --server https://your-paca-instance.example.com"
}
```

---

### `GET /api/v1/projects/:projectId/agents/:agentId/acp-bridge-status`

Live connected/disconnected status for the agent's local bridge, proxied from `services/ai-agent`'s presence check.

**Response:**
```json
{ "connected": true }
```

---

## Agent MCP Servers

### `GET /api/v1/projects/:projectId/agents/:agentId/mcp-servers`

List MCP servers configured on an agent. **Response:** `{"items": [...]}`, each item shaped like the `mcp_servers` entries above.

---

### `POST /api/v1/projects/:projectId/agents/:agentId/mcp-servers`

Add an MCP server to an agent.

**Request body:**
```json
{
  "server_name": "repomix",
  "transport": "stdio",
  "command": "npx",
  "args": ["-y", "repomix@1.4.2", "--mcp"],
  "env": {}
}
```

---

### `PATCH /api/v1/projects/:projectId/agents/:agentId/mcp-servers/:serverId`

Update or enable/disable an MCP server.

---

### `DELETE /api/v1/projects/:projectId/agents/:agentId/mcp-servers/:serverId`

Remove an MCP server from an agent.

---

## Agent Skills

### `GET /api/v1/projects/:projectId/agents/:agentId/skills`

List skills attached to an agent. **Response:** `{"items": [...]}`, each item shaped like the `skills` entries above.

---

### `POST /api/v1/projects/:projectId/agents/:agentId/skills`

Add a skill to an agent.

**Request body (inline):**
```json
{
  "skill_name": "code-review-guide",
  "skill_source": "inline",
  "skill_content": "---\nname: code-review-guide\ndescription: Project code review guidelines.\n---\n\n# Code Review Guidelines\n...",
  "triggers": ["review", "pr"]
}
```

**Request body (GitHub URL):**
```json
{
  "skill_name": "github-workflow",
  "skill_source": "github_url",
  "source_url": "https://github.com/OpenHands/extensions/blob/main/github/SKILL.md",
  "triggers": ["github", "git"]
}
```

`skill_source` is one of `inline`, `marketplace`, or `github_url`.

---

### `PATCH /api/v1/projects/:projectId/agents/:agentId/skills/:skillId`

Update or enable/disable a skill.

---

### `DELETE /api/v1/projects/:projectId/agents/:agentId/skills/:skillId`

Remove a skill.

---

## Agent Environment Variables

`llm`-type agents only — secret environment variables injected into the agent's sandbox container at conversation start (e.g., a repo-specific token an MCP server needs). Unsupported for `acp` agents, which run entirely in the user's own local environment instead.

### `GET /api/v1/projects/:projectId/agents/:agentId/env-vars`

List env vars for an agent. **Response:** `{"items": [...]}`. Values are always redacted (`"***"`) — the plaintext is never returned once set.

---

### `POST /api/v1/projects/:projectId/agents/:agentId/env-vars`

Add an env var.

**Request body:**
```json
{ "key": "GITHUB_TOKEN", "value": "ghp_..." }
```

---

### `PATCH /api/v1/projects/:projectId/agents/:agentId/env-vars/:envVarId`

Replace an env var's value.

---

### `DELETE /api/v1/projects/:projectId/agents/:agentId/env-vars/:envVarId`

Remove an env var.

---

## LLM Models & Skill Templates

Two read-only, project-agnostic reference endpoints under `/api/v1/agents`, used by the agent creation/edit UI:

### `GET /api/v1/agents/llm-models`

Proxies to `services/ai-agent`'s `/llm/models`, returning the verified LLM models available, grouped by provider.

---

### `GET /api/v1/agents/skill-templates`

Lists the built-in skill templates (`developer`, `ba`, `manual-tester`, `po-assistant`) that a new agent's skills can be seeded from — see [overview.md](overview.md#skill-templates).

**Response:**
```json
[
  {
    "slug": "developer",
    "name": "Developer",
    "description": "...",
    "content": "---\nname: developer\n...",
    "triggers": ["implement", "fix", "refactor"]
  }
]
```

---

## Conversations

Conversations are project-scoped, not nested under a specific agent — a project's conversation history spans all of its agents.

### `GET /api/v1/projects/:projectId/conversations`

List conversations for the project, cursor-paginated (not offset-based).

**Query params:** `?status=running&agent_id=<uuid>&cursor=<opaque>&page_size=20`

**Response:**
```json
{
  "items": [
    {
      "id": "uuid",
      "agent_id": "uuid",
      "agent_name": "Dev Bot",
      "agent_handle": "dev-bot",
      "trigger_type": "task_assigned",
      "task_id": "uuid",
      "status": "running",
      "iteration_count": 12,
      "branch_name": "agent/implement-oauth-login",
      "pr_url": null,
      "started_at": "2026-05-19T10:00:00Z",
      "finished_at": null
    }
  ],
  "page_size": 20,
  "next_cursor": "<opaque-or-null>"
}
```

---

### `GET /api/v1/projects/:projectId/conversations/:conversationId`

Get full conversation detail.

---

### `GET /api/v1/projects/:projectId/conversations/:conversationId/events`

Offset-paginated list of conversation events.

**Query params:** `?offset=0&limit=50` (`limit` capped at 200, defaults to 50)

**Response:**
```json
{
  "items": [
    {
      "id": "uuid",
      "event_index": 0,
      "event_type": "MessageAction",
      "event_source": "user",
      "payload": { "message": "Implement the OAuth login flow..." },
      "created_at": "2026-05-19T10:00:01Z"
    },
    {
      "id": "uuid",
      "event_index": 1,
      "event_type": "CmdRunAction",
      "event_source": "agent",
      "payload": { "command": "ls -la /workspace/repo/src" },
      "created_at": "2026-05-19T10:00:03Z"
    }
  ],
  "total": 45
}
```

---

### `POST /api/v1/projects/:projectId/conversations/:conversationId/pause`

Interrupt the in-flight turn without tearing anything down — for `llm` agents the container keeps running; for `acp` agents a `pause_turn` message is forwarded to the bridge. A no-op if nothing is currently running. There is no `resume` endpoint.

**Response:** `200 OK` `{"message": "conversation pause requested"}`

---

### `POST /api/v1/projects/:projectId/conversations/:conversationId/stop`

Permanently stop a conversation. For `llm` agents this also destroys the container; for `acp` agents a `stop_turn` message is forwarded to the bridge instead.

**Response:** `200 OK` `{"message": "conversation stopped"}`

---

### `POST /api/v1/projects/:projectId/conversations/:conversationId/heartbeat`

Keepalive ping (~30s interval) from a client actively viewing a conversation, refreshing its idle timer.

**Response:** `200 OK` `{"status": "ok"}`

---

### `POST /api/v1/projects/:projectId/conversations/:conversationId/messages`

Send an additional message to a conversation. Requires the conversation to already be in `running` status — sending to a `paused` conversation returns an error rather than resuming it.

**Request body:**
```json
{
  "message": "Actually, please also add tests for the new endpoint."
}
```

---

## Agent Chat Sessions

Chat sessions are nested under their agent, not the project — a session is always between one member and one agent.

### `GET /api/v1/projects/:projectId/agents/:agentId/chat-sessions`

List chat sessions for the calling member with this agent. **Response:** `{"items": [...]}`.

---

### `POST /api/v1/projects/:projectId/agents/:agentId/chat-sessions`

Start a new chat session with an agent.

**Request body:**
```json
{
  "message": "Can you help me write the acceptance criteria for PACA-42?"
}
```

**Response:** `201 Created` `{"session": {...}, "conversation": {...}}`.

---

### `POST /api/v1/projects/:projectId/agents/:agentId/chat-sessions/:sessionId/messages`

Send a follow-up message in an existing chat session. **Response:** `201 Created` `{"conversation": {...}}` — the (possibly new) conversation this message triggered. There is no separate "list messages" endpoint — a session's `conversation_id` links to its conversation, whose message history is read via the conversation events endpoint above.
