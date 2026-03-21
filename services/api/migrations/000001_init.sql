-- 000001_init.sql
-- Initial schema for the Paca API service.
-- Run via: psql "$DATABASE_URL" -f migrations/000001_init.sql

BEGIN;

CREATE TABLE IF NOT EXISTS users (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email        TEXT        NOT NULL UNIQUE,
    password_hash TEXT       NOT NULL,
    name         TEXT        NOT NULL DEFAULT '',
    role         TEXT        NOT NULL DEFAULT 'USER',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_users_email      ON users (email);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at) WHERE deleted_at IS NOT NULL;

COMMIT;
