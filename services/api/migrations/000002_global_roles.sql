-- 000002_global_roles.sql
-- Adds global role definitions and user-role assignments.

BEGIN;

CREATE TABLE IF NOT EXISTS global_roles (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL UNIQUE,
    permissions JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_global_roles (
    user_id UUID NOT NULL,
    role_id UUID NOT NULL,
    PRIMARY KEY (user_id, role_id),
    CONSTRAINT fk_user_global_roles_user
        FOREIGN KEY (user_id)
        REFERENCES users(id)
        ON DELETE CASCADE,
    CONSTRAINT fk_user_global_roles_role
        FOREIGN KEY (role_id)
        REFERENCES global_roles(id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_global_roles_user_id ON user_global_roles (user_id);
CREATE INDEX IF NOT EXISTS idx_user_global_roles_role_id ON user_global_roles (role_id);

COMMIT;
