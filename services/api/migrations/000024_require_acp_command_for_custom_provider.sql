-- 000024_require_acp_command_for_custom_provider.sql
-- Enforces "acp_provider = 'custom' requires a non-empty acp_command" at the
-- DB level, mirroring the existing Go-layer validation in agent_service.go's
-- CreateAgent/UpdateAgent (ErrACPCommandRequired). Without this, a direct DB
-- write (or a future code path that bypasses the service layer) could leave
-- a custom-provider ACP agent with an empty command that acp_dispatch.py has
-- nothing to launch.
--
-- Existence-guarded so this is safe to re-run, matching 000022's convention.

BEGIN;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ck_agents_custom_requires_command'
    ) THEN
        ALTER TABLE agents ADD CONSTRAINT ck_agents_custom_requires_command
            CHECK (acp_provider IS DISTINCT FROM 'custom' OR jsonb_array_length(acp_command) > 0);
    END IF;
END $$;

COMMIT;
