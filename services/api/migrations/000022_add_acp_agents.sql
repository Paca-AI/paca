-- 000022_add_acp_agents.sql
-- Adds ACP (Agent Client Protocol) agent support: a second agent "shape"
-- that delegates to a coding CLI (Claude Code, Codex, Gemini CLI, or a
-- custom ACP server) the user runs themselves, locally, in their own
-- already-checked-out source folder — connected to Paca via a local bridge
-- daemon (apps/acp-bridge) over an authenticated WebSocket. No source code
-- is ever cloned into Paca's infrastructure for this agent type.
--
-- ACP agents don't use llm_provider/llm_model/llm_api_key_secret/llm_base_url
-- (those columns stay NOT NULL and are simply stored as '' for acp rows,
-- matching how llm_base_url already defaults to '' today) — no existing
-- constraints need loosening.

BEGIN;

ALTER TABLE agents ADD COLUMN agent_type TEXT NOT NULL DEFAULT 'llm'
    CHECK (agent_type IN ('llm', 'acp'));

ALTER TABLE agents ADD COLUMN acp_provider TEXT
    CHECK (acp_provider IN ('claude-code', 'codex', 'gemini-cli', 'custom'));

-- JSON array of the command + args used to launch the ACP server, e.g.
-- '["npx","-y","@zed-industries/codex-acp"]'. Only required when
-- acp_provider = 'custom'; built-in providers resolve a default command via
-- the OpenHands SDK's own provider registry.
ALTER TABLE agents ADD COLUMN acp_command JSONB NOT NULL DEFAULT '[]'::jsonb;

-- SHA-256 hex digest of the current local-bridge auth token. The plaintext
-- token is shown to the user once (at generation/regeneration time) and never
-- persisted — this column only supports verifying a presented token, not
-- recovering it.
ALTER TABLE agents ADD COLUMN acp_bridge_token_hash TEXT;

ALTER TABLE agents ADD CONSTRAINT ck_agents_acp_requires_provider
    CHECK (agent_type = 'llm' OR acp_provider IS NOT NULL);

COMMIT;
