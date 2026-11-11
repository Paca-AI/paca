-- 000083_add_versions_and_change_outbox.sql
-- Two things that let realtime clients trust the change feed.
--
-- 1. A row version on tasks and sprints, bumped by the database on every
--    update, so a client can tell a stale or duplicate change from a newer one
--    and a writer can say which version it saw (If-Match).
--
-- 2. A transactional outbox: a trigger records every insert, update and delete
--    of tasks and sprints in change_outbox *in the same transaction as the
--    change*. Documents are deliberately not captured: they have no row
--    version, collaborative editing persists them often, and their sync story
--    (content hash, change feed with tombstones) belongs to the docs
--    pull/push work (Paca-AI/paca discussion 722). The service announces a change
--    on the realtime feed just after commit; if the process dies in between, the outbox still holds the
--    row and a reconciler announces it (see internal/worker/change_reconciler.go).
--    Rows are transient: the reconciler deletes them once handled.

ALTER TABLE tasks   ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE sprints ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1;

CREATE OR REPLACE FUNCTION bump_row_version() RETURNS trigger AS $$
BEGIN
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tasks_bump_version ON tasks;
CREATE TRIGGER tasks_bump_version BEFORE UPDATE ON tasks
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

DROP TRIGGER IF EXISTS sprints_bump_version ON sprints;
CREATE TRIGGER sprints_bump_version BEFORE UPDATE ON sprints
    FOR EACH ROW EXECUTE FUNCTION bump_row_version();

CREATE TABLE IF NOT EXISTS change_outbox (
    id          BIGSERIAL PRIMARY KEY,
    project_id  UUID        NOT NULL,
    entity_type TEXT        NOT NULL,
    entity_id   UUID        NOT NULL,
    op          TEXT        NOT NULL,
    version     BIGINT      NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS idx_change_outbox_created_at ON change_outbox (created_at);

-- Reading the columns directly keeps the row from being serialised
-- (to_jsonb(NEW) would materialise a task's description on every write).
CREATE OR REPLACE FUNCTION capture_versioned_change() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        INSERT INTO change_outbox (project_id, entity_type, entity_id, op, version)
        VALUES (OLD.project_id, TG_ARGV[0], OLD.id, 'delete', OLD.version);
    ELSE
        INSERT INTO change_outbox (project_id, entity_type, entity_id, op, version)
        VALUES (NEW.project_id, TG_ARGV[0], NEW.id, lower(TG_OP), NEW.version);
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tasks_capture_change ON tasks;
CREATE TRIGGER tasks_capture_change AFTER INSERT OR UPDATE OR DELETE ON tasks
    FOR EACH ROW EXECUTE FUNCTION capture_versioned_change('task');

DROP TRIGGER IF EXISTS sprints_capture_change ON sprints;
CREATE TRIGGER sprints_capture_change AFTER INSERT OR UPDATE OR DELETE ON sprints
    FOR EACH ROW EXECUTE FUNCTION capture_versioned_change('sprint');
