-- 000039_add_users_email.sql
-- Adds an optional email address to users, used for notification delivery
-- (e.g. the SMTP plugin's welcome/invite email). Uniqueness is scoped to
-- active (non-deleted) users, matching uni_users_username_active, and NULLs
-- are naturally excluded from a Postgres unique index so existing users
-- without an email are unaffected.

BEGIN;

ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uni_users_email_active
    ON users (email)
    WHERE deleted_at IS NULL AND email IS NOT NULL;

COMMIT;
