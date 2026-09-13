-- 000051_add_conversation_permissions.sql
-- Splits conversation access out of agents.read/agents.write into its own
-- conversations.read/write permission set (see
-- authz.PermissionConversationsRead/Write and router.go's /conversations
-- and /agents/{agentId}/chat-sessions* route blocks). agents.read/write now
-- govern only the agent entity's own configuration (MCP servers, skills,
-- env vars, etc); conversations.read/write govern viewing, starting, and
-- driving a conversation with an already-configured agent.
--
-- Backfilled by what each row's permissions actually already grant today,
-- not by role_name: a name-based match (as 000044_add_environment_permissions.sql
-- used for its own environments.read/write/connect backfill) only reaches
-- the built-in role names, silently skipping any custom project role an
-- admin created via the Roles UI that happens to grant agents.read/write —
-- those are exactly as entitled to a conversations.*/read/write backfill as
-- PROJECT_MANAGER or Editor, and there's no name pattern to match them by.
-- Deriving the grant from the row's own existing permissions instead covers
-- every role uniformly, built-in or custom, and needs no knowledge of any
-- specific role name:
--   - agents.* implies full agent access under the old scheme, so it
--     becomes conversations.* (covers PROJECT_OWNER/PROJECT_MANAGER/Admin
--     and any custom role with full agent access).
--   - agents.read becomes conversations.read (viewing).
--   - agents.write becomes conversations.write (starting/driving).
-- A role with both agents.read and agents.write picks up both new keys,
-- matching Editor's old read+write pair exactly.
--
-- The JSONB `||` merge only adds the listed key, so any other permission a
-- project admin already customised on these rows is preserved; the `@>`
-- containment guard on each UPDATE makes it idempotent (skips rows that
-- already carry the target key, so a rerun is a no-op) and, unlike `->`,
-- never evaluates to SQL NULL for an absent key — see
-- 000052_dedupe_project_role_permissions.sql's own doc comment for why that
-- distinction matters here.
--
-- PROJECT_OWNER / PROJECT_MANAGER / PROJECT_MEMBER / PROJECT_VIEWER are the
-- global role *templates* (project_id IS NULL) from authz.DefaultProjectRoles()
-- — kept in sync here defensively, though bootstrap's
-- seedDefaultProjectRoleTemplates already re-syncs them on every startup.
-- Admin / Editor / Viewer are the actual per-project roles seeded by
-- projectsvc.CreateProject for every project; that codepath has its own
-- hardcoded permission maps (not sourced from DefaultProjectRoles()) and is
-- never re-synced after project creation, so those rows need this backfill
-- too — and so does any custom role, which this permission-driven approach
-- now also covers.

BEGIN;

UPDATE project_roles
SET permissions = permissions || '{"conversations.*": true}'::jsonb,
    updated_at = NOW()
WHERE permissions @> '{"agents.*": true}'::jsonb
  AND NOT permissions @> '{"conversations.*": true}'::jsonb;

UPDATE project_roles
SET permissions = permissions || '{"conversations.read": true}'::jsonb,
    updated_at = NOW()
WHERE permissions @> '{"agents.read": true}'::jsonb
  AND NOT permissions @> '{"conversations.read": true}'::jsonb;

UPDATE project_roles
SET permissions = permissions || '{"conversations.write": true}'::jsonb,
    updated_at = NOW()
WHERE permissions @> '{"agents.write": true}'::jsonb
  AND NOT permissions @> '{"conversations.write": true}'::jsonb;

COMMIT;
