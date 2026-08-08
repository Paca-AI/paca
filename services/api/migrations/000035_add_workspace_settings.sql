-- 000035_add_workspace_settings.sql
-- Adds a singleton workspace_settings row holding instance-wide branding:
-- logo/favicon avatar-style image keys (same shape as 000033/000034 —
-- resolved to presigned display URLs at read time, see
-- attachmentdom.AvatarService), a brand name (used as both the browser tab
-- title and the wordmark text shown next to the logo), and a primary accent
-- color per theme mode.
--
-- The `id boolean primary key default true check (id)` trick guarantees the
-- table can only ever hold the one row seeded below: any second insert would
-- either violate the PK uniqueness (id = true again) or the CHECK (id = false
-- is rejected), so callers never need upsert logic — just `WHERE id = true`.

BEGIN;

CREATE TABLE IF NOT EXISTS workspace_settings (
	id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
	logo_key TEXT,
	logo_thumb_key TEXT,
	favicon_key TEXT,
	favicon_thumb_key TEXT,
	primary_color_light TEXT,
	primary_color_dark TEXT,
	brand_name TEXT,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_by UUID REFERENCES users(id) ON DELETE SET NULL
);

INSERT INTO workspace_settings (id) VALUES (TRUE) ON CONFLICT (id) DO NOTHING;

COMMIT;
