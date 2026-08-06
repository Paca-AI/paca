-- 000031_add_global_agents.sql
-- Adds "global" agents: a second agent scope alongside the existing
-- project-owned agents. A global agent has no owning project (project_id
-- IS NULL) and is instead attached to zero or more projects by being added
-- to their project_members list (the same mechanism a human member uses,
-- see project_members.agent_id + uq_pm_project_agent from 000008 — that
-- index is already scoped per (project_id, agent_id), not per agent_id
-- alone, so it already supports one agent belonging to many projects; no
-- change needed there). Global-scope admin capabilities (create users, set
-- up global roles, manage projects) are granted via global_role_id, which
-- mirrors users.role_id.
--
-- Every ALTER below is IF NOT EXISTS / existence-guarded so this migration is
-- safe to re-run against a database where some or all of it already landed.

BEGIN;

-- -------------------------------------------------------------------------
-- AGENTS: agent_scope discriminator, optional project_id, optional
-- global_role_id.
-- -------------------------------------------------------------------------

-- Existing rows all have project_id NOT NULL already and get agent_scope
-- backfilled to 'project' by the DEFAULT below — zero behavior change for
-- every agent that exists today.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS agent_scope TEXT NOT NULL DEFAULT 'project'
    CHECK (agent_scope IN ('project', 'global'));

-- project_id becomes optional: NULL means the agent is global-scoped and is
-- associated with projects only indirectly, via project_members rows (added
-- through the same "invite a member" flow used for humans). The existing
-- fk_agents_project FK (000008) stays ON DELETE CASCADE — a NULL project_id
-- is never matched by a FK ON DELETE action, so deleting a project still
-- cascades to that project's own project-scoped agents exactly as today,
-- and never touches global agents.
ALTER TABLE agents ALTER COLUMN project_id DROP NOT NULL;

-- global_role_id mirrors users.role_id: the platform-level role governing
-- what a global agent may do via admin-shaped tools (create users, manage
-- global roles, manage projects) when it has no project context. Nullable —
-- a global agent with no role has zero global-scope permissions but can
-- still be invited into projects and act under whatever project role it's
-- granted there. ON DELETE RESTRICT matches fk_users_role_id's convention
-- (000001) — deleting an in-use global role must be blocked, not silently
-- orphan the agent's permissions.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS global_role_id UUID
    REFERENCES global_roles(id)
    ON DELETE RESTRICT;

-- Keep the two shapes cleanly separated: a project-scoped agent's
-- permissions come only from its project_role_id (as today, via
-- project_members); a global agent's project_id is always NULL and it never
-- carries a project_role_id of its own. Relaxable later if a future need
-- arises for a project-scoped agent to also hold a global role.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_agents_scope'
    ) THEN
        ALTER TABLE agents
            ADD CONSTRAINT ck_agents_scope CHECK (
                (agent_scope = 'project' AND project_id IS NOT NULL AND global_role_id IS NULL)
                OR
                (agent_scope = 'global' AND project_id IS NULL)
            );
    END IF;
END $$;

-- uq_agents_project_handle (000008) only constrains rows where project_id
-- IS NOT NULL to a real value — standard SQL unique-index semantics treat
-- every NULL in project_id as distinct from every other NULL, so it
-- provides no uniqueness protection for global agents (two global agents
-- could otherwise both be named "bot"). Add a second partial index scoped
-- to project_id IS NULL to close that gap.
CREATE UNIQUE INDEX IF NOT EXISTS uq_agents_global_handle
    ON agents (handle)
    WHERE deleted_at IS NULL AND project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_agents_scope ON agents (agent_scope);
CREATE INDEX IF NOT EXISTS idx_agents_global_role_id ON agents (global_role_id) WHERE global_role_id IS NOT NULL;

-- -------------------------------------------------------------------------
-- AGENT CHAT SESSIONS: project_id/member_id become optional together,
-- actor_user_id carries the human identity directly for a global chat
-- session (started from the home page / admin pages, not a project).
-- -------------------------------------------------------------------------

ALTER TABLE agent_chat_sessions ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE agent_chat_sessions ALTER COLUMN member_id DROP NOT NULL;

-- RESTRICT (not SET NULL/CASCADE): users in this system are only ever
-- soft-deleted (users.deleted_at), never hard-DELETEd, so this FK action is
-- not expected to fire in practice; RESTRICT is the conservative choice
-- that can never silently leave a row with neither actor set.
ALTER TABLE agent_chat_sessions ADD COLUMN IF NOT EXISTS actor_user_id UUID
    REFERENCES users(id)
    ON DELETE RESTRICT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_agent_chat_sessions_actor'
    ) THEN
        ALTER TABLE agent_chat_sessions
            ADD CONSTRAINT ck_agent_chat_sessions_actor CHECK (
                (project_id IS NOT NULL AND member_id IS NOT NULL AND actor_user_id IS NULL)
                OR
                (project_id IS NULL AND member_id IS NULL AND actor_user_id IS NOT NULL)
            );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_agent_chat_sessions_agent_actor_user
    ON agent_chat_sessions (agent_id, actor_user_id) WHERE actor_user_id IS NOT NULL;

-- -------------------------------------------------------------------------
-- AGENT CONVERSATIONS: project_id becomes optional, actor_user_id is a new
-- third actor state alongside the already-nullable triggered_by_member_id.
-- -------------------------------------------------------------------------

ALTER TABLE agent_conversations ALTER COLUMN project_id DROP NOT NULL;

-- triggered_by_member_id is already nullable (000018_add_automation_workflows,
-- see that migration) and NULL there means "the automation-workflow engine,
-- no human behind it" — a meaning this migration must not collide with.
-- actor_user_id is a new, additive third state: "a human triggered this via
-- the global chat, with no project-member context." All three states stay
-- distinguishable:
--   triggered_by_member_id SET              -> project-scoped human member
--   triggered_by_member_id NULL, actor NULL -> automation engine (unchanged)
--   actor_user_id SET                       -> global-chat human actor
-- No trigger_type change needed here — global chat conversations reuse the
-- existing 'chat_message' value; the distinguishing factor is
-- project_id IS NULL + actor_user_id IS NOT NULL.
ALTER TABLE agent_conversations ADD COLUMN IF NOT EXISTS actor_user_id UUID
    REFERENCES users(id)
    ON DELETE RESTRICT;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_agent_conversations_actor'
    ) THEN
        ALTER TABLE agent_conversations
            ADD CONSTRAINT ck_agent_conversations_actor CHECK (
                NOT (triggered_by_member_id IS NOT NULL AND actor_user_id IS NOT NULL)
            );
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_agent_conversations_actor_user
    ON agent_conversations (actor_user_id) WHERE actor_user_id IS NOT NULL;

COMMIT;
