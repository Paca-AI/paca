-- 000056_set_admin_role_wildcard_permission.sql
-- The per-project "Admin" role (seeded by projectsvc.CreateProject for every
-- project) is meant to always have every project permission, present and
-- future — enumerating each domain's *All wildcard by hand meant every new
-- permission namespace (most recently project.settings.*/views.* in
-- 000054) needed its own backfill migration just to keep Admin whole, which
-- is exactly the bug this migration fixes: existing Admin rows only had
-- project.settings.* because 000054 explicitly listed it, and any future
-- addition would repeat the same gap until backfilled. Replacing the whole
-- permissions map with the bare "*" (authz.PermissionAll) — already how
-- SUPER_ADMIN and the legacy "ADMIN" users.role claim work — means Admin
-- needs no further backfill migrations ever again; hasPermission checks
-- granted["*"] before anything else (internal/platform/authz/authorizer.go).
--
-- This is a full replace, not a `||` merge like 000044/000054/000055 used —
-- "*" already implies every key an Admin row could otherwise hold, so
-- keeping the old enumerated keys alongside it would only be dead data.
-- Only rows actually named "Admin" are touched; Editor/Viewer and the
-- PROJECT_OWNER/PROJECT_MANAGER/PROJECT_MEMBER/PROJECT_VIEWER global
-- templates (project_id IS NULL) are unaffected.

BEGIN;

UPDATE project_roles
SET permissions = '{"*": true}'::jsonb,
    updated_at = NOW()
WHERE role_name = 'Admin' AND project_id IS NOT NULL;

COMMIT;
