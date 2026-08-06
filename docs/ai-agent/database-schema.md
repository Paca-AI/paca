# AI Agent — Database Schema

This document describes the tables that support AI Agents in Paca — both **project agents** (owned by a single project) and **global agents** (owned by none, invited into projects on demand). It reflects the actual current schema, not the original design; see `services/api/migrations/` for the authoritative, ordered source of truth.

For the full workspace-wide schema (users, projects, tasks, etc.), see [`docs/architecture/database-schema.md`](../architecture/database-schema.md), which includes this same DBML alongside everything else.

## Migrations

| File | Purpose |
|---|---|
| `000008_add_ai_agents.sql` | Original tables: `agents`, `agent_mcp_servers`, `agent_skills`, `agent_chat_sessions`, `agent_conversations`, `agent_conversation_events`. Extends `project_members` with `member_type`/`agent_id` so an agent can be a project member alongside humans. |
| `000010_add_trigger_prompts_to_agents.sql`, `000019_drop_agent_trigger_prompts.sql` | Added, then dropped, per-trigger system prompt columns on `agents` — superseded by the ai-agent service's fixed trigger-context skills. Net effect on the current schema: none. |
| `000017_add_agent_environment_variables.sql` | Adds `agent_environment_variables`: per-agent secret env vars, encrypted at rest, injected into the sandbox at run time. |
| `000020_drop_agent_clone_pr_permissions.sql` | Drops `agents.can_clone_repos`/`can_create_prs` — cloning and PR creation became runtime capabilities available to every agent, not a per-agent toggle. |
| `000022_add_acp_agents.sql` | Adds ACP (Agent Client Protocol) agents: `agent_type` ('llm' \| 'acp'), `acp_provider`, `acp_command`, `acp_bridge_token_hash`. An ACP agent delegates to a coding CLI the user runs locally, connected over an authenticated bridge daemon, instead of running an LLM loop in Paca's own infrastructure. |
| `000031_add_global_agents.sql` | Adds **global agents** — see below. |

---

## Global Agents

Every agent before `000031` was a **project agent**: owned by exactly one project (`agents.project_id NOT NULL`), created there, and deleted along with it. `000031` adds a second kind:

- **Project agent** (`agent_scope = 'project'`, unchanged): `project_id` set, no `global_role_id`. Behaves exactly as before.
- **Global agent** (`agent_scope = 'global'`): `project_id` is `NULL`. It has no home project of its own — instead it:
  - Chats with any user directly from the home page and admin pages, with no project context (`agent_chat_sessions`/`agent_conversations` rows where `project_id IS NULL` and `actor_user_id` identifies the human instead of a `project_members` row).
  - Is **invited into a project** exactly the way a human is added as a member: `POST /projects/:id/members` with `agent_id` instead of `user_id`, creating an ordinary `project_members` row. Once invited, it behaves identically to a project agent inside that project — task assignment, `@mention`, project chat, project-role permissions all work unmodified, because none of that logic was ever keyed on `agents.project_id`, only on the `project_members` row existing.
  - Can be invited into **many projects at once** — `uq_pm_project_agent` (`000008`) was already scoped per `(project_id, agent_id)`, not per `agent_id` alone, so this needed no schema change. Each project it's in gets independent conversations; a single conversation is always scoped to one project (or to none, for the global chat).
  - Has its own admin-scope permission set via `global_role_id` (nullable FK to `global_roles`, mirroring `users.role_id`) — what it may do when acting with no project context, e.g. from the home/admin chat.

`ck_agents_scope` enforces the two shapes stay mutually exclusive:

```sql
(agent_scope = 'project' AND project_id IS NOT NULL AND global_role_id IS NULL)
OR
(agent_scope = 'global' AND project_id IS NULL)
```

A global chat session or conversation has no `project_members` row to identify the human by (there may be none — the agent might not be invited into any project yet), so `agent_chat_sessions` and `agent_conversations` each gain an `actor_user_id UUID REFERENCES users(id)` column that stands in for `member_id`/`triggered_by_member_id` in that case. Both tables enforce "exactly one of the project-scoped shape or the global-actor shape" via a CHECK constraint (`ck_agent_chat_sessions_actor`, `ck_agent_conversations_actor`) — see the table definitions below.

**Scope note:** global agents can chat, and — once invited into a project — do project work the same as any project agent. They cannot create or manage user accounts or global roles; no MCP tool exposes that capability to any agent, project or global (a deliberate safety boundary, not a schema limitation).

---

## Tables

### `agents`

```dbml
Table agents {
  id uuid [primary key]
  project_id uuid [null, ref: > projects.id, note: 'NULL for a global-scope agent (agent_scope = global). The existing ON DELETE CASCADE FK is never triggered by a NULL value, so deleting a project still cleans up only its own project agents.']
  agent_scope varchar [not null, default: 'project', note: 'project | global. See ck_agents_scope.']
  global_role_id uuid [null, ref: > global_roles.id, note: 'Only ever set for a global-scope agent. ON DELETE RESTRICT, mirrors fk_users_role_id.']
  name varchar [not null, note: 'Display name shown in the project member list / agent picker']
  handle varchar [not null, note: '@mention handle. Unique per project for a project agent; unique workspace-wide for a global agent.']
  avatar_url varchar [null]

  // Shape: LLM agent vs ACP agent (000022)
  agent_type varchar [not null, default: 'llm', note: 'llm | acp']

  // LLM configuration — empty string for agent_type = acp
  llm_provider varchar [not null, note: 'LiteLLM provider prefix, e.g. anthropic, openai']
  llm_model varchar [not null, note: 'LiteLLM model name, e.g. claude-sonnet-4-6']
  llm_api_key_secret varchar [not null, note: 'Encrypted at rest; never returned by the API']
  llm_base_url varchar [null, note: 'Optional custom base URL (e.g. Azure or a local LLM)']

  // ACP configuration — set only for agent_type = acp
  acp_provider varchar [null, note: 'claude-code | codex | gemini-cli | custom. Required when agent_type = acp.']
  acp_command jsonb [not null, default: '[]', note: 'Launch command + args; only meaningful when acp_provider = custom']
  acp_bridge_token_hash varchar [null, note: 'SHA-256 digest of the current local-bridge auth token. Plaintext shown once, never persisted. No global-scope bridge-token endpoint exists yet — see Known Gaps.']

  system_prompt text [not null, default: '']
  max_iterations integer [not null, default: 50, note: 'Hard cap on agent reasoning steps per conversation']
  timeout_minutes integer [not null, default: 30, note: 'Wall-clock timeout for a single conversation']
  git_committer_name varchar [not null, default: 'paca-agent']
  git_committer_email varchar [not null]

  created_by uuid [null, ref: > users.id]
  created_at timestamp [not null]
  updated_at timestamp [not null]
  deleted_at timestamp [null]

  indexes {
    (project_id, handle) [unique, note: 'Partial unique: WHERE deleted_at IS NULL AND project_id IS NOT NULL']
    (handle) [unique, note: 'Partial unique: WHERE deleted_at IS NULL AND project_id IS NULL — global agents']
    (agent_scope)
    (global_role_id) [note: 'Partial: WHERE global_role_id IS NOT NULL']
  }
}
```

> A previous version of this document described a separate `agent_types` template table (with `slug`, `default_llm_provider`, etc.) and an `agents.agent_type_id` FK to it. Neither ever existed in the schema that actually shipped in `000008` — this doc was aspirational, not a description of the real migration. Agent presets today are a small client-side constant (`AGENT_PRESETS` in `apps/web/src/lib/agent-api.ts`) that just pre-fills the create form, not a database-backed concept.

### `agent_environment_variables` (000017)

Per-agent secret environment variables, encrypted at rest, injected into the agent's sandbox at run time.

```dbml
Table agent_environment_variables {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id, note: 'ON DELETE CASCADE']
  key text [not null]
  encrypted_value text [not null]
  created_at timestamp [not null]
  updated_at timestamp [not null]

  indexes {
    (agent_id, key) [unique]
  }
}
```

### `agent_mcp_servers`

Custom MCP server configurations attached to an agent. Each row is one entry in the `mcpServers` map. Scope-agnostic — works identically for a project or global agent, keyed only by `agent_id`.

```dbml
Table agent_mcp_servers {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id]
  server_name varchar [not null, note: 'Key in mcpServers map, e.g. "fetch", "repomix"']
  transport varchar [not null, note: 'stdio | sse | http | oauth']
  command varchar [null, note: 'Binary to execute for stdio transport']
  args jsonb [not null, default: '[]']
  url varchar [null, note: 'Server URL for sse/http/oauth transport']
  env jsonb [not null, default: '{}']
  is_enabled boolean [not null, default: true]
  created_at timestamp [not null]
  updated_at timestamp [not null]

  indexes {
    (agent_id, server_name) [unique]
  }
}
```

### `agent_skills`

Skills associated with an agent, stored as full `SKILL.md` content and materialized into the sandbox workspace at conversation start. Scope-agnostic, same as `agent_mcp_servers` above.

```dbml
Table agent_skills {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id]
  skill_name varchar [not null]
  skill_source varchar [not null, note: 'inline | marketplace | github_url']
  skill_content text [not null, default: '']
  source_url varchar [null]
  triggers jsonb [not null, default: '[]']
  is_enabled boolean [not null, default: true]
  created_at timestamp [not null]
  updated_at timestamp [not null]

  indexes {
    (agent_id, skill_name) [unique]
  }
}
```

### `agent_chat_sessions`

Persistent chat sessions between a human and an agent. Each session accumulates messages across possibly-many conversations and can be resumed.

```dbml
Table agent_chat_sessions {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id]
  project_id uuid [null, ref: > projects.id, note: 'A project chat session. NULL for a global chat session — see actor_user_id.']
  member_id uuid [null, ref: > project_members.id, note: 'The human project member chatting with a project agent. NULL for a global chat session.']
  actor_user_id uuid [null, ref: > users.id, note: 'The human chatting with a global agent from the home/admin pages, identified directly — there may be no project_members row for them at all. NULL for a project chat session. ON DELETE RESTRICT (users are only ever soft-deleted).']
  title varchar [null, note: 'Auto-generated or user-set session title']
  last_message_at timestamp [null]
  created_at timestamp [not null]
  updated_at timestamp [not null]
}
```

`ck_agent_chat_sessions_actor` enforces exactly one shape:

```sql
(project_id IS NOT NULL AND member_id IS NOT NULL AND actor_user_id IS NULL)
OR
(project_id IS NULL AND member_id IS NULL AND actor_user_id IS NOT NULL)
```

### `agent_conversations`

Tracks each agent run. One row per trigger invocation — a task assignment, an `@mention`, a chat message, or (for a global agent) a global chat message.

```dbml
Table agent_conversations {
  id uuid [primary key, note: 'Also used as the underlying OpenHands SDK conversation_id for state persistence']
  agent_id uuid [not null, ref: > agents.id]
  project_id uuid [null, ref: > projects.id, note: 'NULL for a global-chat conversation (project_id IS NULL + actor_user_id IS NOT NULL)']

  // Trigger context
  trigger_type varchar [not null, note: 'task_assigned | comment_mention | chat_message | description_write | automation_message. Global chat always uses chat_message — project_id IS NULL is what distinguishes it.']
  task_id uuid [null, ref: > tasks.id]
  comment_id uuid [null, note: 'task_activities row id for the triggering comment']
  chat_session_id uuid [null, ref: > agent_chat_sessions.id]
  triggered_by_member_id uuid [null, ref: > project_members.id, note: 'A project-scoped human member. NULL for the automation-workflow engine or a global-chat conversation (see actor_user_id).']
  actor_user_id uuid [null, ref: > users.id, note: 'Set only for a global-chat conversation — the human chatting with a global agent, identified directly. ON DELETE RESTRICT.']

  // Execution state
  status varchar [not null, default: 'queued', note: 'queued | running | paused | finished | failed | stopped']
  container_id varchar [null]
  host_port integer [null]
  iteration_count integer [not null, default: 0]
  error_message text [null]

  // Repository context — always null for a global-chat conversation; a
  // conversation with no project has no repo to clone
  repo_plugin_id uuid [null]
  repo_clone_url varchar [null]
  branch_name varchar [null]
  pr_url varchar [null]

  persistence_dir varchar [null]

  started_at timestamp [null]
  finished_at timestamp [null]
  created_at timestamp [not null]
  updated_at timestamp [not null]
}
```

`ck_agent_conversations_actor` enforces `triggered_by_member_id` and `actor_user_id` are never both set — the three resulting actor states are:

| `triggered_by_member_id` | `actor_user_id` | Meaning |
|---|---|---|
| set | NULL | Project-scoped human member triggered this |
| NULL | NULL | Automation-workflow engine triggered this, no human involved |
| NULL | set | A human triggered this via the global chat, outside any project |

### `agent_conversation_events`

Individual events emitted during a conversation — unchanged by `000031`; a global conversation's events are rows here exactly like a project conversation's.

```dbml
Table agent_conversation_events {
  id uuid [primary key]
  conversation_id uuid [not null, ref: > agent_conversations.id]
  event_index integer [not null, note: 'Sequential index within the conversation (0-based)']
  event_type varchar [not null, note: 'OpenHands SDK event type: MessageAction | CmdRunAction | FileEditAction | AgentFinishAction | CmdOutputObservation | etc.']
  event_source varchar [not null, note: 'agent | user | system | environment']
  payload jsonb [not null, default: '{}']
  created_at timestamp [not null]

  indexes {
    (conversation_id, event_index) [unique]
  }
}
```

---

## Modified: `project_members`

Unchanged by `000031` — this is what makes "invite a global agent into a project" reuse the existing member-add path rather than needing new schema. From `000008`:

```sql
ALTER TABLE project_members
  ADD COLUMN member_type VARCHAR NOT NULL DEFAULT 'human'
  CHECK (member_type IN ('human', 'agent'));

ALTER TABLE project_members
  ADD COLUMN agent_id UUID NULL REFERENCES agents(id);

ALTER TABLE project_members
  ADD CONSTRAINT ck_pm_member_type_ref
  CHECK (
    (member_type = 'human' AND user_id IS NOT NULL AND agent_id IS NULL)
    OR
    (member_type = 'agent' AND agent_id IS NOT NULL AND user_id IS NULL)
  );

-- Scoped per (project_id, agent_id), not per agent_id alone — this is what
-- lets one global agent be invited into many projects at once.
CREATE UNIQUE INDEX uq_pm_project_agent
  ON project_members (project_id, agent_id)
  WHERE deleted_at IS NULL AND member_type = 'agent';
```

---

## Schema Relationships (DBML)

```dbml
// A project agent belongs to a project; a global agent belongs to none and
// is instead exposed as a project member per project it's invited into.
Ref: agents.project_id > projects.id
Ref: agents.global_role_id > global_roles.id
Ref: project_members.agent_id > agents.id [note: 'null for human members']

// Agent configuration — scope-agnostic, keyed only by agent_id
Ref: agent_mcp_servers.agent_id > agents.id [delete: cascade]
Ref: agent_skills.agent_id > agents.id [delete: cascade]
Ref: agent_environment_variables.agent_id > agents.id [delete: cascade]

// Conversations reference the agent and, for a project conversation, the
// project/task/triggering member; a global-chat conversation instead
// carries actor_user_id and a null project_id
Ref: agent_conversations.agent_id > agents.id
Ref: agent_conversations.project_id > projects.id
Ref: agent_conversations.task_id > tasks.id
Ref: agent_conversations.triggered_by_member_id > project_members.id
Ref: agent_conversations.actor_user_id > users.id
Ref: agent_conversations.chat_session_id > agent_chat_sessions.id

// Events belong to a conversation
Ref: agent_conversation_events.conversation_id > agent_conversations.id [delete: cascade]

// Chat sessions tie an agent to a human — via a project member for a
// project session, or directly via actor_user_id for a global session
Ref: agent_chat_sessions.agent_id > agents.id
Ref: agent_chat_sessions.project_id > projects.id
Ref: agent_chat_sessions.member_id > project_members.id
Ref: agent_chat_sessions.actor_user_id > users.id
```

---

## Index Strategy

| Table | Key indexes |
|---|---|
| `agents` | `(project_id, handle)` partial unique (project agents), `(handle)` partial unique (global agents), `(agent_scope)`, `(global_role_id)` partial |
| `agent_environment_variables` | `(agent_id, key)` unique |
| `agent_conversations` | `(agent_id, status)`, `(task_id)`, `(chat_session_id)`, `(actor_user_id)` partial |
| `agent_conversation_events` | `(conversation_id, event_index)` unique, `(conversation_id, event_type)` |
| `agent_chat_sessions` | `(agent_id, member_id)`, `(project_id, member_id)`, `(agent_id, actor_user_id)` partial |

---

## Known Gaps

These are deliberate scoping decisions from the global-agents implementation, not oversights to silently work around:

- **No ACP bridge token endpoint at global scope.** `POST /projects/:projectId/agents/:agentId/acp-bridge-token` is project-scoped only. A global agent can be created with `agent_type = 'acp'`, but has no way to generate a bridge token — the admin UI for creating a global agent only offers `agent_type = 'llm'` for this reason. ACP's whole premise (a local CLI running against a checked-out repo) doesn't have an obvious global-scope equivalent, since a global chat has no project/repo context.
- **No "list all global conversations across agents" endpoint.** Only per-agent chat sessions are listable (`GET /agents/:agentId/chat-sessions`). The Agent Chats page (`apps/web/src/routes/_authenticated/agent-chats`) fans out one session-list query per chattable agent rather than a single aggregate call.
