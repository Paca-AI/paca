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
-- tighten PROJECT_MEMBER/Editor's new views grant per-project via the role
-- editor, which is the actual point of splitting these out.
--
-- PROJECT_MEMBER/Editor and PROJECT_VIEWER/Viewer get no project.settings.*
-- grant at all, read or write: redefining task types/statuses/custom fields
-- is an Admin-level (project schema) action (write), and there is no
-- dedicated read permission for the schema — viewing it is implied by
-- tasks.read, which every role here already holds (see authz.
-- PermissionProjectSettingsTaskTypesWrite's doc comment). Both blocks below
-- exist only for their views.* grant now. (This migration originally also
-- granted project.settings.*.{read,write} here; corrected in place rather
-- than via a follow-up migration since — being a purely additive `||` merge
-- — the only databases where that history matters are ones that haven't
-- applied this version yet. An already-migrated database's existing Editor/
-- Viewer rows keep whatever this migration granted them at the time; narrow
-- those separately if needed.)

BEGIN;

UPDATE project_roles
SET permissions = permissions || '{"project.settings.*": true, "views.*": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_OWNER', 'PROJECT_MANAGER', 'Admin');

UPDATE project_roles
SET permissions = permissions || '{"views.read": true, "views.write": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_MEMBER', 'Editor');

UPDATE project_roles
SET permissions = permissions || '{"views.read": true}'::jsonb,
    updated_at = NOW()
WHERE role_name IN ('PROJECT_VIEWER', 'Viewer');

COMMIT;
