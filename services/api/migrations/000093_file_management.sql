-- 000093_file_management.sql
-- Drive-style file management (docs/product/file-management.md).
--
-- Every uploaded file already has a row in `files`, so that table becomes the
-- manageable item: it gains a scope (project_id; NULL = the general
-- workspace scope), a folder, a source and trash columns. Only folders are
-- new (file_folders). The view file_items presents both as one list.
--
-- Files are classified from their storage-key prefix by a trigger, so the
-- existing upload services (task attachments, doc images, annotation
-- screenshots) keep working unchanged:
--   tasks/<taskId>/...       -> source task_attachment, folder "Task attachments/<KEY-n>"
--   docs/<docId>/...         -> source doc,             folder "Document files"
--   annotations/<fileId>/... -> source annotation,      folder "Annotation screenshots"
--                               (assigned once an annotation references it)
--   files/<scope>/...        -> source upload (the file manager sets scope and folder)
-- Anything else (e.g. the transient avatar handshake rows) is source 'other'
-- and never appears in the manager.
--
-- The last section gives existing roles the files:read / files:write actions.

CREATE TABLE IF NOT EXISTS file_folders (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID        REFERENCES projects(id) ON DELETE CASCADE,
    parent_id   UUID        REFERENCES file_folders(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    system_key  TEXT        CHECK (system_key IN ('task_attachments', 'task', 'docs', 'annotations')),
    created_by  UUID        REFERENCES accounts(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Trash: a trashed folder and everything below it share one deleted_at,
    -- so a restore brings back exactly that subtree.
    deleted_at  TIMESTAMPTZ,
    deleted_by  UUID        REFERENCES accounts(id) ON DELETE SET NULL,
    CONSTRAINT chk_file_folders_name CHECK (btrim(name) <> '' AND name !~ '[/\\]')
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_file_folders_system
    ON file_folders (project_id, system_key) WHERE system_key IN ('task_attachments', 'docs', 'annotations');
CREATE UNIQUE INDEX IF NOT EXISTS ux_file_folders_task
    ON file_folders (parent_id, name) WHERE system_key = 'task';
CREATE INDEX IF NOT EXISTS idx_file_folders_parent ON file_folders (parent_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_file_folders_project ON file_folders (project_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_file_folders_trash ON file_folders (deleted_at) WHERE deleted_at IS NOT NULL;

ALTER TABLE files
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS folder_id  UUID REFERENCES file_folders(id) ON DELETE CASCADE,
    ADD COLUMN IF NOT EXISTS source     TEXT NOT NULL DEFAULT 'other'
        CHECK (source IN ('upload', 'task_attachment', 'doc', 'annotation', 'other')),
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_by UUID REFERENCES accounts(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_files_folder ON files (folder_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_files_project_root ON files (project_id) WHERE folder_id IS NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_files_trash ON files (deleted_at) WHERE deleted_at IS NOT NULL;

-- Per-user state. item_id is a folder or file id; ids are unique across both
-- tables, so one column serves both and there is no foreign key (the purge
-- removes the rows).
CREATE TABLE IF NOT EXISTS file_stars (
    user_id    UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    item_id    UUID        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_file_stars_item ON file_stars (item_id);

CREATE TABLE IF NOT EXISTS file_recents (
    user_id     UUID        NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    item_id     UUID        NOT NULL,
    accessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, item_id)
);
CREATE INDEX IF NOT EXISTS idx_file_recents_user ON file_recents (user_id, accessed_at DESC);
CREATE INDEX IF NOT EXISTS idx_file_recents_item ON file_recents (item_id);

-- -------------------------------------------------------------------------
-- Default (system) folders
-- -------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION file_ensure_system_folder(
    p_project UUID, p_key TEXT, p_name TEXT
) RETURNS UUID AS $$
DECLARE
    v_id UUID;
BEGIN
    SELECT id INTO v_id FROM file_folders WHERE project_id = p_project AND system_key = p_key;
    IF v_id IS NOT NULL THEN
        RETURN v_id;
    END IF;
    INSERT INTO file_folders (project_id, name, system_key)
    VALUES (p_project, p_name, p_key)
    ON CONFLICT DO NOTHING
    RETURNING id INTO v_id;
    IF v_id IS NULL THEN
        SELECT id INTO v_id FROM file_folders WHERE project_id = p_project AND system_key = p_key;
    END IF;
    RETURN v_id;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION file_ensure_task_folder(
    p_project UUID, p_number BIGINT, p_prefix TEXT
) RETURNS UUID AS $$
DECLARE
    v_root UUID;
    v_name TEXT;
    v_id   UUID;
BEGIN
    v_root := file_ensure_system_folder(p_project, 'task_attachments', 'Task attachments');
    v_name := COALESCE(NULLIF(p_prefix, ''), 'TASK') || '-' || p_number::text;
    SELECT id INTO v_id FROM file_folders WHERE parent_id = v_root AND system_key = 'task' AND name = v_name;
    IF v_id IS NOT NULL THEN
        RETURN v_id;
    END IF;
    INSERT INTO file_folders (project_id, parent_id, name, system_key)
    VALUES (p_project, v_root, v_name, 'task')
    ON CONFLICT DO NOTHING
    RETURNING id INTO v_id;
    IF v_id IS NULL THEN
        SELECT id INTO v_id FROM file_folders WHERE parent_id = v_root AND system_key = 'task' AND name = v_name;
    END IF;
    RETURN v_id;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION file_ensure_project_folders(p_project UUID) RETURNS VOID AS $$
BEGIN
    PERFORM file_ensure_system_folder(p_project, 'task_attachments', 'Task attachments');
    PERFORM file_ensure_system_folder(p_project, 'docs', 'Document files');
    PERFORM file_ensure_system_folder(p_project, 'annotations', 'Annotation screenshots');
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION file_on_project_insert() RETURNS trigger AS $$
BEGIN
    PERFORM file_ensure_project_folders(NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_file_project_insert ON projects;
CREATE TRIGGER trg_file_project_insert
    AFTER INSERT ON projects
    FOR EACH ROW EXECUTE FUNCTION file_on_project_insert();

-- -------------------------------------------------------------------------
-- Classification of files by storage key
-- -------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION file_classify(p_key TEXT)
RETURNS TABLE (out_source TEXT, out_project UUID, out_folder UUID) AS $$
DECLARE
    v_uuid_re CONSTANT TEXT := '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
    v_seg     TEXT := split_part(p_key, '/', 2);
    v_project UUID;
    v_number  BIGINT;
    v_prefix  TEXT;
BEGIN
    out_project := NULL;
    out_folder  := NULL;
    IF p_key LIKE 'files/%' THEN
        out_source := 'upload';
    ELSIF p_key LIKE 'tasks/%' THEN
        out_source := 'task_attachment';
        IF v_seg ~ v_uuid_re THEN
            SELECT t.project_id, t.task_number, p.task_id_prefix INTO v_project, v_number, v_prefix
              FROM tasks t JOIN projects p ON p.id = t.project_id WHERE t.id = v_seg::uuid;
            IF FOUND THEN
                out_project := v_project;
                out_folder  := file_ensure_task_folder(v_project, v_number, v_prefix);
            END IF;
        END IF;
    ELSIF p_key LIKE 'docs/%' THEN
        out_source := 'doc';
        IF v_seg ~ v_uuid_re THEN
            SELECT d.project_id INTO v_project FROM documents d WHERE d.id = v_seg::uuid;
            IF FOUND THEN
                out_project := v_project;
                out_folder  := file_ensure_system_folder(v_project, 'docs', 'Document files');
            END IF;
        END IF;
    ELSIF p_key LIKE 'annotations/%' THEN
        out_source := 'annotation';
    ELSE
        out_source := 'other';
    END IF;
    RETURN NEXT;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION files_before_insert() RETURNS trigger AS $$
DECLARE
    c RECORD;
BEGIN
    SELECT * INTO c FROM file_classify(NEW.storage_key);
    NEW.source := c.out_source;
    -- The file manager sets scope and folder itself for its own uploads.
    IF NEW.source <> 'upload' THEN
        NEW.project_id := c.out_project;
        NEW.folder_id  := c.out_folder;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_files_before_insert ON files;
CREATE TRIGGER trg_files_before_insert
    BEFORE INSERT ON files
    FOR EACH ROW EXECUTE FUNCTION files_before_insert();

-- A screenshot has no project until an annotation references it.
CREATE OR REPLACE FUNCTION file_on_annotation_screenshot() RETURNS trigger AS $$
BEGIN
    IF NEW.screenshot_file_id IS NOT NULL THEN
        UPDATE files
           SET project_id = NEW.project_id,
               folder_id  = file_ensure_system_folder(NEW.project_id, 'annotations', 'Annotation screenshots')
         WHERE id = NEW.screenshot_file_id AND source = 'annotation' AND folder_id IS NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_file_annotation_screenshot ON page_annotations;
CREATE TRIGGER trg_file_annotation_screenshot
    AFTER INSERT OR UPDATE OF screenshot_file_id ON page_annotations
    FOR EACH ROW EXECUTE FUNCTION file_on_annotation_screenshot();

-- Removing an attachment from a task sends its file to the trash instead of
-- leaving it in the task's folder with nothing pointing at it. Only files that
-- have a folder: the others are not in file_items, so the trash list and the
-- purge could never reach a row stamped here.
CREATE OR REPLACE FUNCTION file_on_task_attachment_delete() RETURNS trigger AS $$
BEGIN
    UPDATE files SET deleted_at = NOW()
     WHERE id = OLD.file_id AND source = 'task_attachment'
       AND folder_id IS NOT NULL AND deleted_at IS NULL;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_file_task_attachment_delete ON task_attachments;
CREATE TRIGGER trg_file_task_attachment_delete
    AFTER DELETE ON task_attachments
    FOR EACH ROW EXECUTE FUNCTION file_on_task_attachment_delete();

-- -------------------------------------------------------------------------
-- One list of folders and files
-- -------------------------------------------------------------------------

CREATE OR REPLACE VIEW file_items AS
    SELECT d.id, 'folder'::text AS kind, d.project_id, d.parent_id, d.name,
           COALESCE(d.system_key, '') AS system_key, ''::text AS source,
           ''::text AS content_type, 0::bigint AS size, ''::text AS storage_key, ''::text AS bucket,
           d.created_by AS owner_id, d.created_at, d.updated_at, d.deleted_at, d.deleted_by
      FROM file_folders d
    UNION ALL
    SELECT f.id, 'file'::text, f.project_id, f.folder_id, f.file_name,
           ''::text, f.source,
           f.content_type, f.file_size, f.storage_key, f.bucket,
           f.uploaded_by, f.created_at, f.updated_at, f.deleted_at, f.deleted_by
      FROM files f
     WHERE f.upload_status = 'uploaded' AND (f.source = 'upload' OR f.folder_id IS NOT NULL);

-- -------------------------------------------------------------------------
-- Backfill
-- -------------------------------------------------------------------------

DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN SELECT id FROM projects LOOP
        PERFORM file_ensure_project_folders(r.id);
    END LOOP;

    FOR r IN SELECT id, storage_key FROM files WHERE source = 'other' LOOP
        UPDATE files f
           SET source = c.out_source,
               project_id = c.out_project,
               folder_id = c.out_folder
          FROM file_classify(r.storage_key) c
         WHERE f.id = r.id;
    END LOOP;

    FOR r IN SELECT screenshot_file_id AS file_id, project_id FROM page_annotations WHERE screenshot_file_id IS NOT NULL LOOP
        UPDATE files
           SET project_id = r.project_id,
               folder_id  = file_ensure_system_folder(r.project_id, 'annotations', 'Annotation screenshots')
         WHERE id = r.file_id AND source = 'annotation' AND folder_id IS NULL;
    END LOOP;

    -- Task attachments removed from their task before this migration left the
    -- file behind; those files are now unreferenced, so trash them. Rows with
    -- no folder (their task is gone) are not in file_items and stay as they are:
    -- trashing them would hide them from the trash list and the purge.
    UPDATE files SET deleted_at = NOW()
     WHERE source = 'task_attachment' AND upload_status = 'uploaded' AND deleted_at IS NULL
       AND folder_id IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM task_attachments ta WHERE ta.file_id = files.id);
END;
$$;

-- -------------------------------------------------------------------------
-- Grants on existing roles
--
-- Gives existing roles the new files:read / files:write actions
-- (docs/product/file-management.md), so the Files pages work on an upgraded
-- installation without an administrator editing every role by hand.
--
-- Seeding never rewrites the policy of a role that already exists (an
-- administrator may have edited it), so the defaults shipped in
-- bootstrap/defaultroles only reach new installations. Existing roles get:
--
--   * the built-in platform role ADMIN: read and write on the general scope
--     ("file/*"), matching the shipped policy. USER gets nothing: files:read
--     and files:write cover every file of a scope, so the general files are
--     granted to a role on purpose, not to everyone by default;
--   * every other role that grants docs:read or docs:write in an Allow
--     statement confined to projects (every resource under "project/"): the
--     matching files action in the same statement, because files are project
--     content of the same kind (a project's documents and the files embedded in
--     them). A statement that can match "file/*", such as resource "*", is left
--     alone: the general scope is granted deliberately, never derived. Roles
--     with "*" already cover it.

UPDATE roles
   SET policy = jsonb_set(policy, '{statements}',
        COALESCE(policy->'statements', '[]'::jsonb) || jsonb_build_array(jsonb_build_object(
            'sid', 'GeneralFiles',
            'effect', 'Allow',
            'actions', jsonb_build_array('files:read', 'files:write'),
            'resources', jsonb_build_array('file/*'))))
 WHERE project_id IS NULL
   AND name = 'ADMIN'
   AND policy::text NOT LIKE '%files:%';

UPDATE roles r
   SET policy = jsonb_set(r.policy, '{statements}', (
        SELECT COALESCE(jsonb_agg(
            CASE
              WHEN s->>'effect' = 'Allow' AND jsonb_typeof(s->'actions') = 'array'
                   AND jsonb_typeof(s->'resources') = 'array'
                   -- only statements confined to projects: one that can match
                   -- "file/*" (e.g. resource "*") would hand out the general scope
                   AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements_text(s->'resources') AS res
                                    WHERE res NOT LIKE 'project/%') THEN
                jsonb_set(s, '{actions}',
                  (s->'actions')
                  || CASE WHEN (s->'actions') ? 'docs:read'  AND NOT (s->'actions') ? 'files:read'
                          THEN '["files:read"]'::jsonb  ELSE '[]'::jsonb END
                  || CASE WHEN (s->'actions') ? 'docs:write' AND NOT (s->'actions') ? 'files:write'
                          THEN '["files:write"]'::jsonb ELSE '[]'::jsonb END)
              ELSE s
            END ORDER BY ord), '[]'::jsonb)
          FROM jsonb_array_elements(r.policy->'statements') WITH ORDINALITY AS t(s, ord)))
 WHERE jsonb_typeof(r.policy->'statements') = 'array'
   AND NOT (project_id IS NULL AND name = 'ADMIN')
   AND policy::text ~ '"docs:(read|write)"';
