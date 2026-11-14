-- A content hash on documents and their snapshots.
--
-- content_hash is the sha256 (lowercase hex) of a document's content with each
-- block's own "id" removed and keys sorted (see docdom.ContentHash). The same
-- blocks always give the same hash, whatever ids the writer generated or how
-- Postgres re-serialized the JSONB, so the API can tell that a write changes
-- nothing (no snapshot, no updated_at bump, no event) and can use the hash as
-- an ETag / If-Match token without a row version that every autosave, rename
-- and move would bump (migration 000083 left documents out for that reason).
--
-- Existing rows stay NULL: computing the hash needs the Go normalizer, which
-- SQL cannot reproduce. A NULL hash is not an error. The service computes it
-- in memory when it is needed and stores it on the row's next write. A later
-- change adds a backfill for the rows that never get written again.
--
-- doc_snapshots.content_hash is the hash of the version that snapshot holds,
-- so a client can ask for the version it last saw (a merge base) by hash.

ALTER TABLE documents     ADD COLUMN IF NOT EXISTS content_hash TEXT;
ALTER TABLE doc_snapshots ADD COLUMN IF NOT EXISTS content_hash TEXT;

CREATE INDEX IF NOT EXISTS idx_doc_snapshots_document_hash
    ON doc_snapshots (document_id, content_hash);
