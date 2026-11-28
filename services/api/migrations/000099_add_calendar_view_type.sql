-- 000099_add_calendar_view_type.sql
-- Calendar views: a view of type 'calendar' lays tasks out on a month, week
-- or day grid by their start and due dates. It reads the same tasks as the
-- other task layouts, and its settings (calendar_mode, calendar_date_field)
-- live in the view's config, so only the view_type constraint changes.
--
-- The constraint carries on from 000094, which added 'note' to the set
-- 000090 left after renaming 'roadmap' to 'gantt'; 'roadmap' stays out of it
-- (the API stores that alias as 'gantt').

BEGIN;

ALTER TABLE sprint_views
    DROP CONSTRAINT IF EXISTS sprint_views_view_type_check;

ALTER TABLE sprint_views
    ADD CONSTRAINT sprint_views_view_type_check
    CHECK (view_type IN ('table', 'board', 'gantt', 'plugin', 'note', 'calendar'));

COMMIT;
