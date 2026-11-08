-- "automation" notifications: sent by an automation's notify action. Free text
-- about a task, with no human actor — the text lives in a new nullable
-- message column (NULL for every other notification type).
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS message TEXT;

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications
    ADD CONSTRAINT notifications_type_check
    CHECK (type = ANY (ARRAY['assigned'::text, 'mentioned'::text, 'question'::text, 'automation'::text]));
