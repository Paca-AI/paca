-- 000094_add_note_view_type.sql
-- Note views: a view of type 'note' holds free-form documents (sprint goals,
-- meeting minutes, a release checklist) instead of tasks. Its notes are
-- ordinary documents (BlockNote content, snapshots, activity, comments) that
-- belong to the view rather than to the project's docs tree.
--
-- 1. sprint_views.view_type allows 'note' (the 000006/000090 precedent).
--
-- 2. documents.view_id names the note view a document belongs to. A document
--    lives either in the docs tree (view_id NULL; folder_id NULL is the root)
--    or in one view (view_id set, folder_id NULL), never both. Listings of the
--    docs tree, path lookups, search and the change feed only see
--    view_id IS NULL.
--
-- 3. Deleting a view soft-deletes its live notes, the way deleting a document
--    does (deleted_at), so their snapshots and activity stay for the audit
--    log. A BEFORE DELETE trigger does it, which also covers views removed by
--    the sprint_views.sprint_id cascade when their sprint is deleted. The
--    foreign key then sets view_id to NULL on those (deleted) rows; the seq
--    trigger of migration 000088 leaves an already deleted row's seq alone,
--    so that write is not numbered. Such a row reads as a deleted tree
--    document from then on, so a change feed client may get a tombstone for
--    a note it never had; an unknown tombstone is nothing to remove.
--
--    Lock order (see 000088): the doc change counter is taken before any
--    document row. A view delete reaches the counter while it holds the view
--    row, so a note insert takes the view row (FOR KEY SHARE, see
--    CreateDocument in internal/repository/postgres/document_repository.go)
--    before the counter too: view, then counter, then documents.

BEGIN;

ALTER TABLE sprint_views
    DROP CONSTRAINT IF EXISTS sprint_views_view_type_check;

ALTER TABLE sprint_views
    ADD CONSTRAINT sprint_views_view_type_check
    CHECK (view_type IN ('table', 'board', 'gantt', 'plugin', 'note'));

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS view_id UUID REFERENCES sprint_views(id) ON DELETE SET NULL;

ALTER TABLE documents
    DROP CONSTRAINT IF EXISTS chk_documents_view_or_folder;

ALTER TABLE documents
    ADD CONSTRAINT chk_documents_view_or_folder
    CHECK (view_id IS NULL OR folder_id IS NULL);

CREATE INDEX IF NOT EXISTS idx_documents_view_id
    ON documents (view_id) WHERE view_id IS NOT NULL;

CREATE OR REPLACE FUNCTION sprint_views_delete_notes() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM documents WHERE view_id = OLD.id AND deleted_at IS NULL) THEN
        RETURN OLD;
    END IF;
    -- A view removed with its project (a hard project delete): the project's
    -- documents are deleted by that cascade, so there is nothing to keep.
    IF NOT EXISTS (SELECT 1 FROM projects WHERE id = OLD.project_id) THEN
        RETURN OLD;
    END IF;
    -- The counter before the document rows (lockDocSeq).
    INSERT INTO doc_sync_state AS s (project_id) VALUES (OLD.project_id)
    ON CONFLICT (project_id) DO UPDATE SET seq = s.seq;
    UPDATE documents SET deleted_at = NOW()
     WHERE view_id = OLD.id AND deleted_at IS NULL;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sprint_views_delete_notes ON sprint_views;
CREATE TRIGGER trg_sprint_views_delete_notes
    BEFORE DELETE ON sprint_views
    FOR EACH ROW EXECUTE FUNCTION sprint_views_delete_notes();

COMMIT;
