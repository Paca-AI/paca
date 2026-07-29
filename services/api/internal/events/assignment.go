package events

import (
	"context"

	"github.com/Paca-AI/api/internal/platform/messaging"
	"github.com/google/uuid"
)

// PublishAssignmentChanged appends a task.assigned event to
// StreamTaskAssignments — the single payload shape the NotificationConsumer
// and the AI-agent trigger pipeline both read, regardless of whether a
// human PATCH, task creation, or the automation engine changed the
// assignee. extra merges in caller-specific attribution (e.g.
// automation_name / agent_message) on top of the shared fields; pass nil
// when there is none. No-op if publisher is nil, matching how every other
// best-effort event in this package is published.
func PublishAssignmentChanged(ctx context.Context, publisher *messaging.Publisher, taskID, projectID, newAssigneeMemberID uuid.UUID, oldAssigneeMemberID *uuid.UUID, actorUserID uuid.UUID, extra map[string]any) error {
	if publisher == nil {
		return nil
	}
	payload := map[string]any{
		"task_id":                taskID.String(),
		"project_id":             projectID.String(),
		"new_assignee_member_id": newAssigneeMemberID.String(),
		"actor_user_id":          actorUserID.String(),
	}
	if oldAssigneeMemberID != nil {
		payload["old_assignee_member_id"] = oldAssigneeMemberID.String()
	}
	for k, v := range extra {
		payload[k] = v
	}
	return publisher.Append(ctx, StreamTaskAssignments, "task.assigned", payload)
}
