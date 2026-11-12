-- 000085_add_sso_group_policy.sql
-- Lets an admin restrict an OIDC provider to identities that carry an allowed
-- group / role value in a claim of the verified ID token (for example Entra ID
-- group object IDs in "groups", or Keycloak roles in "realm_access.roles").
--
-- group_policy_enabled defaults to false, so existing providers behave exactly
-- as before; group_claim and allowed_groups are ignored until it is turned on.
-- allowed_groups is a JSON array of strings.

BEGIN;

ALTER TABLE oidc_providers
	ADD COLUMN IF NOT EXISTS group_policy_enabled BOOLEAN NOT NULL DEFAULT false,
	ADD COLUMN IF NOT EXISTS group_claim          TEXT    NOT NULL DEFAULT '',
	ADD COLUMN IF NOT EXISTS allowed_groups       JSONB   NOT NULL DEFAULT '[]';

COMMIT;
