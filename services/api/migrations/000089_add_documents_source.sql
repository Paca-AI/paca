-- Where a managed document copy comes from, and whether it is locked.
--
-- A managed copy is a document whose real home is a file in a code
-- repository: a coding agent pushes the file to a document path
-- (PUT /projects/{projectId}/docs/by-path), and the repository wins whenever
-- the two disagree. source records that file as
-- {"repository": "...", "path": "...", "commit": "..."} (commit is absent when
-- the pushed file had uncommitted changes). It is NULL on every document that
-- was created in Paca, which is every existing row.
--
-- source_locked marks a managed copy that people should not edit in Paca: a
-- content or title write that does not come from the same repository is
-- refused, so an edit is made in the file and pushed instead. A person with
-- docs:write can unlock it.

ALTER TABLE documents ADD COLUMN IF NOT EXISTS source JSONB;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS source_locked BOOLEAN NOT NULL DEFAULT false;
