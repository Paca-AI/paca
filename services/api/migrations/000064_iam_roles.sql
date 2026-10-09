-- 000064_iam_roles.sql
-- IAM-style roles: one `roles` table holding JSON policies and a
-- `role_attachments` table binding roles to users/agents, platform-wide or
-- scoped to one project. See docs/architecture/authorization.md.
--
-- This migration converts the legacy model into the new one, in one
-- transaction, and verifies the result before committing:
--
--   global_roles            -> roles (platform; legacy_kind 'global')
--   users.role_id           -> platform-wide user attachments
--   agents.global_role_id   -> platform-wide agent attachments
--   project_roles           -> roles (legacy_kind 'project'; project_id = owner
--                              project, NULL for templates)
--   project_members         -> attachments scoped to the member's project
--   restricted agents/envs  -> NOT converted (see below)
--
-- The legacy tables and columns are NOT dropped here (a later migration
-- does that), so the legacy resolver keeps working side by side until then.
--
-- Mapping rules that are not obvious:
--
-- * A legacy key becomes an action by replacing its LAST "." with ":"
--   ("tasks.write" -> "tasks:write"); the domains global_roles and
--   project.roles both become "roles" ("project.roles.write" -> "roles:write").
--   This is exactly iam.LegacyKeyToAction (a Go test asserts they agree).
--   A legacy wildcard "X.*" matched every key starting with "X."; when that
--   prefix spans several IAM domains (e.g. "project.settings.*" covers
--   "project.settings.task_types:write"), the covered built-in actions are
--   listed explicitly, and the wildcard action itself is dropped if it names
--   no real domain.
-- * GHSA-hjcj-373w-vq8m: a global role reached into a project only through
--   "*". So a global role's "*" becomes "*" on "*", while every other key
--   applies only to the platform roots (never "project/*").
-- * Agents never used their global role inside a project (the legacy
--   resolver consulted only the agent's project membership there). A global
--   agent whose global role holds "*" is therefore attached to a derived
--   role ("<name> (agents)", legacy_kind 'global_agent') granting "*" on the
--   platform roots only.
-- * Membership implied projects.read, so every migrated project role allows
--   projects:read on its project.
-- * Restricted agents/environments (agents/environments.access_mode =
--   'restricted' and their *_access_grants) are NOT converted. They become
--   open to everyone the migrated roles allow; the migration emits a NOTICE
--   with the counts. Admins recreate restrictions with ordinary Deny roles
--   (docs/guides/roles-and-policies.md). The legacy gate covered
--   conversations.read/write (agents) and environments.read/write/connect
--   (environments), so a Deny on those actions is the equivalent.
--   Editing this file in place after a dev DB was migrated requires a rebuild
--   (or deleting the generated 'Restrict ...' / 'Access to ...' roles).

BEGIN;

-- -------------------------------------------------------------------------
-- Schema
-- -------------------------------------------------------------------------

CREATE TABLE roles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    policy      JSONB       NOT NULL,
    -- Owner project; NULL = platform role (global role or project template).
    project_id  UUID        REFERENCES projects(id) ON DELETE CASCADE,
    is_system   BOOLEAN     NOT NULL DEFAULT FALSE,
    is_default  BOOLEAN     NOT NULL DEFAULT FALSE,
    -- 'global' | 'global_agent' | 'project'; source
    -- row id. Used by the self-check below; dropped with the legacy tables.
    legacy_kind TEXT,
    legacy_id   UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- At most one default role (auto-attached to new users / global agents).
CREATE UNIQUE INDEX uq_roles_single_default ON roles (is_default) WHERE is_default;
CREATE UNIQUE INDEX uq_roles_platform_name ON roles (name) WHERE project_id IS NULL;
CREATE UNIQUE INDEX uq_roles_project_name  ON roles (project_id, name) WHERE project_id IS NOT NULL;
CREATE INDEX idx_roles_legacy ON roles (legacy_kind, legacy_id);

CREATE TABLE role_attachments (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id        UUID        NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    principal_type TEXT        NOT NULL CHECK (principal_type IN ('user', 'agent')),
    principal_id   UUID        NOT NULL,
    -- NULL = platform-wide; otherwise the role only applies inside this project.
    project_id     UUID        REFERENCES projects(id) ON DELETE CASCADE,
    created_by     UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Two partial indexes instead of NULLS NOT DISTINCT (portable to older PG).
CREATE UNIQUE INDEX uq_role_attachments_platform
    ON role_attachments (role_id, principal_type, principal_id) WHERE project_id IS NULL;
CREATE UNIQUE INDEX uq_role_attachments_project
    ON role_attachments (role_id, principal_type, principal_id, project_id) WHERE project_id IS NOT NULL;
CREATE INDEX idx_role_attachments_principal ON role_attachments (principal_type, principal_id);
CREATE INDEX idx_role_attachments_project   ON role_attachments (project_id);

-- -------------------------------------------------------------------------
-- Helpers (session-temporary; dropped at the end).
-- The block between the >>> / <<< markers is also loaded by
-- test/integration/iam_migration_test.go to compare the key mapping with
-- iam.LegacyKeyToAction — keep the markers.
-- -------------------------------------------------------------------------

-- >>> iam-migration-helpers

-- Every built-in legacy permission key (internal/platform/authz/permissions.go,
-- wildcards excluded). Used to expand legacy wildcards and as the probe set
-- of the self-check.
CREATE FUNCTION pg_temp.legacy_builtin_keys() RETURNS text[] LANGUAGE sql IMMUTABLE AS $$
    SELECT ARRAY[
        'users.read', 'users.write', 'users.delete',
        'global_roles.read', 'global_roles.write', 'global_roles.assign',
        'projects.read', 'projects.write', 'projects.create', 'projects.delete',
        'project.members.read', 'project.members.write',
        'project.roles.read', 'project.roles.write',
        'project.activities.read', 'project.export',
        'tasks.read', 'tasks.write',
        'project.settings.task_types.write', 'project.settings.task_statuses.write',
        'project.settings.custom_fields.write',
        'sprints.read', 'sprints.write', 'views.read', 'views.write', 'docs.read', 'docs.write',
        'agents.read', 'agents.write', 'conversations.read', 'conversations.write',
        'environments.read', 'environments.write', 'environments.connect',
        'workflows.read', 'workflows.write',
        'annotations.read', 'annotations.write', 'annotations.resolve',
        'settings.write', 'settings.sso.write', 'plugins.read', 'plugins.write'
    ]::text[]
$$;

-- Mirror of iam.LegacyKeyToAction.
CREATE FUNCTION pg_temp.legacy_key_to_action(k text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN k = '*' THEN k
        WHEN k ~ '^global_roles\.[^.]*$'  THEN 'roles:' || substr(k, length('global_roles.') + 1)
        WHEN k ~ '^project\.roles\.[^.]*$' THEN 'roles:' || substr(k, length('project.roles.') + 1)
        WHEN position('.' IN k) = 0 THEN k
        ELSE regexp_replace(k, '\.([^.]*)$', ':\1')
    END
$$;

-- Mirror of iam.MatchAction.
CREATE FUNCTION pg_temp.iam_action_match(pat text, act text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
    SELECT pat = '*'
        OR (right(pat, 2) = ':*' AND starts_with(act, left(pat, -1)))
        OR pat = act
$$;

-- Mirror of iam.MatchResource, compiled to an anchored regex so the
-- self-check can match in bulk: mid-path "*" = exactly one segment,
-- trailing "*" = zero or more remaining segments ("project/p1/*" also
-- covers "project/p1").
CREATE FUNCTION pg_temp.iam_resource_regex(pat text) RETURNS text LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    ps text[] := string_to_array(pat, '/');
    np int := coalesce(array_length(ps, 1), 0);
    re text := '';
BEGIN
    IF pat = '*' THEN
        RETURN '^.*$';
    END IF;
    FOR i IN 1..np LOOP
        IF ps[i] = '*' AND i = np THEN
            RETURN '^' || re || '(/.*)?$';
        END IF;
        re := re || CASE WHEN i > 1 THEN '/' ELSE '' END
                 || CASE WHEN ps[i] = '*' THEN '[^/]*'
                         ELSE regexp_replace(ps[i], '([.^$*+?()\[\]{}|\\-])', '\\\1', 'g') END;
    END LOOP;
    RETURN '^' || re || '$';
END
$$;

CREATE FUNCTION pg_temp.iam_resource_match(pat text, res text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
    SELECT res ~ pg_temp.iam_resource_regex(pat)
$$;

-- Legacy permission-map decoding: mirror of postgres.permissionsFromJSON
-- (object values true / non-zero number / "true" (any case), or an array of
-- strings; keys trimmed, empty keys ignored).
CREATE FUNCTION pg_temp.legacy_keys(perms jsonb) RETURNS SETOF text LANGUAGE sql IMMUTABLE AS $$
    SELECT DISTINCT key FROM (
        SELECT regexp_replace(e.k, '^\s+|\s+$', '', 'g') AS key
        FROM jsonb_each(CASE WHEN jsonb_typeof(perms) = 'object' THEN perms ELSE '{}'::jsonb END) AS e(k, v)
        WHERE (jsonb_typeof(e.v) = 'boolean' AND e.v = 'true'::jsonb)
           OR (jsonb_typeof(e.v) = 'number'  AND (e.v #>> '{}')::numeric <> 0)
           OR (jsonb_typeof(e.v) = 'string'  AND lower(e.v #>> '{}') = 'true')
        UNION ALL
        SELECT regexp_replace(x #>> '{}', '^\s+|\s+$', '', 'g')
        FROM jsonb_array_elements(CASE WHEN jsonb_typeof(perms) = 'array' THEN perms ELSE '[]'::jsonb END) AS a(x)
        WHERE jsonb_typeof(x) = 'string'
    ) s
    WHERE key <> ''
$$;

-- Mirror of authz.hasPermission: "*", exact key, or an "X.*" prefix match.
CREATE FUNCTION pg_temp.legacy_has(keys text[], req text) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
    SELECT '*' = ANY(keys)
        OR req = ANY(keys)
        OR EXISTS (SELECT 1 FROM unnest(keys) AS k WHERE right(k, 2) = '.*' AND starts_with(req, left(k, -1)))
$$;

-- Legacy permission map -> sorted, de-duplicated IAM action list.
CREATE FUNCTION pg_temp.legacy_actions(perms jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    WITH keys AS (
        SELECT k FROM pg_temp.legacy_keys(perms) AS k
    ), expanded AS (
        -- builtin keys covered by a legacy "X.*" wildcard, as actions
        SELECT k.k AS wildcard, pg_temp.legacy_key_to_action(b) AS act
        FROM keys k, unnest(pg_temp.legacy_builtin_keys()) AS b
        WHERE k.k <> '*' AND right(k.k, 2) = '.*' AND starts_with(b, left(k.k, -1))
    ), acts AS (
        -- every key's own mapping, except a wildcard whose action matches no
        -- built-in action while its legacy prefix did cover some (it names a
        -- prefix of real domains, not a domain: e.g. "project.settings:*")
        SELECT pg_temp.legacy_key_to_action(k.k) AS act
        FROM keys k
        WHERE NOT (
            EXISTS (SELECT 1 FROM expanded e WHERE e.wildcard = k.k)
            AND NOT EXISTS (
                SELECT 1 FROM unnest(pg_temp.legacy_builtin_keys()) AS b
                WHERE pg_temp.iam_action_match(pg_temp.legacy_key_to_action(k.k), pg_temp.legacy_key_to_action(b))
            )
        )
        UNION
        -- expanded actions the wildcard's own action does not already match
        SELECT e.act FROM expanded e
        WHERE NOT pg_temp.iam_action_match(pg_temp.legacy_key_to_action(e.wildcard), e.act)
    ), filtered AS (
        -- Filter out non-existent :read actions for project.settings
        -- These legacy permissions were checked via tasks:read in the old system
        SELECT act FROM acts
        WHERE act NOT IN (
            'project.settings.custom_fields:read',
            'project.settings.task_statuses:read',
            'project.settings.task_types:read'
        )
    )
    SELECT COALESCE(jsonb_agg(act ORDER BY act), '[]'::jsonb) FROM (SELECT DISTINCT act FROM filtered) d
$$;

-- <<< iam-migration-helpers

-- Platform resource roots a named (non-"*") global permission applies to
-- (GHSA-hjcj: never "project/*").
CREATE FUNCTION pg_temp.platform_roots() RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT '["user","user/*","role","role/*","plugin","plugin/*","settings","sso","agent","agent/*","project"]'::jsonb
$$;

CREATE FUNCTION pg_temp.stmt(sid text, effect text, actions jsonb, resources jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT jsonb_build_object('sid', sid, 'effect', effect, 'actions', actions, 'resources', resources)
$$;

CREATE FUNCTION pg_temp.policy(statements jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT jsonb_build_object('version', '2026-10-01', 'statements', statements)
$$;

-- Global role policy: "*" -> * on *; anything else -> its actions on the
-- platform roots.
CREATE FUNCTION pg_temp.global_policy(perms jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT pg_temp.policy(CASE
        WHEN acts @> '["*"]' THEN jsonb_build_array(pg_temp.stmt('Migrated', 'Allow', '["*"]', '["*"]'))
        WHEN jsonb_array_length(acts) = 0 THEN '[]'::jsonb
        ELSE jsonb_build_array(pg_temp.stmt('Migrated', 'Allow', acts, pg_temp.platform_roots()))
    END)
    FROM (SELECT pg_temp.legacy_actions(perms) AS acts) a
$$;

-- Project role policy. proj NULL = template (applies to project/*, only ever
-- reached through a project-scoped attachment). roles:* actions apply to the
-- project's role subtree; everything else to the whole project. projects:read
-- is always included (membership implied it).
CREATE FUNCTION pg_temp.project_policy(perms jsonb, proj uuid) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    WITH a AS (
        SELECT pg_temp.legacy_actions(perms) AS acts,
               CASE WHEN proj IS NULL THEN 'project/*' ELSE 'project/' || proj || '/*' END AS main_res,
               CASE WHEN proj IS NULL THEN 'project/*/role/*' ELSE 'project/' || proj || '/role/*' END AS role_res
    ), split AS (
        SELECT a.*,
               (SELECT COALESCE(jsonb_agg(x ORDER BY x), '[]'::jsonb) FROM (
                    SELECT DISTINCT x FROM (
                        SELECT jsonb_array_elements_text(a.acts) AS x
                        UNION ALL SELECT 'projects:read'
                    ) u WHERE x NOT LIKE 'roles:%') v) AS main_acts,
               (SELECT COALESCE(jsonb_agg(x ORDER BY x), '[]'::jsonb)
                  FROM jsonb_array_elements_text(a.acts) AS x WHERE x LIKE 'roles:%') AS role_acts
        FROM a
    )
    SELECT pg_temp.policy(CASE
        WHEN acts @> '["*"]' THEN jsonb_build_array(pg_temp.stmt('Migrated', 'Allow', '["*"]', jsonb_build_array(main_res)))
        WHEN jsonb_array_length(role_acts) = 0 THEN jsonb_build_array(pg_temp.stmt('Migrated', 'Allow', main_acts, jsonb_build_array(main_res)))
        ELSE jsonb_build_array(
            pg_temp.stmt('Migrated', 'Allow', main_acts, jsonb_build_array(main_res)),
            pg_temp.stmt('MigratedRoles', 'Allow', role_acts, jsonb_build_array(role_res)))
    END)
    FROM split
$$;

-- Legacy permissions the old resolver granted a principal at global scope
-- (ListGlobalPermissions / ListAgentGlobalPermissions).
CREATE FUNCTION pg_temp.legacy_global_keys(ptype text, pid uuid) RETURNS text[] LANGUAGE sql STABLE AS $$
    SELECT COALESCE(array_agg(DISTINCT k), '{}') FROM (
        SELECT lk.k FROM users u
        JOIN global_roles gr ON gr.id = u.role_id
        CROSS JOIN LATERAL pg_temp.legacy_keys(gr.permissions) AS lk(k)
        WHERE ptype = 'user' AND u.id = pid AND u.deleted_at IS NULL
        UNION ALL
        SELECT lk.k FROM agents a
        JOIN global_roles gr ON gr.id = a.global_role_id
        CROSS JOIN LATERAL pg_temp.legacy_keys(gr.permissions) AS lk(k)
        WHERE ptype = 'agent' AND a.id = pid AND a.agent_scope = 'global' AND a.deleted_at IS NULL
    ) s
$$;

-- Legacy permissions at project scope: a user's global role only via "*"
-- (GHSA-hjcj), an agent's global role never; plus the project role of the
-- active membership; plus projects.read for any active membership.
CREATE FUNCTION pg_temp.legacy_project_keys(ptype text, pid uuid, proj uuid) RETURNS text[] LANGUAGE sql STABLE AS $$
    SELECT COALESCE(array_agg(DISTINCT k), '{}') FROM (
        SELECT k FROM unnest(pg_temp.legacy_global_keys(ptype, pid)) AS k
        WHERE ptype = 'user' AND k = '*'
        UNION ALL
        SELECT lk.k FROM project_members pm
        JOIN project_roles pr ON pr.id = pm.project_role_id
        CROSS JOIN LATERAL pg_temp.legacy_keys(pr.permissions) AS lk(k)
        WHERE pm.project_id = proj AND pm.deleted_at IS NULL
          AND ((ptype = 'user' AND pm.user_id = pid) OR (ptype = 'agent' AND pm.agent_id = pid))
        UNION ALL
        SELECT 'projects.read' FROM project_members pm
        WHERE pm.project_id = proj AND pm.deleted_at IS NULL
          AND ((ptype = 'user' AND pm.user_id = pid) OR (ptype = 'agent' AND pm.agent_id = pid))
    ) s
$$;

-- -------------------------------------------------------------------------
-- Working sets
-- -------------------------------------------------------------------------

-- Active members whose principal (user or agent) is not soft-deleted.
CREATE TEMP TABLE iam_members ON COMMIT DROP AS
SELECT pm.id AS member_id, pm.project_id, pm.project_role_id,
       CASE WHEN pm.member_type = 'agent' THEN 'agent' ELSE 'user' END AS ptype,
       CASE WHEN pm.member_type = 'agent' THEN pm.agent_id ELSE pm.user_id END AS pid
FROM project_members pm
LEFT JOIN users  u ON u.id = pm.user_id
LEFT JOIN agents a ON a.id = pm.agent_id
WHERE pm.deleted_at IS NULL
  AND ((pm.member_type = 'agent' AND a.id IS NOT NULL AND a.deleted_at IS NULL)
    OR (pm.member_type <> 'agent' AND u.id IS NOT NULL AND u.deleted_at IS NULL));

-- -------------------------------------------------------------------------
-- 1. Global roles -> platform roles, attached to users and global agents
-- -------------------------------------------------------------------------

INSERT INTO roles (name, policy, is_system, is_default, legacy_kind, legacy_id, created_at, updated_at)
SELECT gr.name, pg_temp.global_policy(gr.permissions), gr.name = 'SUPER_ADMIN', gr.is_default,
       'global', gr.id, gr.created_at, gr.updated_at
FROM global_roles gr;

-- Derived agent-only copy of each "*" global role held by a global agent.
INSERT INTO roles (name, description, policy, legacy_kind, legacy_id, created_at, updated_at)
SELECT gr.name || ' (agents)'
         || CASE WHEN EXISTS (SELECT 1 FROM roles x WHERE x.project_id IS NULL AND x.name = gr.name || ' (agents)')
                 THEN ' (' || left(gr.id::text, 8) || ')' ELSE '' END,
       'Migrated from global role ' || gr.name || ' for agents: agents never used a global role inside a project.',
       pg_temp.policy(jsonb_build_array(pg_temp.stmt('Migrated', 'Allow', '["*"]', pg_temp.platform_roots()))),
       'global_agent', gr.id, gr.created_at, gr.updated_at
FROM global_roles gr
WHERE pg_temp.legacy_actions(gr.permissions) @> '["*"]'
  AND EXISTS (SELECT 1 FROM agents a
              WHERE a.global_role_id = gr.id AND a.agent_scope = 'global' AND a.deleted_at IS NULL);

INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id)
SELECT r.id, 'user', u.id, NULL
FROM users u
JOIN roles r ON r.legacy_kind = 'global' AND r.legacy_id = u.role_id
WHERE u.deleted_at IS NULL;

INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id)
SELECT COALESCE(ga.id, g.id), 'agent', a.id, NULL
FROM agents a
JOIN roles g ON g.legacy_kind = 'global' AND g.legacy_id = a.global_role_id
LEFT JOIN roles ga ON ga.legacy_kind = 'global_agent' AND ga.legacy_id = a.global_role_id
WHERE a.agent_scope = 'global' AND a.deleted_at IS NULL;

-- -------------------------------------------------------------------------
-- 2. Project roles -> roles, attached to members scoped to their project
-- -------------------------------------------------------------------------

INSERT INTO roles (name, policy, project_id, is_system, legacy_kind, legacy_id, created_at, updated_at)
SELECT pr.role_name
         || CASE WHEN pr.project_id IS NULL
                      AND EXISTS (SELECT 1 FROM roles x WHERE x.project_id IS NULL AND x.name = pr.role_name)
                 THEN ' (project template)' ELSE '' END,
       pg_temp.project_policy(pr.permissions, pr.project_id),
       pr.project_id,
       pr.project_id IS NOT NULL AND pr.role_name = 'Admin' AND pg_temp.legacy_actions(pr.permissions) = '["*"]'::jsonb,
       'project', pr.id, pr.created_at, pr.updated_at
FROM project_roles pr;

-- >>> project-attachments
INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id)
SELECT r.id, m.ptype, m.pid, m.project_id
FROM iam_members m
JOIN roles r ON r.legacy_kind = 'project' AND r.legacy_id = m.project_role_id;
-- <<< project-attachments

-- -------------------------------------------------------------------------
-- 3. Restricted agents / environments: NOT converted
-- -------------------------------------------------------------------------

-- Legacy access_mode = 'restricted' agents/environments and their grant
-- tables are deliberately not converted into roles. They become open to every
-- principal the roles above allow; an admin recreates restrictions with
-- ordinary Deny roles (docs/guides/roles-and-policies.md). The legacy
-- columns/tables stay untouched. Report what was left behind.
DO $$
DECLARE
    n_agents int;
    n_envs   int;
BEGIN
    SELECT count(*) INTO n_agents FROM agents
     WHERE access_mode = 'restricted' AND deleted_at IS NULL;
    SELECT count(*) INTO n_envs FROM environments
     WHERE access_mode = 'restricted' AND deleted_at IS NULL;
    RAISE NOTICE 'IAM migration: % restricted agent(s) and % restricted environment(s) were NOT converted; they are now open to everyone allowed to see their project until an admin adds Deny roles (see docs/guides/roles-and-policies.md)',
        n_agents, n_envs;
END
$$;

-- -------------------------------------------------------------------------
-- 4. Self-check: re-derive every principal's access with the legacy rules
--    and with a SQL re-evaluation of the new policies, and abort on any
--    difference. The Go golden test (test/integration/iam_migration_test.go,
--    using the real evaluator) is the authoritative proof; this guards the
--    broad grants on real data at upgrade time.
--
--    Probes, for every non-deleted user and agent:
--      a. every built-in key at platform scope (user/<0> or role/<0>);
--      b. every built-in key in every project the principal is a member of,
--         plus a project nobody belongs to (project/<0>), which shows what
--         platform-wide grants reach in an arbitrary project;
--    project.roles.* keys are probed only in projects and global_roles.*
--    only at platform scope, as in the legacy route table (both map to
--    roles:*). roles:* keys are probed on project/<P>/role/<0>.
--
-- -------------------------------------------------------------------------

-- Mirror of the evaluator's condition handling (kept for any conditioned
-- statement; this migration itself writes none).
-- Returns NULL for anything else, which a Deny treats as matching and an
-- Allow as not matching (fail closed, as iam.Evaluate does).
CREATE FUNCTION pg_temp.iam_cond_holds(conds jsonb, principal_id text) RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    op text; kv jsonb; k text; v jsonb; vals text[]; hit boolean;
BEGIN
    IF conds IS NULL OR conds = 'null'::jsonb OR conds = '{}'::jsonb THEN
        RETURN true;
    END IF;
    FOR op, kv IN SELECT * FROM jsonb_each(conds) LOOP
        IF op NOT IN ('In', 'NotIn', 'StringEquals', 'StringNotEquals') THEN
            RETURN NULL;
        END IF;
        FOR k IN SELECT jsonb_object_keys(kv) LOOP
            IF k <> 'principal.id' THEN
                RETURN NULL;
            END IF;
        END LOOP;
    END LOOP;
    FOR op, kv IN SELECT * FROM jsonb_each(conds) LOOP
        FOR k, v IN SELECT * FROM jsonb_each(kv) LOOP
            vals := CASE jsonb_typeof(v)
                        WHEN 'array'  THEN ARRAY(SELECT jsonb_array_elements_text(v))
                        WHEN 'string' THEN ARRAY[v #>> '{}']
                    END;
            IF vals IS NULL THEN
                RETURN NULL;
            END IF;
            hit := principal_id = ANY(vals);
            IF op IN ('In', 'StringEquals') AND NOT hit THEN RETURN false; END IF;
            IF op IN ('NotIn', 'StringNotEquals') AND hit THEN RETURN false; END IF;
        END LOOP;
    END LOOP;
    RETURN true;
END
$$;

CREATE TEMP TABLE iam_principals ON COMMIT DROP AS
SELECT 'user'::text AS ptype, id AS pid FROM users WHERE deleted_at IS NULL
UNION ALL
SELECT 'agent', id FROM agents WHERE deleted_at IS NULL;

-- Every migrated statement, flattened per attachment, with its condition
-- already evaluated for the attached principal and its resources compiled.
CREATE TEMP TABLE iam_stmts ON COMMIT DROP AS
SELECT ra.principal_type AS ptype, ra.principal_id AS pid,
       CASE WHEN ra.project_id IS NOT NULL THEN 'project/' || ra.project_id END AS att_project,
       st->>'effect' AS eff,
       pg_temp.iam_cond_holds(st->'conditions', ra.principal_id::text) AS holds,
       ARRAY(SELECT jsonb_array_elements_text(st->'actions')) AS actions,
       ARRAY(SELECT pg_temp.iam_resource_regex(x) FROM jsonb_array_elements_text(st->'resources') AS x) AS res_regexes
FROM role_attachments ra
JOIN roles r ON r.id = ra.role_id
CROSS JOIN LATERAL jsonb_array_elements(r.policy->'statements') AS st;
CREATE INDEX ON iam_stmts (ptype, pid);
ANALYZE iam_stmts;

-- Mirror of iam.Evaluate: a project-scoped attachment only applies inside
-- its project; default deny; any matching Deny wins.
CREATE FUNCTION pg_temp.iam_eval(p_type text, p_id uuid, p_act text, p_res text) RETURNS boolean LANGUAGE sql STABLE AS $$
    SELECT COALESCE(bool_or(s.eff = 'Allow' AND s.holds IS TRUE), false)
       AND NOT COALESCE(bool_or(s.eff = 'Deny' AND s.holds IS NOT FALSE), false)
    FROM iam_stmts s
    WHERE s.ptype = p_type AND s.pid = p_id
      AND (s.att_project IS NULL OR p_res = s.att_project OR starts_with(p_res, s.att_project || '/'))
      AND EXISTS (SELECT 1 FROM unnest(s.actions) AS a WHERE pg_temp.iam_action_match(a, p_act))
      AND EXISTS (SELECT 1 FROM unnest(s.res_regexes) AS x WHERE p_res ~ x)
$$;

-- Legacy permission sets, computed once per principal / (principal, project).
CREATE TEMP TABLE iam_legacy_global ON COMMIT DROP AS
SELECT p.ptype, p.pid, pg_temp.legacy_global_keys(p.ptype, p.pid) AS keys
FROM iam_principals p;

CREATE TEMP TABLE iam_project_pairs ON COMMIT DROP AS
SELECT DISTINCT ptype, pid, project_id FROM iam_members
UNION
SELECT ptype, pid, '00000000-0000-0000-0000-000000000000'::uuid FROM iam_principals;

CREATE TEMP TABLE iam_legacy_project ON COMMIT DROP AS
SELECT pp.ptype, pp.pid, pp.project_id, pg_temp.legacy_project_keys(pp.ptype, pp.pid, pp.project_id) AS keys
FROM iam_project_pairs pp;
CREATE INDEX ON iam_legacy_project (ptype, pid, project_id);

CREATE TEMP TABLE iam_probe_keys ON COMMIT DROP AS
SELECT k AS legacy_key,
       pg_temp.legacy_key_to_action(k) AS action,
       k NOT LIKE 'project.roles.%' AS at_platform,
       k NOT LIKE 'global_roles.%'  AS in_project,
       pg_temp.legacy_key_to_action(k) LIKE 'roles:%' AS is_roles
FROM unnest(pg_temp.legacy_builtin_keys()) AS k;

CREATE TEMP TABLE iam_selfcheck (
    probe text, ptype text, pid uuid, legacy_key text, action text, resource text,
    legacy_allowed boolean, iam_allowed boolean
) ON COMMIT DROP;

-- a. platform scope
INSERT INTO iam_selfcheck (probe, ptype, pid, legacy_key, action, resource, legacy_allowed)
SELECT 'platform', g.ptype, g.pid, k.legacy_key, k.action,
       CASE WHEN k.is_roles THEN 'role/00000000-0000-0000-0000-000000000000'
            ELSE 'user/00000000-0000-0000-0000-000000000000' END,
       pg_temp.legacy_has(g.keys, k.legacy_key)
FROM iam_legacy_global g
CROSS JOIN iam_probe_keys k
WHERE k.at_platform;

-- b. project scope (member projects + one project nobody belongs to)
INSERT INTO iam_selfcheck (probe, ptype, pid, legacy_key, action, resource, legacy_allowed)
SELECT 'project', lp.ptype, lp.pid, k.legacy_key, k.action,
       'project/' || lp.project_id
         || CASE WHEN k.is_roles THEN '/role/00000000-0000-0000-0000-000000000000' ELSE '' END,
       pg_temp.legacy_has(lp.keys, k.legacy_key)
FROM iam_legacy_project lp
CROSS JOIN iam_probe_keys k
WHERE k.in_project
  AND (lp.project_id = '00000000-0000-0000-0000-000000000000'
       OR EXISTS (SELECT 1 FROM iam_members m
                  WHERE m.ptype = lp.ptype AND m.pid = lp.pid AND m.project_id = lp.project_id));

UPDATE iam_selfcheck SET iam_allowed = pg_temp.iam_eval(ptype, pid, action, resource);

DO $$
DECLARE
    probes int;
    bad int;
    sample text;
BEGIN
    SELECT count(*), count(*) FILTER (WHERE legacy_allowed IS DISTINCT FROM iam_allowed)
      INTO probes, bad
      FROM iam_selfcheck;
    IF bad > 0 THEN
        SELECT string_agg(format('%s %s:%s %s on %s legacy=%s iam=%s',
                                 probe, ptype, pid, legacy_key, resource, legacy_allowed, iam_allowed), E'\n')
          INTO sample
          FROM (SELECT * FROM iam_selfcheck WHERE legacy_allowed IS DISTINCT FROM iam_allowed
                ORDER BY probe, ptype, pid, legacy_key LIMIT 20) s;
        RAISE EXCEPTION 'IAM migration self-check failed: % of % probes differ from the legacy model', bad, probes
            USING DETAIL = sample;
    END IF;
    RAISE NOTICE 'IAM migration self-check passed (% probes)', probes;
END
$$;

-- -------------------------------------------------------------------------
-- Cleanup of the session-temporary helpers (temp tables drop on commit).
-- -------------------------------------------------------------------------

DROP FUNCTION pg_temp.iam_eval(text, uuid, text, text);
DROP FUNCTION pg_temp.iam_cond_holds(jsonb, text);
DROP FUNCTION pg_temp.legacy_project_keys(text, uuid, uuid);
DROP FUNCTION pg_temp.legacy_global_keys(text, uuid);
DROP FUNCTION pg_temp.project_policy(jsonb, uuid);
DROP FUNCTION pg_temp.global_policy(jsonb);
DROP FUNCTION pg_temp.policy(jsonb);
DROP FUNCTION pg_temp.stmt(text, text, jsonb, jsonb);
DROP FUNCTION pg_temp.platform_roots();
DROP FUNCTION pg_temp.legacy_actions(jsonb);
DROP FUNCTION pg_temp.legacy_has(text[], text);
DROP FUNCTION pg_temp.legacy_keys(jsonb);
DROP FUNCTION pg_temp.iam_resource_match(text, text);
DROP FUNCTION pg_temp.iam_resource_regex(text);
DROP FUNCTION pg_temp.iam_action_match(text, text);
DROP FUNCTION pg_temp.legacy_key_to_action(text);
DROP FUNCTION pg_temp.legacy_builtin_keys();


-- -------------------------------------------------------------------------
-- >>> plugin-manifest-actions
-- Folded plugin manifest conversion (previously migration 000065).
-- -------------------------------------------------------------------------
-- >>> plugin-manifest-helpers
CREATE FUNCTION pg_temp.plugin_key_to_action(k text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN k = '*' OR position(':' IN k) > 0 THEN k  -- already an action
        WHEN k ~ '^global_roles\.[^.]*$'  THEN 'roles:' || substr(k, length('global_roles.') + 1)
        WHEN k ~ '^project\.roles\.[^.]*$' THEN 'roles:' || substr(k, length('project.roles.') + 1)
        WHEN position('.' IN k) = 0 THEN k
        ELSE regexp_replace(k, '\.([^.]*)$', ':\1')
    END
$$;

-- One middleware stage: requirePermissions -> requireActions.
CREATE FUNCTION pg_temp.plugin_convert_middleware(mw jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN jsonb_typeof(mw) = 'object' AND lower(mw->>'name') = 'requirepermissions' THEN
            (mw - 'name' - 'permissions') || jsonb_build_object(
                'name', 'requireActions',
                'actions', COALESCE((
                    SELECT jsonb_agg(pg_temp.plugin_key_to_action(p.k) ORDER BY p.ord)
                    FROM jsonb_array_elements_text(
                        CASE WHEN jsonb_typeof(mw->'permissions') = 'array' THEN mw->'permissions' ELSE '[]'::jsonb END
                    ) WITH ORDINALITY AS p(k, ord)
                ), '[]'::jsonb))
        ELSE mw
    END
$$;

-- A whole manifest: every route's middlewares (an absent/null middlewares
-- list and an empty one are both kept as they are — the difference is
-- load-bearing, see plugindom.PluginRoute.Middlewares).
CREATE FUNCTION pg_temp.plugin_convert_manifest(m jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN jsonb_typeof(m->'backend'->'routes') = 'array' AND jsonb_array_length(m->'backend'->'routes') > 0 THEN
            jsonb_set(m, '{backend,routes}', (
                SELECT jsonb_agg(
                    CASE
                        WHEN jsonb_typeof(r.route->'middlewares') = 'array' AND jsonb_array_length(r.route->'middlewares') > 0 THEN
                            jsonb_set(r.route, '{middlewares}', (
                                SELECT jsonb_agg(pg_temp.plugin_convert_middleware(x.mw) ORDER BY x.ord)
                                FROM jsonb_array_elements(r.route->'middlewares') WITH ORDINALITY AS x(mw, ord)
                            ))
                        ELSE r.route
                    END ORDER BY r.ord)
                FROM jsonb_array_elements(m->'backend'->'routes') WITH ORDINALITY AS r(route, ord)
            ))
        ELSE m
    END
$$;

-- Every requiredPermission string anywhere in the document and every key of
-- customPermissions, converted to an action. Recursive over objects/arrays.
CREATE FUNCTION pg_temp.plugin_convert_permission_keys(j jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE jsonb_typeof(j)
        WHEN 'object' THEN (
            SELECT COALESCE(jsonb_object_agg(e.k,
                CASE
                    WHEN e.k = 'requiredPermission' AND jsonb_typeof(e.v) = 'string'
                        THEN to_jsonb(pg_temp.plugin_key_to_action(e.v #>> '{}'))
                    WHEN e.k = 'customPermissions' AND jsonb_typeof(e.v) = 'array'
                        THEN (SELECT COALESCE(jsonb_agg(
                                CASE WHEN jsonb_typeof(c.x) = 'object' AND jsonb_typeof(c.x->'key') = 'string'
                                     THEN jsonb_set(c.x, '{key}', to_jsonb(pg_temp.plugin_key_to_action(c.x->>'key')))
                                     ELSE c.x END ORDER BY c.o), '[]'::jsonb)
                              FROM jsonb_array_elements(e.v) WITH ORDINALITY AS c(x, o))
                    ELSE pg_temp.plugin_convert_permission_keys(e.v)
                END), '{}'::jsonb)
            FROM jsonb_each(j) AS e(k, v))
        WHEN 'array' THEN (
            SELECT COALESCE(jsonb_agg(pg_temp.plugin_convert_permission_keys(a.x) ORDER BY a.o), '[]'::jsonb)
            FROM jsonb_array_elements(j) WITH ORDINALITY AS a(x, o))
        ELSE j
    END
$$;
-- <<< plugin-manifest-helpers

UPDATE plugins
SET manifest = pg_temp.plugin_convert_manifest(manifest),
    updated_at = NOW()
WHERE manifest::text ILIKE '%requirepermissions%';

UPDATE plugins
SET manifest = pg_temp.plugin_convert_permission_keys(manifest),
    updated_at = NOW()
WHERE manifest::text ~ '(requiredPermission|customPermissions)'
  AND manifest IS DISTINCT FROM pg_temp.plugin_convert_permission_keys(manifest);

DROP FUNCTION pg_temp.plugin_convert_permission_keys(jsonb);
DROP FUNCTION pg_temp.plugin_convert_manifest(jsonb);
DROP FUNCTION pg_temp.plugin_convert_middleware(jsonb);
DROP FUNCTION pg_temp.plugin_key_to_action(text);

-- -------------------------------------------------------------------------
-- Legacy role columns are now optional for IAM-backed assignments.
-- -------------------------------------------------------------------------
ALTER TABLE users ALTER COLUMN role_id DROP NOT NULL;
ALTER TABLE project_members ALTER COLUMN project_role_id DROP NOT NULL;

-- -------------------------------------------------------------------------
-- >>> project-role-resources
CREATE FUNCTION pg_temp.project_role_resources(res jsonb, proj uuid) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT COALESCE(jsonb_agg(DISTINCT m.mapped), '[]'::jsonb)
    FROM (
        SELECT CASE
                   WHEN r = 'project/' || proj OR r LIKE 'project/' || proj || '/%' THEN r
                   WHEN r = '*' THEN 'project/' || proj || '/*'
                   WHEN r = 'project/*' THEN 'project/' || proj
                   WHEN r LIKE 'project/*/%' THEN 'project/' || proj || substr(r, 10)
               END AS mapped
        FROM jsonb_array_elements_text(COALESCE(res, '[]'::jsonb)) AS r
    ) m
    WHERE m.mapped IS NOT NULL
$$;

CREATE FUNCTION pg_temp.project_role_policy(pol jsonb, proj uuid) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT jsonb_set(pol, '{statements}', COALESCE((
        SELECT jsonb_agg(jsonb_set(st, '{resources}', fixed) ORDER BY ord)
        FROM jsonb_array_elements(COALESCE(pol->'statements', '[]'::jsonb)) WITH ORDINALITY AS t(st, ord)
        CROSS JOIN LATERAL (SELECT pg_temp.project_role_resources(st->'resources', proj) AS fixed) f
        WHERE jsonb_array_length(fixed) > 0
    ), '[]'::jsonb))
$$;

-- <<< project-role-resources

-- -------------------------------------------------------------------------
-- >>> roles-assign-compatibility
-- Add roles:assign to eligible project-member management statements.
-- 'add' = qualifies, 'skip' = grants members:write but its resources do not
-- reach role resources, 'none' = leave alone.
CREATE OR REPLACE FUNCTION pg_temp.assign_state(st jsonb) RETURNS text LANGUAGE sql IMMUTABLE AS $$
    SELECT CASE
        WHEN st->>'effect' IS DISTINCT FROM 'Allow' THEN 'none'
        WHEN jsonb_typeof(st->'actions') IS DISTINCT FROM 'array' THEN 'none'
        WHEN NOT (st->'actions' ? 'project.members:write' OR st->'actions' ? 'project.members:*') THEN 'none'
        WHEN st->'actions' ? 'roles:assign' OR st->'actions' ? 'roles:*' OR st->'actions' ? '*' THEN 'none'
        WHEN EXISTS (
            SELECT 1 FROM jsonb_array_elements_text(COALESCE(st->'resources', '[]'::jsonb)) AS r
            WHERE r = '*' OR r = 'project/*'
               OR r ~ '^project/[^/]+/\*$'
               OR r ~ '^project/[^/]+/role/\*$'
        ) THEN 'add'
        ELSE 'skip'
    END
$$;

CREATE OR REPLACE FUNCTION pg_temp.assign_policy(pol jsonb) RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
    SELECT jsonb_set(pol, '{statements}', COALESCE((
        SELECT jsonb_agg(
                   CASE WHEN pg_temp.assign_state(st) = 'add'
                        THEN jsonb_set(st, '{actions}', (st->'actions') || '"roles:assign"'::jsonb)
                        ELSE st END
                   ORDER BY ord)
        FROM jsonb_array_elements(pol->'statements') WITH ORDINALITY AS t(st, ord)
    ), '[]'::jsonb))
$$;

DO $$
DECLARE
    n_roles int;
    n_added int;
    n_skipped int;
BEGIN
    SELECT count(DISTINCT r.id), count(*)
      INTO n_roles, n_added
      FROM roles r
      CROSS JOIN LATERAL jsonb_array_elements(
          CASE WHEN jsonb_typeof(r.policy->'statements') = 'array' THEN r.policy->'statements' ELSE '[]'::jsonb END
      ) AS s(st)
     WHERE pg_temp.assign_state(s.st) = 'add';
    SELECT count(*) INTO n_skipped
      FROM roles r
      CROSS JOIN LATERAL jsonb_array_elements(
          CASE WHEN jsonb_typeof(r.policy->'statements') = 'array' THEN r.policy->'statements' ELSE '[]'::jsonb END
      ) AS s(st)
     WHERE pg_temp.assign_state(s.st) = 'skip';
    RAISE NOTICE '000068: added roles:assign to % statement(s) in % role(s); skipped % statement(s) whose resources do not cover role resources',
        n_added, n_roles, n_skipped;
END $$;

UPDATE roles
   SET policy = pg_temp.assign_policy(policy),
       updated_at = NOW()
 WHERE jsonb_typeof(policy->'statements') = 'array'
   AND policy IS DISTINCT FROM pg_temp.assign_policy(policy);
COMMIT;
