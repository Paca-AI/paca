-- 000023_add_acp_bridge_token_hash_index.sql
-- Adds a unique index on agents.acp_bridge_token_hash. It's looked up on
-- every ACP bridge WebSocket handshake (see services/ai-agent's
-- find_agent_by_bridge_token_hash, called from routes/bridge.py before
-- accept()) and previously had no index at all, forcing a full table scan
-- per connection attempt. Unique because a token hash collision between two
-- agents would let one agent's bridge daemon authenticate as another.
--
-- Partial (agent_type = 'acp' AND deleted_at IS NULL) since the column is
-- only ever set for non-deleted ACP agents; NULL values (LLM agents, or ACP
-- agents that haven't generated a token yet) don't participate in a unique
-- index's uniqueness check, so this is safe to add without a backfill.

BEGIN;

CREATE UNIQUE INDEX IF NOT EXISTS uq_agents_acp_bridge_token_hash
    ON agents (acp_bridge_token_hash)
    WHERE agent_type = 'acp' AND deleted_at IS NULL;

COMMIT;
