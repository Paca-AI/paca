-- Team chat (docs/architecture/chat-and-meetings.md), in one migration:
-- core, role statements, direct messages, mentions/follows/saved, stars and
-- agent DMs. Every statement is guarded, so it is safe to re-run.

-- Chat core (docs/architecture/chat-and-meetings.md): channels, members,
-- messages and reactions.
--
-- A channel lives in one scope. Only project channels are served by the API so
-- far; the workspace scope is in the schema so DMs and workspace channels do
-- not need another migration. messages.seq is gapless per channel: it is
-- allocated by incrementing channels.last_seq in the inserting transaction, so
-- clients catch up with "after_seq".

CREATE TABLE IF NOT EXISTS channels (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    scope       TEXT        NOT NULL CHECK (scope IN ('workspace', 'project')),
    project_id  UUID        REFERENCES projects(id) ON DELETE CASCADE,
    kind        TEXT        NOT NULL CHECK (kind IN ('public', 'private', 'dm', 'group_dm', 'voice')),
    name        TEXT        NOT NULL DEFAULT '',
    topic       TEXT        NOT NULL DEFAULT '',
    last_seq    BIGINT      NOT NULL DEFAULT 0,
    archived_at TIMESTAMPTZ,
    created_by  UUID        REFERENCES accounts(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_channels_scope_project CHECK ((scope = 'project') = (project_id IS NOT NULL))
);

-- A name is unique among the live named channels of a project.
CREATE UNIQUE INDEX IF NOT EXISTS uq_channels_project_name
    ON channels (project_id, lower(name))
    WHERE archived_at IS NULL AND kind IN ('public', 'private', 'voice');
CREATE INDEX IF NOT EXISTS idx_channels_project ON channels (project_id) WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS channel_members (
    channel_id        UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    account_id        UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    last_read_seq     BIGINT      NOT NULL DEFAULT 0,
    notification_pref TEXT        NOT NULL DEFAULT 'default' CHECK (notification_pref IN ('default', 'all', 'mentions', 'none')),
    joined_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_channel_members_account ON channel_members (account_id);

CREATE TABLE IF NOT EXISTS messages (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id        UUID        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    seq               BIGINT      NOT NULL,
    thread_root_id    UUID        REFERENCES messages(id) ON DELETE CASCADE,
    author_account_id UUID        NOT NULL REFERENCES accounts(id),
    content           JSONB       NOT NULL,
    content_text      TEXT        NOT NULL DEFAULT '',
    edited_at         TIMESTAMPTZ,
    deleted_at        TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_messages_channel_seq UNIQUE (channel_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages (thread_root_id, seq) WHERE thread_root_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS message_reactions (
    message_id UUID        NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    account_id UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    emoji      TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (message_id, account_id, emoji)
);

-- Every existing project gets its #general channel.
INSERT INTO channels (scope, project_id, kind, name, topic)
SELECT 'project', p.id, 'public', 'general', ''
FROM projects p
WHERE p.deleted_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM channels c WHERE c.project_id = p.id AND lower(c.name) = 'general' AND c.archived_at IS NULL);

-- Gives the project roles that already exist the chat statements the shipped
-- templates now carry (a collection statement on the project itself, which the
-- channel list and create routes are gated on, and the per-channel ones) (internal/bootstrap/defaultroles/policies). Editors read
-- and write public channels and the private ones they belong to; Analysts and
-- Viewers read them. The project Admin role holds "*" on its project and needs
-- nothing. A role that already has a statement with sid ChatPublicChannels is
-- left alone, so this is safe to re-run and never overwrites an edit.

UPDATE roles r
SET policy = jsonb_set(
        r.policy, '{statements}',
        COALESCE(r.policy->'statements', '[]'::jsonb) || jsonb_build_array(
            jsonb_build_object(
                'sid', 'ChatCollection', 'effect', 'Allow',
                'actions', CASE WHEN r.name = 'Editor'
                                THEN '["channels:read","channels:write"]'::jsonb
                                ELSE '["channels:read"]'::jsonb END,
                'resources', jsonb_build_array('project/' || r.project_id::text)),
            jsonb_build_object(
                'sid', 'ChatPublicChannels', 'effect', 'Allow',
                'actions', CASE WHEN r.name = 'Editor'
                                THEN '["channels:read","channels:write","messages:write"]'::jsonb
                                ELSE '["channels:read"]'::jsonb END,
                'resources', jsonb_build_array('project/' || r.project_id::text || '/channel/*'),
                'conditions', '{"In":{"channel.kind":["public","voice"]}}'::jsonb),
            jsonb_build_object(
                'sid', 'ChatPrivateChannels', 'effect', 'Allow',
                'actions', CASE WHEN r.name = 'Editor'
                                THEN '["channels:read","channels:write","messages:write"]'::jsonb
                                ELSE '["channels:read"]'::jsonb END,
                'resources', jsonb_build_array('project/' || r.project_id::text || '/channel/*'),
                'conditions', '{"StringEquals":{"channel.kind":["private"],"channel.member_ids":["${principal.id}"]}}'::jsonb))),
    updated_at = NOW()
WHERE r.project_id IS NOT NULL
  AND r.name IN ('Editor', 'Analyst', 'Viewer')
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_array_elements(COALESCE(r.policy->'statements', '[]'::jsonb)) s
      WHERE s->>'sid' = 'ChatPublicChannels');

-- Direct messages (docs/architecture/chat-and-meetings.md): a one-to-one
-- conversation between two accounts of any kind (user, agent, service
-- account). It is a workspace-scope channel of kind 'dm' with no project.
--
-- dm_key is the two account ids in sorted order, so a pair has exactly one DM
-- whichever side starts it (find-or-create is race-safe on the unique index).

ALTER TABLE channels ADD COLUMN IF NOT EXISTS dm_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_channels_dm_key ON channels (dm_key) WHERE dm_key IS NOT NULL;

ALTER TABLE channels DROP CONSTRAINT IF EXISTS chk_channels_dm;
ALTER TABLE channels ADD CONSTRAINT chk_channels_dm CHECK (
    (kind = 'dm') = (dm_key IS NOT NULL)
    AND (kind NOT IN ('dm', 'group_dm') OR scope = 'workspace')
);

-- Every account may start and read direct messages it belongs to, through the
-- platform role every account holds. The shipped USER policy now carries the
-- statements; roles that already exist get them here, unless an administrator
-- already has (a role with a DirectMessages statement is left alone). The
-- ChatCollection statement also grants messages:write on the bare collection:
-- it lets a client show the composer; every conversation is still judged by the
-- DirectMessages statement (membership).
UPDATE roles r
SET policy = jsonb_set(
        r.policy, '{statements}',
        COALESCE(r.policy->'statements', '[]'::jsonb) || jsonb_build_array(
            jsonb_build_object(
                'sid', 'ChatCollection', 'effect', 'Allow',
                'actions', '["channels:read","channels:write","messages:write"]'::jsonb,
                'resources', '["channel"]'::jsonb),
            jsonb_build_object(
                'sid', 'DirectMessages', 'effect', 'Allow',
                'actions', '["channels:read","channels:write","messages:write"]'::jsonb,
                'resources', '["channel/*"]'::jsonb,
                'conditions', '{"StringEquals":{"channel.kind":["dm"],"channel.member_ids":["${principal.id}"]}}'::jsonb))),
    updated_at = NOW()
WHERE r.name = 'USER' AND r.project_id IS NULL AND r.is_system
  AND NOT EXISTS (
      SELECT 1 FROM jsonb_array_elements(COALESCE(r.policy->'statements', '[]'::jsonb)) s
      WHERE s->>'sid' = 'DirectMessages');

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

-- A member can star a channel or direct message: starred ones are listed first,
-- in their own section of the chat sidebar. Starring a channel the account does
-- not follow yet makes it follow it.
ALTER TABLE channel_members ADD COLUMN IF NOT EXISTS starred BOOLEAN NOT NULL DEFAULT FALSE;

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
