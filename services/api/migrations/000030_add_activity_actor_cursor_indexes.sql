-- Composite indexes for keyset-paginated "activities by actor" queries (the
-- agent activity feed), mirroring idx_agent_conversations_cursor_pagination
-- (migration 000025). Query pattern per table:
--   WHERE actor_id = ? AND deleted_at IS NULL AND (created_at, id) < (?, ?)
--   ORDER BY created_at DESC, id DESC
-- The old single-column actor index on task_activities is superseded by the
-- composite one below (a composite index's leading column still serves
-- plain actor_id lookups). doc_activities had no actor index at all.
DROP INDEX IF EXISTS idx_task_activities_actor_id;

CREATE INDEX IF NOT EXISTS idx_task_activities_actor_cursor
    ON task_activities (actor_id, created_at DESC, id DESC)
    WHERE actor_id IS NOT NULL AND deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_doc_activities_actor_cursor
    ON doc_activities (actor_id, created_at DESC, id DESC)
    WHERE actor_id IS NOT NULL AND deleted_at IS NULL;
