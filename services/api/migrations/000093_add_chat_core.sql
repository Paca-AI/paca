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
