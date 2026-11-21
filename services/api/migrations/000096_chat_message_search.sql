-- Message search (docs/architecture/chat-and-meetings.md, Phase 2): a trigram
-- index so ILIKE '%…%' on a message's plain text does not scan the table.
-- pg_trgm is created by migration 000091.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_messages_content_text_trgm
    ON messages USING gin (content_text gin_trgm_ops)
    WHERE deleted_at IS NULL;
