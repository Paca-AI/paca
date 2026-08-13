-- 000038_add_smtp_skip_verify.sql (EVUP)
-- Adds an opt-in flag to skip TLS certificate verification when connecting to
-- the SMTP server. Needed for shared mail hosting where the server presents a
-- wildcard certificate for the provider's domain (e.g. *.skymail.net.br)
-- rather than the customer's SMTP hostname (e.g. smtp.elosclub.com.br), which
-- would otherwise fail Go's x509 hostname verification. Insecure by design —
-- off by default, the admin turns it on knowingly.

BEGIN;

ALTER TABLE workspace_settings
	ADD COLUMN IF NOT EXISTS smtp_skip_verify BOOLEAN NOT NULL DEFAULT FALSE;

COMMIT;
