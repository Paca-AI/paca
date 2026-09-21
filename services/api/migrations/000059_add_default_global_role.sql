-- 000059_add_default_global_role.sql
-- Lets one global role be marked as the default: the role a new user starts
-- with, and a new global agent too, until someone with global_roles.assign
-- changes it.
--
-- Until now the API hardcoded the role *named* 'USER' for every new account,
-- so deleting that role (allowed once no user held it) made creating a user
-- fail. The default is data now, like task_statuses.is_default: at most one
-- global role is the default (the partial unique index below), the service
-- refuses to delete it, and the API changes it with
-- PUT /admin/global-roles/:roleId/set-default.
--
-- New accounts have always been created as USER, so USER is the default of
-- every existing deployment. If a deployment has already deleted USER there
-- is no default until the next start: seedDefaultRoles re-creates the built-in
-- roles and marks USER as the default when none is set.
--
-- IF NOT EXISTS throughout so this migration is safe to re-run.

BEGIN;

ALTER TABLE global_roles
    ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;

-- At most one default global role.
CREATE UNIQUE INDEX IF NOT EXISTS uq_global_roles_one_default
    ON global_roles (is_default) WHERE is_default = true;

UPDATE global_roles
   SET is_default = true
 WHERE name = 'USER'
   AND NOT EXISTS (SELECT 1 FROM global_roles WHERE is_default = true);

COMMIT;
