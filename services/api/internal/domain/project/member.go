package projectdom

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	roledom "github.com/Paca-AI/api/internal/domain/role"
)

// ProjectMember represents a member (human or agent) of a project.
type ProjectMember struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	// AccountID is the member's account (a user, an agent or a service
	// account): the canonical identity of the membership.
	AccountID uuid.UUID
	UserID    uuid.UUID // the account id for a human member, zero-value otherwise
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
	MemberType  string // "human" | "agent" | "service_account"
	AgentID     *uuid.UUID
	AgentName   string
	AgentHandle string
	// AgentType/AgentLLMProvider/AgentACPProvider mirror the agent's own
	// fields (agentdom.Agent) — used by the frontend to pick a default
	// provider-logo avatar when the agent has no custom avatar uploaded.
	// Only meaningful when IsAgent() is true.
	AgentType        string // "llm" | "acp" | "provider_cli" | "meeting"
	AgentLLMProvider string
	AgentACPProvider *string
	// AgentCLIProvider mirrors agentdom.Agent.CLIProvider (provider_cli agents).
	AgentCLIProvider *string
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

// MemberFilter narrows a cursor-paginated member list. Zero values match
// everything.
type MemberFilter struct {
	// Search is split on whitespace; every word must appear
	// (case-insensitively) in the member's username, display name or agent
	// handle.
	Search string
	// RoleID, when set, keeps only members holding that project role.
	RoleID *uuid.UUID
	// MemberType, when non-empty, is "human", "agent" or "service_account".
	MemberType string
	// AccountIDs, when non-empty, keeps only members whose account id or
	// membership id is listed — the batch lookup used to resolve known ids to
	// names/avatars (feeds carry either kind of id).
	AccountIDs []uuid.UUID
	// SkipRoles leaves ProjectMember.Roles empty, saving the roles lookup for
	// callers that only need who a member is (names, avatars).
	SkipRoles bool
}

type memberCursor struct {
	Key *string `json:"k"`
	ID  string  `json:"id"`
}

// SortKey is the name the member list orders by (before lowercasing): the
// username, else the agent handle, else the display name. Mirrors the SQL
// COALESCE(u.username, a.handle, ac.name) the repository sorts on.
func (m *ProjectMember) SortKey() string {
	switch {
	case m.Username != "":
		return m.Username
	case m.AgentHandle != "":
		return m.AgentHandle
	case m.AgentName != "":
		return m.AgentName
	}
	return m.FullName
}

// EncodeMemberCursor builds an opaque cursor from the last member on a page.
// It carries the sort key and membership id, so it stays valid even if that
// member's row disappears between page loads.
func EncodeMemberCursor(m *ProjectMember) string {
	k := m.SortKey()
	b, _ := json.Marshal(memberCursor{Key: &k, ID: m.ID.String()})
	return base64.URLEncoding.EncodeToString(b)
}

// DecodeMemberCursor returns the (sort key, membership id) position in a token
// produced by EncodeMemberCursor. Any failure wraps ErrInvalidMemberCursor.
func DecodeMemberCursor(s string) (string, uuid.UUID, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("%w: base64: %v", ErrInvalidMemberCursor, err)
	}
	var c memberCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return "", uuid.Nil, fmt.Errorf("%w: json: %v", ErrInvalidMemberCursor, err)
	}
	if c.Key == nil {
		return "", uuid.Nil, fmt.Errorf("%w: missing key", ErrInvalidMemberCursor)
	}
	id, err := uuid.Parse(c.ID)
	if err != nil {
		return "", uuid.Nil, fmt.Errorf("%w: id: %v", ErrInvalidMemberCursor, err)
	}
	return *c.Key, id, nil
}
