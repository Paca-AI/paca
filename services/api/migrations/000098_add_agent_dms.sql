-- A direct message with a global agent is answered by the agent: what the person
-- writes is forwarded to the agent's chat session, and the agent's reply is
-- posted back into the channel as a message of the agent's own account.
--
-- chat_agent_dms is the link between that channel and the agent runtime:
--   session_id      the agent chat session the channel talks to (created on the
--                   first message, reused for the following ones),
--   forwarded_seq   the newest channel message already handed to the agent, so a
--                   message written while the agent is busy is sent when it is free,
--   conversation_id the run whose reply is being waited for,
--   bridged_index   the last event of that run already turned into a message.
CREATE TABLE IF NOT EXISTS chat_agent_dms (
    channel_id      UUID        PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    agent_id        UUID        NOT NULL,
    account_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    session_id      UUID        REFERENCES agent_chat_sessions(id) ON DELETE SET NULL,
    forwarded_seq   BIGINT      NOT NULL DEFAULT 0,
    conversation_id UUID,
    bridged_index   INTEGER     NOT NULL DEFAULT -1,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_chat_agent_dms_session ON chat_agent_dms (session_id) WHERE session_id IS NOT NULL;

-- The agent run a message reports on (its "View run" link).
ALTER TABLE messages ADD COLUMN IF NOT EXISTS run_id UUID;
