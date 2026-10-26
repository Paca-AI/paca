-- "question" notifications: an agent asked its conversation's owner a
-- multiple-choice question (ask_user_question MCP tool). They point at a
-- conversation rather than a task, and a global-chat conversation belongs to
-- no project, so project_id becomes optional.
ALTER TABLE notifications ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS conversation_id UUID
        REFERENCES agent_conversations(id) ON DELETE CASCADE;

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
ALTER TABLE notifications
    ADD CONSTRAINT notifications_type_check
    CHECK (type = ANY (ARRAY['assigned'::text, 'mentioned'::text, 'question'::text]));
