-- Run in the migration runner's transaction. Block project writes until both
-- deduplication and the unique index are committed; ordinary reads can continue.
LOCK TABLE projects IN SHARE ROW EXCLUSIVE MODE;

-- Keep a durable audit trail, including after a project is deleted. No FK:
-- this is migration history, not part of the project lifecycle.
CREATE TABLE IF NOT EXISTS project_prefix_changes_000093 (
    project_id UUID PRIMARY KEY,
    old_prefix TEXT NOT NULL,
    new_prefix TEXT NOT NULL,
    changed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DO $$
DECLARE
    duplicate RECORD;
    suffix BIGINT;
    candidate TEXT;
BEGIN
    FOR duplicate IN
        SELECT id, task_id_prefix
        FROM (
            SELECT id, task_id_prefix, created_at,
                   row_number() OVER (
                       PARTITION BY upper(task_id_prefix)
                       ORDER BY created_at, id
                   ) AS position
            FROM projects
            WHERE task_id_prefix <> '' AND deleted_at IS NULL
        ) ranked
        WHERE position > 1
        ORDER BY created_at, id
    LOOP
        suffix := 2;
        LOOP
            IF length(suffix::text) > 10 THEN
                RAISE EXCEPTION 'No task ID prefix available for project %', duplicate.id;
            END IF;
            candidate := left(upper(duplicate.task_id_prefix), 10 - length(suffix::text)) || suffix::text;
            EXIT WHEN NOT EXISTS (
                SELECT 1 FROM projects
                WHERE upper(task_id_prefix) = candidate
                  AND task_id_prefix <> '' AND deleted_at IS NULL
            );
            suffix := suffix + 1;
        END LOOP;

        INSERT INTO project_prefix_changes_000093 (project_id, old_prefix, new_prefix)
        VALUES (duplicate.id, duplicate.task_id_prefix, candidate);
        UPDATE projects SET task_id_prefix = candidate WHERE id = duplicate.id;
    END LOOP;
END $$;

-- IF NOT EXISTS also tolerates a retry after a crash between the migration's
-- commit and the runner recording its filename in schema_migrations.
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_task_id_prefix
    ON projects (upper(task_id_prefix))
    WHERE task_id_prefix <> '' AND deleted_at IS NULL;
