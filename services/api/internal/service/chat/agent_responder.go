package chatsvc

import (
	"context"
	"errors"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// agentChats is what the responder needs of the agent service: its global chat.
type agentChats interface {
	StartGlobalChatSession(ctx context.Context, agentID, actorAccountID uuid.UUID, message string, contextItems []agentdom.ContextItemRef, onBusy string) (*agentdom.AgentChatSession, *agentdom.AgentConversation, error)
	SendGlobalChatMessage(ctx context.Context, sessionID, actorAccountID uuid.UUID, message string, contextItems []agentdom.ContextItemRef, onBusy string) (*agentdom.AgentConversation, error)
}

// GlobalAgentResponder answers direct messages with a global agent through the
// agent's global chat sessions, the same path the home page chat uses. Any
// signed-in person may chat with any global agent, so it adds no new access.
type GlobalAgentResponder struct{ chats agentChats }

// NewGlobalAgentResponder returns a responder over the agent service.
func NewGlobalAgentResponder(chats agentChats) *GlobalAgentResponder {
	return &GlobalAgentResponder{chats: chats}
}

var _ chatdom.AgentResponder = (*GlobalAgentResponder)(nil)

// Send implements chatdom.AgentResponder. A run that has to wait for a free
// slot is queued rather than refused.
func (g *GlobalAgentResponder) Send(ctx context.Context, agentID, accountID uuid.UUID, sessionID *uuid.UUID, text string) (uuid.UUID, uuid.UUID, error) {
	if sessionID == nil {
		session, conv, err := g.chats.StartGlobalChatSession(ctx, agentID, accountID, text, nil, agentdom.OnBusyQueue)
		if err != nil {
			return uuid.Nil, uuid.Nil, busy(err)
		}
		return session.ID, conv.ID, nil
	}
	conv, err := g.chats.SendGlobalChatMessage(ctx, *sessionID, accountID, text, nil, agentdom.OnBusyQueue)
	if err != nil {
		return uuid.Nil, uuid.Nil, busy(err)
	}
	return *sessionID, conv.ID, nil
}

func busy(err error) error {
	if errors.Is(err, agentdom.ErrConversationBusy) {
		return chatdom.ErrAgentBusy
	}
	return err
}

// directTriggers is what the mention trigger needs of the gated agent triggers.
type directTriggers interface {
	TriggerDirectMessage(ctx context.Context, projectID, agentID uuid.UUID, triggeredByMemberID *uuid.UUID, message string) (*agentdom.AgentConversation, error)
}

// MentionTrigger starts an agent run for a channel mention through the same
// gated trigger the automation engine uses for a message with no task: the
// person must be allowed to use the agent and its environment.
type MentionTrigger struct{ triggers directTriggers }

// NewMentionTrigger returns a trigger over the (gated) agent triggers.
func NewMentionTrigger(t directTriggers) *MentionTrigger { return &MentionTrigger{triggers: t} }

var _ chatdom.AgentTrigger = (*MentionTrigger)(nil)

// TriggerChannelMention implements chatdom.AgentTrigger.
func (m *MentionTrigger) TriggerChannelMention(ctx context.Context, projectID, agentID, memberID uuid.UUID, message string) (uuid.UUID, error) {
	conv, err := m.triggers.TriggerDirectMessage(ctx, projectID, agentID, &memberID, message)
	if err != nil {
		return uuid.Nil, err
	}
	return conv.ID, nil
}
