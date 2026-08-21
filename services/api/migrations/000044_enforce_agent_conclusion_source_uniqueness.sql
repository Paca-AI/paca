-- Migration 000044: make an authoritative turn publishable to a task once.
--
-- The UI may cache publication history, but correctness is enforced here so
-- pagination, concurrent clients, or direct API calls cannot attribute the
-- same stable turn result to the same task more than once.

BEGIN;

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_conclusion_publication_source_task
    ON agent_conclusion_publications (source_turn_id, target_task_id)
    WHERE kind = 'published';

COMMIT;
