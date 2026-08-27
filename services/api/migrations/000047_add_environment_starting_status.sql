-- 000047_add_environment_starting_status.sql
-- Adds "starting" to environments.status' CHECK constraint —
-- environmentdom.StatusStarting, the transitional status
-- environmentsvc.Service.StartEnvironment now sets immediately after
-- queuing a start command onto StreamEnvironmentCommands, before
-- worker.EnvironmentCommandConsumer has actually asked agent-runner to
-- start the backing container/Pod. Needed because that agent-runner call
-- can legitimately take longer than an HTTP request should stay open (see
-- StartEnvironment's own doc comment) — moving it off the request path
-- means the environment sits in a distinct "queued, not yet running"
-- state in the meantime, not "running" (not true yet) and not "stopped"
-- (would look like nothing happened).
--
-- The constraint has no explicit name in 000042_add_environments.sql, so
-- Postgres assigned it the default "environments_status_check" — dropped
-- and recreated here, IF EXISTS, so this migration is safe to re-run
-- (see database.RunMigrationsFS's own doc comment: there is no
-- migration-tracking table, every file here re-runs on every startup).

BEGIN;

ALTER TABLE environments DROP CONSTRAINT IF EXISTS environments_status_check;
ALTER TABLE environments ADD CONSTRAINT environments_status_check
    CHECK (status IN ('creating', 'starting', 'running', 'stopping', 'stopped', 'suspended', 'error', 'deleting'));

COMMIT;
