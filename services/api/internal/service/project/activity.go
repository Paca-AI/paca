package projectsvc

import (
	"context"

	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/events"
	activitysvc "github.com/Paca-AI/api/internal/service/activity"
)

// Activity topics for the project itself, its members and its roles. Not in
// events/topics.go: nothing but the activity log listens for them.
const (
	TopicProjectUpdated    = "project.updated"
	TopicMemberAdded       = "member.added"
	TopicMemberRoleChanged = "member.role_changed"
	TopicMemberUpdated     = "member.updated"
	TopicMemberRemoved     = "member.removed"
	TopicRoleCreated       = "role.created"
	TopicRoleUpdated       = "role.updated"
	TopicRoleDeleted       = "role.deleted"
)

// WithActivityRecorder sets where project, member and role changes are
// recorded. Without it nothing is recorded.
func (s *Service) WithActivityRecorder(rec activitysvc.Recorder) *Service {
	s.activity = rec
	return s
}

// record records one project-level change; the actor comes from the request
// context.
func (s *Service) record(ctx context.Context, projectID uuid.UUID, entity events.EntityType, entityID uuid.UUID, topic string, payload map[string]any) {
	s.activity.Record(ctx, activitysvc.Entry{
		ProjectID:  projectID,
		EntityType: entity,
		EntityID:   entityID,
		Topic:      topic,
		Payload:    payload,
	})
}

// memberPayload carries the member's display name so the entry still reads
// correctly after the member is removed.
func memberPayload(m *projectdom.ProjectMember) map[string]any {
	name := m.FullName
	switch {
	case m.AgentID != nil && m.AgentName != "":
		name = m.AgentName
	case name == "":
		name = m.Username
	}
	return map[string]any{
		"member_id":   m.ID.String(),
		"name":        name,
		"member_type": m.MemberType,
		"role_name":   m.RoleName,
	}
}
