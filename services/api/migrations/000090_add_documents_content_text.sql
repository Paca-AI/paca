-- 000090_add_documents_content_text.sql
-- Indexed document search that matches across formatting (Paca-AI/paca#747).
--
-- pg_trgm comes first, so a database role that may not create it fails here,
-- before anything else in this file is applied (the file runs in one
-- transaction, so nothing is half-applied either way). pg_trgm is a trusted
-- extension (PostgreSQL 13 and later): the migration role needs the CREATE
-- privilege on the database, which the database owner has. If the migration
-- role lacks it, have a superuser (or the database owner) run, once:
--
--     CREATE EXTENSION IF NOT EXISTS pg_trgm;
--
-- in the Paca database, then start the API again. The stock postgres images
-- (postgres:16-alpine in deploy/) ship pg_trgm with their contrib modules.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- content_text is the document's plain text, one line per block, the inline
-- text runs of a block joined with nothing between them (docdom.ContentText),
-- so "foo **bar**" is stored as "foo bar" and a search for "foo bar" finds it.
-- The API writes it with every insert and update of content. Rows written
-- before this migration stay NULL until the background backfill fills them
-- (internal/worker/doc_hash_backfill.go); search still finds a NULL row
-- through the old JSON-path match, and an empty document stores '' rather
-- than NULL, so NULL always means "not filled yet".
ALTER TABLE documents ADD COLUMN IF NOT EXISTS content_text TEXT;

-- Trigram indexes serve ILIKE '%…%' for queries of three or more characters.
CREATE INDEX IF NOT EXISTS idx_documents_content_text_trgm
    ON documents USING gin (content_text gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_documents_title_trgm
    ON documents USING gin (title gin_trgm_ops);

-- The rows still waiting for the backfill. Search ORs a "content_text IS NULL
-- AND <JSON-path match>" branch onto the two trigram matches; with this
-- partial index every branch of the OR has an index, so the planner can
-- combine them in a bitmap scan instead of reading the whole project. Once
-- the backfill is done the index is empty.
CREATE INDEX IF NOT EXISTS idx_documents_content_text_null
    ON documents (project_id) WHERE content_text IS NULL;
