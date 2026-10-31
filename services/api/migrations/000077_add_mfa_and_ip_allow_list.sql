-- TOTP multi-factor authentication and IP/CIDR allow-lists for password login.
-- Every default leaves existing installs unchanged: no enrollments, no CIDRs,
-- and a policy that is off (no MFA requirement, empty IP list), so applying it
-- to every role by default changes nothing until an admin turns something on.

-- One row per user who has started TOTP enrollment. enabled=false is a pending
-- enrollment (secret issued, first code not yet confirmed). The secret is
-- AES-GCM encrypted at rest; recovery codes are stored as SHA-256 hashes and
-- removed as they are used (JSON arrays of strings). last_used_step blocks replay of a TOTP code.
CREATE TABLE IF NOT EXISTS user_mfa (
    user_id        UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_enc     TEXT NOT NULL,
    enabled        BOOLEAN NOT NULL DEFAULT FALSE,
    recovery_codes JSONB NOT NULL DEFAULT '[]',
    last_used_step BIGINT NOT NULL DEFAULT 0,
    failed_attempts INT NOT NULL DEFAULT 0,
    locked_until   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Per-account allow-list for password login; empty means unrestricted.
ALTER TABLE users ADD COLUMN IF NOT EXISTS allowed_cidrs JSONB NOT NULL DEFAULT '[]';

-- Singleton instance policy (id = true, as workspace_settings).
-- privileged_roles names the platform roles the policy applies to, or holds
-- the single entry "*" for every role (the default); for any account it
-- covers, require_mfa demands an enrolled second factor and a
-- non-empty ip_allow_list restricts password login to those CIDRs.
CREATE TABLE IF NOT EXISTS auth_security_settings (
    id              BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
    privileged_roles JSONB NOT NULL DEFAULT '["*"]',
    require_mfa     BOOLEAN NOT NULL DEFAULT FALSE,
    ip_allow_list   JSONB NOT NULL DEFAULT '[]',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      UUID
);
INSERT INTO auth_security_settings (id) VALUES (TRUE) ON CONFLICT DO NOTHING;
