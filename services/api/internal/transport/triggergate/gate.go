// Package triggergate is the authorization gate for agent runs that are not
// started by an HTTP request of their own: task assignment, automation
// messages, comment mentions (workers and services call the agent service
// directly) and the description-write trigger. It is the non-HTTP sibling of
// the route gates in internal/transport/http/middleware: a decorator around
// the real trigger implementation, injected where it is wired, so the agent
// service itself holds no authorization logic.
//
// The member that triggers the run must be allowed conversations:write on
// project/<P>/agent/<A> and environments:read on project/<P>/environment/<E>
// for the environment the run will use (the agent's default; triggers carry
// no environment of their own). A trigger with no member behind it (an
// unattended automation firing) is not checked. A denial is a FORBIDDEN
// error — the caller decides, as before, whether that skips the run or
// surfaces — and any failure to resolve the member or reach the authorizer
// denies.
package triggergate

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// Triggers is the set of agent-run starters the gate decorates; satisfied by
// the agent service.
type Triggers interface {
	TriggerTaskAssigned(ctx context.Context, projectID, agentID, taskID uuid.UUID, triggeredByMemberID *uuid.UUID, note string) (*agentdom.AgentConversation, error)
	TriggerDirectMessage(ctx context.Context, projectID, agentID uuid.UUID, triggeredByMemberID *uuid.UUID, message string) (*agentdom.AgentConversation, error)
	TriggerCommentMention(ctx context.Context, projectID, agentID, taskID, commentID, triggeredByMemberID uuid.UUID, message string) (*agentdom.AgentConversation, error)
	TriggerDescriptionWrite(ctx context.Context, projectID, agentID, taskID, triggeredByMemberID uuid.UUID) (*agentdom.AgentConversation, error)
}

// MemberFinder resolves a project_members row; satisfied by the project repository.
type MemberFinder interface {
	FindMemberByID(ctx context.Context, memberID uuid.UUID) (*projectdom.ProjectMember, error)
}

// AgentEnvironments returns an agent's default environment, scoped to the
// project (agentdom.ErrAgentNotFound when the agent is not visible in it,
// nil when it has none).
type AgentEnvironments interface {
	DefaultEnvironmentID(ctx context.Context, projectID, agentID uuid.UUID) (*uuid.UUID, error)
}

// Gate decorates Triggers with the authorization checks above.
type Gate struct {
	next    Triggers
	authz   iam.Checker
	members MemberFinder
	envs    AgentEnvironments
}

// New returns the gate around next.
func New(next Triggers, authz iam.Checker, members MemberFinder, envs AgentEnvironments) *Gate {
	return &Gate{next: next, authz: authz, members: members, envs: envs}
}

var errForbidden = apierr.New(apierr.CodeForbidden, "you don't have access to use this agent or its environment")

// allow reports nil when the member may run the agent; uuid.Nil means no
// actor behind the trigger.
func (g *Gate) allow(ctx context.Context, projectID, agentID uuid.UUID, memberID uuid.UUID) error {
	if memberID == uuid.Nil {
		return nil
	}
	if g.authz == nil || g.members == nil || g.envs == nil {
		return fmt.Errorf("trigger gate: not configured")
	}
	member, err := g.members.FindMemberByID(ctx, memberID)
	if err != nil {
		return fmt.Errorf("trigger gate: resolve member: %w", err)
	}
	if member.ProjectID != projectID {
		return errForbidden
	}
	principal := iam.User(member.UserID.String())
	if member.MemberType == "agent" && member.AgentID != nil {
		principal = iam.Agent(member.AgentID.String())
	}
	project := iam.ProjectResource(projectID.String())
	ok, err := iam.AllowedAll(ctx, g.authz, principal, project+"/agent/"+agentID.String(), iam.ActionConversationsWrite)
	if err != nil {
		return fmt.Errorf("trigger gate: check agent: %w", err)
	}
	if !ok {
		return errForbidden
	}
	envID, err := g.envs.DefaultEnvironmentID(ctx, projectID, agentID)
	if err != nil {
		return err
	}
	if envID == nil {
		return nil
	}
	ok, err = iam.AllowedAll(ctx, g.authz, principal, project+"/environment/"+envID.String(), iam.ActionEnvironmentsRead)
	if err != nil {
		return fmt.Errorf("trigger gate: check environment: %w", err)
	}
	if !ok {
		return errForbidden
	}
	return nil
}

func memberOrNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// TriggerTaskAssigned implements Triggers.
func (g *Gate) TriggerTaskAssigned(ctx context.Context, projectID, agentID, taskID uuid.UUID, by *uuid.UUID, note string) (*agentdom.AgentConversation, error) {
	if err := g.allow(ctx, projectID, agentID, memberOrNil(by)); err != nil {
		return nil, err
	}
	return g.next.TriggerTaskAssigned(ctx, projectID, agentID, taskID, by, note)
}

// TriggerDirectMessage implements Triggers.
func (g *Gate) TriggerDirectMessage(ctx context.Context, projectID, agentID uuid.UUID, by *uuid.UUID, message string) (*agentdom.AgentConversation, error) {
	if err := g.allow(ctx, projectID, agentID, memberOrNil(by)); err != nil {
		return nil, err
	}
	return g.next.TriggerDirectMessage(ctx, projectID, agentID, by, message)
}

// TriggerCommentMention implements Triggers.
func (g *Gate) TriggerCommentMention(ctx context.Context, projectID, agentID, taskID, commentID, by uuid.UUID, message string) (*agentdom.AgentConversation, error) {
	if err := g.allow(ctx, projectID, agentID, by); err != nil {
		return nil, err
	}
	return g.next.TriggerCommentMention(ctx, projectID, agentID, taskID, commentID, by, message)
}

// TriggerDescriptionWrite implements Triggers.
func (g *Gate) TriggerDescriptionWrite(ctx context.Context, projectID, agentID, taskID, by uuid.UUID) (*agentdom.AgentConversation, error) {
	if err := g.allow(ctx, projectID, agentID, by); err != nil {
		return nil, err
	}
	return g.next.TriggerDescriptionWrite(ctx, projectID, agentID, taskID, by)
}

// gatedService is an agentdom.Service whose description-write trigger goes
// through the gate; every other method is the wrapped service's.
type gatedService struct {
	agentdom.Service
	gate *Gate
}

// TriggerDescriptionWrite overrides the wrapped service's.
func (s gatedService) TriggerDescriptionWrite(ctx context.Context, projectID, agentID, taskID, by uuid.UUID) (*agentdom.AgentConversation, error) {
	return s.gate.TriggerDescriptionWrite(ctx, projectID, agentID, taskID, by)
}

// WrapService returns svc with its description-write trigger gated, for
// callers (HTTP handlers) that hold the whole agent service interface.
func (g *Gate) WrapService(svc agentdom.Service) agentdom.Service {
	return gatedService{Service: svc, gate: g}
}
