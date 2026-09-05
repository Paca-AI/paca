-- 000051_add_conversation_permissions.sql
-- Splits conversation access out of agents.read/agents.write into its own
-- conversations.read/write permission set (see
-- authz.PermissionConversationsRead/Write and router.go's /conversations
-- and /agents/{agentId}/chat-sessions* route blocks). agents.read/write now
-- govern only the agent entity's own configuration (MCP servers, skills,
-- env vars, etc); conversations.read/write govern viewing, starting, and
-- driving a conversation with an already-configured agent.
--
-- Grant the new permissions to existing project_roles rows for the
-- built-in role names, mirroring 000044_add_environment_permissions.sql's
-- own environments.read/write/connect backfill:
--   - PROJECT_OWNER / PROJECT_MANAGER / PROJECT_MEMBER / PROJECT_VIEWER are
--     the global role *templates* (project_id IS NULL) from
--     authz.DefaultProjectRoles() — kept in sync here defensively, though
--     bootstrap's seedDefaultProjectRoleTemplates already re-syncs them
--     on every startup.
--   - Admin / Editor / Viewer are the actual per-project roles seeded by
--     projectsvc.CreateProject for every project; that codepath has its own
--     hardcoded permission maps (not sourced from DefaultProjectRoles()) and
--     is never re-synced after project creation, so those rows need this
--     backfill too.
--
-- The JSONB `||` merge only adds/overwrites the listed keys, so any other
-- permission a project admin already customised on these rows is preserved.
-- Role renames after creation mean this name-based match isn't 100%
-- precise, but it's the same signal the application code itself uses to
-- identify these roles (see RoleName == "Admin" in projectsvc.CreateProject).

BEGIN;

UPDATE project_roles
SET permissions = permissions || '{"conversations.*": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_OWNER', 'PROJECT_MANAGER', 'Admin');

UPDATE project_roles
SET permissions = permissions || '{"conversations.read": true, "conversations.write": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_MEMBER', 'Editor');

UPDATE project_roles
SET permissions = permissions || '{"conversations.read": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_VIEWER', 'Viewer');

COMMIT;
