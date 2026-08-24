-- Database-backed OIDC configuration for the administrator settings page.
-- oidc_configured distinguishes an untouched installation (environment
-- configuration remains authoritative) from an intentional database-backed
-- disabled configuration.

BEGIN;

ALTER TABLE workspace_settings
    ADD COLUMN IF NOT EXISTS oidc_configured BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS oidc_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS oidc_issuer_url TEXT,
    ADD COLUMN IF NOT EXISTS oidc_client_id TEXT,
    ADD COLUMN IF NOT EXISTS oidc_client_secret_enc TEXT,
    ADD COLUMN IF NOT EXISTS oidc_scopes TEXT,
    ADD COLUMN IF NOT EXISTS oidc_redirect_url TEXT,
    ADD COLUMN IF NOT EXISTS oidc_display_name TEXT,
    ADD COLUMN IF NOT EXISTS oidc_username_claim TEXT,
    ADD COLUMN IF NOT EXISTS local_login_enabled BOOLEAN NOT NULL DEFAULT TRUE;

COMMIT;
