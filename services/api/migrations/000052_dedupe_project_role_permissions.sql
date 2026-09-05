-- 000052_dedupe_project_role_permissions.sql
-- Cleans up project_roles rows that carry both a domain wildcard (e.g.
-- "environments.*": true) and one or more of the individual permissions it
-- already covers (e.g. "environments.read": true) — harmless for
-- authorization (authz.hasPermission already treats the wildcard as
-- granting the individual key regardless), but confusing wherever a role's
-- raw permission map is rendered as-is, such as the project Roles page's
-- badge list (RolesSettings.tsx's activePermissions), which shows every
-- true key verbatim and so displayed both "environments.*" and
-- "environments.read" side by side for an affected role.
--
-- This is data debt from 000044_add_environment_permissions.sql (and
-- structurally could recur for any future permission split the same way):
-- that migration's `permissions || '{"environments.*": true}'` merge only
-- ever adds the new wildcard key — it never removes whatever individual
-- environments.read/write/connect keys a role already had from before the
-- environments.*/read/write/connect split existed, so any project old
-- enough to have picked up those individual keys pre-split ended up with
-- both forever after. The editor UI itself never re-introduces this
-- (ProjectRoleFormDialog's normalizePermissionsToWildcards always collapses
-- to either the wildcard or the individual keys, never both), so this is a
-- one-time backfill, not an ongoing invariant this migration needs to keep
-- re-enforcing — the WHERE clause below only rewrites a row whose
-- deduplicated permissions actually differ from what's stored, so an
-- already-clean row is left untouched (and its updated_at unbumped) on
-- every rerun.
--
-- Scoped to project_roles generically (every row, built-in role names and
-- custom ones alike, both real per-project rows and the project_id IS NULL
-- global templates) rather than name-matched like 000044 — this is a
-- structural cleanup of whatever the data actually looks like, not a grant
-- of new permissions tied to specific role names.

BEGIN;

UPDATE project_roles pr
SET permissions = sub.new_permissions,
    updated_at = NOW()
FROM (
    SELECT pr2.id,
           jsonb_object_agg(kv.key, kv.value) AS new_permissions
    FROM project_roles pr2,
         jsonb_each(pr2.permissions) AS kv(key, value)
    WHERE NOT (
        -- kv.key is redundant when it's a plain (non-wildcard) key granted
        -- true, and its own domain wildcard is also granted true on the
        -- same row — e.g. key "environments.read" is dropped when
        -- "environments.*" is also true. A key's "domain" is everything
        -- before its last dot ("project.members" for
        -- "project.members.read"), matching the frontend's own keyPrefix()
        -- (apps/web/src/lib/permissions.ts) exactly.
        kv.key NOT LIKE '%.*'
        AND kv.value = 'true'::jsonb
        AND pr2.permissions -> (substring(kv.key from '^(.*)\.[^.]+$') || '.*') = 'true'::jsonb
    )
    GROUP BY pr2.id
) AS sub
WHERE pr.id = sub.id
  AND sub.new_permissions IS DISTINCT FROM pr.permissions;

COMMIT;
