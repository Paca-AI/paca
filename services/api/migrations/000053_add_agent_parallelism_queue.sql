-- 000053_add_agent_parallelism_queue.sql
-- Adds the schema behind per-agent parallelism limits and the task queue
-- backing them (https://github.com/Paca-AI/paca/issues/462):
--
--   * agents.parallelism_limit — the maximum number of conversations that
--     may be simultaneously "running" for a given agent. Default 1 —
--     without an explicit opt-in, an agent works through its conversations
--     one at a time instead of racing several turns against the same
--     working directory. Global per agent_id, not per-project: a global
--     agent's conversations all still share the same default
--     environment/working directory regardless of which project triggered
--     them.
--
--   * agent_pending_triggers — the durable backlog behind that limit. When
--     a new conversation's trigger can't be dispatched immediately because
--     its agent is already at parallelism_limit running conversations, the
--     trigger's topic + flat payload (the exact fields publishTrigger would
--     have appended to the paca:agent:triggers stream) are persisted here
--     instead, and the conversation is left in status 'queued'.
--     worker.AgentQueueConsumer replays the oldest row for an agent once a
--     running slot frees up (a conversation reaches a terminal status), or
--     when an agent's parallelism_limit is raised. One row per queued
--     conversation (conversation_id is unique) — a conversation can only
--     ever be waiting to start once.
--
--   * agent_pending_triggers.environment_id/environment_folder_id — a
--     second, independent reason a trigger can end up here: parallelism_limit
--     only bounds how many of ONE agent's own conversations may run at
--     once, but two DIFFERENT agents (or two conversations of the same
--     agent via an explicit per-conversation environment/folder override —
--     see StartChatSession's environment_id/folder_id request fields) can
--     still both aim at the very same shared environment folder at the
--     same time. These fields are also carried inside every trigger's flat
--     payload already (the same fields agent-runner itself reads), but
--     promoted here to real, indexed columns so worker.AgentQueueConsumer
--     can find and dequeue "whichever queued trigger was waiting on this
--     folder" when a running conversation there finishes — the same way it
--     already does by agent_id.
--
-- IF NOT EXISTS throughout so this migration is safe to re-run.

BEGIN;

ALTER TABLE agents ADD COLUMN IF NOT EXISTS parallelism_limit INTEGER NOT NULL DEFAULT 1;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_agents_parallelism_limit_positive'
    ) THEN
        ALTER TABLE agents ADD CONSTRAINT ck_agents_parallelism_limit_positive CHECK (parallelism_limit >= 1);
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS agent_pending_triggers (
    id uuid PRIMARY KEY,
    agent_id uuid NOT NULL REFERENCES agents(id),
    conversation_id uuid NOT NULL UNIQUE REFERENCES agent_conversations(id),
    topic varchar NOT NULL,
    payload jsonb NOT NULL,
    -- ON DELETE SET NULL mirrors agent_conversations.environment_id/
    -- environment_folder_id (migration 000042): without it, the default
    -- ON DELETE NO ACTION would make EnvironmentRepository.DeleteFolder's
    -- plain `DELETE FROM environment_folders` fail with a foreign-key
    -- violation whenever the folder still has a queued trigger pointing at
    -- it — exactly the steady state for an environment-backed agent
    -- sitting at its parallelism_limit, i.e. the primary case this feature
    -- exists for. A trigger whose target folder disappears out from under
    -- it degrades to environment-wide scope (see folderOverlapPredicate's
    -- NULL-folder handling) rather than blocking the delete.
    environment_id uuid NULL REFERENCES environments(id) ON DELETE SET NULL,
    environment_folder_id uuid NULL REFERENCES environment_folders(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Backs "give me the oldest pending trigger for this agent" (FIFO dequeue).
CREATE INDEX IF NOT EXISTS idx_agent_pending_triggers_agent_created ON agent_pending_triggers(agent_id, created_at);

-- Backs "give me the oldest pending trigger waiting on this folder,
-- regardless of which agent it belongs to" (FIFO dequeue) — partial, since
-- most pending triggers (an agent's own ephemeral-sandbox backlog) never
-- set these at all.
CREATE INDEX IF NOT EXISTS idx_agent_pending_triggers_folder_created
    ON agent_pending_triggers(environment_id, environment_folder_id, created_at)
    WHERE environment_id IS NOT NULL;

-- Backs checkFolderCapacity's CountRunningConversationsInFolder, run on
-- every dispatch decision for an environment-attached agent — mirrors
-- idx_agent_conversations_agent_status's (agent_id, status) shape for the
-- folder axis instead of the agent axis.
CREATE INDEX IF NOT EXISTS idx_agent_conversations_environment_status
    ON agent_conversations(environment_id, status)
    WHERE environment_id IS NOT NULL;

COMMIT;
