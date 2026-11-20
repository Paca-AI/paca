-- 000095_note_view_single_document.sql
-- A note view holds exactly one document (migration 000094 let it hold a
-- list). At most one live document may name a given view_id.
--
-- Existing views with several live notes keep the first in manual order
-- (position, created_at, id) and the others are soft-deleted (deleted_at, the
-- way deleting a document does), so their snapshots and activity stay for the
-- audit log. The doc change counter is taken before any document row, as in
-- 000088 and the view delete trigger of 000094.
--
-- Rerunnable: the dedupe finds nothing the second time and the index is
-- created IF NOT EXISTS.

BEGIN;

INSERT INTO doc_sync_state AS s (project_id)
SELECT DISTINCT d.project_id
  FROM documents d
 WHERE d.view_id IS NOT NULL AND d.deleted_at IS NULL
   AND EXISTS (
       SELECT 1 FROM documents o
        WHERE o.view_id = d.view_id AND o.deleted_at IS NULL AND o.id <> d.id)
ON CONFLICT (project_id) DO UPDATE SET seq = s.seq;

UPDATE documents d
   SET deleted_at = NOW()
  FROM (
      SELECT id,
             ROW_NUMBER() OVER (PARTITION BY view_id ORDER BY position, created_at, id) AS rn
        FROM documents
       WHERE view_id IS NOT NULL AND deleted_at IS NULL
  ) ranked
 WHERE d.id = ranked.id AND ranked.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS uq_documents_note_view
    ON documents (view_id) WHERE view_id IS NOT NULL AND deleted_at IS NULL;

COMMIT;
