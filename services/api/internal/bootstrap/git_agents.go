package bootstrap

import (
	"context"
	"errors"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	gitdom "github.com/Paca-AI/api/internal/domain/git"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
	gitsvc "github.com/Paca-AI/api/internal/service/git"
)

// gitAgentLookup tells the Git handler whether an agent id from a URL is one of
// that URL's project's agents (global agents are not).
type gitAgentLookup struct {
	repo interface {
		FindAgentByID(ctx context.Context, id uuid.UUID) (*agentdom.Agent, error)
		FindConversationByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error)
	}
}

// AgentName names an agent for the footer of what it posts; empty when unknown.
func (l gitAgentLookup) AgentName(ctx context.Context, agentID uuid.UUID) string {
	a, err := l.repo.FindAgentByID(ctx, agentID)
	if err != nil {
		return ""
	}
	return a.Name
}

// OwnsConversation reports whether the conversation is the agent's, in the project.
func (l gitAgentLookup) OwnsConversation(ctx context.Context, projectID, agentID, conversationID uuid.UUID) bool {
	c, err := l.repo.FindConversationByID(ctx, conversationID)
	return err == nil && c.AgentID == agentID && c.ProjectID == projectID
}

func (l gitAgentLookup) AgentInProject(ctx context.Context, projectID, agentID uuid.UUID) (string, bool, error) {
	a, err := l.repo.FindAgentByID(ctx, agentID)
	if err != nil {
		if errors.Is(err, agentdom.ErrAgentNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	if a.AgentScope == agentdom.AgentScopeGlobal || a.ProjectID != projectID {
		return "", false, nil
	}
	return a.Name, true, nil
}

// gitAgentDirectory tells the Git router which agents a project has and how
// their conversations are doing.
type gitAgentDirectory struct {
	repo interface {
		ListAllAgents(ctx context.Context, projectID uuid.UUID, scope agentdom.AgentScope) ([]*agentdom.Agent, error)
		FindConversationByID(ctx context.Context, id uuid.UUID) (*agentdom.AgentConversation, error)
	}
}

func (d gitAgentDirectory) ProjectAgents(ctx context.Context, projectID uuid.UUID) ([]gitsvc.AgentRef, error) {
	list, err := d.repo.ListAllAgents(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	out := make([]gitsvc.AgentRef, 0, len(list))
	for _, a := range list {
		out = append(out, gitsvc.AgentRef{ID: a.ID, Handle: a.Handle, Name: a.Name})
	}
	return out, nil
}

func (d gitAgentDirectory) ConversationStatus(ctx context.Context, id uuid.UUID) (string, error) {
	c, err := d.repo.FindConversationByID(ctx, id)
	if err != nil {
		if errors.Is(err, agentdom.ErrConversationNotFound) {
			return "", gitdom.ErrNotFound
		}
		return "", err
	}
	return c.Status, nil
}

// gitRunStarter starts an agent conversation from a Git event. The event's
// prompt is the conversation's first message, exactly like an automation message.
type gitRunStarter struct {
	agents interface {
		TriggerDirectMessage(ctx context.Context, projectID, agentID uuid.UUID, triggeredByMemberID *uuid.UUID, message string) (*agentdom.AgentConversation, error)
	}
}

func (s gitRunStarter) StartRun(ctx context.Context, projectID, agentID uuid.UUID, message string) (uuid.UUID, error) {
	c, err := s.agents.TriggerDirectMessage(ctx, projectID, agentID, nil, message)
	if err != nil {
		return uuid.Nil, err
	}
	return c.ID, nil
}

// gitAutomationEmitter queues Git events for the automation engine, on the
// stream plugin-emitted trigger events use.
type gitAutomationEmitter struct{ pub *messaging.Publisher }

func (e gitAutomationEmitter) EmitAutomationEvent(ctx context.Context, topic string, payload map[string]any) error {
	return e.pub.Append(ctx, events.StreamPluginTriggerEvents, topic, payload)
}
