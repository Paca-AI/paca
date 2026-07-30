-- 000027_add_automation_graph.sql
-- Replaces the workflow feature (task-dependency DAG + status rules + status
-- transitions) with a single n8n-style automation graph: a project-scoped
-- graph of Trigger/Condition/Action nodes connected by edges. See
-- docs/architecture (automation redesign) for the full rationale.
--
-- "Done" for a task is no longer derived from a per-workflow transition
-- chain — task_statuses.category = 'done' (already project-scoped, already
-- exists) is used directly, so no transition-chain table is recreated here.

BEGIN;

-- -------------------------------------------------------------------------
-- AUTOMATIONS
-- Project-scoped automation graph, replacing `workflows`.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS automations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        VARCHAR(255) NOT NULL,
    description TEXT,
    status      VARCHAR(20) NOT NULL DEFAULT 'draft'
                    CHECK (status IN ('draft', 'active', 'archived')),
    created_by  UUID REFERENCES project_members(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_automations_project ON automations (project_id) WHERE deleted_at IS NULL;

-- -------------------------------------------------------------------------
-- AUTOMATION NODES
-- One row per Trigger/Condition/Action step. `config` carries every
-- type-specific field (trigger config, condition leaf, action fields,
-- including a condition leaf's or action's optional task-retarget and a
-- trigger's optional target_task_id) as JSONB — replaces `automation_rules`
-- (flattened trigger/conditions/actions) and `workflow_nodes`.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS automation_nodes (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    automation_id UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    kind          VARCHAR(20) NOT NULL CHECK (kind IN ('trigger', 'condition', 'action')),
    type          VARCHAR(100) NOT NULL,
    config        JSONB NOT NULL DEFAULT '{}'::jsonb,
    pos_x         DOUBLE PRECISION NOT NULL DEFAULT 0,
    pos_y         DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_automation_nodes_automation ON automation_nodes (automation_id);
-- Hot lookup: "find every enabled trigger node of type X across active
-- automations" — the entry point of every engine event, so it's scoped to
-- kind = 'trigger' rows only (a small fraction of all nodes).
CREATE INDEX IF NOT EXISTS idx_automation_nodes_trigger_type ON automation_nodes (type) WHERE kind = 'trigger';

-- -------------------------------------------------------------------------
-- AUTOMATION EDGES
-- Directed link between two nodes. source_handle is null for Trigger/Action
-- sources (a single outgoing path) and a branch id for a Condition source
-- (one of its N branches, or the literal 'else'). Replaces `workflow_edges`.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS automation_edges (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    automation_id   UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    source_node_id  UUID NOT NULL REFERENCES automation_nodes(id) ON DELETE CASCADE,
    source_handle   VARCHAR(100),
    target_node_id  UUID NOT NULL REFERENCES automation_nodes(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT no_self_automation_edge CHECK (source_node_id <> target_node_id)
);

CREATE INDEX IF NOT EXISTS idx_automation_edges_automation_source ON automation_edges (automation_id, source_node_id);
CREATE INDEX IF NOT EXISTS idx_automation_edges_target ON automation_edges (target_node_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_automation_edges
    ON automation_edges (source_node_id, COALESCE(source_handle, ''), target_node_id);

-- -------------------------------------------------------------------------
-- AUTOMATION RUNS
-- One row per graph execution (a matched trigger starting a walk). task_id
-- is nullable: a cron/api_trigger/predecessor_done trigger's target_task_id
-- is itself optional, and when unset the graph can only reach call_api or
-- trigger_ai_agent actions (neither needs a task to run), so the resulting
-- run has no task at all.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS automation_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    automation_id   UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    trigger_node_id UUID NOT NULL REFERENCES automation_nodes(id) ON DELETE CASCADE,
    task_id         UUID REFERENCES tasks(id) ON DELETE CASCADE,
    status          VARCHAR(20) NOT NULL DEFAULT 'running'
                        CHECK (status IN ('running', 'completed', 'failed')),
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_automation_runs_automation ON automation_runs (automation_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_automation_runs_task ON automation_runs (task_id);

-- -------------------------------------------------------------------------
-- AUTOMATION RUN STEPS
-- One row per node actually visited within a run — the per-node execution
-- trace the prior design lacked entirely.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS automation_run_steps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES automation_runs(id) ON DELETE CASCADE,
    node_id         UUID NOT NULL REFERENCES automation_nodes(id) ON DELETE CASCADE,
    status          VARCHAR(20) NOT NULL CHECK (status IN ('completed', 'failed', 'skipped')),
    input_snapshot  JSONB,
    output_snapshot JSONB,
    error           TEXT,
    executed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_automation_run_steps_run ON automation_run_steps (run_id, executed_at);

-- -------------------------------------------------------------------------
-- AUTOMATION DUE-DATE FIRES
-- Tracks which (automation, task) pairs have already fired for a
-- due_date_reached trigger, so the leader-locked polling scheduler doesn't
-- refire on every poll tick once a task has already triggered once.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS automation_due_date_fires (
    automation_id UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    node_id       UUID NOT NULL REFERENCES automation_nodes(id) ON DELETE CASCADE,
    task_id       UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    fired_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (node_id, task_id)
);

-- -------------------------------------------------------------------------
-- AUTOMATION CRON FIRES
-- Recurring "last fired" anchor for cron trigger nodes. One row per node,
-- upserted on every fire — unlike automation_due_date_fires (a one-shot per
-- (node, task) log), cron always targets the same fixed task (or none), so
-- the node_id alone identifies the schedule's state.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS automation_cron_fires (
    node_id       UUID PRIMARY KEY REFERENCES automation_nodes(id) ON DELETE CASCADE,
    automation_id UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    last_fired_at TIMESTAMPTZ NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_automation_cron_fires_automation ON automation_cron_fires (automation_id);

-- -------------------------------------------------------------------------
-- AUTOMATION WEBHOOK TOKENS
-- Webhook secret tokens for api_trigger nodes. At most one active
-- (revoked_at IS NULL) token per node — regenerating revokes the old one
-- and inserts a new one in the same transaction. Only the SHA-256 hash is
-- stored; the raw token is shown to the user once, at generation time, and
-- is never persisted.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS automation_webhook_tokens (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id       UUID NOT NULL REFERENCES automation_nodes(id) ON DELETE CASCADE,
    automation_id UUID NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    token_prefix  VARCHAR(8) NOT NULL,
    token_hash    VARCHAR(64) NOT NULL,
    created_by    UUID REFERENCES project_members(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_automation_webhook_tokens_active_node
    ON automation_webhook_tokens (node_id) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_automation_webhook_tokens_node ON automation_webhook_tokens (node_id);

-- -------------------------------------------------------------------------
-- trigger_ai_agent's direct-message capability: when its trigger has no
-- target task, it fires a standalone message at the agent (see
-- agentsvc.TriggerDirectMessage) instead of assigning a task, using a new
-- agent_conversations.trigger_type value this constraint must allow.
--
-- 'automation_message' is also listed in 000011_add_description_write_trigger.sql's
-- copy of this same constraint, which always runs first on every boot (see
-- that file's comment) — this ADD CONSTRAINT here is therefore a no-op
-- re-assertion of a constraint 000011 already established with the right
-- value list, kept for this file's own readability/self-containedness.
-- -------------------------------------------------------------------------

ALTER TABLE agent_conversations
    DROP CONSTRAINT IF EXISTS agent_conversations_trigger_type_check;

ALTER TABLE agent_conversations
    ADD CONSTRAINT agent_conversations_trigger_type_check
    CHECK (trigger_type IN ('task_assigned', 'comment_mention', 'chat_message', 'description_write', 'automation_message'));

-- -------------------------------------------------------------------------
-- DROP the old workflow feature outright (early-stage product, no
-- production data worth migrating — see the redesign's explicit "totally
-- remove, don't migrate" decision). FK-safe order: edges/rules/transitions
-- before nodes before the parent table.
-- -------------------------------------------------------------------------

DROP TABLE IF EXISTS workflow_edges;
DROP TABLE IF EXISTS workflow_status_transitions;
DROP TABLE IF EXISTS workflow_status_rules;
DROP TABLE IF EXISTS workflow_nodes;
DROP TABLE IF EXISTS workflows;

COMMIT;
