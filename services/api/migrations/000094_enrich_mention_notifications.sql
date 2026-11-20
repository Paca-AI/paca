-- Mention notifications that say where: the comment a mention points at, where
-- it was made (a comment, a description, a document), and a stored excerpt so
-- the card survives the comment being edited or deleted. The task's sprint and
-- status are not stored; they are joined when the notification is read.
ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS comment_id     UUID,
    ADD COLUMN IF NOT EXISTS mention_source TEXT,
    ADD COLUMN IF NOT EXISTS excerpt        TEXT;

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_mention_source_check;
ALTER TABLE notifications
    ADD CONSTRAINT notifications_mention_source_check
    CHECK (mention_source IS NULL OR mention_source IN ('comment', 'description', 'document', 'doc_comment'));

-- Unread mentions per task (task-card badges, sprint header counts).
CREATE INDEX IF NOT EXISTS idx_notifications_unread_mentions
    ON notifications (recipient_account_id, task_id)
    WHERE read_at IS NULL AND type = 'mentioned' AND task_id IS NOT NULL;

-- When each person last looked at a task, for the "unread since" divider in
-- the task's activity pane.
CREATE TABLE IF NOT EXISTS task_reads (
    account_id   UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    task_id      UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, task_id)
);
CREATE INDEX IF NOT EXISTS idx_task_reads_task ON task_reads (task_id);
