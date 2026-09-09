-- 000055_add_agent_environment_access_grants.sql
-- Lets a project admin restrict a specific agent or environment to only
-- certain project members, instead of every member holding agents.read/
-- environments.read+connect being able to use every agent/environment in
-- the project. Two independent layers: the existing agents.*/environments.*
-- permissions remain the ceiling ("can this member ever use agents/
-- environments at all"); access_mode + the grant tables below add a
-- per-instance allow-list on top ("which specific ones").
--
-- access_mode defaults to 'open' — zero behavior change for every existing
-- agent/environment until an admin explicitly flips one to 'restricted' and
-- grants specific members. There is deliberately no bypass for
-- agents.write/environments.write holders: configuring a restricted
-- resource stays a separate capability from being allowed to use it (the
-- same split environments.connect already makes against
-- environments.write).
--
-- member_id references project_members.id, not a raw user/agent id — the
-- same convention agent_chat_sessions.member_id and task assignees already
-- use — which is what naturally scopes a *global* agent's grants per
-- project (the same global agent can be open in one project and restricted
-- in another, since it gets a separate project_members row per project it's
-- invited into).
--
-- IF NOT EXISTS / guarded constraints throughout so this migration is safe
-- to re-run.

BEGIN;

ALTER TABLE agents ADD COLUMN IF NOT EXISTS access_mode TEXT NOT NULL DEFAULT 'open';
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_agents_access_mode'
    ) THEN
        ALTER TABLE agents ADD CONSTRAINT ck_agents_access_mode CHECK (access_mode IN ('open', 'restricted'));
    END IF;
END $$;

ALTER TABLE environments ADD COLUMN IF NOT EXISTS access_mode TEXT NOT NULL DEFAULT 'open';
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_environments_access_mode'
    ) THEN
        ALTER TABLE environments ADD CONSTRAINT ck_environments_access_mode CHECK (access_mode IN ('open', 'restricted'));
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS agent_access_grants (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID        NOT NULL,
    member_id   UUID        NOT NULL,
    granted_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_access_grants_agent
        FOREIGN KEY (agent_id)
        REFERENCES agents(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_agent_access_grants_member
        FOREIGN KEY (member_id)
        REFERENCES project_members(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_agent_access_grants_granted_by
        FOREIGN KEY (granted_by)
        REFERENCES users(id)
        ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_access_grants_agent_member
    ON agent_access_grants (agent_id, member_id);
-- Backs "which restricted agents is this member granted" — used to decorate
-- ListAgents without an N+1 grant check per row.
CREATE INDEX IF NOT EXISTS idx_agent_access_grants_member_id
    ON agent_access_grants (member_id);

CREATE TABLE IF NOT EXISTS environment_access_grants (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    environment_id UUID        NOT NULL,
    member_id      UUID        NOT NULL,
    granted_by     UUID,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_environment_access_grants_environment
        FOREIGN KEY (environment_id)
        REFERENCES environments(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_environment_access_grants_member
        FOREIGN KEY (member_id)
        REFERENCES project_members(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_environment_access_grants_granted_by
        FOREIGN KEY (granted_by)
        REFERENCES users(id)
        ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_environment_access_grants_environment_member
    ON environment_access_grants (environment_id, member_id);
CREATE INDEX IF NOT EXISTS idx_environment_access_grants_member_id
    ON environment_access_grants (member_id);

COMMIT;
