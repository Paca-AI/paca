-- Drop the per-trigger system prompt columns on agents.
-- Superseded by the ai-agent service's default skill set + fixed
-- trigger-context skills (services/ai-agent/src/skills/, src/agent/trigger_skills.py) —
-- action-specific behavior is no longer stored as free-text prompts per agent.
--
-- 000010/000011 no longer add these columns (they used to, every boot,
-- immediately undone by this file — see 000010 for why that leaked ghost
-- columns), so every clause below is now a permanent no-op. Left in place:
-- it's still correct, and still the right fix for any database that reaches
-- this file with the columns actually present (e.g. one restored from a
-- backup taken before 000010/000011 were neutered).

ALTER TABLE agents
    DROP COLUMN IF EXISTS task_trigger_prompt,
    DROP COLUMN IF EXISTS doc_comment_trigger_prompt,
    DROP COLUMN IF EXISTS chat_trigger_prompt,
    DROP COLUMN IF EXISTS description_write_trigger_prompt;
