-- A member can star a channel or direct message: starred ones are listed first,
-- in their own section of the chat sidebar. Starring a channel the account does
-- not follow yet makes it follow it.
ALTER TABLE channel_members ADD COLUMN IF NOT EXISTS starred BOOLEAN NOT NULL DEFAULT FALSE;
