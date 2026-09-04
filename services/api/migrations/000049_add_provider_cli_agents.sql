-- 000049_add_provider_cli_agents.sql
-- Adds the provider_cli agent type: an agent that runs via Goose's own
-- "CLI providers" feature (GOOSE_PROVIDER=claude-code|codex|cursor-agent|
-- gemini-cli), shelling out to a locally-authenticated coding CLI instead
-- of calling a model API directly. See agentdom.Agent's doc comment on
-- CLIProvider for the full design.
--
-- cli_provider mirrors acp_provider's shape (nullable varchar, validated in
-- application code against a fixed set — see agentdom.ValidCLIProviders)
-- but is a distinct column/concept: acp_provider names which local ACP
-- client the *user's own machine* runs (apps/acp-bridge); cli_provider
-- names which CLI Goose itself shells out to *inside* the agent-runner
-- sandbox/environment container.
--
-- cli_auth_mode defaults to 'login' (the only path every provider
-- supports today) rather than 'api_key' (unsupported for cursor-agent, and
-- requires an extra encrypted field even for the providers that do support
-- it).
--
-- cli_login_verified_at is set only by the "Verify login" action (a file-
-- existence probe inside the agent's default environment — never by
-- actually invoking the CLI) — advisory/informational only, never
-- re-validated automatically, so it can go stale if a login later expires;
-- surfaced to the user as a "last verified" timestamp, not a guarantee.
--
-- cli_model and cli_api_key_secret are NOT NULL DEFAULT '' rather than
-- nullable, matching the existing llm_model/llm_api_key_secret convention
-- (see 000008_add_ai_agents.sql) — the Go repository scans every agent row
-- through one shared agentRecord struct regardless of agent_type, and
-- these two are plain (non-pointer) string fields there, same as their LLM
-- counterparts; cli_provider stays nullable, matching acp_provider's own
-- *string treatment, since unlike the other two it's meaningful metadata
-- in its own right (nil vs. "" is a real distinction worth keeping).
--
-- IF NOT EXISTS throughout so this migration is safe to re-run.
--
-- The ADD COLUMN clause below intentionally does NOT carry the NOT NULL
-- DEFAULT '' constraint for cli_model/cli_api_key_secret — this file has
-- no version tracking (see database.RunMigrationsFS: every *.sql file
-- re-runs on every startup, unconditionally, and must be idempotent on its
-- own), and ADD COLUMN IF NOT EXISTS is a no-op — constraints included —
-- once the column already exists. An earlier revision of this same file
-- (during this feature's own development) added these two columns as
-- plain nullable VARCHAR/TEXT; once that ran once against a real database,
-- no later edit to the ADD COLUMN line could ever retroactively add the
-- constraint, which is exactly what produced a live
-- "converting NULL to string is unsupported" scan error. The ALTER COLUMN
-- block below is the fix: it backfills any existing NULLs and (re-)applies
-- the NOT NULL DEFAULT '' constraint unconditionally, regardless of
-- whether ADD COLUMN just created the column or found it already there —
-- safe to re-run indefinitely, since UPDATE ... WHERE ... IS NULL and
-- SET DEFAULT/SET NOT NULL are themselves idempotent.

BEGIN;

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS cli_provider VARCHAR(32),
    ADD COLUMN IF NOT EXISTS cli_model VARCHAR(255),
    ADD COLUMN IF NOT EXISTS cli_auth_mode VARCHAR(16) NOT NULL DEFAULT 'login',
    ADD COLUMN IF NOT EXISTS cli_api_key_secret TEXT,
    ADD COLUMN IF NOT EXISTS cli_login_verified_at TIMESTAMPTZ;

UPDATE agents SET cli_model = '' WHERE cli_model IS NULL;
ALTER TABLE agents ALTER COLUMN cli_model SET DEFAULT '';
ALTER TABLE agents ALTER COLUMN cli_model SET NOT NULL;

UPDATE agents SET cli_api_key_secret = '' WHERE cli_api_key_secret IS NULL;
ALTER TABLE agents ALTER COLUMN cli_api_key_secret SET DEFAULT '';
ALTER TABLE agents ALTER COLUMN cli_api_key_secret SET NOT NULL;

-- 000022_add_acp_agents.sql left two CHECK constraints on agents that both
-- need widening for provider_cli, neither of which ADD COLUMN IF NOT
-- EXISTS above can touch (they're constraints on already-existing columns,
-- not something this migration adds):
--
-- 1. agent_type's own CHECK, declared inline
--    (`agent_type TEXT ... CHECK (agent_type IN ('llm','acp'))`) with no
--    explicit name, so Postgres auto-named it agents_agent_type_check —
--    the exact constraint a live INSERT of a provider_cli row hit
--    ("violates check constraint agents_agent_type_check"). A CHECK
--    constraint can only be widened by dropping and re-adding it (there is
--    no ALTER CONSTRAINT ... for this), so this explicitly re-declares it
--    under that same auto-generated name — safe to re-run: dropping a
--    constraint that's already gone, or re-adding one identical to what's
--    already there, are both no-ops.
--
-- 2. ck_agents_acp_requires_provider, which enforces "a non-llm agent must
--    carry the provider info its type needs" — originally written as
--    `agent_type = 'llm' OR acp_provider IS NOT NULL`, which would reject
--    every provider_cli row outright (they set cli_provider, never
--    acp_provider). Replaced with a version that checks each type against
--    its own provider column.
ALTER TABLE agents DROP CONSTRAINT IF EXISTS agents_agent_type_check;
ALTER TABLE agents ADD CONSTRAINT agents_agent_type_check
    CHECK (agent_type IN ('llm', 'acp', 'provider_cli'));

ALTER TABLE agents DROP CONSTRAINT IF EXISTS ck_agents_acp_requires_provider;
ALTER TABLE agents ADD CONSTRAINT ck_agents_acp_requires_provider
    CHECK (
        agent_type = 'llm'
        OR (agent_type = 'acp' AND acp_provider IS NOT NULL)
        OR (agent_type = 'provider_cli' AND cli_provider IS NOT NULL)
    );

COMMIT;
