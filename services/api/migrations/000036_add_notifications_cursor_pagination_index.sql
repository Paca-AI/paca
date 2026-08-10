-- Composite index for cursor-based keyset pagination on notifications.
-- The query pattern is:
--   WHERE recipient_user_id = ? AND (created_at, id) < (?, ?)
--   ORDER BY created_at DESC, id DESC LIMIT n
-- The leading recipient_user_id column filters down to a single user's
-- notifications before the keyset comparison, avoiding a full-table scan.
-- Same shape as idx_agent_conversations_cursor_pagination (000025).
CREATE INDEX IF NOT EXISTS idx_notifications_cursor_pagination
    ON notifications (recipient_user_id, created_at DESC, id DESC);
