-- 000046_add_agent_default_folder.sql
-- Adds default_folder_id: which folder inside an agent's default_environment_id
-- (migration 000042_add_environments.sql) the agent's conversations should
-- work in by default, alongside default_environment_id itself — a folder
-- only ever exists inside one specific environment, so this column is
-- meaningless without default_environment_id also being set (enforced by
-- services/api, not a DB constraint — see agentdom.Agent.DefaultFolderID's
-- doc comment). ON DELETE SET NULL, same as default_environment_id: a
-- deleted folder just falls back to no default rather than blocking the
-- delete.
--
-- IF NOT EXISTS so this migration is safe to re-run.

BEGIN;

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS default_folder_id UUID
    REFERENCES environment_folders(id)
    ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_agents_default_folder_id
    ON agents (default_folder_id)
    WHERE default_folder_id IS NOT NULL;

COMMIT;
