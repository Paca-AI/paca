package projectdom

import (
	"time"

	"github.com/google/uuid"
)

// ProjectMember represents a member (human or agent) of a project with an assigned role.
type ProjectMember struct {
	ID            uuid.UUID
	ProjectID     uuid.UUID
	UserID        uuid.UUID // zero-value for agent members
	ProjectRoleID uuid.UUID
	// Populated by JOIN for display purposes.
	Username  string
	FullName  string
	RoleName  string
	CreatedAt time.Time
	DeletedAt *time.Time
	// Agent-specific fields (populated for member_type = 'agent')
	MemberType  string // "human" | "agent"
	AgentID     *uuid.UUID
	AgentName   string
	AgentHandle string
	// IsService (Galaxy ADR-038): the joined user is a non-human
	// service/bridge account (users.is_service); display-only.
	IsService bool
}

// IsAgent returns true if this member is an AI agent.
func (m *ProjectMember) IsAgent() bool {
	return m.MemberType == "agent"
}

// DisplayName returns the display name regardless of member type.
func (m *ProjectMember) DisplayName() string {
	if m.IsAgent() {
		return m.AgentName
	}
	return m.FullName
}
