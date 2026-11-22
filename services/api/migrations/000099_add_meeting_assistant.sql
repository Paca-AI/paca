-- Meeting assistant (docs/architecture/chat-and-meetings.md, Phase 3b).
--
-- 1. A fourth agent type, "meeting": an agent account whose runtime is the
--    LiveKit worker (services/meeting-agent), not agent-runner. It reuses the
--    llm_* columns for its reasoning model; everything specific to listening
--    and behaviour lives in agent_meeting_settings.
-- 2. call_agent_sessions: which assistant was invited into which call, by
--    whom and for whom (the requester gets its proposals).
-- 3. call_transcript_segments: finalized speech turns, flushed by the worker.
-- 4. agent_proposals: what an assistant proposes; a person's browser performs
--    the write after approving (see "Forwarded tool calls" in the doc).
-- 5. The "Meeting assistant" project role for every existing project. The
--    policy must match internal/bootstrap/defaultroles/policies/
--    project_meeting_assistant.json with PROJECT_ID replaced.

BEGIN;

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_agent_type_check;
ALTER TABLE agents ADD CONSTRAINT agents_agent_type_check
    CHECK (agent_type IN ('llm', 'acp', 'provider_cli', 'meeting'));

ALTER TABLE agents DROP CONSTRAINT IF EXISTS ck_agents_acp_requires_provider;
ALTER TABLE agents ADD CONSTRAINT ck_agents_acp_requires_provider
    CHECK (
        agent_type IN ('llm', 'meeting')
        OR (agent_type = 'acp' AND acp_provider IS NOT NULL)
        OR (agent_type = 'provider_cli' AND cli_provider IS NOT NULL)
    );

CREATE TABLE IF NOT EXISTS agent_meeting_settings (
    agent_id                  UUID        PRIMARY KEY REFERENCES agents(id) ON DELETE CASCADE,
    stt_provider              TEXT        NOT NULL DEFAULT '',
    stt_model                 TEXT        NOT NULL DEFAULT '',
    stt_api_key_secret        TEXT        NOT NULL DEFAULT '',
    stt_base_url              TEXT        NOT NULL DEFAULT '',
    stt_languages             TEXT[]      NOT NULL DEFAULT '{}',
    mode                      TEXT        NOT NULL DEFAULT 'on_demand'
                              CHECK (mode IN ('on_demand', 'auto_suggest', 'notes_only')),
    suggest_interval_seconds  INTEGER     NOT NULL DEFAULT 120 CHECK (suggest_interval_seconds BETWEEN 30 AND 3600),
    proposal_kinds            TEXT[]      NOT NULL DEFAULT '{task.create,task.update,task.comment,doc.create_from_notes}',
    max_proposals_per_call    INTEGER     NOT NULL DEFAULT 10 CHECK (max_proposals_per_call BETWEEN 0 AND 100),
    store_transcript          BOOLEAN     NOT NULL DEFAULT TRUE,
    transcript_retention_days INTEGER     NOT NULL DEFAULT 30 CHECK (transcript_retention_days BETWEEN 1 AND 3650),
    announcement              TEXT        NOT NULL DEFAULT '',
    summary_target            TEXT        NOT NULL DEFAULT 'channel_message'
                              CHECK (summary_target IN ('none', 'channel_message')),
    default_target            TEXT        NOT NULL DEFAULT 'requester'
                              CHECK (default_target IN ('requester', 'host')),
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS call_agent_sessions (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    call_session_id UUID        NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
    agent_id        UUID        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    invited_by      UUID        REFERENCES accounts(id) ON DELETE SET NULL,
    dispatch_id     TEXT        NOT NULL DEFAULT '',
    -- 'dispatched' until the worker fetches its settings, then 'active', then
    -- 'left'. 'failed' when dispatch itself failed.
    status          TEXT        NOT NULL DEFAULT 'dispatched'
                    CHECK (status IN ('dispatched', 'active', 'left', 'failed')),
    joined_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at         TIMESTAMPTZ
);
-- An assistant is in a call once.
CREATE UNIQUE INDEX IF NOT EXISTS uq_call_agent_sessions_open
    ON call_agent_sessions (call_session_id, agent_id) WHERE left_at IS NULL;

CREATE TABLE IF NOT EXISTS call_transcript_segments (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    call_session_id UUID        NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
    channel_id      UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    speaker_id      UUID        REFERENCES accounts(id) ON DELETE SET NULL,
    text            TEXT        NOT NULL,
    spoken_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Deleted by the retention sweep once older than the agent's retention.
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_call_transcript_session ON call_transcript_segments (call_session_id, spoken_at);
CREATE INDEX IF NOT EXISTS idx_call_transcript_expiry ON call_transcript_segments (expires_at);

CREATE TABLE IF NOT EXISTS agent_proposals (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    channel_id        UUID        REFERENCES channels(id) ON DELETE CASCADE,
    call_session_id   UUID        REFERENCES call_sessions(id) ON DELETE SET NULL,
    agent_account_id  UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    target_account_id UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    kind              TEXT        NOT NULL,
    payload           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    status            TEXT        NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'claimed', 'resolved', 'rejected', 'expired', 'failed')),
    result            JSONB       NOT NULL DEFAULT '{}'::jsonb,
    resolved_by       UUID        REFERENCES accounts(id) ON DELETE SET NULL,
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_agent_proposals_target ON agent_proposals (target_account_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_proposals_channel ON agent_proposals (channel_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_agent_proposals_call ON agent_proposals (call_session_id);

INSERT INTO roles (name, description, policy, project_id, is_system)
SELECT 'Meeting assistant',
       'For meeting agents: read tasks, sprints and docs, and propose changes for people to approve.',
       replace($policy${
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "ReadProject",
      "effect": "Allow",
      "actions": [
        "projects:read"
      ],
      "resources": [
        "project/PROJECT_ID"
      ]
    },
    {
      "sid": "Read",
      "effect": "Allow",
      "actions": [
        "docs:read",
        "project.members:read",
        "sprints:read",
        "tasks:read"
      ],
      "resources": [
        "project/PROJECT_ID/*"
      ]
    },
    {
      "sid": "Propose",
      "effect": "Allow",
      "actions": [
        "proposals:write"
      ],
      "resources": [
        "project/PROJECT_ID",
        "project/PROJECT_ID/proposal/*"
      ]
    },
    {
      "sid": "ChatCollection",
      "effect": "Allow",
      "actions": [
        "channels:read"
      ],
      "resources": [
        "project/PROJECT_ID"
      ]
    },
    {
      "sid": "ChatPublicChannels",
      "effect": "Allow",
      "actions": [
        "channels:read",
        "messages:write"
      ],
      "resources": [
        "project/PROJECT_ID/channel/*"
      ],
      "conditions": {
        "In": {
          "channel.kind": [
            "public",
            "voice"
          ]
        }
      }
    }
  ]
}$policy$, 'PROJECT_ID', p.id::text)::jsonb,
       p.id,
       false
  FROM projects p
 WHERE NOT EXISTS (
        SELECT 1 FROM roles r WHERE r.project_id = p.id AND r.name = 'Meeting assistant');

-- The Editor and Analyst roles that already exist get the two statements the
-- shipped templates now carry: bring an assistant into a call, and review the
-- proposals an assistant made for them (the condition is what keeps it to their
-- own). Added only where the statement is missing.
UPDATE roles r
SET policy = jsonb_set(r.policy, '{statements}',
        (r.policy->'statements')
        || (CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(r.policy->'statements') s WHERE s->>'sid' = 'InviteAssistants')
                 THEN '[]'::jsonb
                 ELSE jsonb_build_array(jsonb_build_object(
                        'sid', 'InviteAssistants', 'effect', 'Allow',
                        'actions', jsonb_build_array('calls:invite_agent'),
                        'resources', jsonb_build_array('project/' || r.project_id::text, 'project/' || r.project_id::text || '/agent/*'))) END)
        || (CASE WHEN EXISTS (SELECT 1 FROM jsonb_array_elements(r.policy->'statements') s WHERE s->>'sid' = 'ResolveOwnProposals')
                 THEN '[]'::jsonb
                 ELSE jsonb_build_array(jsonb_build_object(
                        'sid', 'ResolveOwnProposals', 'effect', 'Allow',
                        'actions', jsonb_build_array('proposals:resolve'),
                        'resources', jsonb_build_array('project/' || r.project_id::text || '/proposal/*'),
                        'conditions', jsonb_build_object('StringEquals', jsonb_build_object('proposal.target_id', jsonb_build_array('${principal.id}'))))) END)),
    updated_at = NOW()
WHERE r.project_id IS NOT NULL AND r.name IN ('Editor', 'Analyst');

COMMIT;
