-- iteration_count was a stored counter that nothing on any runtime path ever
-- incremented (the only writer, AgentRepository.UpdateConversation, is only
-- called from tests), so it stayed at its default 0 forever and the "N
-- iterations" pill never reflected reality. Rather than wire up a new writer
-- that could drift out of sync again, the API now computes the count live
-- from agent_conversation_events (see agent_repository.go's conversationCols),
-- so the stored column is no longer needed.
ALTER TABLE agent_conversations DROP COLUMN IF EXISTS iteration_count;
