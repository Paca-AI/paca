-- 000079_add_acp_model_effort.sql
-- Optional per-agent model and reasoning-effort overrides for ACP agents.
-- Empty string means "use the provider's own default"; apps/acp-bridge applies
-- a non-empty value after session/new, only if the session actually offers it.

BEGIN;

ALTER TABLE agents
    ADD COLUMN acp_model  TEXT NOT NULL DEFAULT '',
    ADD COLUMN acp_effort TEXT NOT NULL DEFAULT '';

COMMIT;
