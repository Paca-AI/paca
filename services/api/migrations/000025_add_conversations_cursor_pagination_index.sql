-- Composite index for cursor-based keyset pagination on conversations.
-- The query pattern is:
--   WHERE project_id = ? AND (created_at, id) < (?, ?)
--   ORDER BY created_at DESC, id DESC LIMIT n
-- The leading project_id column filters down to a single project before
-- the keyset comparison, avoiding a full-table scan.
CREATE INDEX IF NOT EXISTS idx_agent_conversations_cursor_pagination
    ON agent_conversations (project_id, created_at DESC, id DESC);
