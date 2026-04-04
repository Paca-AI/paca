-- 000001_init.sql
-- Full schema for the Paca API service (consolidated from previous migrations).
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

CREATE UNIQUE INDEX IF NOT EXISTS uni_users_username ON users (username);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_role_id    ON users (role_id);

-- -------------------------------------------------------------------------
-- PROJECTS & TEAM MANAGEMENT
-- -------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS projects (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    settings    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
    id              UUID NOT NULL PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL,
    user_id         UUID NOT NULL,
    project_role_id UUID NOT NULL,
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
        ON DELETE RESTRICT,
    CONSTRAINT uq_project_members_project_user UNIQUE (project_id, user_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_project_roles_project_role_name
    ON project_roles (project_id, role_name)
    WHERE project_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_project_roles_template_role_name
    ON project_roles (role_name)
    WHERE project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_project_members_project_id ON project_members (project_id);
CREATE INDEX IF NOT EXISTS idx_project_members_user_id    ON project_members (user_id);
CREATE INDEX IF NOT EXISTS idx_project_members_role_id    ON project_members (project_role_id);

-- -------------------------------------------------------------------------
-- SEED DATA: global roles and template project roles
-- -------------------------------------------------------------------------

INSERT INTO global_roles (id, name, permissions, created_at, updated_at)
VALUES
    (gen_random_uuid(), 'SUPER_ADMIN', '{"*": true}'::jsonb, NOW(), NOW()),
    (gen_random_uuid(), 'ADMIN',       '{"users.*": true, "global_roles.*": true, "projects.*": true}'::jsonb, NOW(), NOW()),
    (gen_random_uuid(), 'USER',        '{"users.read": true}'::jsonb, NOW(), NOW())
ON CONFLICT (name) DO UPDATE
SET permissions = EXCLUDED.permissions,
    updated_at  = NOW();

INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at)
VALUES
    (gen_random_uuid(), NULL, 'PROJECT_OWNER',
     '{"projects.*": true, "project.members.*": true, "project.roles.*": true, "tasks.*": true, "sprints.*": true}'::jsonb,
     NOW(), NOW()),
    (gen_random_uuid(), NULL, 'PROJECT_MANAGER',
     '{"projects.read": true, "projects.write": true, "project.members.read": true, "project.members.write": true, "tasks.*": true, "sprints.*": true}'::jsonb,
     NOW(), NOW()),
    (gen_random_uuid(), NULL, 'PROJECT_MEMBER',
     '{"projects.read": true, "tasks.read": true, "tasks.write": true, "sprints.read": true}'::jsonb,
     NOW(), NOW()),
    (gen_random_uuid(), NULL, 'PROJECT_VIEWER',
     '{"projects.read": true, "tasks.read": true, "sprints.read": true}'::jsonb,
     NOW(), NOW())
ON CONFLICT DO NOTHING;

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
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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

CREATE TABLE IF NOT EXISTS tasks (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    task_type_id   UUID        REFERENCES task_types(id) ON DELETE SET NULL,
    status_id      UUID        REFERENCES task_statuses(id) ON DELETE SET NULL,
    sprint_id      UUID        REFERENCES sprints(id) ON DELETE SET NULL,
    parent_task_id UUID        REFERENCES tasks(id) ON DELETE SET NULL,
    title          TEXT        NOT NULL,
    description    TEXT,
    importance     INTEGER     NOT NULL DEFAULT 0 CHECK (importance >= 0),
    board_position INTEGER     NOT NULL DEFAULT 0,
    assignee_id    UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    reporter_id    UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    custom_fields  JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tasks_project_id     ON tasks (project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status_id      ON tasks (status_id);
CREATE INDEX IF NOT EXISTS idx_tasks_sprint_id      ON tasks (sprint_id);
CREATE INDEX IF NOT EXISTS idx_tasks_deleted_at     ON tasks (deleted_at) WHERE deleted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_board_position ON tasks (status_id, board_position);

COMMIT;
