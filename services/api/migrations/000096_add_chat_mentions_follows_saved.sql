-- Chat: mentions, followed threads and saved messages (the Inbox's Mentions,
-- Threads and Saved tabs; docs/architecture/chat-and-meetings.md).

-- Who a message mentions. Written with the message and rewritten when it is
-- edited. A mention is unread while its message is past the mentioned
-- account's read pointer in that channel.
CREATE TABLE IF NOT EXISTS message_mentions (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_message_mentions_account ON message_mentions (account_id, message_id);

-- The threads an account follows. last_read_seq is the newest reply (a channel
-- seq) it has seen. An account follows a thread it replies in, is mentioned in
-- or starts, and can follow or unfollow any by hand.
CREATE TABLE IF NOT EXISTS thread_follows (
    account_id      UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    root_message_id UUID        NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    last_read_seq   BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, root_message_id)
);
CREATE INDEX IF NOT EXISTS idx_thread_follows_root ON thread_follows (root_message_id);

CREATE TABLE IF NOT EXISTS saved_messages (
    account_id UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_id UUID        NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, message_id)
);
CREATE INDEX IF NOT EXISTS idx_saved_messages_account ON saved_messages (account_id, created_at DESC);
