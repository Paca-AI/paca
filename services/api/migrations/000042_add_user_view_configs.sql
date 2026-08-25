-- 000042_add_user_view_configs.sql
-- Per-user overrides for interaction view settings and filters.
--
-- A sprint_views row holds the project-shared view definition (name, type,
-- position) plus a shared default config. Personal tweaks a member makes in
-- the "View settings" panel — sort, field sum, page size, visible fields,
-- collapsed columns and every filter dimension — must stay private to that
-- member instead of overwriting the shared row for everyone. Those personal
-- configs live here, keyed by (view_id, user_id), and are overlaid on read.
--
-- Rows are removed automatically when either the view or the user is deleted.

BEGIN;

CREATE TABLE IF NOT EXISTS user_view_configs (
    view_id    UUID NOT NULL REFERENCES sprint_views(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    config     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (view_id, user_id)
);

-- Supports the ON DELETE CASCADE from users and per-user lookups that filter
-- by user_id (user_id is the trailing column of the primary key, so it is not
-- usable as a leading index on its own).
CREATE INDEX IF NOT EXISTS idx_user_view_configs_user_id
    ON user_view_configs (user_id);

COMMIT;
