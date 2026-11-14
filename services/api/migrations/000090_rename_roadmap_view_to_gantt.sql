-- 000090_rename_roadmap_view_to_gantt.sql
-- The timeline layout is now a Gantt chart, so its view_type is renamed from
-- 'roadmap' to 'gantt'. Every existing view keeps its config, position and
-- personal overrides; only the type changes. A view still carrying the name
-- the timeline was seeded with ("Roadmap") is renamed to match; a name a
-- person chose is left as it is.
--
-- The API still accepts 'roadmap' as a deprecated alias for one release and
-- stores it as 'gantt', so the constraint no longer allows it.

BEGIN;

ALTER TABLE sprint_views
    DROP CONSTRAINT IF EXISTS sprint_views_view_type_check;

UPDATE sprint_views
   SET name = CASE WHEN name = 'Roadmap' THEN 'Gantt' ELSE name END,
       view_type = 'gantt'
 WHERE view_type = 'roadmap';

ALTER TABLE sprint_views
    ADD CONSTRAINT sprint_views_view_type_check
    CHECK (view_type IN ('table', 'board', 'gantt', 'plugin'));

COMMIT;
