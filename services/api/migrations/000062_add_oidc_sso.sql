-- 000062_add_oidc_sso.sql
-- Adds SSO / OpenID Connect sign-in: admin-configured identity providers
-- (oidc_providers) and the link between a Paca account and the identity a
-- provider asserts for it (user_identities).
--
-- client_secret holds the OAuth client secret encrypted with ENCRYPTION_KEY
-- (see secret.Encryptor), or plaintext when no key is configured — the same
-- fallback every other at-rest secret in this schema uses. It is never
-- returned by the API.
--
-- scopes and allowed_domains are JSON arrays of strings. An empty
-- allowed_domains means "any email domain".
--
-- A user_identities row is keyed by (provider_id, subject): the OIDC "sub"
-- claim is the only identifier a provider guarantees to be stable and unique,
-- so it — not the email — is what signs a returning user back in. Deleting a
-- provider deletes its links; deleting a user deletes theirs.

BEGIN;

CREATE TABLE IF NOT EXISTS oidc_providers (
	id              UUID        PRIMARY KEY,
	slug            TEXT        NOT NULL,
	display_name    TEXT        NOT NULL,
	issuer_url      TEXT        NOT NULL,
	client_id       TEXT        NOT NULL,
	client_secret   TEXT        NOT NULL DEFAULT '',
	scopes          JSONB       NOT NULL DEFAULT '["openid","profile","email"]',
	enabled         BOOLEAN     NOT NULL DEFAULT false,
	auto_provision  BOOLEAN     NOT NULL DEFAULT true,
	link_by_email   BOOLEAN     NOT NULL DEFAULT false,
	allowed_domains JSONB       NOT NULL DEFAULT '[]',
	created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uni_oidc_providers_slug ON oidc_providers (slug);

CREATE TABLE IF NOT EXISTS user_identities (
	id            UUID        PRIMARY KEY,
	user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	provider_id   UUID        NOT NULL REFERENCES oidc_providers(id) ON DELETE CASCADE,
	subject       TEXT        NOT NULL,
	email         TEXT,
	created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
	last_login_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uni_user_identities_provider_subject
	ON user_identities (provider_id, subject);
CREATE INDEX IF NOT EXISTS idx_user_identities_user ON user_identities (user_id);

-- Email lookups (SSO link-by-email, the duplicate-email check) compare
-- case-insensitively: providers lowercase addresses, while users.email keeps
-- whatever case an admin typed. Not UNIQUE — existing rows may already
-- differ only by case, and that must not block the upgrade.
CREATE INDEX IF NOT EXISTS idx_users_email_lower_active
	ON users (lower(email))
	WHERE deleted_at IS NULL AND email IS NOT NULL;

COMMIT;
