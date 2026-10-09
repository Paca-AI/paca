package dto

import (
	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

// --- Project Member DTOs ----------------------------------------------------

// AddProjectMemberRequest is the body for POST /v1/projects/:projectId/members.
// Exactly one of UserID/AgentID must be set: UserID adds a human member;
// AgentID invites an existing global agent into the project — the same
// action, just for an agent instead of a human.
type AddProjectMemberRequest struct {
	UserID  uuid.UUID  `json:"user_id,omitempty"`
	AgentID *uuid.UUID `json:"agent_id,omitempty"`
	// RoleIDs are the roles the new member holds in the project: platform
	// roles and the project's own roles. At least one is required.
	RoleIDs []uuid.UUID `json:"role_ids"`
	// Description is the human member's Jev-facing description — see
	// projectdom.ProjectMember.Description. Ignored when inviting an agent,
	// whose description lives on the agent itself.
	Description string `json:"description,omitempty"`
}

// UpdateProjectMemberRequest is the body for PATCH /v1/projects/:projectId/members/:memberId.
// It edits the member's description; a member's roles are replaced with
// PUT /v1/projects/:projectId/members/:memberId/roles.
type UpdateProjectMemberRequest struct {
	// Description is only meaningful for a human member — see
	// projectdom.ProjectMember.Description.
	Description *string `json:"description"`
}

// ProjectMemberResponse is the public representation of a project membership.
type ProjectMemberResponse struct {
	ID        uuid.UUID `json:"id"`
	ProjectID uuid.UUID `json:"project_id"`
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	FullName  string    `json:"full_name"`
	// Roles are the roles the member holds in the project, sorted by name.
	Roles       []RoleSummaryResponse `json:"roles"`
	MemberType  string                `json:"member_type"`
	AgentID     *uuid.UUID            `json:"agent_id,omitempty"`
	AgentName   string                `json:"agent_name,omitempty"`
	AgentHandle string                `json:"agent_handle,omitempty"`
	// AvatarURL/AvatarThumbURL are presigned GET URLs for whichever of
	// user/agent backs this member, populated by the handler (not this
	// mapper) via attachmentdom.AvatarService — nil when no avatar has been
	// uploaded.
	AvatarURL      *string `json:"avatar_url,omitempty"`
	AvatarThumbURL *string `json:"avatar_thumb_url,omitempty"`
	// AgentType/AgentLLMProvider/AgentACPProvider mirror the agent's own
	// fields — only meaningful when MemberType == "agent". The frontend uses
	// these to pick a default provider-logo avatar when AvatarURL is unset.
	AgentType        string  `json:"agent_type,omitempty"`
	AgentLLMProvider string  `json:"agent_llm_provider,omitempty"`
	AgentACPProvider *string `json:"agent_acp_provider,omitempty"`
	// Description is only meaningful for a human member — see
	// projectdom.ProjectMember.Description.
	Description string `json:"description"`
}

// ProjectMemberFromEntity maps a domain ProjectMember to a ProjectMemberResponse DTO.
func ProjectMemberFromEntity(m *projectdom.ProjectMember) ProjectMemberResponse {
	resp := ProjectMemberResponse{
		ID:          m.ID,
		ProjectID:   m.ProjectID,
		UserID:      m.UserID,
		Roles:       RoleSummariesFromEntities(m.Roles),
		MemberType:  m.MemberType,
		AgentID:     m.AgentID,
		AgentName:   m.AgentName,
		AgentHandle: m.AgentHandle,

		AgentType:        m.AgentType,
		AgentLLMProvider: m.AgentLLMProvider,
		AgentACPProvider: m.AgentACPProvider,
		Description:      m.Description,
	}
	if m.IsAgent() {
		// For agent members, populate username/full_name from agent fields so
		// existing display logic (full_name || username) works without change.
		resp.FullName = m.AgentName
		resp.Username = m.AgentHandle
	} else {
		resp.Username = m.Username
		resp.FullName = m.FullName
	}
	return resp
}

// MemberAvatarKeys returns the avatar object-storage keys backing m —
// whichever of the user/agent pair actually applies — for the handler to
// resolve into presigned URLs.
func MemberAvatarKeys(m *projectdom.ProjectMember) (key, thumbKey *string) {
	if m.IsAgent() {
		return m.AgentAvatarKey, m.AgentAvatarThumbKey
	}
	return m.UserAvatarKey, m.UserAvatarThumbKey
}
