-- In-app notifications for a person @mentioned in a document comment. They
-- point at a document instead of a task.
ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS document_id UUID
        REFERENCES documents(id) ON DELETE CASCADE;
