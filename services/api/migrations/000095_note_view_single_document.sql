-- 000095_note_view_single_document.sql
-- A note view holds exactly one document (migration 000094 let it hold a
-- list). At most one live document may name a given view_id.
--
-- Existing views with several live notes keep the first in manual order
-- (position, created_at, id) and the others are soft-deleted (deleted_at, the
-- way deleting a document does), so their snapshots and activity stay for the
-- audit log.
--
-- Instances still running the code of 000094 may overlap this migration during
-- a rolling deploy and keep inserting notes, so the dedupe and the index build
-- must see a stable table: documents is locked SHARE (writers wait, readers do
-- not; CREATE UNIQUE INDEX needs that much anyway) before either. Lock order
-- (see 000088): the doc change counter is taken before any document row, so
-- the counter of every project that has a note view is taken first, then the
-- table. A writer holding a counter and waiting for the table lock would
-- otherwise deadlock with the counter taken after it.
--
-- The keeper is chosen by (position, created_at, id) while the API lists a
-- view by (position, title, id); the two cannot disagree afterwards because
-- the extras are soft-deleted and a list shows only live rows.
--
-- LOCK TABLE blocks every writer of documents until COMMIT: the dedupe and the
-- index build are short at current table sizes, but run this off-peak on a
-- very large documents table.
--
-- Rerunnable: the dedupe finds nothing the second time and the index is
-- created IF NOT EXISTS.

BEGIN;

INSERT INTO doc_sync_state AS s (project_id)
SELECT DISTINCT project_id FROM sprint_views WHERE view_type = 'note'
ON CONFLICT (project_id) DO UPDATE SET seq = s.seq;

LOCK TABLE documents IN SHARE MODE;

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
