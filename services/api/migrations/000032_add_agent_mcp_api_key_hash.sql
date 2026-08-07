-- 000032_add_agent_mcp_api_key_hash.sql
-- Adds a per-agent MCP API key, mirroring acp_bridge_token_hash (000022 /
-- 000023): only its SHA-256 hash is persisted, the plaintext is shown once
-- at generation time, and generating a new one overwrites the old hash so
-- the previous key immediately stops authenticating. Used by an ACP agent's
-- MCP connect command (PACA_API_KEY) so its tool calls are attributed to
-- the agent itself, resolved directly from the key via
-- FindAgentByMCPAPIKeyHash — unlike the old shared AGENT_API_KEY, this key
-- is bound to exactly one agent and carries no impersonation risk for any
-- other agent.
--
-- Partial + unique for the same reason as uq_agents_acp_bridge_token_hash:
-- NULL (no key generated yet) doesn't participate in the uniqueness check,
-- so this is safe to add without a backfill; unique so a hash collision
-- can never let one agent authenticate as another.

BEGIN;

ALTER TABLE agents ADD COLUMN IF NOT EXISTS mcp_api_key_hash TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_agents_mcp_api_key_hash
    ON agents (mcp_api_key_hash)
    WHERE agent_type = 'acp' AND deleted_at IS NULL;

COMMIT;
