-- 000061_unify_activities.sql
-- One activity table for every entity in a project — tasks, docs, sprints,
-- views, automations, environments, members. Written by the single
-- worker.ActivityConsumer from the single StreamActivities stream (comments,
-- which must return their row synchronously, are inserted directly by their
-- service; the consumer's ON CONFLICT (id) makes the stream copy a no-op).
--
-- Replaces task_activities and doc_activities: their rows are copied in below
-- with their original IDs, so existing comment/activity IDs keep resolving,
-- and the old tables are then dropped so no activity is stored twice. The
-- copy and the drop run in one transaction — a failed copy drops nothing.
--
-- entity_id carries no foreign key: this is an audit trail, and a cascade
-- would erase the record of a deletion together with the thing deleted. Only
-- project_id cascades; actors are SET NULL so a removed member's history
-- survives.
--
-- IF NOT EXISTS / ON CONFLICT throughout so this migration is safe to re-run.

BEGIN;

CREATE TABLE IF NOT EXISTS activities (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id     UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    entity_type    TEXT        NOT NULL,
    entity_id      UUID,
    actor_id       UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    origin         TEXT        NOT NULL DEFAULT 'system',
    activity_type  TEXT        NOT NULL,
    content        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ
);

-- Per-entity timelines (task detail, doc detail).
CREATE INDEX IF NOT EXISTS idx_activities_entity
    ON activities (entity_type, entity_id, created_at);
-- Project-wide admin feed, newest first.
CREATE INDEX IF NOT EXISTS idx_activities_project_created
    ON activities (project_id, created_at DESC, id DESC);
-- Per-actor feeds (agent activity tab, admin actor filter) — replaces the
-- two actor cursor indexes from 000030.
CREATE INDEX IF NOT EXISTS idx_activities_actor_cursor
    ON activities (actor_id, created_at DESC, id DESC)
    WHERE actor_id IS NOT NULL AND deleted_at IS NULL;

-- Guarded so a re-run after the drop is a no-op rather than an error.
DO $$
BEGIN
    IF to_regclass('public.task_activities') IS NOT NULL THEN
        INSERT INTO activities (id, project_id, entity_type, entity_id, actor_id, origin, activity_type, content, created_at, updated_at, deleted_at)
        SELECT ta.id, t.project_id, 'task', ta.task_id, ta.actor_id,
               CASE WHEN ta.actor_id IS NULL THEN 'system' ELSE 'user' END,
               ta.activity_type, ta.content, ta.created_at, ta.updated_at, ta.deleted_at
        FROM task_activities ta
        JOIN tasks t ON t.id = ta.task_id
        ON CONFLICT (id) DO NOTHING;
    END IF;
    IF to_regclass('public.doc_activities') IS NOT NULL THEN
        INSERT INTO activities (id, project_id, entity_type, entity_id, actor_id, origin, activity_type, content, created_at, updated_at, deleted_at)
        SELECT da.id, d.project_id, 'doc', da.document_id, da.actor_id,
               CASE WHEN da.actor_id IS NULL THEN 'system' ELSE 'user' END,
               da.activity_type, da.content, da.created_at, da.updated_at, da.deleted_at
        FROM doc_activities da
        JOIN documents d ON d.id = da.document_id
        ON CONFLICT (id) DO NOTHING;
    END IF;
END $$;

DROP TABLE IF EXISTS task_activities;
DROP TABLE IF EXISTS doc_activities;

COMMIT;
