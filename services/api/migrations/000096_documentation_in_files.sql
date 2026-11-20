-- 000096_documentation_in_files.sql
-- One folder tree for Documentation and Files (docs/product/unified-documentation-files.md).
--
-- doc_folders rows move into file_folders, keeping their ids, under a locked
-- per-project system folder `documentation`. Every folder at or below it has
-- area = 'docs'. A document is the Markdown file "<title>.md" in that tree: it
-- appears in file_items next to folders and files, and goes to the file
-- manager's trash (documents.trashed_at) like they do.
--
-- doc_folders itself stays, as an updatable VIEW over that subtree, with the
-- shape the docs code has always used (a top-level doc folder has a NULL
-- parent, created_by is a project member id). Existing docs queries, the IAM
-- attribute loaders and their tests therefore keep working unchanged; the view
-- is retired once the docs code reads file_folders directly.
--
-- Deleting a doc folder trashes it with its subfolders, documents and files.

-- -------------------------------------------------------------------------
-- file_folders gains what documentation needs
-- -------------------------------------------------------------------------

ALTER TABLE file_folders
    ADD COLUMN IF NOT EXISTS position          INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS area              TEXT CHECK (area IN ('docs')),
    -- Docs record the author as a project member; keep it next to created_by.
    ADD COLUMN IF NOT EXISTS created_by_member UUID REFERENCES project_members(id) ON DELETE SET NULL;

-- Documents in the trash: documents.deleted_at stays the soft-delete (and the
-- change feed's tombstone) it always was. trashed_at marks the deletions a user
-- can undo: set when a document is deleted from the app, or goes with a folder,
-- and cleared by a restore. Rows deleted before this migration, and the notes a
-- deleted view takes with it, have no trashed_at, so they are not in the trash
-- and are never purged. When the retention ends a trashed document's content and
-- snapshots are erased but the tombstone row stays (the feed needs it).
ALTER TABLE documents ADD COLUMN IF NOT EXISTS trashed_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS idx_documents_trashed ON documents (trashed_at) WHERE trashed_at IS NOT NULL;

ALTER TABLE file_folders DROP CONSTRAINT IF EXISTS file_folders_system_key_check;
ALTER TABLE file_folders ADD CONSTRAINT file_folders_system_key_check
    CHECK (system_key IN ('task_attachments', 'task', 'docs', 'annotations', 'documentation'));

DROP INDEX IF EXISTS ux_file_folders_system;
CREATE UNIQUE INDEX IF NOT EXISTS ux_file_folders_system
    ON file_folders (project_id, system_key)
    WHERE system_key IN ('task_attachments', 'docs', 'annotations', 'documentation');
CREATE INDEX IF NOT EXISTS idx_file_folders_area ON file_folders (project_id, parent_id) WHERE area = 'docs';

-- area follows the parent: the Documentation folder and everything below it.
-- A parent that cannot be seen (inserted by the same statement) leaves the
-- given value alone.
CREATE OR REPLACE FUNCTION file_folder_set_area() RETURNS trigger AS $$
DECLARE
    p RECORD;
BEGIN
    IF NEW.system_key = 'documentation' THEN
        NEW.area := 'docs';
    ELSIF NEW.parent_id IS NULL THEN
        NEW.area := NULL;
    ELSE
        SELECT area, system_key INTO p FROM file_folders WHERE id = NEW.parent_id;
        IF FOUND THEN
            NEW.area := CASE WHEN p.area = 'docs' THEN 'docs' ELSE NULL END;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_file_folder_area ON file_folders;
CREATE TRIGGER trg_file_folder_area
    BEFORE INSERT OR UPDATE OF parent_id ON file_folders
    FOR EACH ROW EXECUTE FUNCTION file_folder_set_area();

-- A moved folder carries its subtree into or out of the area.
CREATE OR REPLACE FUNCTION file_folder_cascade_area() RETURNS trigger AS $$
BEGIN
    WITH RECURSIVE sub AS (
        SELECT id FROM file_folders WHERE parent_id = NEW.id
        UNION ALL
        SELECT c.id FROM file_folders c JOIN sub s ON c.parent_id = s.id
    )
    UPDATE file_folders SET area = NEW.area WHERE id IN (SELECT id FROM sub);
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_file_folder_cascade_area ON file_folders;
CREATE TRIGGER trg_file_folder_cascade_area
    AFTER UPDATE ON file_folders
    FOR EACH ROW WHEN (OLD.area IS DISTINCT FROM NEW.area) EXECUTE FUNCTION file_folder_cascade_area();

-- -------------------------------------------------------------------------
-- The Documentation system folder, one per project
-- -------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION file_ensure_project_folders(p_project UUID) RETURNS VOID AS $$
BEGIN
    PERFORM file_ensure_system_folder(p_project, 'task_attachments', 'Task attachments');
    PERFORM file_ensure_system_folder(p_project, 'docs', 'Document files');
    PERFORM file_ensure_system_folder(p_project, 'annotations', 'Annotation screenshots');
    PERFORM file_ensure_system_folder(p_project, 'documentation', 'Documentation');
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN SELECT id FROM projects LOOP
        PERFORM file_ensure_system_folder(r.id, 'documentation', 'Documentation');
    END LOOP;
END;
$$;

-- -------------------------------------------------------------------------
-- Move doc_folders into file_folders (same ids)
-- -------------------------------------------------------------------------

-- Nothing may still point at the table.
ALTER TABLE documents        DROP CONSTRAINT IF EXISTS documents_folder_id_fkey;
DROP TRIGGER IF EXISTS doc_folders_bump_seq ON doc_folders;

INSERT INTO file_folders (id, project_id, parent_id, name, position, area, created_by, created_by_member, created_at, updated_at)
SELECT d.id,
       d.project_id,
       COALESCE(d.parent_id, (SELECT r.id FROM file_folders r
                               WHERE r.project_id = d.project_id AND r.system_key = 'documentation')),
       -- file_folders names may not contain a backslash; docs allowed it.
       COALESCE(NULLIF(btrim(replace(d.name, '\', '-')), ''), 'Folder'),
       d.position,
       'docs',
       (SELECT pm.account_id FROM project_members pm WHERE pm.id = d.created_by),
       d.created_by,
       d.created_at,
       d.updated_at
  FROM doc_folders d;

DROP TABLE doc_folders;

ALTER TABLE documents
    ADD CONSTRAINT documents_folder_id_fkey FOREIGN KEY (folder_id) REFERENCES file_folders(id) ON DELETE SET NULL;

-- -------------------------------------------------------------------------
-- doc_folders: the docs view of the Documentation subtree
-- -------------------------------------------------------------------------

CREATE VIEW doc_folders AS
SELECT f.id,
       f.project_id,
       CASE WHEN f.parent_id = (SELECT r.id FROM file_folders r
                                 WHERE r.project_id = f.project_id AND r.system_key = 'documentation')
            THEN NULL ELSE f.parent_id END AS parent_id,
       f.name,
       f.position,
       f.created_by_member AS created_by,
       f.created_at,
       f.updated_at
  FROM file_folders f
 WHERE f.area = 'docs' AND f.system_key IS NULL AND f.deleted_at IS NULL;

CREATE OR REPLACE FUNCTION doc_folders_view_insert() RETURNS trigger AS $$
DECLARE
    v_root UUID := file_ensure_system_folder(NEW.project_id, 'documentation', 'Documentation');
BEGIN
    INSERT INTO file_folders (id, project_id, parent_id, name, position, created_by, created_by_member, created_at, updated_at)
    VALUES (COALESCE(NEW.id, gen_random_uuid()), NEW.project_id, COALESCE(NEW.parent_id, v_root), NEW.name,
            COALESCE(NEW.position, 0),
            (SELECT pm.account_id FROM project_members pm WHERE pm.id = NEW.created_by), NEW.created_by,
            COALESCE(NEW.created_at, NOW()), COALESCE(NEW.updated_at, NOW()))
    RETURNING id INTO NEW.id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION doc_folders_view_update() RETURNS trigger AS $$
DECLARE
    v_root UUID := file_ensure_system_folder(OLD.project_id, 'documentation', 'Documentation');
BEGIN
    UPDATE file_folders
       SET parent_id = COALESCE(NEW.parent_id, v_root),
           name = NEW.name,
           position = NEW.position,
           updated_at = NEW.updated_at
     WHERE id = OLD.id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Deleting a documentation folder trashes it with everything below: its
-- subfolders, documents and files get one shared timestamp and come back
-- together on restore (documents.trashed_at, above).
CREATE OR REPLACE FUNCTION doc_folders_view_delete() RETURNS trigger AS $$
DECLARE
    v_ts TIMESTAMPTZ := now();
BEGIN
    WITH RECURSIVE sub AS (
        SELECT id FROM file_folders WHERE id = OLD.id AND deleted_at IS NULL
        UNION ALL
        SELECT c.id FROM file_folders c JOIN sub s ON c.parent_id = s.id WHERE c.deleted_at IS NULL
    )
    UPDATE file_folders SET deleted_at = v_ts, updated_at = v_ts WHERE id IN (SELECT id FROM sub);

    UPDATE documents SET deleted_at = v_ts, trashed_at = v_ts
     WHERE deleted_at IS NULL AND folder_id IN (SELECT id FROM file_folders WHERE deleted_at = v_ts);
    UPDATE files SET deleted_at = v_ts, updated_at = v_ts
     WHERE deleted_at IS NULL AND folder_id IN (SELECT id FROM file_folders WHERE deleted_at = v_ts);
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER doc_folders_instead_insert INSTEAD OF INSERT ON doc_folders
    FOR EACH ROW EXECUTE FUNCTION doc_folders_view_insert();
CREATE TRIGGER doc_folders_instead_update INSTEAD OF UPDATE ON doc_folders
    FOR EACH ROW EXECUTE FUNCTION doc_folders_view_update();
CREATE TRIGGER doc_folders_instead_delete INSTEAD OF DELETE ON doc_folders
    FOR EACH ROW EXECUTE FUNCTION doc_folders_view_delete();

-- -------------------------------------------------------------------------
-- Change feed: folder writes in the Documentation subtree bump folders_seq
-- exactly as writes to doc_folders did.
-- -------------------------------------------------------------------------

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

CREATE TRIGGER file_folders_docs_bump_ins AFTER INSERT ON file_folders
    FOR EACH ROW WHEN (NEW.area = 'docs' AND NEW.system_key IS NULL)
    EXECUTE FUNCTION doc_folders_bump_seq();
CREATE TRIGGER file_folders_docs_bump_upd AFTER UPDATE ON file_folders
    FOR EACH ROW WHEN (NEW.area = 'docs' AND NEW.system_key IS NULL)
    EXECUTE FUNCTION doc_folders_bump_seq();
CREATE TRIGGER file_folders_docs_bump_del AFTER DELETE ON file_folders
    FOR EACH ROW WHEN (OLD.area = 'docs' AND OLD.system_key IS NULL)
    EXECUTE FUNCTION doc_folders_bump_seq();

-- -------------------------------------------------------------------------
-- file_items: one list of folders, documents and files
--
--   * area is 'docs' for everything in the Documentation subtree (its folders,
--     its documents and the files inside it); the router requires the docs
--     actions there.
--   * A document is the Markdown file "<title>.md" (text/markdown). Its parent
--     is its folder, or the Documentation folder when it has none. Only
--     documents of the docs tree appear (notes belong to a view); a trashed one
--     appears with deleted_at = trashed_at, like any trashed file.
--   * Documents carry no storage object; the file manager never renames, moves
--     or downloads one (the docs editor and the docs API do).
-- -------------------------------------------------------------------------

CREATE OR REPLACE VIEW file_items AS
    SELECT d.id, 'folder'::text AS kind, d.project_id, d.parent_id, d.name,
           COALESCE(d.system_key, '') AS system_key, ''::text AS source,
           ''::text AS content_type, 0::bigint AS size, ''::text AS storage_key, ''::text AS bucket,
           d.created_by AS owner_id, d.created_at, d.updated_at, d.deleted_at, d.deleted_by,
           COALESCE(d.area, '') AS area
      FROM file_folders d
    UNION ALL
    SELECT f.id, 'file'::text, f.project_id, f.folder_id, f.file_name,
           ''::text, f.source,
           f.content_type, f.file_size, f.storage_key, f.bucket,
           f.uploaded_by, f.created_at, f.updated_at, f.deleted_at, f.deleted_by,
           COALESCE(ff.area, '')
      FROM files f
      LEFT JOIN file_folders ff ON ff.id = f.folder_id
     WHERE f.upload_status = 'uploaded' AND (f.source = 'upload' OR f.folder_id IS NOT NULL)
    UNION ALL
    SELECT dc.id, 'doc'::text, dc.project_id,
           COALESCE(dc.folder_id, (SELECT r.id FROM file_folders r
                                    WHERE r.project_id = dc.project_id AND r.system_key = 'documentation')),
           dc.title || '.md',
           ''::text, ''::text,
           'text/markdown'::text, 0::bigint, ''::text, ''::text,
           (SELECT pm.account_id FROM project_members pm WHERE pm.id = dc.created_by),
           dc.created_at, dc.updated_at, dc.trashed_at, NULL::uuid,
           'docs'::text
      FROM documents dc
     WHERE dc.view_id IS NULL AND (dc.deleted_at IS NULL OR dc.trashed_at IS NOT NULL);
