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
