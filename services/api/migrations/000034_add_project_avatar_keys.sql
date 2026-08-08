-- 000034_add_project_avatar_keys.sql
-- Adds avatar upload support for projects, same shape as 000033's
-- users/agents columns: avatar_key/avatar_thumb_key hold the object-storage
-- keys of the two server-generated image variants (see
-- attachmentdom.AvatarService), resolved to presigned display URLs at read
-- time rather than stored as URLs.

BEGIN;

ALTER TABLE projects ADD COLUMN IF NOT EXISTS avatar_key TEXT;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS avatar_thumb_key TEXT;

COMMIT;
