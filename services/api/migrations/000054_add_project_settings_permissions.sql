-- 000054_add_project_settings_permissions.sql
-- Splits task-schema administration (task types, task statuses, custom
-- field definitions) out of tasks.write into a new project.settings.*
-- namespace (see authz.PermissionProjectSettings{TaskTypes,TaskStatuses,
-- CustomFields}), and splits view CRUD out of the borrowed sprints.write
-- into its own views.* permission (see authz.PermissionViews{Read,Write}).
-- Editing a task's own content — including moving it between existing
-- statuses — stays on tasks.write; redefining what statuses/types/custom
-- fields exist for the project is now a separate, independently grantable
-- capability.
--
-- Backfills the existing built-in role rows the same way
-- 000044_add_environment_permissions.sql backfilled environments.*:
--   - PROJECT_OWNER / PROJECT_MANAGER / PROJECT_MEMBER / PROJECT_VIEWER are
--     the global role *templates* (project_id IS NULL) from
--     authz.DefaultProjectRoles() — kept in sync here defensively, though
--     bootstrap's seedDefaultProjectRoleTemplates already re-syncs them on
--     every startup.
--   - Admin / Editor / Viewer are the actual per-project roles seeded by
--     projectsvc.CreateProject for every project; that codepath is never
--     re-synced after project creation, so those rows need this backfill
--     too.
--
-- The JSONB `||` merge only adds/overwrites the listed keys, so any other
-- permission a project admin already customised on these rows is preserved
-- — nobody's existing effective access shrinks. A project owner can now
-- tighten PROJECT_MEMBER/Editor's new settings/views grants per-project via
-- the role editor, which is the actual point of splitting these out.

BEGIN;

UPDATE project_roles
SET permissions = permissions || '{"project.settings.*": true, "views.*": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_OWNER', 'PROJECT_MANAGER', 'Admin');

UPDATE project_roles
SET permissions = permissions || '{
        "project.settings.task_types.read": true,
        "project.settings.task_types.write": true,
        "project.settings.task_statuses.read": true,
        "project.settings.task_statuses.write": true,
        "project.settings.custom_fields.read": true,
        "project.settings.custom_fields.write": true,
        "views.read": true,
        "views.write": true
    }'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_MEMBER', 'Editor');

UPDATE project_roles
SET permissions = permissions || '{
        "project.settings.task_types.read": true,
        "project.settings.task_statuses.read": true,
        "project.settings.custom_fields.read": true,
        "views.read": true
    }'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_VIEWER', 'Viewer');

COMMIT;
