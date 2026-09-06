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

-- A role granted the bare global wildcard "*" needs nothing else stored
-- alongside it (authz.hasPermission short-circuits on "*" before looking at
-- any other key) — collapse straight to {"*": true}, mirroring the
-- frontend's dedupeGrantedPermissions (apps/web/src/lib/permissions.ts).
-- Done as its own pass, before the domain-wildcard dedup below, so that
-- dedup only ever has to reason about domain wildcards (e.g.
-- "environments.*"), never the "*" superset case.
UPDATE project_roles
SET permissions = '{"*": true}'::jsonb,
    updated_at = NOW()
WHERE permissions @> '{"*": true}'::jsonb
  AND permissions IS DISTINCT FROM '{"*": true}'::jsonb;

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
        --
        -- COALESCE(..., 'false'::jsonb) matters: `jsonb -> missing_key`
        -- returns SQL NULL, not jsonb false, when that domain has no
        -- wildcard key at all in this row (the common case — most keys
        -- have no wildcard sibling present). Without the COALESCE, that
        -- NULL propagates through the surrounding AND/NOT into a NULL
        -- WHERE-clause result, which Postgres treats the same as FALSE —
        -- i.e. the row is silently dropped from jsonb_object_agg below,
        -- permanently deleting a perfectly legitimate, non-redundant
        -- permission any time the same role also happens to carry any
        -- other domain-wildcard key at all (verified against
        -- PROJECT_MANAGER's own shape: "projects.read"/"projects.write"/
        -- "project.members.read"/"project.members.write" have no
        -- "projects.*"/"project.members.*" sibling, but PROJECT_MANAGER
        -- also carries "tasks.*" etc., which was enough to trigger this and
        -- wipe out those four keys). COALESCE forces the missing-sibling
        -- case to a real `false`, so the key is correctly recognised as
        -- "not redundant" and kept.
        kv.key NOT LIKE '%.*'
        AND kv.value = 'true'::jsonb
        AND COALESCE(
            pr2.permissions -> (substring(kv.key from '^(.*)\.[^.]+$') || '.*'),
            'false'::jsonb
        ) = 'true'::jsonb
    )
    GROUP BY pr2.id
) AS sub
WHERE pr.id = sub.id
  AND sub.new_permissions IS DISTINCT FROM pr.permissions;

COMMIT;
