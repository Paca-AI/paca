-- 000037_add_goose_acp_provider.sql
-- Adds 'goose' to the set of built-in acp_provider values (Go-layer
-- validation already updated in agentdom.ValidACPProviders/ACPProviderGoose).
-- Goose isn't in the OpenHands SDK's own ACP provider registry, unlike
-- claude-code/codex/gemini-cli, so apps/acp-bridge's runner.py resolves its
-- default command (`goose acp`) via a small local override instead — see
-- docs/ai-agent/goose-migration.md.
--
-- The original CHECK from 000022_add_acp_agents.sql was added inline via
-- `ADD COLUMN ... CHECK (...)`, so Postgres auto-named it
-- agents_acp_provider_check (the standard <table>_<column>_check
-- convention for an unnamed column constraint).
--
-- Existence-guarded so this is safe to re-run, matching 000022/000024's
-- convention.

BEGIN;

ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_acp_provider_check;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_agents_acp_provider'
    ) THEN
        ALTER TABLE agents ADD CONSTRAINT ck_agents_acp_provider
            CHECK (acp_provider IN ('claude-code', 'codex', 'gemini-cli', 'goose', 'custom'));
    END IF;
END $$;

COMMIT;
