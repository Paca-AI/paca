package projectsvc

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/events"
)

// ListMembers returns all members of the given project.
func (s *Service) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}
	return s.repo.ListMembers(ctx, projectID)
}

// CountDistinctAgentsByProjects returns the distinct agent member count
// across projectIDs. Unlike ListMembers, this skips the per-project
// FindByID existence check — it's a cross-project aggregate over a caller-
// supplied ID set (e.g. every project a user can access), not a lookup
// scoped to one project the caller is expected to already know exists.
func (s *Service) CountDistinctAgentsByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error) {
	return s.repo.CountDistinctAgentsByProjects(ctx, projectIDs)
}

// AddMember adds a member to a project holding the given roles — a human
// (in.UserID) or, when in.AgentID is set, invites an existing global agent
// into the project (the "invite" flow: the same action as adding a human,
// just for an agent). The membership row and the project-scoped role
// attachments are written in one transaction; the roles must exist and be
// attachable in this project (checked by the repository).
func (s *Service) AddMember(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}
	if len(in.RoleIDs) == 0 {
		return nil, roledom.ErrRoleRequired
	}

	if in.AgentID != nil {
		return s.addAgentMember(ctx, projectID, *in.AgentID, in.RoleIDs, in.CreatedBy)
	}

	_, err := s.repo.FindMember(ctx, projectID, in.UserID)
	if err == nil {
		return nil, projectdom.ErrMemberAlreadyAdded
	}
	if !errors.Is(err, projectdom.ErrMemberNotFound) {
		return nil, err
	}

	m := &projectdom.ProjectMember{
		ID:          uuid.New(),
		ProjectID:   projectID,
		UserID:      in.UserID,
		Description: strings.TrimSpace(in.Description),
	}
	if err := s.repo.AddMember(ctx, m, in.RoleIDs, in.CreatedBy); err != nil {
		return nil, err
	}
	s.roleGrantsChanged(in.RoleIDs)

	// Re-fetch to populate username and roles via JOIN.
	added, err := s.repo.FindMember(ctx, projectID, in.UserID)
	if err != nil {
		return nil, err
	}
	s.record(ctx, projectID, events.EntityMember, added.ID, TopicMemberAdded, memberPayload(added))
	return added, nil
}

// addAgentMember validates and invites an existing global agent into a
// project — the AgentID branch of AddMember. Only global-scope agents can
// be invited (a project-scoped agent already belongs to exactly one project
// by construction); the agent must not already be a member; and its handle
// must not collide with any agent already visible in this project (its own
// project-scoped agents plus any other invited global agents), since
// @mention resolution is handle-based within a project.
func (s *Service) addAgentMember(ctx context.Context, projectID, agentID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) (*projectdom.ProjectMember, error) {
	if s.agents == nil {
		return nil, projectdom.ErrAgentNotInvitable
	}
	agent, err := s.agents.FindAgentByID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent.AgentScope != agentdom.AgentScopeGlobal {
		return nil, projectdom.ErrAgentNotInvitable
	}

	if _, err := s.repo.FindMemberByAgent(ctx, projectID, agentID); err == nil {
		return nil, projectdom.ErrMemberAlreadyAdded
	} else if !errors.Is(err, projectdom.ErrMemberNotFound) {
		return nil, err
	}

	if existing, err := s.agents.FindAgentByHandle(ctx, projectID, agent.Handle); err == nil && existing.ID != agentID {
		return nil, projectdom.ErrAgentHandleConflict
	}

	memberID := uuid.New()
	if err := s.repo.AddAgentMember(ctx, memberID, projectID, agentID, roleIDs, createdBy); err != nil {
		return nil, err
	}
	s.roleGrantsChanged(roleIDs)
	added, err := s.repo.FindMemberByAgent(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	s.record(ctx, projectID, events.EntityMember, added.ID, TopicMemberAdded, memberPayload(added))
	return added, nil
}

// RemoveMember removes a user from the project.
func (s *Service) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return err
	}
	if _, err := s.repo.FindMember(ctx, projectID, userID); err != nil {
		return err
	}
	return s.repo.RemoveMember(ctx, projectID, userID)
}

// AddAgentMember inserts an agent as a project member holding roleIDs.
func (s *Service) AddAgentMember(ctx context.Context, memberID, projectID, agentID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) error {
	if len(roleIDs) == 0 {
		return roledom.ErrRoleRequired
	}
	if err := s.repo.AddAgentMember(ctx, memberID, projectID, agentID, roleIDs, createdBy); err != nil {
		return err
	}
	s.roleGrantsChanged(roleIDs)
	return nil
}

// RemoveAgentMember soft-deletes the agent's membership record.
func (s *Service) RemoveAgentMember(ctx context.Context, projectID, agentID uuid.UUID) error {
	return s.repo.RemoveAgentMember(ctx, projectID, agentID)
}

// UpdateMemberDescription changes a member's Jev-facing description by member ID.
func (s *Service) UpdateMemberDescription(ctx context.Context, projectID, memberID uuid.UUID, description string) (*projectdom.ProjectMember, error) {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return nil, err
	}

	member, err := s.repo.FindMemberByID(ctx, memberID)
	if err != nil {
		return nil, err
	}

	if member.ProjectID != projectID {
		return nil, projectdom.ErrMemberNotFound
	}

	if err := s.repo.UpdateMemberDescription(ctx, memberID, description); err != nil {
		return nil, err
	}

	updated, err := s.repo.FindMemberByID(ctx, memberID)
	if err != nil {
		return nil, err
	}
	payload := memberPayload(updated)
	payload["changes"] = []string{"description"}
	s.record(ctx, projectID, events.EntityMember, memberID, TopicMemberUpdated, payload)
	return updated, nil
}

// RemoveMemberByMemberID removes a project member by member ID.
func (s *Service) RemoveMemberByMemberID(ctx context.Context, projectID, memberID uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, projectID); err != nil {
		return err
	}

	member, err := s.repo.FindMemberByID(ctx, memberID)
	if err != nil {
		return err
	}

	if member.ProjectID != projectID {
		return projectdom.ErrMemberNotFound
	}

	if err := s.repo.RemoveMemberByMemberID(ctx, memberID); err != nil {
		return err
	}
	s.record(ctx, projectID, events.EntityMember, memberID, TopicMemberRemoved, memberPayload(member))
	return nil
}
