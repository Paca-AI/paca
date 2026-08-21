# Database Schema

Interactive diagram: [https://dbdiagram.io/d/Paca-69c212ae78c6c4bc7a4fc190](https://dbdiagram.io/d/Paca-69c212ae78c6c4bc7a4fc190)

> **Note:** The DBML diagram above may lag behind the latest migrations. The authoritative source is `services/api/migrations/`. The schema below reflects the current migration state.

## Current Migration State

| File | Purpose |
|---|---|
| `000001_init.sql` | Full consolidated baseline schema (v0.1.x): `global_roles`, `users`, `projects` (with `task_id_prefix`), `project_roles`, `project_members`, `task_types`, `task_statuses`, `sprints`, `sprint_views` (with `view_context`), `view_task_positions`, `custom_field_definitions`, `task_counters`, `tasks`, `files`, `task_attachments`, `task_activities`, `doc_folders`, `documents`, `doc_snapshots`, `doc_activities`, `notifications`, `api_keys`, `plugins`, `plugin_extension_settings`, and seed data. |
| `000002_add_story_points.sql` | Adds `story_points INTEGER` (nullable, >= 0) to `tasks`. |
| `000003_add_project_is_public.sql` | Adds `is_public BOOLEAN` to `projects` for anonymous read access. |
| `000004_add_plugins.sql` | Adds `plugins` and `plugin_extension_settings` tables for the plugin system. |
| `000005_migrate_checklists_to_plugin.sql` | Drops legacy `task_checklists` and `task_checklist_items` tables (moved to `com.paca.checklist` plugin). |
| `000006_add_plugin_view_type.sql` | Extends `sprint_views.view_type` CHECK to allow `'plugin'` as a valid view type. |
| `000007_remove_github_tables.sql` | Drops GitHub integration tables (`github_integrations`, `github_repositories`, `github_pull_requests`, `github_task_pr_links`, `github_task_branches`) — migrated to plugins. |
| `000008_add_ai_agents.sql` | Adds AI agent tables: `agents`, `agent_mcp_servers`, `agent_skills`, `agent_chat_sessions`, `agent_conversations`, `agent_conversation_events`. Modifies `project_members` to add `member_type` and `agent_id` (makes `user_id` nullable) for agent membership support. |
| `000017_add_agent_environment_variables.sql` | Adds `agent_environment_variables` (per-agent secret env vars, encrypted at rest). |
| `000022_add_acp_agents.sql` | Adds ACP (Agent Client Protocol) agent support to `agents`: `agent_type` ('llm' \| 'acp'), `acp_provider`, `acp_command`, `acp_bridge_token_hash` — a second agent "shape" that delegates to a local coding CLI over a bridge daemon instead of running an LLM loop in-cluster. |
| `000031_add_global_agents.sql` | Adds "global" agents — an agent with no owning project (`agents.project_id` nullable, `agent_scope` discriminator, `global_role_id`) that is instead attached to zero or more projects via ordinary `project_members` rows, the same mechanism used to add a human member. Adds `actor_user_id` to `agent_chat_sessions` and `agent_conversations` for chat sessions/conversations started from the home page or admin pages, outside any project. See the comment above the `agents` table below. |
| `000038_add_agent_conversation_audience.sql` | Adds explicit owner-private/project-shared conversation audience metadata and constraints. |
| `000042_add_agent_task_handoffs.sql` | Adds internal task-run handoffs for later task execution continuity; handoffs are not task publications. |
| `000043_add_agent_turns_and_conclusions.sql` | Adds authoritative agent turns, fenced run attempts, immutable context snapshots/results, reliable outbox delivery, and append-only human-confirmed conclusion publications for owner-private Project Chats. See [`docs/ai-agent/private-chats.md`](../ai-agent/private-chats.md). |

*(Migrations between `000008` and `000017`/`000022`/`000031` that touch other subsystems — tasks, sprints, docs, notifications, etc. — are omitted here; see `services/api/migrations/` for the full, authoritative list.)*

## Schema (DBML)

```dbml
// --- USER & GLOBAL ROLE MANAGEMENT ---
Table users {
  id uuid [primary key]
  username varchar [unique, not null]
  password_hash varchar [not null]
  full_name varchar
  role_id uuid [ref: > global_roles.id, not null]
  must_change_password boolean [not null, default: false]
  created_at timestamp
  updated_at timestamp
  deleted_at timestamp [null]
}

Table global_roles {
  id uuid [primary key]
  name varchar [unique, not null]
  permissions jsonb [not null]
  created_at timestamp
  updated_at timestamp
}

// --- PROJECT & TEAM MANAGEMENT ---
Table projects {
  id uuid [primary key]
  name varchar [not null]
  description text [not null, default: '']
  task_id_prefix varchar [not null, default: '', note: 'Short uppercase alphanumeric tag prepended to task_number to form human-readable task ID, e.g. "PACA" → "PACA-1"']
  settings jsonb [not null, default: '{}']
  is_public boolean [not null, default: false, note: 'Allows anonymous read access when true']
  created_by uuid [ref: > users.id]
  created_at timestamp
}

Table project_roles {
  id uuid [primary key]
  project_id uuid [ref: > projects.id]
  role_name varchar
  permissions jsonb
}

Table project_members {
  id uuid [primary key]
  project_id uuid [ref: > projects.id]
  user_id uuid [null, ref: > users.id, note: 'null for agent members']
  project_role_id uuid [ref: > project_roles.id]
  member_type varchar [not null, default: 'human', note: 'human | agent']
  agent_id uuid [null, ref: > agents.id, note: 'null for human members']
  created_at timestamp [not null]
  deleted_at timestamp [null, note: 'Soft-delete timestamp. Re-adding a removed member restores the row rather than inserting a new one.']

  indexes {
    (project_id, user_id) [unique, note: 'Partial unique: WHERE deleted_at IS NULL AND member_type = human']
    (project_id, agent_id) [unique, note: 'Partial unique: WHERE deleted_at IS NULL AND member_type = agent']
  }
}

// --- TASK CONFIGURATION ---
Table task_types {
  id uuid [primary key]
  project_id uuid [ref: > projects.id]
  name varchar
  icon varchar
  color varchar
  description text
  is_default boolean [not null, default: false, note: 'True for the single default type seeded at project creation (Task). Only one type per project should have is_default = true.']
  is_system boolean [not null, default: false, note: 'True for system-managed types (Epic, Subtask). System types are seeded at project creation and cannot be created, edited, or deleted by users. They are displayed in a read-only section on the Task Types settings page and are excluded from inline task creation type pickers unless explicitly supported.']
}

Table task_statuses {
  id uuid [primary key]
  project_id uuid [ref: > projects.id]
  name varchar
  color varchar
  position integer
  category varchar [note: 'backlog | refinement | ready | todo | inprogress | done']
  is_default boolean [not null, default: false, note: 'True for the single default status seeded at project creation. Only one status per project should have is_default = true.']
}

// --- TASK COUNTERS ---
Table task_counters {
  project_id uuid [primary key, ref: > projects.id, note: 'Tracks the per-project sequential task number so that every task within a project gets a human-readable, monotonically increasing identifier.']
  last_value bigint [not null, default: 0, note: 'The last task number assigned to a task in this project']
}

// --- TASKS ---
Table tasks {
  id uuid [primary key]
  project_id uuid [ref: > projects.id]
  task_number bigint [not null, default: 0, note: 'Project-scoped sequential ID (1, 2, 3, …) assigned at creation and never reused. Unique per project via uq_tasks_project_task_number constraint.']
  task_type_id uuid [ref: > task_types.id]
  status_id uuid [ref: > task_statuses.id]
  sprint_id uuid [ref: > sprints.id]
  parent_task_id uuid [null, ref: > tasks.id]
  title varchar [not null]
  description jsonb [null, note: 'BlockNote rich-text document stored as a JSON array of block objects. null means no description. Each block object follows the BlockNote schema: { id, type, props, content, children }.']
  importance integer [not null, default: 0, note: 'unsigned; higher = more important']
  story_points integer [null, note: 'Story point estimate; must be >= 0 if set']
  reporter_id uuid [ref: > project_members.id]
  custom_fields jsonb [not null, default: '{}']
  start_date date [null]
  due_date date [null]
  tags jsonb [not null, default: '[]']
  created_at timestamp
  updated_at timestamp
  deleted_at timestamp [null, note: 'Soft-delete timestamp. Non-null rows are excluded from normal queries.']
}

Table custom_field_definitions {
  id uuid [primary key]
  project_id uuid [not null, ref: > projects.id]
  field_key varchar [not null, note: 'Unique per project; immutable after creation']
  display_name varchar [not null]
  field_type varchar [not null, note: 'text | number | date | select | multi_select | boolean | url']
  options jsonb [null, note: 'Ordered list of option labels for select / multi_select types']
  is_required boolean [not null, default: false]
  created_at timestamp
  updated_at timestamp

  indexes {
    (project_id, field_key) [unique]
  }
}

// --- SPRINTS & VIEWS ---
Table sprints {
  id uuid [primary key]
  project_id uuid [ref: > projects.id]
  name varchar
  start_date date
  end_date date
  goal text
  status varchar [note: 'planned | active | completed. Multiple sprints per project may be active simultaneously.']
}

Table sprint_views {
  id uuid [primary key]
  sprint_id uuid [null, ref: > sprints.id, note: 'null for project-level views (backlog, timeline); set for sprint views']
  project_id uuid [not null, ref: > projects.id]
  name varchar [not null]
  view_type varchar [not null, note: 'Layout: table | board | roadmap | plugin']
  view_context varchar [not null, note: 'Interaction discriminator: sprint | backlog | timeline. sprint rows always have sprint_id set; backlog and timeline rows have sprint_id = null.']
  position double [not null, default: 0, note: 'Zero-based tab order within the interaction; lower = further left in the tab bar. Updated on drag-to-reorder.']
  config jsonb [note: '''
    View display settings.  All keys are optional; unset keys fall back to
    per-project or system defaults.

    fields      array<string>  Ordered list of visible column names.
                               e.g. ["title","assignees","status","importance"]
    column_by   string         Field used to group board columns or table
                               groups.  e.g. "status" (default for board/sprint
                               views), "sprint" (default for product-backlog
                               Table view — groups tasks into sprint columns
                               plus an "Unassigned" column for tasks with no
                               sprint).
    swimlanes   string|null    Field used to create horizontal swimlane bands
                               across the view.  null = no swimlanes.
    sort_by     string         "manual" = user-defined drag order stored in
                               view_task_positions.  Any other value is a
                               field name used for automatic sort.
                               e.g. "importance", "created_at", "manual".
    field_sum   string         Aggregate shown in group/column headings.
                               "count" (default) = number of tasks.  Can be
                               any numeric custom field key.
    slice_by    string|null    Additional filter dimension applied to the
                               visible task set.  null = no slice.
    For plugin views: plugin_id, plugin_component are stored here.
  ''']
  created_at timestamp
  updated_at timestamp
}

Table view_task_positions {
  id uuid [primary key]
  view_id uuid [ref: > sprint_views.id]
  task_id uuid [ref: > tasks.id]
  position double [not null, default: 0, note: 'Zero-based index within its group_key; lower = higher in list']
  group_key varchar [null, note: 'Value of the column_by field for this task (e.g. status name, assignee id) or swimlane key.  null = ungrouped.']

  indexes {
    (view_id, task_id) [unique]
  }
}

// --- FILES ---
Table files {
  id uuid [primary key]
  storage_key text [unique, not null, note: 'Key in the object-store (S3-compatible)']
  bucket text [not null, note: 'S3 bucket name']
  file_name text [not null]
  content_type text [not null, default: 'application/octet-stream']
  file_size bigint [not null, default: 0]
  upload_status text [not null, default: 'pending', note: 'pending | uploaded | failed']
  multipart_upload_id text [null, note: 'Non-null only while a multipart upload is in progress']
  uploaded_by uuid [ref: > users.id]
  created_at timestamp
  updated_at timestamp
}

Table task_attachments {
  id uuid [primary key]
  task_id uuid [not null, ref: > tasks.id]
  file_id uuid [not null, ref: > files.id]
  created_by uuid [ref: > users.id]
  created_at timestamp [not null]

  indexes {
    (task_id, file_id) [unique]
  }
}

// A task can have multiple assignees; task_assignees is the join table.
// member_id cascades on delete, so removing a member drops just their one
// join row rather than clearing the whole task.
Table task_assignees {
  task_id uuid [not null, ref: > tasks.id]
  member_id uuid [not null, ref: > project_members.id]
  assigned_at timestamp [not null]

  indexes {
    (task_id, member_id) [pk]
  }
}

// --- DOCUMENTATION ---
Table doc_folders {
  id         uuid [primary key]
  project_id uuid [not null, ref: > projects.id]
  parent_id  uuid [null, ref: > doc_folders.id, note: 'null = root; self-reference for nested folders']
  name       varchar [not null]
  position   integer [not null, default: 0, note: 'Zero-based order among siblings']
  created_by uuid [null, ref: > project_members.id]
  created_at timestamp
  updated_at timestamp
}

Table documents {
  id         uuid [primary key]
  project_id uuid [not null, ref: > projects.id]
  folder_id  uuid [null, ref: > doc_folders.id, note: 'null = root (no folder)']
  title      varchar [not null, default: 'Untitled']
  content    jsonb [null, note: 'BlockNote rich-text document stored as a JSON array of block objects. null means no content. Each block follows the BlockNote schema: { id, type, props, content, children }.']
  position   integer [not null, default: 0, note: 'Zero-based order within the same folder/root']
  created_by uuid [null, ref: > project_members.id]
  updated_by uuid [null, ref: > project_members.id]
  created_at timestamp
  updated_at timestamp
  deleted_at timestamp [null, note: 'Soft-delete timestamp']
}

Table doc_snapshots {
  id              uuid [primary key]
  document_id     uuid [not null, ref: > documents.id]
  title           varchar [not null, note: 'Title at the time of the snapshot']
  content         jsonb [null, note: 'BlockNote content at the time of the snapshot']
  snapshot_number bigint [not null, default: 0, note: 'Monotonically increasing per document; set by trigger']
  created_by      uuid [null, ref: > project_members.id]
  created_at      timestamp
}

Table doc_activities {
  id            uuid [primary key]
  document_id   uuid [not null, ref: > documents.id]
  actor_id      uuid [null, ref: > project_members.id, note: 'NULL for system events or if the member was removed']
  activity_type varchar [not null, note: 'doc.created | doc.updated | doc.deleted | doc.moved | doc.folder.created | doc.folder.updated | doc.folder.deleted | comment']
  content       jsonb [not null, default: '{}', note: 'For doc.updated: [{field, old, new}]. For comment: {text}. For doc.moved: {from_folder_id, to_folder_id}.']
  created_at    timestamp
  updated_at    timestamp
  deleted_at    timestamp [null, note: 'Soft-delete for comments']
}

Table task_activities {
  id uuid [primary key]
  task_id uuid [not null, ref: > tasks.id]
  actor_id uuid [null, ref: > project_members.id, note: 'References project_members(id). Resolved from the authenticated user UUID by the ActivityConsumer at consume-time using the task project_id. NULL for system events or if the member was removed before the stream message was processed.']
  activity_type varchar [not null]
  content jsonb [not null, default: '{}']
  created_at timestamp
  updated_at timestamp
  deleted_at timestamp [null, note: 'Soft-delete for comments']
}

// --- PLUGINS ---
Table plugins {
  id uuid [primary key]
  name text [unique, not null, note: 'reverse-DNS id, e.g. "com.paca.checklist"']
  version text [not null, default: '0.0.0', note: 'semver, e.g. "1.0.0"']
  manifest jsonb [not null, default: '{}', note: 'Full plugin.json contents (routes, extension points, event subscriptions, etc.)']
  enabled boolean [not null, default: true]
  installed_at timestamp
  updated_at timestamp
}

Table plugin_extension_settings {
  id uuid [primary key]
  plugin_id uuid [not null, ref: > plugins.id]
  extension_point text [not null, note: 'Extension point identifier, e.g. "task.detail.section"']
  settings jsonb [not null, default: '{}', note: 'System-wide ordering and visibility settings: {order, hidden}']
  updated_at timestamp

  indexes {
    (plugin_id, extension_point) [unique]
  }
}

// --- NOTIFICATIONS ---
Table notifications {
  id                uuid [primary key]
  recipient_user_id uuid [not null, ref: > users.id, note: 'The user who receives the notification']
  actor_member_id   uuid [null, ref: > project_members.id, note: 'The project member who triggered the notification']
  type              varchar [not null, note: 'assigned | mentioned']
  task_id           uuid [null, ref: > tasks.id]
  project_id        uuid [not null, ref: > projects.id]
  read_at           timestamp [null, note: 'When the notification was marked as read']
  created_at        timestamp
}

// --- API KEY ---
Table api_keys {
  id uuid [primary key]
  user_id uuid [not null, ref: > users.id]
  name text [not null]
  key_prefix text [not null, note: 'First 8 hex characters of the raw key, for display only']
  key_hash text [not null, unique, note: 'SHA-256 hex digest of the raw key used for lookup/validation']
  last_used_at timestamp [null]
  expires_at timestamp [null]
  created_at timestamp
  revoked_at timestamp [null]
}

// --- AI AGENTS (000008, extended by 000017 / 000022 / 000031) ---
//
// Global Agents (000031)
// -----------------------
// An agent has two possible scopes, discriminated by agent_scope:
//   'project' (default) — owned by exactly one project (project_id set),
//                          same as every agent before this migration.
//   'global'             — project_id is NULL. Chats on the home page and
//                          admin pages (no project context — see
//                          agent_chat_sessions/agent_conversations'
//                          actor_user_id below) and is attached to projects
//                          only indirectly, by being added as an ordinary
//                          project_members row — the exact same "invite a
//                          member" action used for a human, just with
//                          agent_id set instead of user_id. Because
//                          uq_pm_project_agent (000008) is scoped per
//                          (project_id, agent_id) rather than per agent_id
//                          alone, one global agent can be invited into many
//                          projects simultaneously, and behaves there
//                          exactly like a project-scoped agent (task
//                          assignment, @mention, project chat) — nothing
//                          else keys off agents.project_id, only the
//                          project_members row.
// global_role_id mirrors users.role_id: it governs what a global agent may
// do at global scope (i.e. via project-management-shaped tools called from
// the home/admin chat, with no project context of its own). A project-scoped
// agent never has a global_role_id — see ck_agents_scope.
Table agents {
  id uuid [primary key]
  project_id uuid [null, ref: > projects.id, note: 'NULL for a global-scope agent (agent_scope = global)']
  agent_scope varchar [not null, default: 'project', note: '''project | global. ck_agents_scope enforces:
    project -> project_id NOT NULL AND global_role_id NULL
    global  -> project_id NULL''']
  global_role_id uuid [null, ref: > global_roles.id, note: 'Only ever set for a global-scope agent. ON DELETE RESTRICT, mirrors fk_users_role_id.']
  name varchar [not null]
  handle varchar [not null, note: '@mention handle. Unique per project for a project-scoped agent; unique workspace-wide for a global agent (see indexes below).']
  avatar_url varchar [null]
  agent_type varchar [not null, default: 'llm', note: '''llm | acp (000022).
    llm — runs an LLM reasoning loop in-cluster via llm_provider/llm_model/llm_api_key_secret.
    acp — delegates to a coding CLI (Claude Code, Codex, Gemini CLI, or custom) the user runs
          locally, connected over an authenticated bridge daemon; llm_* columns stay empty.''']
  llm_provider varchar [not null, note: 'LiteLLM provider prefix, e.g. anthropic, openai. Empty string for agent_type = acp.']
  llm_model varchar [not null, note: 'LiteLLM model name, e.g. claude-sonnet-4-6. Empty string for agent_type = acp.']
  llm_api_key_secret varchar [not null, note: 'Encrypted at rest; never returned by the API. Empty string for agent_type = acp.']
  llm_base_url varchar [null]
  acp_provider varchar [null, note: 'claude-code | codex | gemini-cli | goose | custom. Required when agent_type = acp (ck_agents_acp_requires_provider).']
  acp_command jsonb [not null, default: '[]', note: 'JSON array of the launch command + args. Only meaningful when acp_provider = custom.']
  acp_bridge_token_hash varchar [null, note: 'SHA-256 hex digest of the current local-bridge auth token. The plaintext is shown once, never persisted.']
  system_prompt text [not null, default: '']
  max_iterations integer [not null, default: 50]
  timeout_minutes integer [not null, default: 30]
  git_committer_name varchar [not null, default: 'paca-agent']
  git_committer_email varchar [not null]
  created_by uuid [null, ref: > users.id]
  created_at timestamp
  updated_at timestamp
  deleted_at timestamp [null]

  indexes {
    (project_id, handle) [unique, note: 'Partial unique: WHERE deleted_at IS NULL AND project_id IS NOT NULL']
    (handle) [unique, note: 'Partial unique: WHERE deleted_at IS NULL AND project_id IS NULL — closes the gap standard SQL NULL semantics leave in the index above for global agents']
    (agent_scope)
    (global_role_id) [note: 'Partial: WHERE global_role_id IS NOT NULL']
  }
}

// Per-agent secret environment variables, encrypted at rest, injected into
// the agent's sandbox container at run time (000017).
Table agent_environment_variables {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id]
  key varchar [not null]
  encrypted_value text [not null]
  created_at timestamp
  updated_at timestamp

  indexes {
    (agent_id, key) [unique]
  }
}

Table agent_mcp_servers {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id]
  server_name varchar [not null, note: 'Key in mcpServers map']
  transport varchar [not null, note: 'stdio | sse | http | oauth']
  command varchar [null]
  args jsonb [not null, default: '[]']
  url varchar [null]
  env jsonb [not null, default: '{}']
  is_enabled boolean [not null, default: true]
  created_at timestamp
  updated_at timestamp

  indexes {
    (agent_id, server_name) [unique]
  }
}

Table agent_skills {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id]
  skill_name varchar [not null]
  skill_source varchar [not null, note: 'inline | marketplace | github_url']
  skill_content text [not null, default: '']
  source_url varchar [null]
  triggers jsonb [not null, default: '[]']
  is_enabled boolean [not null, default: true]
  created_at timestamp
  updated_at timestamp

  indexes {
    (agent_id, skill_name) [unique]
  }
}

// project_id/member_id (a project chat) and actor_user_id (a global chat)
// are mutually exclusive — ck_agent_chat_sessions_actor (000031) requires
// exactly one side set: project_id+member_id both NOT NULL and actor_user_id
// NULL, or project_id+member_id both NULL and actor_user_id NOT NULL.
Table agent_chat_sessions {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id]
  project_id uuid [null, ref: > projects.id, note: 'NULL for a global chat session (see actor_user_id)']
  member_id uuid [null, ref: > project_members.id, note: 'The human member chatting with a project agent. NULL for a global chat session.']
  actor_user_id uuid [null, ref: > users.id, note: 'The human chatting with a global agent from the home/admin pages, identified directly (no project_members row need exist). NULL for a project chat session. ON DELETE RESTRICT.']
  title varchar [null]
  last_message_at timestamp [null]
  created_at timestamp
  updated_at timestamp
}

// triggered_by_member_id and actor_user_id are never both set
// (ck_agent_conversations_actor, 000031) — three distinguishable actor
// states: a project-scoped human member (triggered_by_member_id set), the
// automation-workflow engine (both NULL, unchanged since 000018), or a human
// chatting with a global agent (actor_user_id set, project_id NULL).
Table agent_conversations {
  id uuid [primary key, note: 'Also used as the ACP session/conversation identifier agent-runner passes to Goose']
  agent_id uuid [not null, ref: > agents.id]
  project_id uuid [null, ref: > projects.id, note: 'NULL for a global-chat conversation (project_id IS NULL + actor_user_id IS NOT NULL)']
  trigger_type varchar [not null, note: 'task_assigned | comment_mention | chat_message | description_write. Global chat reuses chat_message — project_id IS NULL is what distinguishes it.']
  task_id uuid [null, ref: > tasks.id]
  comment_id uuid [null]
  chat_session_id uuid [null, ref: > agent_chat_sessions.id]
  triggered_by_member_id uuid [null, ref: > project_members.id, note: 'NULL for the automation engine or a global-chat conversation (see actor_user_id)']
  actor_user_id uuid [null, ref: > users.id, note: 'Set only for a global-chat conversation. ON DELETE RESTRICT.']
  status varchar [not null, default: 'queued', note: 'queued | running | paused | finished | failed | stopped']
  container_id varchar [null]
  host_port integer [null]
  iteration_count integer [not null, default: 0]
  error_message text [null]
  repo_plugin_id uuid [null]
  repo_clone_url varchar [null]
  branch_name varchar [null]
  pr_url varchar [null]
  persistence_dir varchar [null]
  started_at timestamp [null]
  finished_at timestamp [null]
  created_at timestamp
  updated_at timestamp
}

Table agent_conversation_events {
  id uuid [primary key]
  conversation_id uuid [not null, ref: > agent_conversations.id]
  event_index integer [not null, note: 'Sequential index within the conversation (0-based)']
  event_type varchar [not null, note: 'ACP session/update kind for llm agents: agent_message_chunk, tool_call, tool_call_update, turn_end']
  event_source varchar [not null, note: 'agent | user | system | environment']
  payload jsonb [not null, default: '{}']
  created_at timestamp

  indexes {
    (conversation_id, event_index) [unique]
  }
}
```
