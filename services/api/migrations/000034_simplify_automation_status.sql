-- 000034_simplify_automation_status.sql
-- Collapses the automation lifecycle from three statuses (draft, active,
-- archived) to two (active, inactive): draft and archived both become
-- inactive, matching automationdom.Status. Inactive automations stay fully
-- editable — there's no longer a status that locks editing.

BEGIN;

-- Drop the old (draft/active/archived) constraint first — it would
-- otherwise reject the UPDATE below, since 'inactive' isn't in its allowed
-- set yet.
ALTER TABLE automations DROP CONSTRAINT IF EXISTS automations_status_check;

UPDATE automations SET status = 'inactive' WHERE status IN ('draft', 'archived');

ALTER TABLE automations ALTER COLUMN status SET DEFAULT 'inactive';
ALTER TABLE automations ADD CONSTRAINT automations_status_check CHECK (status IN ('active', 'inactive'));

COMMIT;
