-- 000060_add_jev_integration.sql
-- Combines the schema needed to integrate Jev (the AI decision API, see
-- internal/platform/jev) across agent/member descriptions, task field
-- auto-fill, task auto-assign, and per-project Jev credentials.
-- (Squashed from the formerly-separate 000060-000063 migrations, which were
-- never committed — see each section below for what used to be its own
-- file.)
--
-- IF NOT EXISTS / guarded DO blocks throughout so this migration is safe to
-- re-run.

BEGIN;

-- --- Agent & member descriptions (Jev criteria) -----------------------------
-- Adds a free-text description to agents and to human project members, used
-- as the `criteria` description Jev is given when deciding which agent
-- should handle a chat, or which member a task should be assigned to.
--
-- agents.description is user-authored; the provider/model suffix Jev
-- actually sees is composed at call time (agentdom.ComposeJevDescription),
-- never stored merged, so it stays accurate as an agent's provider/model
-- changes and the raw field stays clean to edit.
--
-- project_members.description is only meaningful for human members (agents
-- already have agents.description); it captures per-project context (e.g.
-- "frontend lead on this project") that can legitimately differ from one
-- project to the next for the same person.

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS description TEXT;

ALTER TABLE project_members
    ADD COLUMN IF NOT EXISTS description TEXT;

-- --- Task field auto-fill tracking -------------------------------------------
-- Adds the bookkeeping worker.TaskAutofillConsumer needs to auto-fill task
-- fields the user left blank at creation, via Jev, without ever overwriting
-- a field a human explicitly set.
--
-- task_field_sources records which fields a human has explicitly set on a
-- task (a row's presence = "a human set this field key", by task_handler.go
-- at creation and task_handler.go on every subsequent PATCH). It's a
-- general provenance record, not scoped to the one-shot autofill pass alone
-- — but that one-shot pass is, today, its only reader: only the rows
-- present at creation time matter for deciding what worker.
-- TaskAutofillConsumer is allowed to fill in.
--
-- tasks.jev_autofilled_at guards that same consumer against double-processing
-- on at-least-once Valkey Streams redelivery (see worker.ActivityConsumer's
-- processPending for the general redelivery mechanism this project uses) —
-- NULL means "not processed yet"; the consumer sets it in the same
-- transaction as any field writes, and skips a task where it's already set.

CREATE TABLE IF NOT EXISTS task_field_sources (
    task_id    UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    field_key  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, field_key)
);

ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS jev_autofilled_at TIMESTAMPTZ;

-- --- Task assignment mode -----------------------------------------------------
-- Lets a task's assignee be resolved by Jev instead of picked manually.
-- assignee_ids is a set via the task_assignees join table (see
-- 000021_add_task_assignees.sql), not a single nullable FK, so there's no
-- room for an "auto" sentinel value in the array itself — hence this
-- separate mode column instead.
--
-- 'manual' (default): assignee_ids is only ever changed by an explicit
-- PATCH. 'auto': worker.TaskAutoAssignConsumer resolves an assignee via Jev
-- whenever assignee_ids is empty (re-checked on every task.created/
-- task.updated event for the task — see that consumer's own doc comment;
-- naturally idempotent since a resolved assignee makes assignee_ids
-- non-empty, and a task Jev couldn't confidently resolve is reverted back
-- to 'manual', so no separate tracking column is needed). A human manually
-- changing assignee_ids flips this back to 'manual' (service/task's
-- UpdateTask), mirroring agent_conversations.title_set_by_user's "once
-- touched by a human, stop auto-managing it" precedent
-- (000058_add_conversation_title.sql).

ALTER TABLE tasks
    ADD COLUMN IF NOT EXISTS assignment_mode TEXT NOT NULL DEFAULT 'manual';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'tasks_assignment_mode_check'
    ) THEN
        ALTER TABLE tasks
            ADD CONSTRAINT tasks_assignment_mode_check
            CHECK (assignment_mode IN ('manual', 'auto'));
    END IF;
END $$;

-- --- Per-project Jev config ---------------------------------------------------
-- Moves Jev credentials from an instance-wide env var (JEV_ENABLED/
-- JEV_API_KEY, install.sh/upgrade.sh) to per-project settings, so each
-- project can point at its own Jev-compatible provider — TypeSafe (the
-- default) or a compatible third party such as OpenJev
-- (https://openjev.sh/docs), which implements the same state/questions/
-- choice/score/noul contract at a different host and default model name.
--
-- jev_api_key_secret is encrypted at rest exactly like agents.
-- llm_api_key_secret (see 000008_add_ai_agents.sql and
-- platform/secret.Encryptor) — services/api/internal/service/project's
-- encryptJevKey/decryptJevKey mirror agentsvc.Service's encryptKey exactly.
-- Never returned by the normal project read/list endpoints — only a
-- computed jev_configured boolean is (see ProjectResponse).
--
-- jev_base_url/jev_model are plain (not secret) and blank by default,
-- meaning "use platform/jev's built-in TypeSafe defaults" — see
-- jev.New's doc comment.
--
-- All three default to '' rather than being nullable so every existing row
-- scans cleanly into a plain Go string with no NULL-handling required (the
-- codebase has twice this session hit "converting NULL to string is
-- unsupported" from a nullable TEXT column added via ALTER TABLE without a
-- default — see the fixes to task_repository.go's taskWithPositionRow and
-- agent_repository.go's agentSelectColsBase).

ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS jev_api_key_secret TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS jev_base_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS jev_model TEXT NOT NULL DEFAULT '';

COMMIT;
