-- 000033_add_avatar_keys.sql
-- Adds avatar upload support for users and agents. Each owner row stores the
-- object-storage key for two derived, server-generated image variants
-- (avatar_key = 256x256 "full", avatar_thumb_key = 64x64 "thumb") rather
-- than a FK into the `files` table — that table is only used transiently
-- for the pending-upload handshake (see attachmentdom.AvatarService); once
-- an upload completes, the resulting keys are copied here directly so every
-- list endpoint (team page, task lists, activity feeds) can presign a
-- display URL with zero extra joins.
--
-- agents.avatar_url (000008) is dropped: it was never populated by any code
-- path (no Create/Update DTO ever exposed it), so this is a pure rename to
-- the key-based representation used by the new upload flow.

BEGIN;

ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_key TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_thumb_key TEXT;

ALTER TABLE agents DROP COLUMN IF EXISTS avatar_url;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS avatar_key TEXT;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS avatar_thumb_key TEXT;

COMMIT;
