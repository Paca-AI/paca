-- 000043_add_conversation_acp_session_id.sql
-- Adds agent_conversations.acp_session_id: the goose ACP sessionId
-- (session/new's result) an environment-backed LLM conversation's first
-- turn creates, persisted so every later turn can call session/load
-- instead of session/new — giving goose its own conversation memory back
-- across turns (session/new always starts blank, which was silently
-- discarding every earlier turn's context) instead of cold-starting a
-- fresh, history-less session on every single reply. Written directly by
-- agent-runner's own Postgres connection (see
-- ConversationRepository.GetACPSessionID/SetACPSessionID), never by
-- services/api — same convention as agent_conversations.status/
-- iteration_count.
--
-- NULL for every conversation this doesn't apply to: an ephemeral
-- (non-environment) LLM conversation keeps its live session entirely in
-- agent-runner's own in-memory chatsandbox.Registry instead (see
-- Handler.keepSandboxAlive's EnvironmentID guard), and an ACP-type agent's
-- session is owned by apps/acp-bridge, never this table at all.
--
-- IF NOT EXISTS so this migration is safe to re-run.

BEGIN;

ALTER TABLE agent_conversations ADD COLUMN IF NOT EXISTS acp_session_id TEXT;

COMMIT;
