-- 000086_add_sso_only_mode.sql
-- SSO-only mode: an instance policy that turns off password sign-in for every
-- account except the default super admin, so a user removed at the identity
-- provider cannot keep signing in with a local password.
--
-- users.bootstrap_admin marks the default super admin: the account the API
-- seeds from ADMIN_USERNAME / ADMIN_PASSWORD at startup (see seedAdmin). It is
-- set by the API on every start, never through the API, and at most one
-- account holds it. Other SUPER_ADMIN or admin accounts are not exempt.
--
-- auth_security_settings.sso_only defaults to false, so existing installs are
-- unchanged until an admin turns it on.

BEGIN;

ALTER TABLE users
	ADD COLUMN IF NOT EXISTS bootstrap_admin BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS uni_users_bootstrap_admin
	ON users ((TRUE)) WHERE bootstrap_admin;

ALTER TABLE auth_security_settings
	ADD COLUMN IF NOT EXISTS sso_only BOOLEAN NOT NULL DEFAULT FALSE;

COMMIT;
