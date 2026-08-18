-- 000040_add_password_set_tokens.sql
-- Single-use tokens that let a user set their password via an emailed link
-- (e.g. the SMTP plugin's welcome/invite email) without ever transmitting
-- the password itself. Only the token's hash is stored, mirroring how
-- passwords themselves are never stored in plaintext.

BEGIN;

CREATE TABLE IF NOT EXISTS password_set_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uni_password_set_tokens_token_hash
    ON password_set_tokens (token_hash);

-- Speeds up "does this user already have a live token" lookups; most rows
-- are quickly marked used, so indexing only the still-live ones keeps it small.
CREATE INDEX IF NOT EXISTS idx_password_set_tokens_user_id_active
    ON password_set_tokens (user_id)
    WHERE used_at IS NULL;

COMMIT;
