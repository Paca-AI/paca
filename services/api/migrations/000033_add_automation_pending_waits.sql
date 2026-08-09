-- 000033_add_automation_pending_waits.sql
-- Lets a graph walk pause at certain node types and resume later once the
-- thing it's waiting on resolves:
--   - trigger_ai_agent: pauses until the agent conversation it started
--     reaches a terminal status, reported on StreamAgentConversationStatus
--     — see worker.AutomationConsumer.handleAgentConversationStatus.
--   - wait: pauses until a configured resume_at timestamp passes, polled by
--     WaitScheduler — see worker.AutomationConsumer.walkWait/resumeAfterDelay.
-- One row exists per in-flight pause in the relevant table; the worker
-- deletes it (ClaimPendingAgentWait / ClaimDueDelays) once it resolves, then
-- resumes the walk from node_id's outgoing edges.
--
-- context is the walk's task/sprint identifiers at the moment it paused —
-- stored as a single JSON blob (automationdom.WalkContext) rather than one
-- column per context type, so a future context type (e.g. epic, release) is
-- just a new struct field, not a new column/migration.

BEGIN;

CREATE TABLE IF NOT EXISTS automation_pending_agent_waits (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES automation_runs(id) ON DELETE CASCADE,
    node_id         UUID NOT NULL REFERENCES automation_nodes(id) ON DELETE CASCADE,
    automation_id   UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    conversation_id UUID NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    context         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One wait per conversation: ClaimPendingAgentWait deletes by conversation_id
-- alone, and a conversation can only ever be the thing exactly one node visit
-- is waiting on.
CREATE UNIQUE INDEX IF NOT EXISTS uq_automation_pending_agent_waits_conversation
    ON automation_pending_agent_waits (conversation_id);
-- Hot lookup: "does run_id still have anything outstanding" — checked after
-- every resolved wait to decide whether the run can be finalized.
CREATE INDEX IF NOT EXISTS idx_automation_pending_agent_waits_run
    ON automation_pending_agent_waits (run_id);

CREATE TABLE IF NOT EXISTS automation_pending_delays (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        UUID NOT NULL REFERENCES automation_runs(id) ON DELETE CASCADE,
    node_id       UUID NOT NULL REFERENCES automation_nodes(id) ON DELETE CASCADE,
    automation_id UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    context       JSONB NOT NULL DEFAULT '{}'::jsonb,
    resume_at     TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Hot lookup: "which delays are due now" — scanned by the WaitScheduler on
-- every tick.
CREATE INDEX IF NOT EXISTS idx_automation_pending_delays_resume_at
    ON automation_pending_delays (resume_at);
-- Hot lookup: "does run_id still have anything outstanding" — checked after
-- every resolved delay/wait to decide whether the run can be finalized.
CREATE INDEX IF NOT EXISTS idx_automation_pending_delays_run
    ON automation_pending_delays (run_id);

COMMIT;
