-- 000084_add_feedback_enabled_setting.sql
-- Lets an instance admin turn the in-app feedback entry points ("Send
-- feedback" menu item, "Report this issue" buttons) on or off. Defaults to on.

BEGIN;

ALTER TABLE workspace_settings
	ADD COLUMN IF NOT EXISTS feedback_enabled BOOLEAN NOT NULL DEFAULT TRUE;

COMMIT;
