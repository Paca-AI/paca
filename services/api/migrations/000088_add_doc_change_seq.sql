-- 000088_add_doc_change_seq.sql
-- A per-project change sequence for documents, so a client that mirrors a
-- project's docs can ask "what changed since my last pull" (the change feed,
-- GET /projects/{projectId}/docs/changes) instead of listing everything.
--
-- 1. doc_sync_state holds one counter per project. next_doc_seq() increments
--    it and returns the new value. The increment locks the project's row until
--    the transaction commits, so two writers in one project cannot commit out
--    of order: a reader that sees seq = N sees every change numbered <= N.
--
-- 2. documents.seq is the number of the row's last write, set by a trigger on
--    every insert and update (a soft delete is an update). created_seq is the
--    number the row was inserted with and never changes. The feed needs both:
--    seq to find what changed, created_seq to tell which documents a client
--    with an older cursor can already know about.
--
--    Two writes leave seq alone. A row that is already soft-deleted keeps the
--    number of its deletion, so a later write to it (the folder foreign key
--    setting folder_id to NULL, a repeated delete) cannot make the deletion
--    look newer than it is. And a statement run with the setting
--    paca.skip_doc_seq = 'on' is not numbered at all: only the content hash
--    backfill uses it, because filling in a hash changes nothing a client can
--    see (see internal/worker/doc_hash_backfill.go).
--
-- 3. doc_sync_state.folders_seq is the number of the last folder insert,
--    update or delete. Folders are hard-deleted and have no tombstones, so the
--    feed sends the whole folder tree whenever folders_seq is past the
--    client's cursor. Deleting a folder also sets folder_id to NULL on its
--    documents through the foreign key; that is an update of each document, so
--    each gets a new seq from the documents trigger.
--
-- doc_sync_state has no foreign key to projects on purpose: the triggers run
-- inside the cascade of a hard project delete, where the project row is
-- already gone. A row left behind by that is three numbers.

CREATE TABLE IF NOT EXISTS doc_sync_state (
    project_id  UUID   PRIMARY KEY,
    seq         BIGINT NOT NULL DEFAULT 0,
    folders_seq BIGINT NOT NULL DEFAULT 0
);

ALTER TABLE documents ADD COLUMN IF NOT EXISTS seq         BIGINT NOT NULL DEFAULT 0;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS created_seq BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_documents_project_seq ON documents (project_id, seq);

CREATE OR REPLACE FUNCTION next_doc_seq(project UUID) RETURNS BIGINT AS $$
    INSERT INTO doc_sync_state AS s (project_id, seq) VALUES (project, 1)
    ON CONFLICT (project_id) DO UPDATE SET seq = s.seq + 1
    RETURNING seq;
$$ LANGUAGE sql;

-- Number the rows that exist already, per project, oldest first. The trigger
-- is dropped first so this UPDATE is not numbered a second time; on a re-run
-- no row has seq = 0 and nothing happens.
DROP TRIGGER IF EXISTS documents_bump_seq ON documents;

WITH numbered AS (
    SELECT d.id,
           d.project_id,
           COALESCE(s.seq, 0) + row_number() OVER (PARTITION BY d.project_id ORDER BY d.created_at, d.id) AS n
    FROM documents d
    LEFT JOIN doc_sync_state s ON s.project_id = d.project_id
    WHERE d.seq = 0
)
UPDATE documents d SET seq = numbered.n, created_seq = numbered.n
FROM numbered WHERE d.id = numbered.id;

INSERT INTO doc_sync_state AS s (project_id, seq)
SELECT project_id, max(seq) FROM documents GROUP BY project_id
ON CONFLICT (project_id) DO UPDATE SET seq = GREATEST(s.seq, EXCLUDED.seq);

CREATE OR REPLACE FUNCTION documents_bump_seq() RETURNS trigger AS $$
BEGIN
    IF current_setting('paca.skip_doc_seq', true) = 'on' THEN
        RETURN NEW;
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.deleted_at IS NOT NULL AND NEW.deleted_at IS NOT NULL THEN
        RETURN NEW;
    END IF;
    NEW.seq := next_doc_seq(NEW.project_id);
    IF TG_OP = 'INSERT' THEN
        NEW.created_seq := NEW.seq;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER documents_bump_seq BEFORE INSERT OR UPDATE ON documents
    FOR EACH ROW EXECUTE FUNCTION documents_bump_seq();

-- Two statements, not "SET folders_seq = next_doc_seq(...)": that would update
-- the row from inside a statement that is already updating it.
CREATE OR REPLACE FUNCTION doc_folders_bump_seq() RETURNS trigger AS $$
DECLARE
    pid UUID;
    n   BIGINT;
BEGIN
    IF TG_OP = 'DELETE' THEN
        pid := OLD.project_id;
    ELSE
        pid := NEW.project_id;
    END IF;
    n := next_doc_seq(pid);
    UPDATE doc_sync_state SET folders_seq = n WHERE project_id = pid;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS doc_folders_bump_seq ON doc_folders;
CREATE TRIGGER doc_folders_bump_seq AFTER INSERT OR UPDATE OR DELETE ON doc_folders
    FOR EACH ROW EXECUTE FUNCTION doc_folders_bump_seq();
