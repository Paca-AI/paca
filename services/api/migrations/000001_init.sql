-- 000001_init.sql
-- Full schema for the Paca API service (consolidated from all previous migrations).
-- This is the baseline schema for v0.1.1 and earlier. For upgrades, apply subsequent migrations (e.g. 000002_add_story_points.sql) after this file.
-- Run via: psql "$DATABASE_URL" -f migrations/000001_init.sql

BEGIN;

-- -------------------------------------------------------------------------
-- GLOBAL ROLES
-- Must be created before users because users.role_id references this table.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS global_roles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    permissions JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uni_global_roles_name ON global_roles (name);

-- -------------------------------------------------------------------------
-- USERS
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS users (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    username             TEXT        NOT NULL,
    password_hash        TEXT        NOT NULL,
    full_name            TEXT        NOT NULL DEFAULT '',
    role_id              UUID        NOT NULL,
    must_change_password BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at           TIMESTAMPTZ,
    CONSTRAINT fk_users_role_id
        FOREIGN KEY (role_id)
        REFERENCES global_roles(id)
        ON DELETE RESTRICT
);

-- Scoped to ACTIVE users (deleted_at IS NULL) so this init migration is safe to
-- re-run on every boot (RunMigrations replays all files, relying on idempotency)
-- once a username has a soft-deleted duplicate — which becomes legal at 000016.
-- A FULL unique index here instead fails with 23505 the moment a user is deleted
-- and the name reused, crash-looping the API on its next restart. This matches the
-- final state 000016 (scope_username_unique_to_active) establishes anyway.
CREATE UNIQUE INDEX IF NOT EXISTS uni_users_username ON users (username) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_role_id    ON users (role_id);

-- -------------------------------------------------------------------------
-- PROJECTS & TEAM MANAGEMENT
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS projects (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT        NOT NULL,
    description      TEXT        NOT NULL DEFAULT '',
    -- task_id_prefix: short uppercase alphanumeric tag prepended to task_number
    -- to form a human-readable task ID, e.g. "PACA" → "PACA-1", "PACA-2".
    task_id_prefix   TEXT        NOT NULL DEFAULT '',
    settings         JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS project_roles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID,
    role_name   TEXT        NOT NULL,
    permissions JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_project_roles_project
        FOREIGN KEY (project_id)
        REFERENCES projects(id)
        ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS project_members (
    id              UUID        NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID        NOT NULL,
    user_id         UUID        NOT NULL,
    project_role_id UUID        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,
    CONSTRAINT fk_project_members_project
        FOREIGN KEY (project_id)
        REFERENCES projects(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_project_members_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_project_members_role
        FOREIGN KEY (project_role_id)
        REFERENCES project_roles(id)
        ON DELETE RESTRICT
);

-- Unique active membership per project+user (soft-deleted rows are excluded).
CREATE UNIQUE INDEX IF NOT EXISTS uq_project_members_active
    ON project_members (project_id, user_id)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_project_roles_project_role_name
    ON project_roles (project_id, role_name)
    WHERE project_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_project_roles_template_role_name
    ON project_roles (role_name)
    WHERE project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_project_members_project_id ON project_members (project_id);
CREATE INDEX IF NOT EXISTS idx_project_members_user_id    ON project_members (user_id);
CREATE INDEX IF NOT EXISTS idx_project_members_role_id    ON project_members (project_role_id);
CREATE INDEX IF NOT EXISTS idx_project_members_deleted_at ON project_members (deleted_at) WHERE deleted_at IS NOT NULL;

-- -------------------------------------------------------------------------
-- SEED DATA: global roles
-- -------------------------------------------------------------------------

INSERT INTO global_roles (id, name, permissions, created_at, updated_at)
VALUES
    (gen_random_uuid(), 'SUPER_ADMIN', '{"*": true}'::jsonb, NOW(), NOW()),
    (gen_random_uuid(), 'ADMIN',       '{"users.*": true, "global_roles.*": true, "projects.*": true}'::jsonb, NOW(), NOW()),
    (gen_random_uuid(), 'USER',        '{"users.read": true}'::jsonb, NOW(), NOW())
ON CONFLICT (name) DO UPDATE
SET permissions = EXCLUDED.permissions,
    updated_at  = NOW();

-- -------------------------------------------------------------------------
-- TASK TYPES
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS task_types (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    icon        TEXT,
    color       TEXT,
    description TEXT,
    is_default  BOOLEAN     NOT NULL DEFAULT false,
    is_system   BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- At most one default task type per project.
CREATE UNIQUE INDEX IF NOT EXISTS uq_task_types_one_default
    ON task_types (project_id) WHERE is_default = true;

-- -------------------------------------------------------------------------
-- TASK STATUSES
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS task_statuses (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    color       TEXT,
    position    INTEGER     NOT NULL DEFAULT 0,
    category    TEXT        NOT NULL CHECK (category IN ('backlog','refinement','ready','todo','inprogress','done')),
    is_default  BOOLEAN     NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- At most one default task status per project.
CREATE UNIQUE INDEX IF NOT EXISTS uq_task_statuses_one_default
    ON task_statuses (project_id) WHERE is_default = true;

-- -------------------------------------------------------------------------
-- SPRINTS
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS sprints (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT        NOT NULL,
    start_date  DATE,
    end_date    DATE,
    goal        TEXT,
    status      TEXT        NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','active','completed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- -------------------------------------------------------------------------
-- CUSTOM FIELD DEFINITIONS
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS custom_field_definitions (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    field_key    TEXT        NOT NULL,
    display_name TEXT        NOT NULL,
    field_type   TEXT        NOT NULL,
    options      JSONB,
    is_required  BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, field_key)
);

-- -------------------------------------------------------------------------
-- TASKS
-- importance: unsigned integer (>=0); higher value = more important.
-- board_position: ordering of the card within its status column on the
--                 kanban board; lower value appears first.
-- -------------------------------------------------------------------------

-- -------------------------------------------------------------------------
-- SPRINT VIEWS
-- Each sprint (or product backlog) can have multiple named views.
-- sprint_id:  NULL for product-backlog views; set for sprint-scoped views.
-- project_id: always set — identifies the owning project.
-- view_type: table | board | roadmap
-- config: jsonb with optional keys:
--   fields      text[]   – ordered list of visible column names
--   column_by   text     – field used to group columns/groups (default: status)
--   swimlanes   text     – horizontal swimlane grouping field (null = none)
--   sort_by     text     – "manual" or any sortable field name
--   field_sum   text     – "count" (default) or a numeric field key
--   slice_by    text     – extra filter dimension (null = none)
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS sprint_views (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    sprint_id    UUID        REFERENCES sprints(id) ON DELETE CASCADE,
    project_id   UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name         TEXT        NOT NULL,
    view_type    TEXT        NOT NULL DEFAULT 'table'
                             CHECK (view_type IN ('table','board','roadmap')),
    view_context TEXT        NOT NULL DEFAULT 'sprint'
                             CHECK (view_context IN ('sprint', 'backlog', 'timeline')),
    config       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    position     DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sprint_views_sprint_id  ON sprint_views (sprint_id);
CREATE INDEX IF NOT EXISTS idx_sprint_views_project_id ON sprint_views (project_id);
CREATE INDEX IF NOT EXISTS idx_sprint_views_context    ON sprint_views (project_id, view_context) WHERE sprint_id IS NULL;

-- -------------------------------------------------------------------------
-- VIEW TASK POSITIONS
-- Stores manually-defined per-view task ordering.
-- Only relevant when sprint_views.config->>'sort_by' = 'manual'.
-- position:  zero-based rank within the group; lower = higher in list.
-- group_key: value of the column_by field for this task (e.g. status name,
--            assignee id).  NULL means the view has no grouping dimension.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS view_task_positions (
    id        UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
    view_id   UUID    NOT NULL REFERENCES sprint_views(id) ON DELETE CASCADE,
    task_id   UUID    NOT NULL,
    position  DOUBLE PRECISION NOT NULL DEFAULT 0,
    group_key TEXT,
    CONSTRAINT uq_view_task_positions_view_task UNIQUE (view_id, task_id)
);

CREATE INDEX IF NOT EXISTS idx_view_task_positions_view_id ON view_task_positions (view_id);

-- -------------------------------------------------------------------------
-- TASK COUNTERS
-- Tracks the per-project sequential task number so that every task within a
-- project gets a human-readable, monotonically increasing identifier.
-- The counter is incremented atomically via INSERT ... ON CONFLICT DO UPDATE.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS task_counters (
    project_id UUID    PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    last_value BIGINT  NOT NULL DEFAULT 0
);

-- -------------------------------------------------------------------------
-- TASKS
-- importance: unsigned integer (>=0); higher value = more important.
-- task_number: project-scoped sequential ID (1, 2, 3, …) assigned at
--              creation and never reused; enables human-readable references
--              like "#42" within a project.
-- Task ordering is managed per-view via the view_task_positions table.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tasks (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    task_number    BIGINT      NOT NULL DEFAULT 0,
    task_type_id   UUID        REFERENCES task_types(id) ON DELETE SET NULL,
    status_id      UUID        REFERENCES task_statuses(id) ON DELETE SET NULL,
    sprint_id      UUID        REFERENCES sprints(id) ON DELETE SET NULL,
    parent_task_id UUID        REFERENCES tasks(id) ON DELETE SET NULL,
    title          TEXT        NOT NULL,
    description    JSONB,
    importance     INTEGER     NOT NULL DEFAULT 0 CHECK (importance >= 0),
    assignee_id    UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    reporter_id    UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    custom_fields  JSONB       NOT NULL DEFAULT '{}'::jsonb,
    start_date     DATE,
    due_date       DATE,
    tags           JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ,
    CONSTRAINT uq_tasks_project_task_number UNIQUE (project_id, task_number)
);

CREATE INDEX IF NOT EXISTS idx_tasks_project_id   ON tasks (project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_task_number  ON tasks (project_id, task_number);
CREATE INDEX IF NOT EXISTS idx_tasks_status_id    ON tasks (status_id);
CREATE INDEX IF NOT EXISTS idx_tasks_sprint_id    ON tasks (sprint_id);
CREATE INDEX IF NOT EXISTS idx_tasks_deleted_at   ON tasks (deleted_at) WHERE deleted_at IS NOT NULL;

-- -------------------------------------------------------------------------
-- FILES
-- Central file-metadata registry.  One row per uploaded object in the
-- object-store (S3-compatible).  Other tables (e.g. task_attachments)
-- reference this table so the same file can be attached to multiple
-- entities without duplicating metadata.
--
-- upload_status lifecycle:
--   pending  → file record created, presigned upload URL issued.
--   uploaded → client confirmed upload (or multipart completed).
--   failed   → upload abandoned / timed-out (candidates for cleanup).
--
-- multipart_upload_id: non-NULL only while a multipart upload is in
--   progress; cleared after CompleteMultipartUpload.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS files (
    id                   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    storage_key          TEXT        NOT NULL UNIQUE,
    bucket               TEXT        NOT NULL,
    file_name            TEXT        NOT NULL,
    content_type         TEXT        NOT NULL DEFAULT 'application/octet-stream',
    file_size            BIGINT      NOT NULL DEFAULT 0,
    upload_status        TEXT        NOT NULL DEFAULT 'pending'
                             CHECK (upload_status IN ('pending','uploaded','failed')),
    multipart_upload_id  TEXT,
    uploaded_by          UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_files_uploaded_by    ON files (uploaded_by);
CREATE INDEX IF NOT EXISTS idx_files_upload_status  ON files (upload_status) WHERE upload_status != 'uploaded';

-- -------------------------------------------------------------------------
-- TASK ATTACHMENTS
-- Links a confirmed file to a task.  The file record itself lives in the
-- files table; task_attachments is a pure join table with audit columns.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS task_attachments (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    file_id    UUID        NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    created_by UUID        REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_task_attachments_task_file UNIQUE (task_id, file_id)
);

CREATE INDEX IF NOT EXISTS idx_task_attachments_task_id ON task_attachments (task_id);
CREATE INDEX IF NOT EXISTS idx_task_attachments_file_id ON task_attachments (file_id);

-- -------------------------------------------------------------------------
-- TASK CHECKLISTS
-- A task can have multiple named checklist groups.
-- position: zero-based order among checklists on the same task.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS task_checklists (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    title      TEXT        NOT NULL,
    position   INTEGER     NOT NULL DEFAULT 0,
    created_by UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_checklists_task_id ON task_checklists (task_id);

-- -------------------------------------------------------------------------
-- TASK CHECKLIST ITEMS
-- Individual checkbox items within a checklist group.
-- checked_by / checked_at: audit trail for who checked the item and when.
-- assignee_id: optional per-item owner, independent of the parent task assignee.
-- position: zero-based order within the checklist; lower = higher in list.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS task_checklist_items (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    checklist_id UUID        NOT NULL REFERENCES task_checklists(id) ON DELETE CASCADE,
    title        TEXT        NOT NULL,
    is_checked   BOOLEAN     NOT NULL DEFAULT FALSE,
    checked_by   UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    checked_at   TIMESTAMPTZ,
    assignee_id  UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    due_date     DATE,
    position     INTEGER     NOT NULL DEFAULT 0,
    created_by   UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_checklist_items_checklist_id ON task_checklist_items (checklist_id, position);

-- -------------------------------------------------------------------------
-- -------------------------------------------------------------------------
-- TASK ACTIVITIES
-- Unified log of system-generated change events and user comments on tasks.
--
-- actor_id:      References project_members(id) — the member who performed the
--                action.  NULL for system events or when the member record has
--                been hard-deleted.
-- activity_type: discriminator; see ActivityType constants in the Go domain.
-- content:       JSONB payload whose shape varies by activity_type.
--                comments  → {"text": "..."}
--                task.updated → {"changes": [{"field":"...","old":"...","new":"..."}]}
--                etc.
-- deleted_at:    soft-delete timestamp; only set for user comments.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS task_activities (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    actor_id      UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    activity_type TEXT        NOT NULL,
    content       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_task_activities_task_id ON task_activities (task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_task_activities_actor_id ON task_activities (actor_id) WHERE actor_id IS NOT NULL;

-- -------------------------------------------------------------------------
-- DOC FOLDERS
-- Self-referencing hierarchy: parent_id = NULL means root-level folder.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS doc_folders (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    parent_id  UUID        REFERENCES doc_folders(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    position   INTEGER     NOT NULL DEFAULT 0,
    created_by UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_doc_folders_project_id ON doc_folders (project_id);
CREATE INDEX IF NOT EXISTS idx_doc_folders_parent_id  ON doc_folders (parent_id);

-- -------------------------------------------------------------------------
-- DOCUMENTS
-- BlockNote content stored as JSONB. Soft-deleted via deleted_at.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS documents (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    folder_id  UUID        REFERENCES doc_folders(id) ON DELETE SET NULL,
    title      TEXT        NOT NULL DEFAULT 'Untitled',
    content    JSONB,
    position   INTEGER     NOT NULL DEFAULT 0,
    created_by UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    updated_by UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_documents_project_id ON documents (project_id);
CREATE INDEX IF NOT EXISTS idx_documents_folder_id  ON documents (folder_id);
CREATE INDEX IF NOT EXISTS idx_documents_deleted_at ON documents (deleted_at) WHERE deleted_at IS NOT NULL;

-- -------------------------------------------------------------------------
-- DOC SNAPSHOTS
-- Point-in-time copies of document content for history and diffing.
-- snapshot_number is auto-incremented per document by the trigger below.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS doc_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     UUID        NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    title           TEXT        NOT NULL,
    content         JSONB,
    snapshot_number BIGINT      NOT NULL DEFAULT 0,
    created_by      UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_doc_snapshots_document_id ON doc_snapshots (document_id);
CREATE UNIQUE INDEX IF NOT EXISTS uni_doc_snapshots_doc_number
    ON doc_snapshots (document_id, snapshot_number);

CREATE OR REPLACE FUNCTION fn_doc_snapshot_number()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- Lock the parent document row to serialize concurrent inserts and
    -- prevent duplicate snapshot_number values due to race conditions.
    PERFORM id FROM documents WHERE id = NEW.document_id FOR UPDATE;
    NEW.snapshot_number := COALESCE(
        (SELECT MAX(snapshot_number) FROM doc_snapshots WHERE document_id = NEW.document_id),
        0
    ) + 1;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_doc_snapshot_number ON doc_snapshots;
CREATE TRIGGER trg_doc_snapshot_number
    BEFORE INSERT ON doc_snapshots
    FOR EACH ROW EXECUTE FUNCTION fn_doc_snapshot_number();

-- -------------------------------------------------------------------------
-- DOC ACTIVITIES
-- Audit log for system events (doc.created, doc.updated, doc.deleted,
-- doc.moved) and user comments. Same pattern as task_activities.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS doc_activities (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id   UUID        NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    actor_id      UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    activity_type TEXT        NOT NULL,
    content       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_doc_activities_document_id ON doc_activities (document_id);
CREATE INDEX IF NOT EXISTS idx_doc_activities_deleted_at  ON doc_activities (deleted_at) WHERE deleted_at IS NOT NULL;

-- -------------------------------------------------------------------------
-- NOTIFICATIONS
-- Task-assignment and @mention notifications.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS notifications (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_member_id   UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    type              TEXT        NOT NULL CHECK (type IN ('assigned', 'mentioned')),
    task_id           UUID        REFERENCES tasks(id) ON DELETE CASCADE,
    project_id        UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    read_at           TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_recipient         ON notifications (recipient_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_recipient_unread  ON notifications (recipient_user_id) WHERE read_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_notifications_task_id           ON notifications (task_id) WHERE task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_notifications_project_id        ON notifications (project_id);

-- -------------------------------------------------------------------------
-- GITHUB INTEGRATIONS
-- One row per project. Stores the encrypted personal access token.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_integrations (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id       UUID        NOT NULL UNIQUE
                                     REFERENCES projects(id) ON DELETE CASCADE,
    access_token_enc TEXT        NOT NULL,  -- AES-256-GCM encrypted PAT (base64)
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_github_integrations_project_id
    ON github_integrations (project_id);

-- -------------------------------------------------------------------------
-- GITHUB REPOSITORIES
-- One row per project. Tracks the linked repository and its webhook.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_repositories (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id         UUID        NOT NULL
                                       REFERENCES projects(id) ON DELETE CASCADE,
    integration_id     UUID        NOT NULL
                                       REFERENCES github_integrations(id) ON DELETE CASCADE,
    owner              TEXT        NOT NULL,
    repo_name          TEXT        NOT NULL,
    full_name          TEXT        NOT NULL,   -- "owner/repo"
    webhook_id         BIGINT,                -- GitHub webhook ID; NULL until created
    webhook_secret_enc TEXT,                  -- encrypted HMAC secret for webhook validation
    default_branch     TEXT        NOT NULL DEFAULT 'main',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_github_repositories_project_repo UNIQUE (project_id, full_name)
);

CREATE INDEX IF NOT EXISTS idx_github_repositories_project_id
    ON github_repositories (project_id);
CREATE INDEX IF NOT EXISTS idx_github_repositories_integration_id
    ON github_repositories (integration_id);
CREATE INDEX IF NOT EXISTS idx_github_repositories_full_name
    ON github_repositories (full_name);

-- -------------------------------------------------------------------------
-- GITHUB PULL REQUESTS
-- Cached PR metadata. Upserted on incoming webhook events or manual linking.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_pull_requests (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repo_id      UUID        NOT NULL REFERENCES github_repositories(id) ON DELETE CASCADE,
    pr_number    INTEGER     NOT NULL,
    github_pr_id BIGINT      NOT NULL,
    title        TEXT        NOT NULL DEFAULT '',
    state        TEXT        NOT NULL DEFAULT 'open',  -- open | closed | merged
    html_url     TEXT        NOT NULL DEFAULT '',
    head_branch  TEXT        NOT NULL DEFAULT '',
    base_branch  TEXT        NOT NULL DEFAULT '',
    author       TEXT        NOT NULL DEFAULT '',
    merged_at    TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_github_pull_requests_repo_pr UNIQUE (repo_id, pr_number)
);

CREATE INDEX IF NOT EXISTS idx_github_pull_requests_project_id
    ON github_pull_requests (project_id);
CREATE INDEX IF NOT EXISTS idx_github_pull_requests_repo_id
    ON github_pull_requests (repo_id);

-- -------------------------------------------------------------------------
-- GITHUB TASK-PR LINKS
-- Many-to-many between tasks and cached pull requests.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_task_pr_links (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    pull_request_id UUID        NOT NULL REFERENCES github_pull_requests(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_github_task_pr_links UNIQUE (task_id, pull_request_id)
);

CREATE INDEX IF NOT EXISTS idx_github_task_pr_links_task_id
    ON github_task_pr_links (task_id);
CREATE INDEX IF NOT EXISTS idx_github_task_pr_links_pull_request_id
    ON github_task_pr_links (pull_request_id);

-- -------------------------------------------------------------------------
-- GITHUB TASK BRANCHES
-- Links a git branch to a task.  Created when:
--   1. A branch is created via POST /projects/:projectId/tasks/:taskId/github/branches.
--   2. A push webhook event arrives and the branch name contains the
--      project's task-ID-prefix pattern (e.g. "feat/PROJ-42").
-- The branch naming rule is: any branch whose name contains the pattern
--   {TASK_ID_PREFIX}-{TASK_NUMBER} (case-insensitive) is linked to that task.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS github_task_branches (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID        NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    repo_id     UUID        NOT NULL REFERENCES github_repositories(id) ON DELETE CASCADE,
    branch_name TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_github_task_branches UNIQUE (task_id, repo_id, branch_name)
);

CREATE INDEX IF NOT EXISTS idx_github_task_branches_task_id
    ON github_task_branches (task_id);
CREATE INDEX IF NOT EXISTS idx_github_task_branches_repo_branch
    ON github_task_branches (repo_id, branch_name);

-- -------------------------------------------------------------------------
-- API KEYS
-- Users can create named API keys to authenticate programmatic API calls
-- instead of using short-lived JWTs.  The raw key is returned ONLY at
-- creation time; the server stores only the SHA-256 hash.
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS api_keys (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL,
    name         TEXT        NOT NULL,
    -- key_prefix: first 8 hex characters of the raw key, for display only.
    key_prefix   TEXT        NOT NULL,
    -- key_hash: SHA-256 hex digest of the raw key used for lookup/validation.
    key_hash     TEXT        NOT NULL,
    last_used_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at   TIMESTAMPTZ,
    CONSTRAINT fk_api_keys_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE
);

CREATE UNIQUE INDEX IF NOT EXISTS uni_api_keys_key_hash ON api_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys (user_id);

COMMIT;
