-- 000058_add_conversation_title.sql
-- Adds a user/Goose-visible name to a conversation, plus a soft-delete
-- column, so the Conversations list (apps/web) can show a name and let the
-- user rename/delete a conversation.
--
-- title is populated one of two ways:
--   - agent-runner best-effort copies it from the underlying Goose ACP
--     session's own title (`_goose/unstable/session/info`) after each turn
--     — see internal/acp.Client.SessionInfo and
--     internal/repository/postgres.ConversationRepository.
--     UpdateTitleFromGoose in services/agent-runner.
--   - the user renames the conversation directly (PATCH .../conversations/
--     :conversationId), which sets title_set_by_user so agent-runner's
--     best-effort copy above never clobbers it again.
-- NULL means "no name yet" — the frontend falls back to its existing
-- agent-name display in that case, so no default needs generating/storing
-- here.
--
-- deleted_at is a soft delete, mirroring agents.deleted_at: a hard DELETE
-- would race agent-runner's own async teardown (still writing
-- agent_conversation_events/status against this conversation_id after a
-- stop is requested) and worker.AgentQueueConsumer's terminal-status
-- lookup (which must keep resolving this row to advance the agent's
-- parallelism queue). Deleted rows are filtered out of the list/get
-- endpoints in the service layer, not hidden from internal lookups.
--
-- IF NOT EXISTS throughout so this migration is safe to re-run.

BEGIN;

ALTER TABLE agent_conversations
    ADD COLUMN IF NOT EXISTS title TEXT,
    ADD COLUMN IF NOT EXISTS title_set_by_user BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

COMMIT;
