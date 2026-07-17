-- 000023_add_user_is_service.sql
-- ADR-038 (Galaxy user-directory sync): marks non-human service/bridge
-- accounts (sdd-sensor, support-bridge, pm-bridge, galaxy-tasks-agent, …) so
-- UIs can badge them and the directory-sync reconciler can recognize them
-- structurally instead of by username convention alone.
--
-- Default FALSE — every existing and future human account is unaffected.

BEGIN;

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_service BOOLEAN NOT NULL DEFAULT FALSE;

COMMIT;
