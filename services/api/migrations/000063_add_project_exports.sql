-- 000063_add_project_exports.sql
-- Adds project_exports: one row per "export this project" request (a zip
-- of the tasks, task comments/activities and docs). The API inserts a
-- 'pending' row and appends a message to the paca.project_exports Valkey
-- stream; worker.ProjectExportConsumer claims the row ('processing'), builds
-- the zip, uploads it to object storage under
-- file_key and marks the row 'completed' (or 'failed' with error_message).
-- Clients poll the row and download through a short-lived presigned URL
-- minted on demand — no URL is ever stored here.
--
-- expires_at is when the stored file stops being downloadable; the consumer's
-- periodic cleanup deletes the object and the row once it has passed.
--
-- IF NOT EXISTS throughout so this migration is safe to re-run (there is no
-- migration-tracking table, every file here re-runs every startup).

BEGIN;

CREATE TABLE IF NOT EXISTS project_exports (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    requested_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    kind          TEXT        NOT NULL DEFAULT 'project_archive',
    status        TEXT        NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    file_key      TEXT,
    file_name     TEXT,
    file_size     BIGINT,
    row_count     INTEGER,
    error_message TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ
);

-- kind's default changed before release; keeps databases that already ran an
-- earlier version of this file in step.
ALTER TABLE project_exports ALTER COLUMN kind SET DEFAULT 'project_archive';

CREATE INDEX IF NOT EXISTS idx_project_exports_project
    ON project_exports (project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_project_exports_expires
    ON project_exports (expires_at) WHERE expires_at IS NOT NULL;

COMMIT;
