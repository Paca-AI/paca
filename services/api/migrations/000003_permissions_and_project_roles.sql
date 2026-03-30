-- 000003_permissions_and_project_roles.sql
-- Adds project-role tables and seeds default global/project roles.

BEGIN;

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
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
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
CREATE INDEX IF NOT EXISTS idx_project_members_user_id ON project_members (user_id);
CREATE INDEX IF NOT EXISTS idx_project_members_role_id ON project_members (project_role_id);

INSERT INTO global_roles (id, name, permissions, created_at, updated_at)
VALUES
    (gen_random_uuid(), 'SUPER_ADMIN', '{"*": true}'::jsonb, NOW(), NOW()),
    (gen_random_uuid(), 'ADMIN', '{"users.*": true, "global_roles.*": true, "projects.*": true}'::jsonb, NOW(), NOW()),
    (gen_random_uuid(), 'USER', '{"users.read": true}'::jsonb, NOW(), NOW())
ON CONFLICT (name) DO UPDATE
SET permissions = EXCLUDED.permissions,
    updated_at = NOW();

UPDATE project_roles
SET permissions = '{"projects.*": true, "project.members.*": true, "project.roles.*": true, "tasks.*": true, "sprints.*": true}'::jsonb,
    updated_at = NOW()
WHERE project_id IS NULL AND role_name = 'PROJECT_OWNER';
INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at)
SELECT gen_random_uuid(), NULL, 'PROJECT_OWNER', '{"projects.*": true, "project.members.*": true, "project.roles.*": true, "tasks.*": true, "sprints.*": true}'::jsonb, NOW(), NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM project_roles WHERE project_id IS NULL AND role_name = 'PROJECT_OWNER'
);

UPDATE project_roles
SET permissions = '{"projects.read": true, "projects.write": true, "project.members.read": true, "project.members.write": true, "tasks.*": true, "sprints.*": true}'::jsonb,
    updated_at = NOW()
WHERE project_id IS NULL AND role_name = 'PROJECT_MANAGER';
INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at)
SELECT gen_random_uuid(), NULL, 'PROJECT_MANAGER', '{"projects.read": true, "projects.write": true, "project.members.read": true, "project.members.write": true, "tasks.*": true, "sprints.*": true}'::jsonb, NOW(), NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM project_roles WHERE project_id IS NULL AND role_name = 'PROJECT_MANAGER'
);

UPDATE project_roles
SET permissions = '{"projects.read": true, "tasks.read": true, "tasks.write": true, "sprints.read": true}'::jsonb,
    updated_at = NOW()
WHERE project_id IS NULL AND role_name = 'PROJECT_MEMBER';
INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at)
SELECT gen_random_uuid(), NULL, 'PROJECT_MEMBER', '{"projects.read": true, "tasks.read": true, "tasks.write": true, "sprints.read": true}'::jsonb, NOW(), NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM project_roles WHERE project_id IS NULL AND role_name = 'PROJECT_MEMBER'
);

UPDATE project_roles
SET permissions = '{"projects.read": true, "tasks.read": true, "sprints.read": true}'::jsonb,
    updated_at = NOW()
WHERE project_id IS NULL AND role_name = 'PROJECT_VIEWER';
INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at)
SELECT gen_random_uuid(), NULL, 'PROJECT_VIEWER', '{"projects.read": true, "tasks.read": true, "sprints.read": true}'::jsonb, NOW(), NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM project_roles WHERE project_id IS NULL AND role_name = 'PROJECT_VIEWER'
);

COMMIT;
