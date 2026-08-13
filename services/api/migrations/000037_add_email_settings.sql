-- 000037_add_email_settings.sql
-- Adds SMTP e-mail configuration to the singleton workspace_settings row and
-- an optional e-mail address to users.
--
-- SMTP settings let an admin configure an outbound mail server from the
-- Settings page ("Configuração de e-mail"). The SMTP password is stored
-- encrypted at rest (AES-256-GCM via the app's ENCRYPTION_KEY, see
-- platform/secret.Encryptor) — never in plaintext.
--
-- send_user_created_email is the "Enviar e-mail de usuário criado" toggle
-- (checked by default): when true, creating a user (or resetting their
-- password) e-mails their credentials to users.email.
--
-- users.email is nullable so pre-existing accounts (seeded admin, etc.) keep
-- working; new-user creation requires it at the application layer.

BEGIN;

ALTER TABLE workspace_settings
	ADD COLUMN IF NOT EXISTS smtp_from_email          TEXT,
	ADD COLUMN IF NOT EXISTS smtp_from_name           TEXT,
	ADD COLUMN IF NOT EXISTS smtp_host                TEXT,
	ADD COLUMN IF NOT EXISTS smtp_port                INTEGER,
	ADD COLUMN IF NOT EXISTS smtp_username            TEXT,
	ADD COLUMN IF NOT EXISTS smtp_password_encrypted  TEXT,
	ADD COLUMN IF NOT EXISTS smtp_use_ssl             BOOLEAN NOT NULL DEFAULT FALSE,
	ADD COLUMN IF NOT EXISTS smtp_use_tls             BOOLEAN NOT NULL DEFAULT FALSE,
	ADD COLUMN IF NOT EXISTS send_user_created_email  BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE users
	ADD COLUMN IF NOT EXISTS email TEXT;

COMMIT;
