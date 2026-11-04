# Database Schema

Interactive diagram: [https://dbdiagram.io/d/Paca-69c212ae78c6c4bc7a4fc190](https://dbdiagram.io/d/Paca-69c212ae78c6c4bc7a4fc190)

> **Note:** The DBML diagram above may lag behind the latest migrations. The authoritative source is `services/api/migrations/`. The schema below reflects the current migration state.

## Current Migration State

| File | Purpose |
|---|---|
| `000001_init.sql` | Full consolidated baseline schema (v0.1.x): `global_roles`, `users`, `projects` (with `task_id_prefix`), `project_roles`, `project_members`, `task_types`, `task_statuses`, `sprints`, `sprint_views` (with `view_context`), `view_task_positions`, `custom_field_definitions`, `task_counters`, `tasks`, `files`, `task_attachments`, `task_activities`, `doc_folders`, `documents`, `doc_snapshots`, `doc_activities`, `notifications`, `api_keys`, `plugins`, `plugin_extension_settings`, and seed data. |
| `000002_add_story_points.sql` | Adds `story_points INTEGER` (nullable, >= 0) to `tasks`. |
| `000003_add_project_is_public.sql` | Adds `is_public BOOLEAN` to `projects` for anonymous read access. Dropped again by `000065_drop_project_is_public.sql`. |
| `000004_add_plugins.sql` | Adds `plugins` and `plugin_extension_settings` tables for the plugin system. |
| `000005_migrate_checklists_to_plugin.sql` | Drops legacy `task_checklists` and `task_checklist_items` tables (moved to `com.paca.checklist` plugin). |
| `000006_add_plugin_view_type.sql` | Extends `sprint_views.view_type` CHECK to allow `'plugin'` as a valid view type. |
| `000007_remove_github_tables.sql` | Drops GitHub integration tables (`github_integrations`, `github_repositories`, `github_pull_requests`, `github_task_pr_links`, `github_task_branches`) — migrated to plugins. |
| `000008_add_ai_agents.sql` | Adds AI agent tables: `agents`, `agent_mcp_servers`, `agent_skills`, `agent_chat_sessions`, `agent_conversations`, `agent_conversation_events`. Modifies `project_members` to add `member_type` and `agent_id` (makes `user_id` nullable) for agent membership support. |
| `000017_add_agent_environment_variables.sql` | Adds `agent_environment_variables` (per-agent secret env vars, encrypted at rest). |
| `000022_add_acp_agents.sql` | Adds ACP (Agent Client Protocol) agent support to `agents`: `agent_type` ('llm' \| 'acp'), `acp_provider`, `acp_command`, `acp_bridge_token_hash` — a second agent "shape" that delegates to a local coding CLI over a bridge daemon instead of running an LLM loop in-cluster. |
| `000031_add_global_agents.sql` | Adds "global" agents — an agent with no owning project (`agents.project_id` nullable, `agent_scope` discriminator, `global_role_id`) that is instead attached to zero or more projects via ordinary `project_members` rows, the same mechanism used to add a human member. Adds `actor_account_id` to `agent_chat_sessions` and `agent_conversations` for chat sessions/conversations started from the home page or admin pages, outside any project. See the comment above the `agents` table below. |
| `000059_add_default_global_role.sql` | Adds `global_roles.is_default` (one default at a time, enforced by the partial unique index `uq_global_roles_one_default`) and marks `USER` the default where none is set. The API used to hardcode the role *named* `USER` for every new account; the default is data now, changed with `PUT /admin/global-roles/:roleId/set-default`. |
| `000061_unify_activities.sql` | Creates the single `activities` table for every entity in a project (task, doc, sprint, view, automation, environment, member) with an `origin` column, copies all `task_activities` and `doc_activities` rows into it under their original IDs, then drops both old tables. |
| `000064_iam_roles.sql` | IAM authorization. Creates `roles` (a JSON `policy` per role; platform roles have `project_id` NULL, project-owned roles carry their project) and `role_attachments` (role to user/agent, platform-wide or scoped to one project), then converts the legacy model in one transaction and self-checks it before commit: `global_roles` and `project_roles` become roles, `users.role_id`, `agents.global_role_id` and `project_members.project_role_id` become attachments, while restricted agents/environments (`access_mode = 'restricted'`) are deliberately **not** converted and become open until an admin adds `Deny` roles. The legacy tables are **not** dropped here. See [authorization](authorization.md). |
| `000065_plugin_manifest_actions.sql` | Rewrites every installed plugin manifest: `requirePermissions` route middleware becomes `requireActions`, and `customPermissions[].key` / `requiredPermission` become IAM actions (`time_logging.manage_all` becomes `time_logging:manage_all`). |
| `000066_relax_legacy_role_columns.sql` | Makes `users.role_id` and `project_members.project_role_id` nullable: new accounts and members get their roles from `role_attachments` only. |
| `000067_project_role_resources.sql` | Rewrites the policies of project-owned roles so every resource lies inside the owning project (`project/*` becomes `project/<id>`, `*` becomes `project/<id>/*`, outside resources are dropped). |
| `000068_add_roles_assign.sql` | Changing a member's roles now needs only `roles:assign`. Adds `roles:assign` to every `Allow` statement that grants `project.members:write` (or `project.members:*`) on resources that reach role resources, so existing roles keep that ability. Idempotent; updates `roles.updated_at` only for changed roles. |
| `000065_drop_project_is_public.sql` | Drops `projects.is_public`. There is no public mode: every project route needs a signed-in caller whose roles allow the action, so the flag (which only let anonymous visitors in) and its data are removed. |

| `000066_unified_accounts.sql` | Introduces `accounts` (one row per user, agent or service account) as the single home for identity, name, avatar and soft-delete; `users` and `agents` become extension tables keyed by the account id; `role_attachments`, `project_members`, `api_keys` and the audit columns reference `accounts`; the duplicated and legacy columns are dropped. See [Accounts model](#accounts-model). |

*(Migrations between `000008` and `000017`/`000022`/`000031` that touch other subsystems — tasks, sprints, docs, notifications, etc. — are omitted here; see `services/api/migrations/` for the full, authoritative list.)*

## Accounts model

Migration `000066` gives every actor (user, agent, service account) one `accounts` row, so API keys, role attachments, project membership and the authn/authz middleware each deal with one kind of principal. Rules:

1. **Every fact is stored once.** No mirrored `deleted_at`, no derived flags, no legacy column kept next to its replacement.
2. **Each rule has one owner.** Credentials resolve in one function, permissions come from `role_attachments` only, "is this account alive" is answered by `accounts` only.
3. **Type-specific data stays out of `accounts`.** It holds only what every kind has.

| Table | Holds |
|---|---|
| `accounts` | `id` (reuses the existing `users.id` / `agents.id`, so no foreign key or `role_attachments.principal_id` was rewritten), `kind` (`user`, `agent`, `service_account`), `name`, `description`, avatar keys, `created_by`, timestamps and `deleted_at`, the **only** soft-delete flag. No `status` column: revoking keys and deleting the account already cover it. |
| `users` | Login data only: `username`, `email`, `password_hash`, `must_change_password`. `user_identities` and `password_set_tokens` stay on `users(id)`: they only make sense for people who log in. Everything that names an actor or recipient (`notifications.recipient_account_id`, `agent_*.actor_account_id`, `user_view_configs.account_id`) references `accounts(id)` (migration `000068`). |
| `agents` | `project_id` (NULL = global agent), `handle` and all agent configuration, including `acp_bridge_token_hash` (a bridge-to-API transport secret, not an account credential). |
| service accounts | No extension table: they need nothing beyond `accounts` plus `api_keys` and `role_attachments`. |
| `api_keys` | `account_id` (was `user_id`) and `created_by` (who minted it; the admin for a service account). An agent's MCP key is an ordinary `api_keys` row (`agents.mcp_api_key_hash` was migrated into it, named `MCP key`). |
| `role_attachments` | `account_id` replaces `principal_type` + `principal_id`, so the type is no longer stored twice and a deleted account can no longer leave orphans. Roles are the only source of permission: `users.role_id`, `agents.global_role_id`, `project_members.project_role_id` and the legacy role tables are gone. |
| `project_members` | `account_id` replaces `user_id` + `agent_id` + `member_type`. `project_members.id` is unchanged, so tasks, activities, assignees, docs and conversations still reference it as the actor. Active membership is unique on `(project_id, account_id)`. |

`users` and `agents` carry a constant `kind` column with a composite FK `(id, kind) -> accounts (id, kind)`, so an extension row can only point at an account of its own kind. Audit columns that name an actor (`files.uploaded_by`, `task_attachments.created_by`, `environments.created_by`, `page_annotations.created_by/resolved_by`, `project_exports.requested_by`, `workspace_settings.updated_by`, ...) now reference `accounts(id)`, because a service account can upload a file.

**Identifier uniqueness.** `users.username`, `users.email` and `agents.handle` must be unique among *live* accounts, but a partial index cannot read `accounts.deleted_at`, and copying the flag would make liveness two facts. One trigger function, `enforce_live_unique_identifiers`, rejects a row whose identifier is held by another live account (`handle` per project, or workspace-wide for a global agent), raising `unique_violation` with the old constraint names. Email is compared case-sensitively, as the old index did. A second trigger, `delete_owning_account`, removes the `accounts` row when a `users`/`agents` row is hard-deleted.

**Migration.** One transaction: create `accounts`; backfill from `users` and `agents` reusing their ids; add the composite FKs; move `agents.mcp_api_key_hash` into `api_keys`; replace the polymorphic columns with `account_id`; retarget the audit columns; drop the duplicated and legacy columns; install the triggers; self-check. Role attachments whose principal no longer exists are deleted, reported only as a `NOTICE`. It is irreversible, so back up first. Because migration and code ship together there is no expand/contract split. `test/integration/accounts_migration_test.go` runs it against a populated database.

**Cost.** Repositories for users and agents always join `accounts` (name, avatar, liveness): one primary-key join, the price of storing each fact once. Authentication, service accounts and the API are covered in [authorization](authorization.md#authentication-and-principals) and the [service accounts API](../api/service-accounts.md).

## Schema (DBML)

```dbml
// --- ACCOUNTS (000066, see "Accounts model" above) ---
// Every actor is one accounts row: a human user, an agent or a service account.
// users / agents are extension tables whose primary key IS the account id (the
// ids of existing users and agents are reused, so no foreign key is rewritten).
// Each fact is stored once: name/description/avatar and the soft-delete flag live
// here and nowhere else. Service accounts need nothing beyond these columns, so
// they have no extension table.
//
// Identifier uniqueness: users.username and agents.handle must be unique among
// LIVE accounts, but a partial index cannot look at accounts.deleted_at. Rather
// than copying deleted_at onto users/agents (a second source of truth), ONE
// constraint-trigger function, attached to users and to agents, rejects a
// duplicate whose account is not deleted. Scope: username workspace-wide;
// handle per project_id (project agent) or workspace-wide (global agent).
Table accounts {
  id uuid [primary key]
  kind text [not null, note: 'user | agent | service_account. The only place an actor\'s type is stored. UNIQUE (id, kind) is the target of the kind-pinning FKs from users and agents.']
  name text [not null, note: 'Display name (was users.full_name and agents.name).']
  description text [not null, default: '']
  avatar_key text [null, note: '256x256 object key (was on users and agents).']
  avatar_thumb_key text [null, note: '64x64 object key.']
  created_by uuid [null, ref: > accounts.id, note: 'Who created the account (was agents.created_by). ON DELETE SET NULL.']
  created_at timestamp [not null]
  updated_at timestamp [not null]
  deleted_at timestamp [null, note: 'The ONLY soft-delete flag. "Is this actor alive" is answered here; users and agents carry no copy. A deleted account no longer authenticates and frees its username / handle.']

  indexes {
    (id, kind) [unique]
    (kind) [note: 'Partial: WHERE deleted_at IS NULL']
  }
}

// --- IAM ROLES (000064) ---
// A role is a JSON policy document. An account (user, agent or service account) holds any
// number of roles through role_attachments; a project member is an account. This replaces global_roles /
// project_roles below, which are legacy: still present after 000064 (their data
// was converted) but no longer read for authorization.
Table roles {
  id uuid [primary key]
  name text [not null, note: 'Unique among platform roles (project_id NULL) and unique per project.']
  description text [not null, default: '']
  policy jsonb [not null, note: 'IAM policy document: {version, statements[{sid, effect, actions, resources, conditions}]}. Saving needs only roles:write (not bounded by the author\'s own grants); a project-owned role\'s resources must all lie inside project_id.']
  project_id uuid [null, ref: > projects.id, note: 'Owner project; NULL = platform role. ON DELETE CASCADE.']
  is_system boolean [not null, default: false, note: 'Managed by Paca (SUPER_ADMIN, ADMIN, USER, each project Admin): editable but not deletable.']
  is_default boolean [not null, default: false, note: 'The role new users and global agents start with. At most one row (partial unique index uq_roles_single_default).']
  legacy_kind text [null, note: "Migration provenance: global | global_agent | project"]
  legacy_id uuid [null]
  created_at timestamp
  updated_at timestamp
}

Table role_attachments {
  id uuid [primary key]
  role_id uuid [not null, ref: > roles.id, note: 'ON DELETE CASCADE']
  account_id uuid [not null, ref: > accounts.id, note: 'Replaces principal_type + principal_id (the type is accounts.kind). A real FK, so deleting an account removes its attachments instead of orphaning them. ON DELETE CASCADE.']
  project_id uuid [null, ref: > projects.id, note: 'NULL = platform-wide; otherwise the role applies only inside this project.']
  created_by uuid [null, ref: > accounts.id]
  created_at timestamp [not null]

  indexes {
    (role_id, account_id) [unique, note: 'Partial: WHERE project_id IS NULL (uq_role_attachments_platform)']
    (role_id, account_id, project_id) [unique, note: 'Partial: WHERE project_id IS NOT NULL (uq_role_attachments_project)']
    (account_id)
    (project_id)
  }
}

// --- USER & GLOBAL ROLE MANAGEMENT ---
// Human-only data. name, avatar and soft-delete live on accounts.
Table users {
  id uuid [primary key, ref: - accounts.id, note: 'Equals accounts.id. Composite FK (id, kind) -> accounts (id, kind) with kind = user pins the type; ON DELETE CASCADE.']
  kind text [not null, default: 'user', note: 'Constant (CHECK kind = user). Exists only so the composite FK can pin the account type; carries no information.']
  username varchar [not null, note: 'Login name. Unique among LIVE accounts, enforced by the shared constraint trigger described under accounts (the index cannot see accounts.deleted_at). Was unique among non-deleted users (000016).']
  email text [null, note: 'Unique among LIVE accounts (same trigger as username; was uni_users_email_active). Plus a plain lower(email) lookup index; queries join accounts for liveness.']
  password_hash varchar [not null]
  must_change_password boolean [not null, default: false]
  // Dropped by the contract step: full_name (-> accounts.name), avatar_key / avatar_thumb_key (-> accounts),
  // role_id (LEGACY, roles are role_attachments), created_at / updated_at / deleted_at (-> accounts).
}

// LEGACY since 000064: converted into roles; no longer read for authorization.
Table global_roles {
  id uuid [primary key]
  name varchar [unique, not null]
  permissions jsonb [not null]
  is_default boolean [not null, default: false, note: 'The role new users and new global agents start with. At most one row (partial unique index uq_global_roles_one_default); the row that holds it cannot be deleted.']
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
  created_by uuid [ref: > accounts.id]
  created_at timestamp
}

// LEGACY since 000064: converted into roles; no longer read for authorization.
Table project_roles {
  id uuid [primary key]
  project_id uuid [ref: > projects.id]
  role_name varchar
  permissions jsonb
}

Table project_members {
  id uuid [primary key, note: 'Stays the actor reference for tasks, activities, assignees, docs and conversations.']
  project_id uuid [ref: > projects.id]
  account_id uuid [not null, ref: > accounts.id, note: 'Replaces user_id + agent_id + member_type + ck_pm_member_type_ref. The member kind is accounts.kind. ON DELETE CASCADE.']
  created_at timestamp [not null]
  deleted_at timestamp [null, note: 'Soft-delete timestamp. Re-adding a removed member restores the row rather than inserting a new one.']
  // project_role_id (LEGACY, nullable since 000066) is dropped by the contract step.

  indexes {
    (project_id, account_id) [unique, note: 'Partial unique: WHERE deleted_at IS NULL']
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
  field_type varchar [not null, note: 'text | number | date | select | multi_select | boolean | url | long_text']
  options jsonb [null, note: 'Ordered list of option labels for select / multi_select types']
  is_required boolean [not null, default: false]
  use_in_search boolean [not null, default: false]
  rows int [not null, default: 3, note: 'Visible lines of a long_text field (2-20); ignored by other types']
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
  uploaded_by uuid [ref: > accounts.id, note: 'Any account (a service account can upload).']
  created_at timestamp
  updated_at timestamp
}

Table task_attachments {
  id uuid [primary key]
  task_id uuid [not null, ref: > tasks.id]
  file_id uuid [not null, ref: > files.id]
  created_by uuid [ref: > accounts.id]
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

Table activities {
  id            uuid [primary key, note: 'Producer-assigned; comments are inserted directly and their stream copy conflicts on this id']
  project_id    uuid [not null, ref: > projects.id]
  entity_type   text [not null, note: 'task | doc | sprint | view | automation | environment | member']
  entity_id     uuid [null, note: 'No FK on purpose: the record of a deletion must outlive the deleted entity']
  actor_id      uuid [null, ref: > project_members.id, note: 'Resolved from the user/agent UUID by ActivityConsumer. NULL for system events or a removed member']
  origin        text [not null, default: 'system', note: 'user | agent | automation | jev | annotation | system']
  activity_type text [not null, note: 'Event topic, e.g. task.updated, doc.moved, sprint.completed, comment']
  content       jsonb [not null, default: '{}']
  created_at    timestamp
  updated_at    timestamp
  deleted_at    timestamp [null, note: 'Soft-delete for comments']
}

// --- PLUGINS ---
Table plugins {
  id uuid [primary key]
  name text [unique, not null, note: 'reverse-DNS id, e.g. "com.paca.checklist"']
  version text [not null, default: '0.0.0', note: 'semver, e.g. "1.0.0"']
  manifest jsonb [not null, default: '{}', note: 'Full plugin.json contents (routes, extension points, event subscriptions, etc.). customPermissions[].scope is project, global or both; nav items\' and extension points\' requiredPermission must be <domain>:<verb> (validated at install/update, no schema change).']
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
  recipient_account_id uuid [not null, ref: > accounts.id, note: 'The account that receives the notification']
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
  account_id uuid [not null, ref: > accounts.id, note: 'Was user_id. Any account kind: a user\'s personal key, a service account\'s key, an agent\'s MCP key (was agents.mcp_api_key_hash). ON DELETE CASCADE.']
  name text [not null]
  key_prefix text [not null, note: 'First 8 hex characters of the raw key, for display only. Empty for keys migrated from agents.mcp_api_key_hash.']
  key_hash text [not null, unique, note: 'SHA-256 hex digest of the raw key used for lookup/validation']
  last_used_at timestamp [null]
  expires_at timestamp [null]
  created_by uuid [null, ref: > accounts.id, note: 'Who minted it (the admin, for a service account). ON DELETE SET NULL.']
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
//                          actor_account_id below) and is attached to projects
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
// 000066 (see "Accounts model"): agent_scope is dropped because it only
// mirrored "project_id IS NULL"; global_role_id, name, avatar_*, created_by,
// created_at, updated_at, deleted_at and mcp_api_key_hash move to accounts /
// role_attachments / api_keys. The agent id equals its accounts.id.
// A hard delete of a project must also delete its project agents' accounts rows
// (agents.project_id cascades to the agents row only; deleting the accounts row
// cascades to agents).
Table agents {
  id uuid [primary key, ref: - accounts.id, note: 'Equals accounts.id. Composite FK (id, kind) -> accounts (id, kind) with kind = agent pins the type; ON DELETE CASCADE.']
  kind text [not null, default: 'agent', note: 'Constant (CHECK kind = agent); only for the composite FK.']
  project_id uuid [null, ref: > projects.id, note: 'NULL = global agent (workspace-level, invited into projects through project_members). Set = project agent. Only agents have a project, so it lives here, not on accounts.']
  handle varchar [not null, note: '@mention handle. Unique among live agents per project (project agent) or workspace-wide (global agent). Same shared constraint trigger as users.username.']
  agent_type varchar [not null, default: 'llm', note: '''llm | acp (000022).
    llm — runs an LLM reasoning loop in-cluster via llm_provider/llm_model/llm_api_key_secret.
    acp — delegates to a coding CLI (Claude Code, Codex, Gemini CLI, or custom) the user runs
          locally, connected over an authenticated bridge daemon; llm_* columns stay empty.''']
  llm_provider varchar [not null, note: 'LiteLLM provider prefix, e.g. anthropic, openai. Empty string for agent_type = acp.']
  llm_model varchar [not null, note: 'LiteLLM model name, e.g. claude-sonnet-4-6. Empty string for agent_type = acp.']
  llm_api_key_secret varchar [not null, note: 'Encrypted at rest; never returned by the API. Empty string for agent_type = acp.']
  llm_base_url varchar [null]
  acp_provider varchar [null, note: 'claude-code | codex | gemini-cli | goose | opencode | custom. Required when agent_type = acp (ck_agents_acp_requires_provider).']
  acp_command jsonb [not null, default: '[]', note: 'JSON array of the launch command + args. Only meaningful when acp_provider = custom.']
  acp_model text [not null, default: '', note: 'Optional model id the bridge requests after session/new. Empty = provider default.']
  acp_effort text [not null, default: '', note: 'Optional reasoning-effort value the bridge requests after session/new. Empty = provider default.']
  acp_bridge_token_hash varchar [null, note: 'SHA-256 hex digest of the current local-bridge auth token. The plaintext is shown once, never persisted.']
  system_prompt text [not null, default: '']
  max_iterations integer [not null, default: 50]
  timeout_minutes integer [not null, default: 30]
  git_committer_name varchar [not null, default: 'paca-agent']
  git_committer_email varchar [not null]
  indexes {
    (project_id) [note: 'Lookups by owning project']
  }
  // Uniqueness of handle among live agents (per project / workspace-wide) is enforced by the
  // shared constraint trigger, because liveness is accounts.deleted_at.
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

// project_id/member_id (a project chat) and actor_account_id (a global chat)
// are mutually exclusive — ck_agent_chat_sessions_actor (000031) requires
// exactly one side set: project_id+member_id both NOT NULL and actor_account_id
// NULL, or project_id+member_id both NULL and actor_account_id NOT NULL.
Table agent_chat_sessions {
  id uuid [primary key]
  agent_id uuid [not null, ref: > agents.id]
  project_id uuid [null, ref: > projects.id, note: 'NULL for a global chat session (see actor_account_id)']
  member_id uuid [null, ref: > project_members.id, note: 'The human member chatting with a project agent. NULL for a global chat session.']
  actor_account_id uuid [null, ref: > accounts.id, note: 'The account chatting with a global agent from the home/admin pages, identified directly (no project_members row need exist). NULL for a project chat session. ON DELETE RESTRICT.']
  title varchar [null]
  last_message_at timestamp [null]
  created_at timestamp
  updated_at timestamp
}

// triggered_by_member_id and actor_account_id are never both set
// (ck_agent_conversations_actor, 000031) — three distinguishable actor
// states: a project-scoped human member (triggered_by_member_id set), the
// automation-workflow engine (both NULL, unchanged since 000018), or a human
// chatting with a global agent (actor_account_id set, project_id NULL).
Table agent_conversations {
  id uuid [primary key, note: 'Also used as the ACP session/conversation identifier agent-runner passes to Goose']
  agent_id uuid [not null, ref: > agents.id]
  project_id uuid [null, ref: > projects.id, note: 'NULL for a global-chat conversation (project_id IS NULL + actor_account_id IS NOT NULL)']
  trigger_type varchar [not null, note: 'task_assigned | comment_mention | chat_message | description_write. Global chat reuses chat_message — project_id IS NULL is what distinguishes it.']
  task_id uuid [null, ref: > tasks.id]
  comment_id uuid [null]
  chat_session_id uuid [null, ref: > agent_chat_sessions.id]
  triggered_by_member_id uuid [null, ref: > project_members.id, note: 'NULL for the automation engine or a global-chat conversation (see actor_account_id)']
  actor_account_id uuid [null, ref: > accounts.id, note: 'Set only for a global-chat conversation. ON DELETE RESTRICT.']
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
