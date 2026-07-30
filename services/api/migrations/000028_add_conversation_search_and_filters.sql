-- 000028_add_conversation_search_and_filters.sql
-- Supports the conversation list's new server-side filters (agent, status,
-- type/trigger_type, date range) and free-text search.
--
-- 1. idx_agent_conversations_trigger_type: mirrors the existing
--    idx_agent_conversations_agent_status composite index, but for
--    trigger_type ("type") filtering.
--
-- 2. agent_conversation_event_search_text(): a best-effort extraction of
--    human-readable text from a conversation event's JSONB payload, covering
--    the known text-bearing paths across OpenHands SDK event types (approximates
--    the per-event-type logic in the frontend's conversation-to-thread-messages.ts).
--    It's used only to build a search index and to match search queries, not for
--    display, so it doesn't need pixel-perfect fidelity with the UI rendering.
--    Marked IMMUTABLE so it can back a GIN expression index below.
--
-- 3. idx_agent_conversation_events_search: a GIN index over
--    to_tsvector('simple', agent_conversation_event_search_text(payload)),
--    letting the conversation list's search filter match conversations whose
--    events contain the search text without a full unindexed JSONB scan.
--    CREATE INDEX CONCURRENTLY isn't usable here — this repo's migration
--    runner (RunMigrationsFS/execInTx) wraps each file in its own
--    transaction, and CONCURRENTLY cannot run inside one.

BEGIN;

CREATE INDEX IF NOT EXISTS idx_agent_conversations_trigger_type
    ON agent_conversations (project_id, trigger_type);

CREATE OR REPLACE FUNCTION agent_conversation_event_search_text(payload JSONB)
RETURNS TEXT
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT string_agg(v, ' ')
    FROM (VALUES
        (payload->>'content'),
        (payload #>> '{llm_message,content}'),
        (payload->>'thought'),
        (payload #>> '{action,message}'),
        (payload #>> '{observation,content}'),
        (payload #>> '{observation,message}'),
        (payload->>'message'),
        (payload->>'error'),
        (payload->>'rejection_reason'),
        (payload->>'output'),
        (payload->>'raw_output'),
        (payload->>'title')
    ) AS t(v)
    WHERE v IS NOT NULL;
$$;

CREATE INDEX IF NOT EXISTS idx_agent_conversation_events_search
    ON agent_conversation_events
    USING GIN (to_tsvector('simple', agent_conversation_event_search_text(payload)));

COMMIT;
