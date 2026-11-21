-- Agents in channels (docs/architecture/chat-and-meetings.md, Phase 2): an
-- @mention of an agent in a project channel starts an agent run, and when the
-- run's turn ends its answer is posted back as a reply in the mention's thread.
--
-- chat_agent_runs links the run to the message that started it:
--   conversation_id    the agent run (agent_conversations.id),
--   source_message_id  the message that mentioned the agent,
--   requester_id       the person who wrote it, mentioned in the reply,
--   post_claimed_at    when a worker began posting the reply: a lease, so a worker
--                      that dies mid-post does not lose the answer for good,
--   posted             the reply is stored (so a repeated turn_end adds nothing).
-- The run page stays the full record (reasoning, tool calls); the message links
-- to it through messages.run_id.
CREATE TABLE IF NOT EXISTS chat_agent_runs (
    conversation_id   UUID        PRIMARY KEY,
    channel_id        UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    source_message_id UUID        NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    thread_root_id    UUID        NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    agent_id          UUID        NOT NULL,
    requester_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    post_claimed_at   TIMESTAMPTZ,
    posted            BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_chat_agent_runs_channel ON chat_agent_runs (channel_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_chat_agent_runs_agent ON chat_agent_runs (agent_id, created_at DESC);
-- The mention rate limit and the run list look runs up by who started them.
CREATE INDEX IF NOT EXISTS idx_chat_agent_runs_requester ON chat_agent_runs (requester_id, created_at DESC);
