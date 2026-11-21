-- Voice and video calls (docs/architecture/chat-and-meetings.md, Phase 3).
--
-- A call is a LiveKit room named after a channel id: a voice channel is a call
-- people drop into, and a huddle is the same room opened from any channel or
-- direct message. At most one call is open per channel. LiveKit's webhooks
-- (room_started / participant_joined / participant_left / room_finished) keep
-- these tables in step with who is actually connected.

CREATE TABLE IF NOT EXISTS call_sessions (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    room_sid   TEXT        NOT NULL DEFAULT '',
    started_by UUID        REFERENCES accounts(id) ON DELETE SET NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at   TIMESTAMPTZ
);

-- One open call per channel: a second join finds the first.
CREATE UNIQUE INDEX IF NOT EXISTS uq_call_sessions_open ON call_sessions (channel_id) WHERE ended_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_call_sessions_channel ON call_sessions (channel_id, started_at DESC);

CREATE TABLE IF NOT EXISTS call_participants (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID        NOT NULL REFERENCES call_sessions(id) ON DELETE CASCADE,
    account_id UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at    TIMESTAMPTZ
);

-- An account is in a call once, however many tabs it connects from.
CREATE UNIQUE INDEX IF NOT EXISTS uq_call_participants_open ON call_participants (session_id, account_id) WHERE left_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_call_participants_account ON call_participants (account_id);

-- Gives the roles that already exist the calls:join action the shipped
-- templates now carry: Editors join calls in the channels they can use, and
-- every account joins the direct messages it belongs to. Moderation
-- (calls:moderate) is held by the project Admin role through "*" and is not
-- granted here.
UPDATE roles r
SET policy = jsonb_set(
        r.policy, '{statements}',
        (SELECT jsonb_agg(
                    CASE WHEN s->>'sid' IN ('ChatPublicChannels', 'ChatPrivateChannels')
                              AND NOT (s->'actions') ? 'calls:join'
                         THEN jsonb_set(s, '{actions}', (s->'actions') || '"calls:join"'::jsonb)
                         ELSE s END
                    ORDER BY ord)
         FROM jsonb_array_elements(r.policy->'statements') WITH ORDINALITY AS t(s, ord))),
    updated_at = NOW()
WHERE r.project_id IS NOT NULL
  AND r.name = 'Editor'
  AND EXISTS (
      SELECT 1 FROM jsonb_array_elements(COALESCE(r.policy->'statements', '[]'::jsonb)) s
      WHERE s->>'sid' IN ('ChatPublicChannels', 'ChatPrivateChannels')
        AND NOT (s->'actions') ? 'calls:join');

UPDATE roles r
SET policy = jsonb_set(
        r.policy, '{statements}',
        (SELECT jsonb_agg(
                    CASE WHEN s->>'sid' = 'DirectMessages'
                              AND NOT (s->'actions') ? 'calls:join'
                         THEN jsonb_set(s, '{actions}', (s->'actions') || '"calls:join"'::jsonb)
                         ELSE s END
                    ORDER BY ord)
         FROM jsonb_array_elements(r.policy->'statements') WITH ORDINALITY AS t(s, ord))),
    updated_at = NOW()
WHERE r.name = 'USER' AND r.project_id IS NULL AND r.is_system
  AND EXISTS (
      SELECT 1 FROM jsonb_array_elements(COALESCE(r.policy->'statements', '[]'::jsonb)) s
      WHERE s->>'sid' = 'DirectMessages'
        AND NOT (s->'actions') ? 'calls:join');
