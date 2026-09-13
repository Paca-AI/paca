-- 000050_add_custom_field_option_colors.sql
-- Issue #458: lets each value of a select/multi_select custom field carry
-- its own optional color, so board/list badges stop rendering every value
-- in the same primary color.
--
-- custom_field_definitions.options was a plain JSONB array of strings
-- (e.g. ["Web","iOS"]). It becomes an array of objects
-- (e.g. [{"value":"Web","color":"#6366f1"},{"value":"iOS","color":null}])
-- so each value can carry a color alongside it. The column itself is
-- unchanged (still JSONB, still nullable) — only the shape of its elements
-- changes, so no ALTER COLUMN is needed, just a data backfill.
--
-- Per database.RunMigrationsFS, every *.sql file re-runs unconditionally on
-- every server startup, so this UPDATE must be idempotent: it only rewrites
-- rows that still contain plain-string elements (jsonb_typeof = 'string'),
-- which becomes false once a row has already been migrated to objects.

BEGIN;

UPDATE custom_field_definitions
SET options = (
    SELECT jsonb_agg(
        CASE
            WHEN jsonb_typeof(elem) = 'string'
                THEN jsonb_build_object('value', elem, 'color', NULL)
            ELSE elem
        END
    )
    FROM jsonb_array_elements(options) AS elem
)
WHERE options IS NOT NULL
  AND jsonb_typeof(options) = 'array'
  AND EXISTS (
      SELECT 1 FROM jsonb_array_elements(options) AS elem
      WHERE jsonb_typeof(elem) = 'string'
  );

COMMIT;
