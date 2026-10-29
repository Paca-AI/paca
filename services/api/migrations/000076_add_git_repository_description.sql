-- What a linked repository is for, in the words of whoever linked it. Lets an
-- agent in a project with several repositories pick the right one
-- (docs/ai-agent/git-agent-workflow.md). Free text, capped by the service.
ALTER TABLE git_repositories ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
