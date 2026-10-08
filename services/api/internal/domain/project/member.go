package projectdom

import (
	"time"

	"github.com/google/uuid"

	roledom "github.com/Paca-AI/api/internal/domain/role"
)

// ProjectMember represents a member (human or agent) of a project.
type ProjectMember struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	UserID    uuid.UUID // zero-value for agent members
	// Roles are the roles attached to the member inside this project (their
	// project-scoped role attachments), sorted by name. Populated by reads.
	Roles []roledom.Summary
	// Populated by JOIN for display purposes.
	Username  string
	FullName  string
	CreatedAt time.Time
	DeletedAt *time.Time
	// Description is free text about this member's role/context on this
	// project (e.g. "frontend lead"). Only meaningful for human members —
	// an agent member's Jev-facing description is agentdom.Agent.Description
	// instead, since an agent's capabilities don't vary per project. Shown
	// to Jev (the AI decision API) when deciding whether to assign this
	// member a task in Auto mode.
	Description string
	// Agent-specific fields (populated for member_type = 'agent')
	MemberType  string // "human" | "agent"
	AgentID     *uuid.UUID
	AgentName   string
	AgentHandle string
	// AgentType/AgentLLMProvider/AgentACPProvider mirror the agent's own
	// fields (agentdom.Agent) — used by the frontend to pick a default
	// provider-logo avatar when the agent has no custom avatar uploaded.
	// Only meaningful when IsAgent() is true.
	AgentType        string // "llm" | "acp"
	AgentLLMProvider string
	AgentACPProvider *string
	// AgentDescription mirrors agentdom.Agent.Description — see
	// ComposeJevDescription, which appends AgentLLMProvider/AgentACPProvider
	// to it at call time (never stored merged), the same composition
	// agentdom.Agent.ComposeJevDescription does from the Agent entity
	// directly. Only meaningful when IsAgent() is true.
	AgentDescription string
	// Avatar object-storage keys, populated by JOIN from whichever of
	// users/agents backs this member (see IsAgent). Both nil when the
	// backing user/agent has no avatar uploaded.
	UserAvatarKey       *string
	UserAvatarThumbKey  *string
	AgentAvatarKey      *string
	AgentAvatarThumbKey *string
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

// RoleNames returns the names of the member's roles in the project.
func (m *ProjectMember) RoleNames() []string {
	out := make([]string, 0, len(m.Roles))
	for _, r := range m.Roles {
		out = append(out, r.Name)
	}
	return out
}
