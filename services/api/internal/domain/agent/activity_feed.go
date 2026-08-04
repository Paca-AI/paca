package agentdom

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ActivitySourceType identifies which activity table an ActivityFeedItem
// came from — an agent's activity feed unions task_activities and
// doc_activities, which have no common parent table.
type ActivitySourceType string

const (
	ActivitySourceTask ActivitySourceType = "task"
	ActivitySourceDoc  ActivitySourceType = "doc"
)

// ActivityFeedItem is one row of an agent's unified task+doc activity feed.
// SourceTitle is the joined task/document title at query time, so the UI can
// render and link to "commented on <title>" without a second round trip.
type ActivityFeedItem struct {
	ID           uuid.UUID
	SourceType   ActivitySourceType
	SourceID     uuid.UUID
	SourceTitle  string
	ActivityType string
	Content      json.RawMessage
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ListAgentActivitiesFilter carries optional filters for listing an agent's
// activity feed. ActorMemberID is required — it's the project_members.id
// that task_activities.actor_id/doc_activities.actor_id are resolved
// against (see FindMemberByAgent), not the agents.id itself.
type ListAgentActivitiesFilter struct {
	ActorMemberID uuid.UUID
	SourceTypes   []ActivitySourceType
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	// Search matches activities whose linked task/document title or raw
	// content contains this text (case-insensitive substring match).
	Search      *string
	CursorAfter *string
}

// ActivityFeedRepository defines storage for an agent's unified activity feed.
type ActivityFeedRepository interface {
	// ListAgentActivities returns up to limit activities matching the
	// filter, ordered newest-first, plus whether more pages remain.
	ListAgentActivities(ctx context.Context, in ListAgentActivitiesFilter, limit int) ([]*ActivityFeedItem, bool, error)
}
