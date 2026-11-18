package chatsvc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// WithAgentResponder lets direct messages with a global agent be answered by
// it: what a person writes is handed to the agent's chat session and its reply
// is posted back (see chat_agent_dms).
func (s *Service) WithAgentResponder(r chatdom.AgentResponder) *Service {
	s.agents = r
	return s
}

// answersDMs reports whether the channel is a direct message with an agent that
// replies in it, which only a global agent does.
func (s *Service) answersDMs(ctx context.Context, ch *chatdom.Channel) bool {
	if s.agents == nil || ch == nil || ch.Kind != chatdom.KindDM || ch.Peer == nil || ch.Peer.Kind != chatdom.AccountAgent {
		return false
	}
	ok, err := s.repo.IsGlobalAgent(ctx, ch.Peer.AccountID)
	return err == nil && ok
}

// forwardToAgent hands the person's messages the agent has not seen to it.
// Messages written while the agent is still answering stay behind and go out
// when it finishes (OnAgentTurnEnd). Failures are logged: the message itself is
// stored and sent.
func (s *Service) forwardToAgent(ctx context.Context, ch *chatdom.Channel, personID uuid.UUID) {
	if !s.answersDMs(ctx, ch) || ch.Peer.AccountID == personID {
		return
	}
	link, err := s.repo.EnsureAgentLink(ctx, ch.ID, ch.Peer.AccountID, personID)
	if err != nil {
		slog.Warn("chat: agent link", "channel_id", ch.ID, "err", err)
		return
	}
	s.forward(ctx, link)
}

func (s *Service) forward(ctx context.Context, link *chatdom.AgentLink) {
	msgs, err := s.repo.MessagesToForward(ctx, link.ChannelID, link.AccountID, link.ForwardedSeq)
	if err != nil || len(msgs) == 0 {
		if err != nil {
			slog.Warn("chat: messages to forward", "channel_id", link.ChannelID, "err", err)
		}
		return
	}
	texts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		texts = append(texts, m.ContentText)
	}
	from, upto := link.ForwardedSeq, msgs[len(msgs)-1].Seq
	claimed, err := s.repo.ClaimForward(ctx, link.ChannelID, from, upto)
	if err != nil || !claimed {
		return // another caller is forwarding the same messages
	}
	sessionID, convID, err := s.agents.Send(ctx, link.AgentID, link.AccountID, link.SessionID, strings.Join(texts, "\n\n"))
	if err != nil {
		_ = s.repo.ReleaseForward(ctx, link.ChannelID, from, upto)
		if errors.Is(err, chatdom.ErrAgentBusy) {
			return // sent when the current answer ends
		}
		slog.Warn("chat: forward to agent", "channel_id", link.ChannelID, "agent_id", link.AgentID, "err", err)
		s.agentSays(ctx, link, "I couldn't pick that up right now. Please try again in a moment.", nil)
		return
	}
	if err := s.repo.SetAgentRun(ctx, link.ChannelID, sessionID, convID); err != nil {
		slog.Warn("chat: record agent run", "channel_id", link.ChannelID, "err", err)
	}
}

// OnAgentTurnEnd turns what the agent wrote in a finished turn into a message
// of the direct message that talks to its session, then sends what the person
// wrote meanwhile. turnEnd is the index of the turn's closing event; a turn at
// or before what was already bridged is a repeated delivery and does nothing. A
// session no direct message talks to is ignored.
func (s *Service) OnAgentTurnEnd(ctx context.Context, sessionID, conversationID uuid.UUID, turnEnd int) error {
	link, err := s.repo.FindAgentLinkBySession(ctx, sessionID)
	if err != nil || link == nil || link.ConversationID == nil || *link.ConversationID != conversationID || turnEnd <= link.BridgedIndex {
		return err
	}
	text, err := s.repo.AgentReply(ctx, conversationID, link.BridgedIndex, turnEnd)
	if err != nil {
		return err
	}
	if text == "" {
		text = "I'm done, but I have nothing to add in words. Open the run to see what I did."
	}
	// Mark first: a repeated delivery of the same turn must not post twice.
	if err := s.repo.SetBridgedIndex(ctx, link.ChannelID, turnEnd); err != nil {
		return err
	}
	s.agentSays(ctx, link, text, &conversationID)
	s.forward(ctx, link)
	return nil
}

// agentSays posts text into the direct message as the agent.
func (s *Service) agentSays(ctx context.Context, link *chatdom.AgentLink, text string, run *uuid.UUID) {
	content := blocksOf(text)
	m := &chatdom.Message{ChannelID: link.ChannelID, AuthorID: link.AgentID, Content: content, ContentText: text, RunID: run}
	if err := s.repo.CreateMessage(ctx, m); err != nil {
		slog.Warn("chat: agent message", "channel_id", link.ChannelID, "err", err)
		return
	}
	full, err := s.repo.FindMessage(ctx, link.ChannelID, m.ID, link.AgentID)
	if err != nil {
		return
	}
	s.notifyMessage(ctx, chatdom.EventMessageNew, uuid.Nil, full, link.AgentID)
}

// blocksOf is text as block content: one paragraph block per blank-line
// separated paragraph.
func blocksOf(text string) []byte {
	type span struct {
		Type   string         `json:"type"`
		Text   string         `json:"text"`
		Styles map[string]any `json:"styles"`
	}
	type block struct {
		Type    string `json:"type"`
		Content []span `json:"content"`
	}
	var blocks []block
	for _, para := range strings.Split(strings.TrimSpace(text), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		blocks = append(blocks, block{Type: "paragraph", Content: []span{{Type: "text", Text: para, Styles: map[string]any{}}}})
	}
	out, _ := json.Marshal(blocks)
	return out
}

// AgentRuns implements chatdom.Service.
func (s *Service) AgentRuns(ctx context.Context, channelID, accountID uuid.UUID) (*chatdom.AgentRuns, error) {
	ch, err := s.requireChannel(ctx, uuid.Nil, channelID, accountID)
	if err != nil {
		return nil, err
	}
	if ch.Kind != chatdom.KindDM || ch.Peer == nil || ch.Peer.Kind != chatdom.AccountAgent {
		return nil, chatdom.ErrDMNotSupported
	}
	out := &chatdom.AgentRuns{}
	if !s.answersDMs(ctx, ch) {
		return out, nil
	}
	out.Replies = true
	link, err := s.repo.EnsureAgentLink(ctx, ch.ID, ch.Peer.AccountID, accountID)
	if err != nil {
		return nil, err
	}
	out.Runs, err = s.repo.ListAgentRuns(ctx, link, chatdom.MaxAgentRuns)
	if err != nil {
		return nil, err
	}
	if len(out.Runs) > 0 {
		switch out.Runs[0].Status {
		case "queued", "running":
			out.Working = true
		}
	}
	return out, nil
}
