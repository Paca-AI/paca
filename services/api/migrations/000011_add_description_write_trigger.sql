-- Originally also added agents.description_write_trigger_prompt; that ADD
-- COLUMN is gone now (see 000010_add_trigger_prompts_to_agents.sql for why —
-- 000019_drop_agent_trigger_prompts.sql always drops this same column again
-- in the same boot, and Postgres never reclaims a dropped column's slot, so
-- re-adding it here every boot was silently burning the agents table's
-- 1600-column ceiling). The trigger_type constraint change below is still
-- needed and unaffected.

-- Extend the trigger_type check constraint to include 'description_write' and
-- 'automation_message'.
--
-- 'automation_message' belongs to 000027_add_automation_graph.sql, not this
-- migration, but it MUST be listed here too: this whole migrations directory
-- is re-executed in full, in lexicographic order, on every boot (see
-- RunMigrationsFS — there is no schema_migrations tracking table), and this
-- file's ADD CONSTRAINT always runs before 000027's. If this file's value
-- list is narrower than what's already been persisted by the time it
-- re-runs, the ADD CONSTRAINT fails validation against existing rows and
-- takes down every future boot until manually fixed. Any value added to
-- this constraint by a later migration must be folded back into this list.
ALTER TABLE agent_conversations
    DROP CONSTRAINT IF EXISTS agent_conversations_trigger_type_check;

ALTER TABLE agent_conversations
    ADD CONSTRAINT agent_conversations_trigger_type_check
    CHECK (trigger_type IN ('task_assigned', 'comment_mention', 'chat_message', 'description_write', 'automation_message'));
