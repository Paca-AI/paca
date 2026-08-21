-- Migration 000043: unified agent chat/session/turn foundation (#392).
--
-- agent_chat_sessions is the user-visible, owner-private conversation.
-- agent_turns is the authoritative boundary for one user input (or one
-- sessionless trigger) through a stable terminal result. agent_conversations
-- remains a runtime-continuity/execution record and may span multiple turns.
--
-- Context snapshots and conclusion publications are append-only audit facts.
-- A private turn result is never made task-visible by this migration: a human
-- must prepare and confirm an agent_conclusion_publication explicitly.

BEGIN;

-- Client retries may safely replay session creation. Separate partial indexes
-- keep project-member and global-user ownership forms unambiguous.
ALTER TABLE agent_chat_sessions
    ADD COLUMN IF NOT EXISTS client_request_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_chat_sessions_member_request
    ON agent_chat_sessions (project_id, member_id, client_request_id)
    WHERE member_id IS NOT NULL AND client_request_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_chat_sessions_actor_request
    ON agent_chat_sessions (actor_user_id, client_request_id)
    WHERE actor_user_id IS NOT NULL AND client_request_id IS NOT NULL;

-- Sources selected for future turns. They are references, not copied content;
-- every turn re-authorizes them and freezes its own immutable snapshot.
CREATE TABLE IF NOT EXISTS agent_session_context_sources (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id            UUID        NOT NULL,
    project_id            UUID        NOT NULL,
    source_type           TEXT        NOT NULL CHECK (source_type IN ('task', 'session', 'run')),
    source_id             UUID        NOT NULL,
    ordinal               INTEGER     NOT NULL CHECK (ordinal >= 0),
    selected_by_member_id UUID        NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_context_source_session
        FOREIGN KEY (session_id) REFERENCES agent_chat_sessions(id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_context_source_project
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_context_source_selector
        FOREIGN KEY (selected_by_member_id) REFERENCES project_members(id) ON DELETE RESTRICT,
    CONSTRAINT uq_agent_context_source_ref UNIQUE (session_id, source_type, source_id),
    CONSTRAINT uq_agent_context_source_ordinal UNIQUE (session_id, ordinal)
);

CREATE INDEX IF NOT EXISTS idx_agent_context_sources_session
    ON agent_session_context_sources (session_id, ordinal);

-- Enforce durable scope invariants in the database as well as the service.
-- Source RBAC is still checked by the service at selection and snapshot time.
CREATE OR REPLACE FUNCTION enforce_agent_context_source_scope()
RETURNS TRIGGER AS $$
DECLARE
    target_project UUID;
    target_member UUID;
    source_project UUID;
    source_member UUID;
    source_audience TEXT;
BEGIN
    SELECT project_id, member_id
      INTO target_project, target_member
      FROM agent_chat_sessions
     WHERE id = NEW.session_id;

    IF target_project IS NULL OR target_project <> NEW.project_id THEN
        RAISE EXCEPTION 'context source project does not match session project';
    END IF;
    IF target_member IS NULL OR target_member <> NEW.selected_by_member_id THEN
        RAISE EXCEPTION 'context source selector is not the session owner';
    END IF;

    IF NEW.source_type = 'task' THEN
        SELECT project_id INTO source_project FROM tasks WHERE id = NEW.source_id;
    ELSIF NEW.source_type = 'session' THEN
        SELECT project_id, member_id
          INTO source_project, source_member
          FROM agent_chat_sessions
         WHERE id = NEW.source_id;
        IF source_member IS NULL OR source_member <> target_member THEN
            RAISE EXCEPTION 'private source session is not owned by selector';
        END IF;
    ELSE
        SELECT t.project_id,
               CASE WHEN t.session_id IS NULL THEN 'project_shared' ELSE 'owner_private' END,
               cs.member_id
          INTO source_project, source_audience, source_member
          FROM agent_turn_runs r
          JOIN agent_turns t ON t.id = r.turn_id
          LEFT JOIN agent_chat_sessions cs ON cs.id = t.session_id
         WHERE r.id = NEW.source_id;
        IF source_audience = 'owner_private'
           AND (source_member IS NULL OR source_member <> target_member) THEN
            RAISE EXCEPTION 'private source run is not owned by selector';
        END IF;
    END IF;

    IF source_project IS NULL OR source_project <> NEW.project_id THEN
        RAISE EXCEPTION 'context source must belong to the session project';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_context_source_scope ON agent_session_context_sources;
CREATE TRIGGER trg_agent_context_source_scope
    BEFORE INSERT OR UPDATE ON agent_session_context_sources
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_context_source_scope();

-- Private chat execution uses a typed, deny-by-default capability policy.
-- Context is data only and can never grant tools. The runner/API enforce this
-- policy at dispatch; the database rejects malformed or mutation-capable
-- private policies even if a caller bypasses the service.
CREATE OR REPLACE FUNCTION agent_private_tool_policy_is_safe(policy JSONB)
RETURNS BOOLEAN AS $$
DECLARE
    capability TEXT;
BEGIN
    IF jsonb_typeof(policy) <> 'object'
       OR policy->>'version' <> '1'
       OR policy->>'mode' <> 'deny_by_default'
       OR jsonb_typeof(policy->'allowed_capabilities') <> 'array'
       OR jsonb_typeof(policy->'context_may_grant') <> 'boolean'
       OR (policy->>'context_may_grant')::boolean
       OR jsonb_array_length(policy->'allowed_capabilities') <> (
            SELECT COUNT(DISTINCT value)
            FROM jsonb_array_elements_text(policy->'allowed_capabilities') AS value
       ) THEN
        RETURN FALSE;
    END IF;
    FOR capability IN SELECT jsonb_array_elements_text(policy->'allowed_capabilities')
    LOOP
        IF capability NOT IN (
            'agents.read', 'docs.read', 'projects.read',
            'sprints.read', 'tasks.read', 'workflows.read'
        ) THEN
            RETURN FALSE;
        END IF;
    END LOOP;
    RETURN TRUE;
EXCEPTION WHEN OTHERS THEN
    RETURN FALSE;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE TABLE IF NOT EXISTS agent_turns (
    id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id               UUID,
    conversation_id          UUID        NOT NULL,
    project_id               UUID,
    agent_id                 UUID        NOT NULL,
    requested_by_member_id   UUID,
    requested_by_user_id     UUID,
    turn_index               INTEGER     NOT NULL CHECK (turn_index > 0),
    input_text               TEXT        NOT NULL,
    status                   TEXT        NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'stopped', 'cancelled', 'timed_out', 'no_output')),
    idempotency_key          TEXT        NOT NULL,
    tool_policy              JSONB       NOT NULL,
    tool_policy_sha256       TEXT        NOT NULL CHECK (length(tool_policy_sha256) = 64),
    command_sha256           TEXT,
    request_sha256           TEXT        NOT NULL CHECK (length(request_sha256) = 64),
    state_version            BIGINT      NOT NULL DEFAULT 0,
    deadline_at              TIMESTAMPTZ,
    started_at               TIMESTAMPTZ,
    finished_at              TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_turn_session
        FOREIGN KEY (session_id) REFERENCES agent_chat_sessions(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_conversation
        FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_project
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_agent
        FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_member
        FOREIGN KEY (requested_by_member_id) REFERENCES project_members(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_user
        FOREIGN KEY (requested_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT ck_agent_turn_actor CHECK (
        NOT (requested_by_member_id IS NOT NULL AND requested_by_user_id IS NOT NULL)
    ),
    CONSTRAINT ck_agent_turn_idempotency_key CHECK (
        length(btrim(idempotency_key)) BETWEEN 1 AND 200
    ),
    CONSTRAINT ck_agent_turn_input_bound CHECK (
        octet_length(input_text) BETWEEN 1 AND 32768
    ),
    CONSTRAINT ck_agent_turn_private_task_mutation_blocked CHECK (
        session_id IS NULL OR (
            agent_private_tool_policy_is_safe(tool_policy)
            AND tool_policy = '{
                "version":1,
                "mode":"deny_by_default",
                "allowed_capabilities":[
                    "agents.read","docs.read","projects.read",
                    "sprints.read","tasks.read","workflows.read"
                ],
                "context_may_grant":false
            }'::jsonb
            AND tool_policy_sha256 = 'a7710f1d5a37f08a2dc33ead965961694eb6456ed987a59a4296bbbe72444f40'
        )
    )
);

-- Keep the development migration repeatable across the earlier PACA-3 draft,
-- which created agent_turns before the three audit hashes existed. Empty draft
-- tables can be upgraded safely; populated draft rows fail closed because the
-- original command/runtime fingerprints cannot be reconstructed faithfully.
ALTER TABLE agent_turns ADD COLUMN IF NOT EXISTS tool_policy_sha256 TEXT;
ALTER TABLE agent_turns ADD COLUMN IF NOT EXISTS command_sha256 TEXT;
ALTER TABLE agent_turns ADD COLUMN IF NOT EXISTS request_sha256 TEXT;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM agent_turns
        WHERE tool_policy_sha256 IS NULL
           OR command_sha256 IS NULL
           OR request_sha256 IS NULL
           OR command_sha256=request_sha256
    ) THEN
        RAISE EXCEPTION 'agent_turns contains incompatible PACA-3 draft audit hashes'
            USING HINT = 'Preserve the database for investigation; draft audit hashes cannot be reconstructed safely.';
    END IF;
END
$$;
ALTER TABLE agent_turns ALTER COLUMN tool_policy_sha256 SET NOT NULL;
ALTER TABLE agent_turns ALTER COLUMN command_sha256 SET NOT NULL;
ALTER TABLE agent_turns ALTER COLUMN request_sha256 SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid='agent_turns'::regclass
          AND conname='chk_agent_turn_tool_policy_sha256'
    ) THEN
        ALTER TABLE agent_turns
            ADD CONSTRAINT chk_agent_turn_tool_policy_sha256
            CHECK (length(tool_policy_sha256)=64);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid='agent_turns'::regclass
          AND conname='chk_agent_turn_command_sha256'
    ) THEN
        ALTER TABLE agent_turns
            ADD CONSTRAINT chk_agent_turn_command_sha256
            CHECK (length(command_sha256)=64);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid='agent_turns'::regclass
          AND conname='chk_agent_turn_request_sha256'
    ) THEN
        ALTER TABLE agent_turns
            ADD CONSTRAINT chk_agent_turn_request_sha256
            CHECK (length(request_sha256)=64);
    END IF;
END
$$;

-- CREATE TABLE IF NOT EXISTS cannot strengthen a constraint left by the draft.
-- Replace the former {"tasks.write":false} check with the canonical typed,
-- deny-by-default policy check used by fresh installations.
ALTER TABLE agent_turns DROP CONSTRAINT IF EXISTS ck_agent_turn_private_task_mutation_blocked;
ALTER TABLE agent_turns
    ADD CONSTRAINT ck_agent_turn_private_task_mutation_blocked CHECK (
        session_id IS NULL OR (
            agent_private_tool_policy_is_safe(tool_policy)
            AND tool_policy = '{
                "version":1,
                "mode":"deny_by_default",
                "allowed_capabilities":[
                    "agents.read","docs.read","projects.read",
                    "sprints.read","tasks.read","workflows.read"
                ],
                "context_may_grant":false
            }'::jsonb
            AND tool_policy_sha256 = 'a7710f1d5a37f08a2dc33ead965961694eb6456ed987a59a4296bbbe72444f40'
        )
    );

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_turn_session_index
    ON agent_turns (session_id, turn_index) WHERE session_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_turn_session_idempotency
    ON agent_turns (session_id, idempotency_key) WHERE session_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_turn_run_idempotency
    ON agent_turns (conversation_id, idempotency_key) WHERE session_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_turn_one_active_per_session
    ON agent_turns (session_id)
    WHERE session_id IS NOT NULL AND status IN ('queued', 'running');
CREATE INDEX IF NOT EXISTS idx_agent_turns_session_created
    ON agent_turns (session_id, turn_index DESC);
CREATE INDEX IF NOT EXISTS idx_agent_turns_conversation
    ON agent_turns (conversation_id, created_at);

-- A turn is allowed to reference only the conversation/runtime owned by the
-- same session, project, agent and human. Legacy sessionless automation turns
-- keep their existing nullable actor form.
CREATE OR REPLACE FUNCTION enforce_agent_turn_scope()
RETURNS TRIGGER AS $$
DECLARE
    conv_project UUID;
    conv_agent UUID;
    conv_session UUID;
    conv_member UUID;
    conv_user UUID;
    conv_trigger TEXT;
    conv_task UUID;
    conv_comment UUID;
    session_project UUID;
    session_agent UUID;
    session_member UUID;
    session_actor_user UUID;
BEGIN
    SELECT project_id, agent_id, chat_session_id,
           triggered_by_member_id, actor_user_id, trigger_type, task_id, comment_id
      INTO conv_project, conv_agent, conv_session, conv_member, conv_user,
           conv_trigger, conv_task, conv_comment
      FROM agent_conversations
     WHERE id = NEW.conversation_id;

    IF NOT FOUND
       OR conv_project IS DISTINCT FROM NEW.project_id
       OR conv_agent IS DISTINCT FROM NEW.agent_id THEN
        RAISE EXCEPTION 'turn scope does not match conversation';
    END IF;

    IF NEW.session_id IS NULL THEN
        IF conv_session IS NOT NULL THEN
            RAISE EXCEPTION 'sessionless turn cannot use a chat conversation';
        END IF;
        IF conv_member IS DISTINCT FROM NEW.requested_by_member_id
           OR conv_user IS DISTINCT FROM NEW.requested_by_user_id THEN
            RAISE EXCEPTION 'turn requester does not match conversation actor';
        END IF;
        RETURN NEW;
    END IF;

    SELECT project_id, agent_id, member_id, actor_user_id
      INTO session_project, session_agent, session_member, session_actor_user
      FROM agent_chat_sessions
     WHERE id = NEW.session_id;

    IF NOT FOUND
       OR conv_session IS DISTINCT FROM NEW.session_id
       OR session_project IS DISTINCT FROM NEW.project_id
       OR session_agent IS DISTINCT FROM NEW.agent_id
       OR session_member IS DISTINCT FROM NEW.requested_by_member_id
       OR session_actor_user IS DISTINCT FROM NEW.requested_by_user_id THEN
        RAISE EXCEPTION 'turn scope does not match chat session';
    END IF;
    IF conv_trigger <> 'chat_message' OR conv_task IS NOT NULL OR conv_comment IS NOT NULL THEN
        RAISE EXCEPTION 'private chat conversation cannot be task-bound';
    END IF;
    -- Membership is a creation-time admission check. A worker must still be
    -- able to finalize or expire an already admitted turn after either member
    -- is revoked; otherwise the session remains permanently blocked.
    IF TG_OP = 'INSERT' AND NEW.project_id IS NOT NULL AND (
        NOT EXISTS (
            SELECT 1 FROM project_members pm
            WHERE pm.id = NEW.requested_by_member_id
              AND pm.project_id = NEW.project_id
              AND pm.member_type = 'human'
              AND pm.deleted_at IS NULL
        )
        OR NOT EXISTS (
            SELECT 1 FROM project_members pm
            WHERE pm.agent_id = NEW.agent_id
              AND pm.project_id = NEW.project_id
              AND pm.member_type = 'agent'
              AND pm.deleted_at IS NULL
        )
    ) THEN
        RAISE EXCEPTION 'chat actor or agent is not an active project member';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_scope ON agent_turns;
CREATE TRIGGER trg_agent_turn_scope
    BEFORE INSERT OR UPDATE ON agent_turns
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_turn_scope();

CREATE OR REPLACE FUNCTION enforce_agent_turn_update()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.session_id IS DISTINCT FROM OLD.session_id
       OR NEW.conversation_id IS DISTINCT FROM OLD.conversation_id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.agent_id IS DISTINCT FROM OLD.agent_id
       OR NEW.requested_by_member_id IS DISTINCT FROM OLD.requested_by_member_id
       OR NEW.requested_by_user_id IS DISTINCT FROM OLD.requested_by_user_id
       OR NEW.turn_index IS DISTINCT FROM OLD.turn_index
       OR NEW.input_text IS DISTINCT FROM OLD.input_text
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.tool_policy IS DISTINCT FROM OLD.tool_policy
       OR NEW.tool_policy_sha256 IS DISTINCT FROM OLD.tool_policy_sha256
       OR NEW.command_sha256 IS DISTINCT FROM OLD.command_sha256
       OR NEW.request_sha256 IS DISTINCT FROM OLD.request_sha256
       OR NEW.deadline_at IS DISTINCT FROM OLD.deadline_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'agent turn audit fields are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_update ON agent_turns;
CREATE TRIGGER trg_agent_turn_update
    BEFORE UPDATE ON agent_turns
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_turn_update();

CREATE TABLE IF NOT EXISTS agent_turn_context_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    turn_id         UUID        NOT NULL UNIQUE,
    schema_version  INTEGER     NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    manifest        JSONB       NOT NULL DEFAULT '[]'::jsonb,
    rendered_text   TEXT        NOT NULL DEFAULT '',
    manifest_sha256 TEXT        NOT NULL CHECK (length(manifest_sha256) = 64),
    total_bytes     INTEGER     NOT NULL CHECK (total_bytes BETWEEN 0 AND 131072),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_turn_snapshot_turn
        FOREIGN KEY (turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    CONSTRAINT ck_agent_turn_snapshot_manifest CHECK (
        jsonb_typeof(manifest) = 'array' AND jsonb_array_length(manifest) <= 64
    )
);

CREATE TABLE IF NOT EXISTS agent_turn_context_items (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id      UUID        NOT NULL,
    ordinal          INTEGER     NOT NULL CHECK (ordinal >= 0),
    source_type      TEXT        NOT NULL CHECK (source_type IN ('task', 'session', 'run')),
    source_id        UUID        NOT NULL,
    source_version   TEXT        NOT NULL,
    source_audience  TEXT        NOT NULL CHECK (source_audience IN ('owner_private', 'project_shared')),
    captured_at      TIMESTAMPTZ NOT NULL,
    content          JSONB       NOT NULL,
    rendered_text    TEXT        NOT NULL,
    content_sha256   TEXT        NOT NULL CHECK (length(content_sha256) = 64),
    byte_count       INTEGER     NOT NULL CHECK (byte_count BETWEEN 0 AND 32768),
    CONSTRAINT ck_agent_context_item_content_bound CHECK (
        octet_length(content::text) <= 32768
        AND octet_length(rendered_text) <= 32768
    ),
    CONSTRAINT fk_agent_context_item_snapshot
        FOREIGN KEY (snapshot_id) REFERENCES agent_turn_context_snapshots(id) ON DELETE RESTRICT,
    CONSTRAINT uq_agent_context_item_ordinal UNIQUE (snapshot_id, ordinal)
);

CREATE TABLE IF NOT EXISTS agent_turn_runs (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    turn_id              UUID        NOT NULL,
    conversation_id      UUID        NOT NULL,
    backend              TEXT        NOT NULL CHECK (backend IN ('llm', 'acp')),
    attempt              INTEGER     NOT NULL DEFAULT 1 CHECK (attempt > 0),
    status               TEXT        NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'stopped', 'cancelled', 'timed_out', 'no_output')),
    claim_token          UUID,
    claimed_by           TEXT,
    lease_expires_at     TIMESTAMPTZ,
    final_event_sequence INTEGER,
    started_at           TIMESTAMPTZ,
    finished_at          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_turn_run_turn
        FOREIGN KEY (turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_run_conversation
        FOREIGN KEY (conversation_id) REFERENCES agent_conversations(id) ON DELETE RESTRICT,
    CONSTRAINT uq_agent_turn_run_attempt UNIQUE (turn_id, attempt),
    CONSTRAINT ck_agent_turn_run_claim CHECK (
        (status = 'queued' AND claim_token IS NULL AND claimed_by IS NULL AND lease_expires_at IS NULL)
        OR (status = 'running' AND claim_token IS NOT NULL AND claimed_by IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR (status IN ('succeeded','failed','stopped','cancelled','timed_out','no_output')
            AND lease_expires_at IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_agent_turn_runs_claimable
    ON agent_turn_runs (status, lease_expires_at, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_turn_one_active_run
    ON agent_turn_runs (turn_id)
    WHERE status IN ('queued', 'running');

CREATE OR REPLACE FUNCTION enforce_agent_turn_context_item_scope()
RETURNS TRIGGER AS $$
DECLARE
    target_session UUID;
    target_project UUID;
    target_member UUID;
    target_turn_index INTEGER;
    source_project UUID;
    source_member UUID;
    expected_audience TEXT;
BEGIN
    SELECT t.session_id, t.project_id, s.member_id, t.turn_index
      INTO target_session, target_project, target_member, target_turn_index
      FROM agent_turn_context_snapshots snapshot
      JOIN agent_turns t ON t.id = snapshot.turn_id
      LEFT JOIN agent_chat_sessions s ON s.id = t.session_id
     WHERE snapshot.id = NEW.snapshot_id;

    -- Follow-up turns always reserve ordinal 0 for an immutable snapshot of
    -- this session's prior stable turns. Supplemental live selections are
    -- shifted by one only in that case; the first turn keeps their original
    -- ordinals because no prior session history exists yet.
    IF target_session IS NOT NULL
       AND NOT (
           target_turn_index > 1
           AND NEW.source_type = 'session'
           AND NEW.source_id = target_session
           AND NEW.ordinal = 0
       )
       AND NOT EXISTS (
        SELECT 1 FROM agent_session_context_sources selected
        WHERE selected.session_id = target_session
          AND selected.source_type = NEW.source_type
          AND selected.source_id = NEW.source_id
          AND selected.ordinal = CASE
              WHEN target_turn_index > 1 THEN NEW.ordinal - 1
              ELSE NEW.ordinal
          END
    ) THEN
        RAISE EXCEPTION 'snapshot item was not selected for this session';
    END IF;

    IF NEW.source_type = 'task' THEN
        SELECT project_id INTO source_project FROM tasks
         WHERE id = NEW.source_id AND deleted_at IS NULL;
        expected_audience := 'project_shared';
    ELSIF NEW.source_type = 'session' THEN
        SELECT project_id, member_id INTO source_project, source_member
          FROM agent_chat_sessions WHERE id = NEW.source_id;
        expected_audience := 'owner_private';
    ELSE
        SELECT t.project_id, s.member_id,
               CASE WHEN t.session_id IS NULL THEN 'project_shared' ELSE 'owner_private' END
          INTO source_project, source_member, expected_audience
          FROM agent_turn_runs r
          JOIN agent_turns t ON t.id = r.turn_id
          LEFT JOIN agent_chat_sessions s ON s.id = t.session_id
         WHERE r.id = NEW.source_id;
    END IF;

    IF source_project IS DISTINCT FROM target_project
       OR expected_audience IS DISTINCT FROM NEW.source_audience
       OR (expected_audience = 'owner_private' AND source_member IS DISTINCT FROM target_member) THEN
        RAISE EXCEPTION 'snapshot context source scope mismatch';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_context_item_scope ON agent_turn_context_items;
CREATE TRIGGER trg_agent_turn_context_item_scope
    BEFORE INSERT ON agent_turn_context_items
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_turn_context_item_scope();

CREATE OR REPLACE FUNCTION enforce_agent_turn_run_scope()
RETURNS TRIGGER AS $$
DECLARE
    expected_conversation UUID;
BEGIN
    SELECT conversation_id INTO expected_conversation
      FROM agent_turns WHERE id = NEW.turn_id;
    IF NOT FOUND OR expected_conversation IS DISTINCT FROM NEW.conversation_id THEN
        RAISE EXCEPTION 'turn run conversation does not match turn';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_run_scope ON agent_turn_runs;
CREATE TRIGGER trg_agent_turn_run_scope
    BEFORE INSERT OR UPDATE ON agent_turn_runs
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_turn_run_scope();

CREATE OR REPLACE FUNCTION enforce_agent_turn_run_update()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.turn_id IS DISTINCT FROM OLD.turn_id
       OR NEW.conversation_id IS DISTINCT FROM OLD.conversation_id
       OR NEW.backend IS DISTINCT FROM OLD.backend
       OR NEW.attempt IS DISTINCT FROM OLD.attempt
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'agent turn run identity is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_run_update ON agent_turn_runs;
CREATE TRIGGER trg_agent_turn_run_update
    BEFORE UPDATE ON agent_turn_runs
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_turn_run_update();

CREATE OR REPLACE FUNCTION enforce_agent_turn_run_state_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status IN ('succeeded','failed','stopped','cancelled','timed_out','no_output') THEN
        RAISE EXCEPTION 'terminal agent turn run is immutable';
    END IF;
    IF NOT (
        (OLD.status = 'queued' AND NEW.status IN ('queued', 'running', 'cancelled', 'timed_out'))
        OR (OLD.status = 'running' AND NEW.status IN (
            'running', 'succeeded', 'failed', 'stopped', 'cancelled', 'timed_out', 'no_output'
        ))
    ) THEN
        RAISE EXCEPTION 'invalid agent turn run state transition';
    END IF;
    IF NEW.status = 'queued' AND (
        NEW.claim_token IS NOT NULL OR NEW.claimed_by IS NOT NULL OR NEW.lease_expires_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'queued run cannot have a claim';
    END IF;
    IF NEW.status = 'running' AND (
        NEW.claim_token IS NULL OR NEW.claimed_by IS NULL OR NEW.lease_expires_at IS NULL
    ) THEN
        RAISE EXCEPTION 'running run requires a complete claim';
    END IF;
    IF NEW.status IN ('succeeded','failed','stopped','cancelled','timed_out','no_output')
       AND NEW.lease_expires_at IS NOT NULL THEN
        RAISE EXCEPTION 'terminal run cannot retain an active lease';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_run_state ON agent_turn_runs;
CREATE TRIGGER trg_agent_turn_run_state
    BEFORE UPDATE ON agent_turn_runs
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_turn_run_state_transition();

CREATE OR REPLACE FUNCTION enforce_agent_turn_state_transition()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status IN ('succeeded','failed','stopped','cancelled','timed_out','no_output') THEN
        RAISE EXCEPTION 'terminal agent turn is immutable';
    END IF;
    IF NOT (
        (OLD.status = 'queued' AND NEW.status IN ('queued', 'running', 'cancelled', 'timed_out'))
        OR (OLD.status = 'running' AND NEW.status IN (
            'running', 'succeeded', 'failed', 'stopped', 'cancelled', 'timed_out', 'no_output'
        ))
        OR OLD.status = NEW.status
    ) THEN
        RAISE EXCEPTION 'invalid agent turn state transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_state ON agent_turns;
CREATE TRIGGER trg_agent_turn_state
    BEFORE UPDATE ON agent_turns
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_turn_state_transition();

CREATE TABLE IF NOT EXISTS agent_turn_results (
    turn_id                 UUID        PRIMARY KEY,
    run_id                  UUID        NOT NULL UNIQUE,
    terminal_status         TEXT        NOT NULL
        CHECK (terminal_status IN ('succeeded', 'failed', 'stopped', 'cancelled', 'timed_out', 'no_output')),
    stable_output           TEXT,
    stable_output_sha256    TEXT,
    stable_output_event_id  UUID,
    generated_by_agent_id   UUID        NOT NULL,
    error_code              TEXT,
    error_message           TEXT,
    runtime_disposition     TEXT        NOT NULL DEFAULT 'retired'
        CHECK (runtime_disposition IN ('reusable', 'retired')),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_turn_result_turn
        FOREIGN KEY (turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_result_run
        FOREIGN KEY (run_id) REFERENCES agent_turn_runs(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_result_agent
        FOREIGN KEY (generated_by_agent_id) REFERENCES agents(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_turn_result_output_event
        FOREIGN KEY (stable_output_event_id) REFERENCES agent_conversation_events(id) ON DELETE RESTRICT,
    CONSTRAINT ck_agent_turn_result_stable_output CHECK (
        (terminal_status = 'succeeded' AND stable_output IS NOT NULL
            AND stable_output_sha256 IS NOT NULL AND length(stable_output_sha256) = 64
            AND stable_output_event_id IS NOT NULL)
        OR
        (terminal_status <> 'succeeded' AND stable_output IS NULL
            AND stable_output_sha256 IS NULL AND stable_output_event_id IS NULL)
    ),
    CONSTRAINT ck_agent_turn_result_output_bound CHECK (
        stable_output IS NULL OR octet_length(stable_output) <= 131072
    )
);

CREATE OR REPLACE FUNCTION enforce_agent_turn_result_scope()
RETURNS TRIGGER AS $$
DECLARE
    run_turn UUID;
    run_status TEXT;
    run_claim UUID;
    run_final_sequence INTEGER;
    turn_agent UUID;
    stable_type TEXT;
    stable_source TEXT;
    stable_payload JSONB;
    stable_claim UUID;
    stable_sequence INTEGER;
BEGIN
    SELECT turn_id, status, claim_token, final_event_sequence
      INTO run_turn, run_status, run_claim, run_final_sequence
      FROM agent_turn_runs WHERE id = NEW.run_id;
    SELECT agent_id INTO turn_agent FROM agent_turns WHERE id = NEW.turn_id;
    IF run_turn IS DISTINCT FROM NEW.turn_id
       OR run_status IS DISTINCT FROM NEW.terminal_status
       OR turn_agent IS DISTINCT FROM NEW.generated_by_agent_id THEN
        RAISE EXCEPTION 'turn result does not match terminal run';
    END IF;
    IF NEW.stable_output_event_id IS NOT NULL THEN
        SELECT e.event_type,e.event_source,e.payload,e.turn_claim_token,e.turn_sequence
          INTO stable_type,stable_source,stable_payload,stable_claim,stable_sequence
          FROM agent_conversation_events e
         WHERE e.id = NEW.stable_output_event_id
           AND e.turn_id = NEW.turn_id
           AND e.turn_run_id = NEW.run_id;
        IF NOT FOUND
           OR stable_type <> 'agent.turn.output.stable'
           OR stable_source <> 'agent'
           OR stable_claim IS DISTINCT FROM run_claim
           OR stable_sequence IS DISTINCT FROM run_final_sequence
           OR stable_payload->>'text' IS DISTINCT FROM NEW.stable_output THEN
            RAISE EXCEPTION 'stable output event does not match turn run';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_result_scope ON agent_turn_results;
CREATE TRIGGER trg_agent_turn_result_scope
    BEFORE INSERT ON agent_turn_results
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_turn_result_scope();

-- Conversation events remain the execution log, but new events are attributed
-- to both the authoritative turn and the fenced run attempt that emitted them.
ALTER TABLE agent_conversation_events
    ADD COLUMN IF NOT EXISTS turn_id UUID
        REFERENCES agent_turns(id) ON DELETE RESTRICT;
ALTER TABLE agent_conversation_events
    ADD COLUMN IF NOT EXISTS turn_run_id UUID
        REFERENCES agent_turn_runs(id) ON DELETE RESTRICT;
ALTER TABLE agent_conversation_events
    ADD COLUMN IF NOT EXISTS turn_sequence INTEGER;
ALTER TABLE agent_conversation_events
    ADD COLUMN IF NOT EXISTS turn_claim_token UUID;

DO $$ BEGIN
IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'ck_agent_conversation_event_turn_attribution'
      AND conrelid = 'agent_conversation_events'::regclass
) THEN
    ALTER TABLE agent_conversation_events
    ADD CONSTRAINT ck_agent_conversation_event_turn_attribution CHECK (
        (turn_id IS NULL AND turn_run_id IS NULL AND turn_sequence IS NULL AND turn_claim_token IS NULL)
        OR
        (turn_id IS NOT NULL AND turn_run_id IS NOT NULL AND turn_sequence IS NOT NULL AND turn_claim_token IS NOT NULL
            AND turn_sequence >= 0)
    );
END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_conversation_events_run_sequence
    ON agent_conversation_events (turn_run_id, turn_sequence)
    WHERE turn_run_id IS NOT NULL AND turn_sequence IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_agent_conversation_events_turn
    ON agent_conversation_events (turn_id, event_index)
    WHERE turn_id IS NOT NULL;

CREATE OR REPLACE FUNCTION enforce_agent_conversation_event_turn_scope()
RETURNS TRIGGER AS $$
DECLARE
    expected_turn UUID;
    expected_conversation UUID;
    expected_claim UUID;
    run_status TEXT;
    run_lease TIMESTAMPTZ;
    latest_attempt INTEGER;
    run_attempt INTEGER;
    turn_status TEXT;
    turn_deadline TIMESTAMPTZ;
BEGIN
    IF NEW.turn_run_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT turn_id, conversation_id, claim_token, status, lease_expires_at, attempt
      INTO expected_turn, expected_conversation, expected_claim, run_status, run_lease, run_attempt
      FROM agent_turn_runs WHERE id = NEW.turn_run_id;
    SELECT MAX(attempt) INTO latest_attempt FROM agent_turn_runs WHERE turn_id = expected_turn;
    SELECT status,deadline_at INTO turn_status,turn_deadline
      FROM agent_turns WHERE id = expected_turn;
    IF expected_turn IS DISTINCT FROM NEW.turn_id
       OR expected_conversation IS DISTINCT FROM NEW.conversation_id
       OR expected_claim IS DISTINCT FROM NEW.turn_claim_token
       OR run_status <> 'running'
       OR run_lease <= NOW()
       OR turn_status <> 'running'
       OR (turn_deadline IS NOT NULL AND turn_deadline <= NOW())
       OR run_attempt IS DISTINCT FROM latest_attempt THEN
        RAISE EXCEPTION 'conversation event does not match turn run';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_conversation_event_turn_scope ON agent_conversation_events;
CREATE TRIGGER trg_agent_conversation_event_turn_scope
    BEFORE INSERT OR UPDATE ON agent_conversation_events
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_conversation_event_turn_scope();

CREATE OR REPLACE FUNCTION reject_attributed_agent_event_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.turn_id IS NOT NULL
       OR (TG_OP = 'UPDATE' AND NEW.turn_id IS NOT NULL) THEN
        RAISE EXCEPTION 'turn-attributed conversation event is append-only';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_attributed_agent_event_immutable ON agent_conversation_events;
CREATE TRIGGER trg_attributed_agent_event_immutable
    BEFORE UPDATE OR DELETE ON agent_conversation_events
    FOR EACH ROW EXECUTE FUNCTION reject_attributed_agent_event_mutation();

-- The old table is retained as an internal/legacy recovery artifact. New
-- rows are idempotent by source_turn_id; legacy rows keep their original
-- per-conversation uniqueness without blocking multiple turns in one runtime.
ALTER TABLE agent_task_handoffs
    ADD COLUMN IF NOT EXISTS source_turn_id UUID
        REFERENCES agent_turns(id) ON DELETE RESTRICT;
ALTER TABLE agent_task_handoffs
    ADD COLUMN IF NOT EXISTS generated_by_agent_id UUID
        REFERENCES agents(id) ON DELETE RESTRICT;
ALTER TABLE agent_task_handoffs
    ADD COLUMN IF NOT EXISTS summary_sha256 TEXT;

DO $$ BEGIN
IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'ck_agent_task_handoff_turn_audit'
      AND conrelid = 'agent_task_handoffs'::regclass
) THEN
    ALTER TABLE agent_task_handoffs
        ADD CONSTRAINT ck_agent_task_handoff_turn_audit CHECK (
            source_turn_id IS NULL OR (
                generated_by_agent_id IS NOT NULL
                AND summary_sha256 ~ '^[0-9a-f]{64}$'
            )
        );
END IF;
END $$;

ALTER TABLE agent_task_handoffs
    DROP CONSTRAINT IF EXISTS uq_agent_task_handoffs_conversation;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_task_handoff_turn
    ON agent_task_handoffs (source_turn_id) WHERE source_turn_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_task_handoff_legacy_conversation
    ON agent_task_handoffs (conversation_id) WHERE source_turn_id IS NULL;

CREATE OR REPLACE FUNCTION enforce_agent_task_handoff_turn_scope()
RETURNS TRIGGER AS $$
DECLARE
    source_conversation UUID;
    source_agent UUID;
    source_task UUID;
    source_status TEXT;
BEGIN
    IF NEW.source_turn_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT t.conversation_id, t.agent_id, c.task_id, r.terminal_status
      INTO source_conversation, source_agent, source_task, source_status
      FROM agent_turns t
      JOIN agent_conversations c ON c.id = t.conversation_id
      JOIN agent_turn_results r ON r.turn_id = t.id
     WHERE t.id = NEW.source_turn_id;

    IF NOT FOUND
       OR source_conversation IS DISTINCT FROM NEW.conversation_id
       OR source_task IS DISTINCT FROM NEW.task_id
       OR source_agent IS DISTINCT FROM NEW.generated_by_agent_id
       OR source_status <> 'succeeded' THEN
        RAISE EXCEPTION 'task handoff does not match a successful source turn';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_task_handoff_turn_scope ON agent_task_handoffs;
CREATE TRIGGER trg_agent_task_handoff_turn_scope
    BEFORE INSERT OR UPDATE ON agent_task_handoffs
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_task_handoff_turn_scope();

CREATE TABLE IF NOT EXISTS agent_conclusion_preparations (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id             UUID        NOT NULL,
    source_turn_id         UUID        NOT NULL,
    target_task_id         UUID        NOT NULL,
    prepared_by_user_id    UUID        NOT NULL,
    prepared_by_member_id  UUID        NOT NULL,
    generated_by_agent_id  UUID        NOT NULL,
    publication_kind       TEXT        NOT NULL DEFAULT 'published'
        CONSTRAINT ck_agent_conclusion_preparation_kind
        CHECK (publication_kind IN ('published', 'revised', 'withdrawn')),
    related_publication_id UUID,
    summary                TEXT        NOT NULL
        CONSTRAINT ck_agent_conclusion_preparation_summary CHECK (
        length(btrim(summary)) > 0 AND octet_length(summary) <= 16384
    ),
    summary_version        INTEGER     NOT NULL DEFAULT 1
        CONSTRAINT ck_agent_conclusion_preparation_summary_version CHECK (summary_version > 0),
    summary_sha256         TEXT        NOT NULL
        CONSTRAINT ck_agent_conclusion_preparation_summary_sha256 CHECK (length(summary_sha256) = 64),
    update_description     BOOLEAN     NOT NULL DEFAULT FALSE,
    description_before     JSONB,
    description_before_sha256 TEXT,
    description_after      JSONB,
    description_after_sha256 TEXT,
    is_frozen              BOOLEAN     NOT NULL DEFAULT TRUE
        CONSTRAINT ck_agent_conclusion_preparation_frozen CHECK (is_frozen),
    state                  TEXT        NOT NULL DEFAULT 'prepared'
        CONSTRAINT ck_agent_conclusion_preparation_state
        CHECK (state IN ('prepared', 'confirmed', 'expired', 'superseded')),
    idempotency_key        TEXT        NOT NULL,
    request_sha256         TEXT        NOT NULL
        CONSTRAINT chk_agent_conclusion_preparation_request_sha256 CHECK (length(request_sha256) = 64),
    expires_at             TIMESTAMPTZ NOT NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_conclusion_preparation_project
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_preparation_turn
        FOREIGN KEY (source_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_preparation_task
        FOREIGN KEY (target_task_id) REFERENCES tasks(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_preparation_user
        FOREIGN KEY (prepared_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_preparation_member
        FOREIGN KEY (prepared_by_member_id) REFERENCES project_members(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_preparation_agent
        FOREIGN KEY (generated_by_agent_id) REFERENCES agents(id) ON DELETE RESTRICT,
    CONSTRAINT uq_agent_conclusion_preparation_request
        UNIQUE (prepared_by_user_id, idempotency_key),
    CONSTRAINT uq_agent_conclusion_preparation_version
        UNIQUE (source_turn_id, target_task_id, summary_version),
    CONSTRAINT ck_agent_conclusion_preparation_idempotency_key CHECK (
        length(btrim(idempotency_key)) BETWEEN 1 AND 200
    ),
    CONSTRAINT ck_agent_conclusion_preparation_relation CHECK (
        (publication_kind = 'published' AND related_publication_id IS NULL)
        OR (publication_kind IN ('revised', 'withdrawn') AND related_publication_id IS NOT NULL)
    ),
    CONSTRAINT ck_agent_conclusion_preparation_description_update CHECK (
        (NOT update_description AND description_before IS NULL
            AND description_before_sha256 IS NULL AND description_after IS NULL
            AND description_after_sha256 IS NULL)
        OR (update_description AND publication_kind <> 'withdrawn'
            AND description_before IS NOT NULL AND length(description_before_sha256) = 64
            AND description_after IS NOT NULL AND length(description_after_sha256) = 64)
    )
);

-- The early PACA-3 draft predated canonical prepare-command fingerprints.
-- Empty draft tables can be upgraded; populated rows cannot be made auditable
-- after the fact and therefore fail closed.
ALTER TABLE agent_conclusion_preparations
    ADD COLUMN IF NOT EXISTS request_sha256 TEXT;
ALTER TABLE agent_conclusion_preparations
    ADD COLUMN IF NOT EXISTS update_description BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS description_before JSONB,
    ADD COLUMN IF NOT EXISTS description_before_sha256 TEXT,
    ADD COLUMN IF NOT EXISTS description_after JSONB,
    ADD COLUMN IF NOT EXISTS description_after_sha256 TEXT;
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM agent_conclusion_preparations
        WHERE request_sha256 IS NULL
    ) THEN
        RAISE EXCEPTION 'agent_conclusion_preparations contains incompatible PACA-3 draft requests'
            USING HINT = 'Preserve the database for investigation; preparation request hashes cannot be reconstructed safely.';
    END IF;
END
$$;
ALTER TABLE agent_conclusion_preparations
    ALTER COLUMN request_sha256 SET NOT NULL;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid='agent_conclusion_preparations'::regclass
          AND conname='chk_agent_conclusion_preparation_request_sha256'
    ) THEN
        ALTER TABLE agent_conclusion_preparations
            ADD CONSTRAINT chk_agent_conclusion_preparation_request_sha256
            CHECK (length(request_sha256)=64);
    END IF;
END
$$;

-- CREATE TABLE IF NOT EXISTS does not add constraints to the earlier PACA-3
-- draft table. Rebuild the complete fresh-table CHECK, UNIQUE, and FK contract
-- explicitly; PostgreSQL validates existing rows while adding it, so an
-- incompatible populated draft fails closed and the whole migration rolls back.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='uq_agent_conclusion_preparation_request') THEN
        ALTER TABLE agent_conclusion_preparations
            ADD CONSTRAINT uq_agent_conclusion_preparation_request
            UNIQUE (prepared_by_user_id, idempotency_key);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='uq_agent_conclusion_preparation_version') THEN
        ALTER TABLE agent_conclusion_preparations
            ADD CONSTRAINT uq_agent_conclusion_preparation_version
            UNIQUE (source_turn_id, target_task_id, summary_version);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_idempotency_key') THEN
        ALTER TABLE agent_conclusion_preparations
            ADD CONSTRAINT ck_agent_conclusion_preparation_idempotency_key
            CHECK (length(btrim(idempotency_key)) BETWEEN 1 AND 200);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_relation') THEN
        ALTER TABLE agent_conclusion_preparations
            ADD CONSTRAINT ck_agent_conclusion_preparation_relation CHECK (
                (publication_kind='published' AND related_publication_id IS NULL)
                OR (publication_kind IN ('revised','withdrawn') AND related_publication_id IS NOT NULL)
            );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_description_update') THEN
        ALTER TABLE agent_conclusion_preparations
            ADD CONSTRAINT ck_agent_conclusion_preparation_description_update CHECK (
                (NOT update_description AND description_before IS NULL
                    AND description_before_sha256 IS NULL AND description_after IS NULL
                    AND description_after_sha256 IS NULL)
                OR (update_description AND publication_kind <> 'withdrawn'
                    AND description_before IS NOT NULL AND length(description_before_sha256) = 64
                    AND description_after IS NOT NULL AND length(description_after_sha256) = 64)
            );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_kind') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT ck_agent_conclusion_preparation_kind
            CHECK (publication_kind IN ('published','revised','withdrawn'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_summary') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT ck_agent_conclusion_preparation_summary
            CHECK (length(btrim(summary)) > 0 AND octet_length(summary) <= 16384);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_summary_version') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT ck_agent_conclusion_preparation_summary_version
            CHECK (summary_version > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_summary_sha256') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT ck_agent_conclusion_preparation_summary_sha256
            CHECK (length(summary_sha256) = 64);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_frozen') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT ck_agent_conclusion_preparation_frozen CHECK (is_frozen);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='ck_agent_conclusion_preparation_state') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT ck_agent_conclusion_preparation_state
            CHECK (state IN ('prepared','confirmed','expired','superseded'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='fk_agent_conclusion_preparation_project') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT fk_agent_conclusion_preparation_project
            FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='fk_agent_conclusion_preparation_turn') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT fk_agent_conclusion_preparation_turn
            FOREIGN KEY (source_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='fk_agent_conclusion_preparation_task') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT fk_agent_conclusion_preparation_task
            FOREIGN KEY (target_task_id) REFERENCES tasks(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='fk_agent_conclusion_preparation_user') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT fk_agent_conclusion_preparation_user
            FOREIGN KEY (prepared_by_user_id) REFERENCES users(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='fk_agent_conclusion_preparation_member') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT fk_agent_conclusion_preparation_member
            FOREIGN KEY (prepared_by_member_id) REFERENCES project_members(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_preparations'::regclass AND conname='fk_agent_conclusion_preparation_agent') THEN
        ALTER TABLE agent_conclusion_preparations ADD CONSTRAINT fk_agent_conclusion_preparation_agent
            FOREIGN KEY (generated_by_agent_id) REFERENCES agents(id) ON DELETE RESTRICT;
    END IF;
END
$$;

CREATE TABLE IF NOT EXISTS agent_conclusion_publications (
    id                       UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id               UUID        NOT NULL,
    target_task_id           UUID        NOT NULL,
    source_turn_id           UUID        NOT NULL,
    preparation_id           UUID        NOT NULL,
    published_by_user_id     UUID        NOT NULL,
    published_by_member_id   UUID        NOT NULL,
    generated_by_agent_id    UUID        NOT NULL,
    kind                     TEXT        NOT NULL DEFAULT 'published'
        CONSTRAINT ck_agent_conclusion_publication_kind
        CHECK (kind IN ('published', 'revised', 'withdrawn')),
    root_publication_id      UUID,
    revises_publication_id   UUID,
    withdraws_publication_id UUID,
    supersedes_publication_id UUID GENERATED ALWAYS AS (
        COALESCE(revises_publication_id, withdraws_publication_id)
    ) STORED,
    summary                  TEXT        NOT NULL
        CONSTRAINT ck_agent_conclusion_publication_summary CHECK (
        length(btrim(summary)) > 0 AND octet_length(summary) <= 16384
    ),
    summary_version          INTEGER     NOT NULL
        CONSTRAINT ck_agent_conclusion_publication_summary_version CHECK (summary_version > 0),
    summary_sha256           TEXT        NOT NULL
        CONSTRAINT ck_agent_conclusion_publication_summary_sha256 CHECK (length(summary_sha256) = 64),
    description_updated      BOOLEAN     NOT NULL DEFAULT FALSE,
    description_before_sha256 TEXT,
    description_after_sha256  TEXT,
    idempotency_key          TEXT        NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_conclusion_publication_project
        FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_task
        FOREIGN KEY (target_task_id) REFERENCES tasks(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_turn
        FOREIGN KEY (source_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_preparation
        FOREIGN KEY (preparation_id) REFERENCES agent_conclusion_preparations(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_user
        FOREIGN KEY (published_by_user_id) REFERENCES users(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_member
        FOREIGN KEY (published_by_member_id) REFERENCES project_members(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_agent
        FOREIGN KEY (generated_by_agent_id) REFERENCES agents(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_root
        FOREIGN KEY (root_publication_id) REFERENCES agent_conclusion_publications(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_revises
        FOREIGN KEY (revises_publication_id) REFERENCES agent_conclusion_publications(id) ON DELETE RESTRICT,
    CONSTRAINT fk_agent_conclusion_publication_withdraws
        FOREIGN KEY (withdraws_publication_id) REFERENCES agent_conclusion_publications(id) ON DELETE RESTRICT,
    CONSTRAINT uq_agent_conclusion_publication_request
        UNIQUE (published_by_user_id, idempotency_key),
    CONSTRAINT uq_agent_conclusion_publication_preparation UNIQUE (preparation_id),
    CONSTRAINT ck_agent_conclusion_publication_idempotency_key CHECK (
        length(btrim(idempotency_key)) BETWEEN 1 AND 200
    ),
    CONSTRAINT ck_agent_conclusion_publication_relation CHECK (
        (kind = 'published' AND root_publication_id IS NULL
            AND revises_publication_id IS NULL AND withdraws_publication_id IS NULL)
        OR (kind = 'revised' AND root_publication_id IS NOT NULL
            AND revises_publication_id IS NOT NULL AND withdraws_publication_id IS NULL)
        OR (kind = 'withdrawn' AND root_publication_id IS NOT NULL
            AND revises_publication_id IS NULL AND withdraws_publication_id IS NOT NULL)
    ),
    CONSTRAINT ck_agent_conclusion_publication_description_update CHECK (
        (NOT description_updated AND description_before_sha256 IS NULL
            AND description_after_sha256 IS NULL)
        OR (description_updated AND kind <> 'withdrawn'
            AND length(description_before_sha256) = 64
            AND length(description_after_sha256) = 64)
    )
);

-- A generated common parent closes the draft's cross-kind branch where one
-- revision and one withdrawal could otherwise supersede the same publication.
ALTER TABLE agent_conclusion_publications
    ADD COLUMN IF NOT EXISTS supersedes_publication_id UUID GENERATED ALWAYS AS (
        COALESCE(revises_publication_id, withdraws_publication_id)
    ) STORED;
ALTER TABLE agent_conclusion_publications
    ADD COLUMN IF NOT EXISTS description_updated BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS description_before_sha256 TEXT,
    ADD COLUMN IF NOT EXISTS description_after_sha256 TEXT;
DROP INDEX IF EXISTS uq_agent_conclusion_revision_parent;
DROP INDEX IF EXISTS uq_agent_conclusion_withdrawal_parent;

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_conclusion_superseded_parent
    ON agent_conclusion_publications (supersedes_publication_id)
    WHERE supersedes_publication_id IS NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='uq_agent_conclusion_publication_request') THEN
        ALTER TABLE agent_conclusion_publications
            ADD CONSTRAINT uq_agent_conclusion_publication_request
            UNIQUE (published_by_user_id, idempotency_key);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='uq_agent_conclusion_publication_preparation') THEN
        ALTER TABLE agent_conclusion_publications
            ADD CONSTRAINT uq_agent_conclusion_publication_preparation
            UNIQUE (preparation_id);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='ck_agent_conclusion_publication_idempotency_key') THEN
        ALTER TABLE agent_conclusion_publications
            ADD CONSTRAINT ck_agent_conclusion_publication_idempotency_key
            CHECK (length(btrim(idempotency_key)) BETWEEN 1 AND 200);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='ck_agent_conclusion_publication_relation') THEN
        ALTER TABLE agent_conclusion_publications
            ADD CONSTRAINT ck_agent_conclusion_publication_relation CHECK (
                (kind='published' AND root_publication_id IS NULL
                    AND revises_publication_id IS NULL AND withdraws_publication_id IS NULL)
                OR (kind='revised' AND root_publication_id IS NOT NULL
                    AND revises_publication_id IS NOT NULL AND withdraws_publication_id IS NULL)
                OR (kind='withdrawn' AND root_publication_id IS NOT NULL
                    AND revises_publication_id IS NULL AND withdraws_publication_id IS NOT NULL)
            );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='ck_agent_conclusion_publication_description_update') THEN
        ALTER TABLE agent_conclusion_publications
            ADD CONSTRAINT ck_agent_conclusion_publication_description_update CHECK (
                (NOT description_updated AND description_before_sha256 IS NULL
                    AND description_after_sha256 IS NULL)
                OR (description_updated AND kind <> 'withdrawn'
                    AND length(description_before_sha256) = 64
                    AND length(description_after_sha256) = 64)
            );
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='ck_agent_conclusion_publication_kind') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT ck_agent_conclusion_publication_kind
            CHECK (kind IN ('published','revised','withdrawn'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='ck_agent_conclusion_publication_summary') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT ck_agent_conclusion_publication_summary
            CHECK (length(btrim(summary)) > 0 AND octet_length(summary) <= 16384);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='ck_agent_conclusion_publication_summary_version') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT ck_agent_conclusion_publication_summary_version
            CHECK (summary_version > 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='ck_agent_conclusion_publication_summary_sha256') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT ck_agent_conclusion_publication_summary_sha256
            CHECK (length(summary_sha256) = 64);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_project') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_project
            FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_task') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_task
            FOREIGN KEY (target_task_id) REFERENCES tasks(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_turn') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_turn
            FOREIGN KEY (source_turn_id) REFERENCES agent_turns(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_preparation') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_preparation
            FOREIGN KEY (preparation_id) REFERENCES agent_conclusion_preparations(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_user') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_user
            FOREIGN KEY (published_by_user_id) REFERENCES users(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_member') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_member
            FOREIGN KEY (published_by_member_id) REFERENCES project_members(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_agent') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_agent
            FOREIGN KEY (generated_by_agent_id) REFERENCES agents(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_root') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_root
            FOREIGN KEY (root_publication_id) REFERENCES agent_conclusion_publications(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_revises') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_revises
            FOREIGN KEY (revises_publication_id) REFERENCES agent_conclusion_publications(id) ON DELETE RESTRICT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid='agent_conclusion_publications'::regclass AND conname='fk_agent_conclusion_publication_withdraws') THEN
        ALTER TABLE agent_conclusion_publications ADD CONSTRAINT fk_agent_conclusion_publication_withdraws
            FOREIGN KEY (withdraws_publication_id) REFERENCES agent_conclusion_publications(id) ON DELETE RESTRICT;
    END IF;
END
$$;

DO $$ BEGIN
IF NOT EXISTS (
    SELECT 1 FROM pg_constraint
    WHERE conname = 'fk_agent_conclusion_preparation_related_publication'
      AND conrelid = 'agent_conclusion_preparations'::regclass
) THEN
    ALTER TABLE agent_conclusion_preparations
        ADD CONSTRAINT fk_agent_conclusion_preparation_related_publication
        FOREIGN KEY (related_publication_id)
        REFERENCES agent_conclusion_publications(id) ON DELETE RESTRICT;
END IF;
END $$;

CREATE OR REPLACE FUNCTION enforce_agent_conclusion_preparation_scope()
RETURNS TRIGGER AS $$
DECLARE
    source_project UUID;
    source_member UUID;
    source_agent UUID;
    target_project UUID;
    actor_user UUID;
    actor_project UUID;
    related_project UUID;
    related_task UUID;
BEGIN
    SELECT t.project_id, s.member_id, r.generated_by_agent_id
      INTO source_project, source_member, source_agent
      FROM agent_turns t
      JOIN agent_chat_sessions s ON s.id = t.session_id
      JOIN agent_turn_results r ON r.turn_id = t.id
       AND r.terminal_status = 'succeeded'
     WHERE t.id = NEW.source_turn_id;
    SELECT project_id INTO target_project
      FROM tasks WHERE id = NEW.target_task_id AND deleted_at IS NULL;
    SELECT user_id, project_id INTO actor_user, actor_project
      FROM project_members
     WHERE id = NEW.prepared_by_member_id AND deleted_at IS NULL;

    IF source_project IS DISTINCT FROM NEW.project_id
       OR source_member IS DISTINCT FROM NEW.prepared_by_member_id
       OR source_agent IS DISTINCT FROM NEW.generated_by_agent_id
       OR target_project IS DISTINCT FROM NEW.project_id
       OR actor_user IS DISTINCT FROM NEW.prepared_by_user_id
       OR actor_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'conclusion preparation scope mismatch';
    END IF;

    IF NEW.related_publication_id IS NOT NULL THEN
        SELECT project_id, target_task_id
          INTO related_project, related_task
          FROM agent_conclusion_publications
         WHERE id = NEW.related_publication_id;
        IF related_project IS DISTINCT FROM NEW.project_id
           OR related_task IS DISTINCT FROM NEW.target_task_id THEN
            RAISE EXCEPTION 'related conclusion publication scope mismatch';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_conclusion_preparation_scope ON agent_conclusion_preparations;
CREATE TRIGGER trg_agent_conclusion_preparation_scope
    BEFORE INSERT ON agent_conclusion_preparations
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_conclusion_preparation_scope();

CREATE OR REPLACE FUNCTION enforce_agent_conclusion_preparation_update()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.project_id IS DISTINCT FROM OLD.project_id
       OR NEW.source_turn_id IS DISTINCT FROM OLD.source_turn_id
       OR NEW.target_task_id IS DISTINCT FROM OLD.target_task_id
       OR NEW.prepared_by_user_id IS DISTINCT FROM OLD.prepared_by_user_id
       OR NEW.prepared_by_member_id IS DISTINCT FROM OLD.prepared_by_member_id
       OR NEW.generated_by_agent_id IS DISTINCT FROM OLD.generated_by_agent_id
       OR NEW.publication_kind IS DISTINCT FROM OLD.publication_kind
       OR NEW.related_publication_id IS DISTINCT FROM OLD.related_publication_id
       OR NEW.summary IS DISTINCT FROM OLD.summary
       OR NEW.summary_version IS DISTINCT FROM OLD.summary_version
       OR NEW.summary_sha256 IS DISTINCT FROM OLD.summary_sha256
       OR NEW.update_description IS DISTINCT FROM OLD.update_description
       OR NEW.description_before IS DISTINCT FROM OLD.description_before
       OR NEW.description_before_sha256 IS DISTINCT FROM OLD.description_before_sha256
       OR NEW.description_after IS DISTINCT FROM OLD.description_after
       OR NEW.description_after_sha256 IS DISTINCT FROM OLD.description_after_sha256
       OR NEW.is_frozen IS DISTINCT FROM OLD.is_frozen
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.request_sha256 IS DISTINCT FROM OLD.request_sha256
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'conclusion preparation content is immutable';
    END IF;
    IF NOT (
        NEW.state = OLD.state
        OR (OLD.state = 'prepared' AND NEW.state IN ('confirmed', 'expired', 'superseded'))
    ) THEN
        RAISE EXCEPTION 'invalid conclusion preparation state transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_conclusion_preparation_update ON agent_conclusion_preparations;
CREATE TRIGGER trg_agent_conclusion_preparation_update
    BEFORE UPDATE ON agent_conclusion_preparations
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_conclusion_preparation_update();

CREATE INDEX IF NOT EXISTS idx_agent_conclusion_publications_task
    ON agent_conclusion_publications (target_task_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_agent_conclusion_publications_turn
    ON agent_conclusion_publications (source_turn_id, created_at, id);

CREATE OR REPLACE FUNCTION enforce_agent_conclusion_publication_scope()
RETURNS TRIGGER AS $$
DECLARE
    prep agent_conclusion_preparations%ROWTYPE;
    related agent_conclusion_publications%ROWTYPE;
    expected_root UUID;
BEGIN
    SELECT * INTO prep FROM agent_conclusion_preparations
     WHERE id = NEW.preparation_id;
    IF NOT FOUND
       OR prep.project_id IS DISTINCT FROM NEW.project_id
       OR prep.target_task_id IS DISTINCT FROM NEW.target_task_id
       OR prep.source_turn_id IS DISTINCT FROM NEW.source_turn_id
       OR prep.prepared_by_user_id IS DISTINCT FROM NEW.published_by_user_id
       OR prep.prepared_by_member_id IS DISTINCT FROM NEW.published_by_member_id
       OR prep.generated_by_agent_id IS DISTINCT FROM NEW.generated_by_agent_id
       OR prep.publication_kind IS DISTINCT FROM NEW.kind
       OR prep.summary IS DISTINCT FROM NEW.summary
       OR prep.summary_version IS DISTINCT FROM NEW.summary_version
       OR prep.summary_sha256 IS DISTINCT FROM NEW.summary_sha256
       OR prep.update_description IS DISTINCT FROM NEW.description_updated
       OR prep.description_before_sha256 IS DISTINCT FROM NEW.description_before_sha256
       OR prep.description_after_sha256 IS DISTINCT FROM NEW.description_after_sha256
       OR prep.state <> 'prepared'
       OR NOT prep.is_frozen
       OR prep.expires_at <= NOW() THEN
        RAISE EXCEPTION 'publication does not match frozen preparation';
    END IF;

    IF NEW.kind = 'published' THEN
        RETURN NEW;
    END IF;
    SELECT * INTO related FROM agent_conclusion_publications
     WHERE id = prep.related_publication_id;
    expected_root := COALESCE(related.root_publication_id, related.id);
    IF NOT FOUND
       OR related.project_id IS DISTINCT FROM NEW.project_id
       OR related.target_task_id IS DISTINCT FROM NEW.target_task_id
       OR NEW.root_publication_id IS DISTINCT FROM expected_root
       OR (NEW.kind = 'revised' AND NEW.revises_publication_id IS DISTINCT FROM related.id)
       OR (NEW.kind = 'withdrawn' AND NEW.withdraws_publication_id IS DISTINCT FROM related.id) THEN
        RAISE EXCEPTION 'publication revision chain mismatch';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_conclusion_publication_scope ON agent_conclusion_publications;
CREATE TRIGGER trg_agent_conclusion_publication_scope
    BEFORE INSERT ON agent_conclusion_publications
    FOR EACH ROW EXECUTE FUNCTION enforce_agent_conclusion_publication_scope();

CREATE TABLE IF NOT EXISTS agent_outbox_events (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_type  TEXT        NOT NULL,
    aggregate_id    UUID        NOT NULL,
    event_type      TEXT        NOT NULL,
    payload         JSONB       NOT NULL,
    idempotency_key TEXT        NOT NULL UNIQUE,
    status          TEXT        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'publishing', 'published', 'dead')),
    attempts        INTEGER     NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    locked_at       TIMESTAMPTZ,
    locked_by       TEXT,
    lock_token      UUID,
    lock_expires_at TIMESTAMPTZ,
    published_at    TIMESTAMPTZ,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_agent_outbox_idempotency_key CHECK (
        length(btrim(idempotency_key)) BETWEEN 1 AND 240
    ),
    CONSTRAINT ck_agent_outbox_lock CHECK (
        (status = 'publishing' AND locked_at IS NOT NULL AND locked_by IS NOT NULL
            AND lock_token IS NOT NULL AND lock_expires_at IS NOT NULL)
        OR
        (status <> 'publishing' AND locked_at IS NULL AND locked_by IS NULL
            AND lock_token IS NULL AND lock_expires_at IS NULL)
    )
);

-- Upgrade the draft worker lock to a token-fenced lease. A draft publishing
-- row has no trustworthy claim identity, so requeue it for safe idempotent
-- delivery before enforcing the stronger invariant.
ALTER TABLE agent_outbox_events ADD COLUMN IF NOT EXISTS lock_token UUID;
ALTER TABLE agent_outbox_events ADD COLUMN IF NOT EXISTS lock_expires_at TIMESTAMPTZ;
UPDATE agent_outbox_events
   SET status='pending', locked_at=NULL, locked_by=NULL,
       lock_token=NULL, lock_expires_at=NULL,
       available_at=LEAST(available_at, NOW())
 WHERE status='publishing'
   AND (lock_token IS NULL OR lock_expires_at IS NULL);
ALTER TABLE agent_outbox_events DROP CONSTRAINT IF EXISTS ck_agent_outbox_lock;
ALTER TABLE agent_outbox_events
    ADD CONSTRAINT ck_agent_outbox_lock CHECK (
        (status = 'publishing' AND locked_at IS NOT NULL AND locked_by IS NOT NULL
            AND lock_token IS NOT NULL AND lock_expires_at IS NOT NULL)
        OR
        (status <> 'publishing' AND locked_at IS NULL AND locked_by IS NULL
            AND lock_token IS NULL AND lock_expires_at IS NULL)
    );

CREATE INDEX IF NOT EXISTS idx_agent_outbox_pending
    ON agent_outbox_events (available_at, created_at)
    WHERE status IN ('pending', 'publishing');

-- Audit facts must never be overwritten in place. Context source selection and
-- preparations remain mutable by design; snapshots/items/results/publications do not.
CREATE OR REPLACE FUNCTION reject_agent_audit_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_agent_turn_snapshot_immutable ON agent_turn_context_snapshots;
CREATE TRIGGER trg_agent_turn_snapshot_immutable
    BEFORE UPDATE OR DELETE ON agent_turn_context_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_agent_audit_mutation();

DROP TRIGGER IF EXISTS trg_agent_turn_context_item_immutable ON agent_turn_context_items;
CREATE TRIGGER trg_agent_turn_context_item_immutable
    BEFORE UPDATE OR DELETE ON agent_turn_context_items
    FOR EACH ROW EXECUTE FUNCTION reject_agent_audit_mutation();

DROP TRIGGER IF EXISTS trg_agent_turn_result_immutable ON agent_turn_results;
CREATE TRIGGER trg_agent_turn_result_immutable
    BEFORE UPDATE OR DELETE ON agent_turn_results
    FOR EACH ROW EXECUTE FUNCTION reject_agent_audit_mutation();

DROP TRIGGER IF EXISTS trg_agent_conclusion_publication_immutable ON agent_conclusion_publications;
CREATE TRIGGER trg_agent_conclusion_publication_immutable
    BEFORE UPDATE OR DELETE ON agent_conclusion_publications
    FOR EACH ROW EXECUTE FUNCTION reject_agent_audit_mutation();

COMMIT;
