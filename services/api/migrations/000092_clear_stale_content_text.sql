-- 000092_clear_stale_content_text.sql
-- Keeps documents.content_text (000091) from going stale (Paca-AI/paca#747).
--
-- A writer that changes content without filling content_text, such as an API
-- pod from before 000091 during a rolling upgrade, or a manual UPDATE, would
-- otherwise leave the old text in the search index for good: the row is not
-- NULL, so the backfill never revisits it, and search keeps returning the
-- document for text it no longer has.
--
-- This trigger turns content_text back to NULL when content changes, unless
-- the transaction declared that it wrote content_text itself with
--
--     SELECT set_config('paca.content_text_written', 'on', true);
--
-- (transaction-local, like paca.skip_doc_seq in 000088). NULL means "not
-- filled": search finds such a row through the old JSON-path match, and the
-- background backfill fills it at the next process start
-- (internal/worker/doc_hash_backfill.go).
--
-- The API declares it before every UPDATE that sets content
-- (updateDocumentLocked), which is how a formatting-only change keeps its
-- filled content_text. content is JSONB, which compares by value: an UPDATE
-- that lists content in SET without changing it keeps content_text. The
-- backfill's UPDATE does not list content, so the trigger does not fire for
-- it. It touches no column that documents_bump_seq reads or writes, so their
-- order does not matter.
CREATE OR REPLACE FUNCTION documents_clear_content_text() RETURNS trigger AS $$
BEGIN
    IF current_setting('paca.content_text_written', true) = 'on' THEN
        RETURN NEW;
    END IF;
    IF NEW.content IS DISTINCT FROM OLD.content THEN
        NEW.content_text := NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS documents_clear_content_text ON documents;
CREATE TRIGGER documents_clear_content_text BEFORE UPDATE OF content ON documents
    FOR EACH ROW EXECUTE FUNCTION documents_clear_content_text();
