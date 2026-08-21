-- Migration 000042: durable task-level agent handoffs (#392).
--
-- A successful task-linked agent conversation's final reply is persisted here
-- so a later conversation on the same task can recover the prior conclusion,
-- even after the original conversation is terminal or the task is closed.
--
-- conversation_id is UNIQUE so a completion retry can never create a second
-- handoff for the same conversation (idempotent persistence).

BEGIN;

CREATE TABLE IF NOT EXISTS agent_task_handoffs (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID        NOT NULL,
    conversation_id UUID        NOT NULL,
    summary         TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_task_handoffs_task
        FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_task_handoffs_conversation
        FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE CASCADE,
    CONSTRAINT uq_agent_task_handoffs_conversation UNIQUE (conversation_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_task_handoffs_task_created
    ON agent_task_handoffs (task_id, created_at DESC);

COMMIT;
