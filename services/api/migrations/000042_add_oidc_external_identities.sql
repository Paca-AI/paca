-- 000042_add_oidc_external_identities.sql
-- OIDC SSO support. user_external_identities maps an external IdP identity
-- (issuer, subject) — the only stable identity key the OIDC spec guarantees —
-- to an internal Paca user. users.password_login_enabled marks JIT-provisioned
-- SSO-only accounts: they carry an unknown random password hash and must never
-- be able to authenticate (or set) a local password by accident.

BEGIN;

CREATE TABLE IF NOT EXISTS user_external_identities (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID        NOT NULL,
    provider      TEXT        NOT NULL DEFAULT 'oidc',
    issuer        TEXT        NOT NULL,
    subject       TEXT        NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_external_identity_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);

-- The stable external identity key: exactly one Paca user per (issuer, sub).
CREATE UNIQUE INDEX IF NOT EXISTS uni_external_identity_issuer_subject
    ON user_external_identities (issuer, subject);

CREATE INDEX IF NOT EXISTS idx_external_identity_user
    ON user_external_identities (user_id);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS password_login_enabled BOOLEAN NOT NULL DEFAULT TRUE;

COMMIT;
