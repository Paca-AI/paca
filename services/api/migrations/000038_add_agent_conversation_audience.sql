-- Explicit transcript audience for agent conversations (generated column).
--
-- Up to this point "who may read a project conversation" was hard-coded in
-- the API layer (everything project-scoped is project-shared), with global
-- chat keyed separately by actor_user_id. That leaves no way to express a
-- task-bound, owner-private chat. This migration adds a first-class
-- discriminator so every read/write path can enforce one consistent rule.
--
-- audience is a pure function of existing columns, so it is a STORED
-- GENERATED column rather than a manually-maintained one:
--   * chat_session_id IS NOT NULL -> a chat_message conversation backed by a
--     chat session (project chat) — owner is that session's member.
--   * actor_user_id   IS NOT NULL -> a global-chat conversation — owner is
--     that user.
--   * both NULL                    -> a sessionless run (task_assigned /
--     comment_mention / description_write / automation_message):
--     project-shared.
--
-- Deriving it (instead of storing + DEFAULT 'project_shared') avoids a
-- fail-open default: an unset/forgotten audience can never make a private
-- chat over-public, and the value can never drift from its source columns.
--
-- The attachment/result scope (task_shared vs session_private) is a separate
-- dimension introduced by the #397 attachment work and is NOT covered here.

BEGIN;

ALTER TABLE agent_conversations
    ADD COLUMN IF NOT EXISTS audience TEXT
    GENERATED ALWAYS AS (
        CASE
            WHEN chat_session_id IS NOT NULL OR actor_user_id IS NOT NULL
            THEN 'owner_private'
            ELSE 'project_shared'
        END
    ) STORED;

COMMIT;
