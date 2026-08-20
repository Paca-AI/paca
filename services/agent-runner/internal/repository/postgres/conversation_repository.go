package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// ConversationRepository mirrors services/ai-agent's
// repositories/conversation_repository.py — direct writes to
// agent_conversations/agent_conversation_events, matching what that
// service already does today (despite docs/architecture/service-
// boundaries.md's Boundary Rule reading as "ai-agent doesn't write to the
// DB" — the actual Python code writes directly; this mirrors reality, not
// the doc).
type ConversationRepository struct {
	db *sqlx.DB
}

// NewConversationRepository builds a ConversationRepository backed by db.
func NewConversationRepository(db *sqlx.DB) *ConversationRepository {
	return &ConversationRepository{db: db}
}

// UpdateStatus mirrors update_conversation_status, always overwriting
// error_message (to NULL when errorMessage is nil) rather than leaving a
// prior value in place — a "paused" status can now carry one too (a
// classified, retryable provider error — see acp.ClassifyProviderError),
// so the next turn's "running" transition must actually clear it, not just
// every status besides "failed" happening to never have set one before.
func (r *ConversationRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string, errorMessage *string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE agent_conversations SET status = $1, error_message = $2, updated_at = now() WHERE id = $3`,
		status, errorMessage, id)
	if err != nil {
		return fmt.Errorf("postgres: update conversation %s status: %w", id, err)
	}
	return nil
}

// FailIfNotTerminal mirrors fail_if_not_terminal: atomically marks a
// conversation failed unless it already reached a terminal status.
// Race-safe against a legitimate terminal status landing concurrently —
// see the Python docstring this mirrors for the watchdog use case this
// exists for.
func (r *ConversationRepository) FailIfNotTerminal(ctx context.Context, id uuid.UUID, errorMessage string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_conversations
		SET status = 'failed', error_message = $1, updated_at = now()
		WHERE id = $2 AND status NOT IN ('finished', 'failed', 'stopped')
	`, errorMessage, id)
	if err != nil {
		return false, fmt.Errorf("postgres: fail-if-not-terminal conversation %s: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("postgres: fail-if-not-terminal conversation %s: rows affected: %w", id, err)
	}
	return n == 1, nil
}

// UpdateStatusIfNotTerminal applies a bridge-reported status only while the
// conversation is still active. A late frame from an executor that was
// stopped or replaced must not resurrect a durable stopped/failed row.
func (r *ConversationRepository) UpdateStatusIfNotTerminal(ctx context.Context, id uuid.UUID, status string, errorMessage *string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_conversations
		SET status = $1, error_message = $2, updated_at = now()
		WHERE id = $3 AND status NOT IN ('finished', 'failed', 'stopped')
	`, status, errorMessage, id)
	if err != nil {
		return false, fmt.Errorf("postgres: update non-terminal conversation %s status: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("postgres: update non-terminal conversation %s rows affected: %w", id, err)
	}
	return n == 1, nil
}

// StaleACPConversation identifies an ACP conversation that was durable
// running but was absent from a freshly connected bridge's active session
// list. Realtime ownership is resolved through GetConversationRealtimeContext
// after the atomic update, keeping this query's RETURNING clause limited to
// columns of the updated table.
type StaleACPConversation struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
}

// ReconcileStaleACPConversations atomically fails running ACP conversations
// owned by agentID that the reconnecting bridge did not report. The active
// list is passed as individual parameters rather than relying on driver-
// specific UUID-array encoders, keeping this compatible with sqlx/pgx.
func (r *ConversationRepository) ReconcileStaleACPConversations(
	ctx context.Context,
	agentID uuid.UUID,
	active []uuid.UUID,
	connectedAt time.Time,
	errorMessage string,
) ([]StaleACPConversation, error) {
	args := []any{agentID, errorMessage, connectedAt}
	filters := []string{
		"c.agent_id = $1",
		"a.agent_type = 'acp'",
		"c.status = 'running'",
		"c.updated_at < $3",
	}
	for _, id := range active {
		args = append(args, id)
		filters = append(filters, fmt.Sprintf("c.id <> $%d", len(args)))
	}

	query := fmt.Sprintf(`
		WITH stale AS (
			SELECT c.id
			FROM agent_conversations c
			JOIN agents a ON a.id = c.agent_id
			WHERE %s
		)
		UPDATE agent_conversations c
		SET status = 'failed', error_message = $2, updated_at = now()
		FROM stale
		WHERE c.id = stale.id
		RETURNING c.id, c.project_id
	`, strings.Join(filters, " AND "))

	var rows []struct {
		ID        uuid.UUID     `db:"id"`
		ProjectID uuid.NullUUID `db:"project_id"`
	}
	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("postgres: reconcile stale ACP conversations for agent %s: %w", agentID, err)
	}

	stale := make([]StaleACPConversation, 0, len(rows))
	for _, row := range rows {
		item := StaleACPConversation{ID: row.ID}
		if row.ProjectID.Valid {
			item.ProjectID = row.ProjectID.UUID
		}
		stale = append(stale, item)
	}
	return stale, nil
}

// NextEventIndex mirrors get_next_event_index: event_index is unique per
// conversation and spans its entire lifetime (not just the current turn),
// so a resumed chat conversation's next turn must continue numbering from
// where the previous one left off.
func (r *ConversationRepository) NextEventIndex(ctx context.Context, conversationID uuid.UUID) (int, error) {
	var next int
	err := r.db.GetContext(ctx, &next,
		`SELECT COALESCE(MAX(event_index), -1) + 1 FROM agent_conversation_events WHERE conversation_id = $1`,
		conversationID)
	if err != nil {
		return 0, fmt.Errorf("postgres: next event index for conversation %s: %w", conversationID, err)
	}
	return next, nil
}

// GetConversationAgentType mirrors get_conversation_agent_type: returns the
// owning agent's ID and agent_type for conversationID. Used by
// handler.Handler.HandleControl to decide whether a stop/pause control
// message should interrupt an in-process llm-type run or forward through
// the ACP bridge dispatch channel instead — see internal/acpbridge.
func (r *ConversationRepository) GetConversationAgentType(ctx context.Context, conversationID uuid.UUID) (agentID uuid.UUID, agentType string, err error) {
	var row struct {
		AgentID   uuid.UUID `db:"agent_id"`
		AgentType string    `db:"agent_type"`
	}
	err = r.db.GetContext(ctx, &row, `
		SELECT a.id AS agent_id, a.agent_type
		FROM agent_conversations c
		JOIN agents a ON a.id = c.agent_id
		WHERE c.id = $1
	`, conversationID)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("postgres: get agent type for conversation %s: %w", conversationID, err)
	}
	return row.AgentID, row.AgentType, nil
}

// GetConversationRealtimeContext mirrors get_conversation_realtime_context:
// returns (project_id, owner_user_id) for routing a realtime event about
// conversationID. owner_user_id is the user whose private room should receive
// the event — the chat-session owner's users.id for a project chat (resolved
// chat_session_id → member_id → user_id), or actor_user_id for a global chat;
// it is nil for a sessionless (project-shared) conversation, which has no
// single owner. Used by the ACP bridge's WebSocket relay
// (internal/acpbridge/server.go), which handles events/turn_status for
// whichever conversation_id the local daemon reports — unlike a normal
// llm-type turn, which already has this on the in-memory Trigger it's
// running, the bridge needs a fresh per-conversation lookup since one
// bridge session (a global-scope ACP agent) can relay conversations with
// different project contexts, or none.
func (r *ConversationRepository) GetConversationRealtimeContext(ctx context.Context, conversationID uuid.UUID) (projectID uuid.UUID, ownerUserID *uuid.UUID, err error) {
	var row struct {
		ProjectID   uuid.NullUUID `db:"project_id"`
		OwnerUserID uuid.NullUUID `db:"owner_user_id"`
	}
	err = r.db.GetContext(ctx, &row,
		`SELECT c.project_id,
		        COALESCE(u.id, c.actor_user_id) AS owner_user_id
		 FROM agent_conversations c
		 LEFT JOIN agent_chat_sessions s ON s.id = c.chat_session_id
		 LEFT JOIN project_members pm ON pm.id = s.member_id
		 LEFT JOIN users u ON u.id = pm.user_id
		 WHERE c.id = $1`, conversationID)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("postgres: get realtime context for conversation %s: %w", conversationID, err)
	}
	if row.OwnerUserID.Valid {
		ownerUserID = &row.OwnerUserID.UUID
	}
	return row.ProjectID.UUID, ownerUserID, nil
}

// InsertEvent mirrors insert_conversation_event. payload must already be
// valid JSON text (the acp package's Event.Raw is exactly that — a
// json.RawMessage, passed straight through with no re-encoding).
//
// id/createdAt are generated by the caller (not by this query, unlike an
// earlier version that used gen_random_uuid()/now()) specifically so the
// same values can also go out over the realtime Pub/Sub payload — the
// frontend needs a stable id and timestamp to construct the exact same
// AgentConversationEvent client-side that a later GET .../events call
// would return, so it can append the live event directly instead of
// re-fetching. See messaging.Publisher.PublishRealtime's doc comment.
func (r *ConversationRepository) InsertEvent(ctx context.Context, id, conversationID uuid.UUID, eventType, eventSource string, eventIndex int, payload []byte, createdAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_conversation_events
			(id, conversation_id, event_type, event_source, event_index, payload, created_at)
		VALUES
			($1, $2, $3, $4, $5, $6::jsonb, $7)
		ON CONFLICT DO NOTHING
	`, id, conversationID, eventType, eventSource, eventIndex, string(payload), createdAt)
	if err != nil {
		return fmt.Errorf("postgres: insert event for conversation %s: %w", conversationID, err)
	}
	return nil
}
