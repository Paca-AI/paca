-- 000022_add_user_oidc_identity.sql
-- ADR-038 (Galaxy identity workstream): adds optional identity-provider
-- columns to users so accounts can be linked to the Vortex OIDC issuer.
--
-- Both columns are nullable — local password accounts are unaffected.
-- Uniqueness is enforced only for non-null values via partial unique
-- indexes, so any number of rows may leave them unset.

BEGIN;

ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS oidc_sub TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uni_users_email
    ON users (email)
    WHERE email IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uni_users_oidc_sub
    ON users (oidc_sub)
    WHERE oidc_sub IS NOT NULL;

COMMIT;
