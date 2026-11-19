package chatsvc

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

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

// Agent-bridge timings; variables so tests can shorten them.
var (
	forwardBusyRetries = 5
	forwardBusyBackoff = time.Second
	recordRunRetries   = 3
)

// forwardAfterTurn sends what the person wrote during a turn. The runner
// publishes turn_end just before it flips the conversation to paused, so the
// agent can still look busy for a moment: wait it out instead of leaving the
// messages behind.
func (s *Service) forwardAfterTurn(ctx context.Context, link *chatdom.AgentLink) {
	for i := 0; i < forwardBusyRetries; i++ {
		if !s.forward(ctx, link) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(forwardBusyBackoff * time.Duration(i+1)):
		}
	}
	slog.Warn("chat: agent still busy, messages left unforwarded", "channel_id", link.ChannelID)
}

// forward reports whether the agent was busy, so the caller may try again.
func (s *Service) forward(ctx context.Context, link *chatdom.AgentLink) (busy bool) {
	// The hand-off outlives the request that triggered it: a client that
	// aborts its POST must not strand a claimed message.
	ctx = context.WithoutCancel(ctx)
	msgs, err := s.repo.MessagesToForward(ctx, link.ChannelID, link.AccountID, link.ForwardedSeq)
	if err != nil || len(msgs) == 0 {
		if err != nil {
			slog.Warn("chat: messages to forward", "channel_id", link.ChannelID, "err", err)
		}
		return false
	}
	texts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		texts = append(texts, m.ContentText)
	}
	from, upto := link.ForwardedSeq, msgs[len(msgs)-1].Seq
	claimed, err := s.repo.ClaimForward(ctx, link.ChannelID, from, upto)
	if err != nil || !claimed {
		return false // another caller is forwarding the same messages
	}
	sessionID, convID, err := s.agents.Send(ctx, link.AgentID, link.AccountID, link.SessionID, strings.Join(texts, "\n\n"))
	if err != nil {
		if rerr := s.repo.ReleaseForward(ctx, link.ChannelID, from, upto); rerr != nil {
			slog.Error("chat: release forward claim", "channel_id", link.ChannelID, "err", rerr)
		}
		if errors.Is(err, chatdom.ErrAgentBusy) {
			return true // sent when the current answer ends
		}
		slog.Warn("chat: forward to agent", "channel_id", link.ChannelID, "agent_id", link.AgentID, "err", err)
		_ = s.agentSays(ctx, link, "I couldn't pick that up right now. Please try again in a moment.", nil)
		return false
	}
	// Without the run recorded the answer would be taken for someone else's.
	for i := 0; i < recordRunRetries; i++ {
		if err = s.repo.SetAgentRun(ctx, link.ChannelID, sessionID, convID); err == nil {
			return false
		}
	}
	slog.Error("chat: record agent run", "channel_id", link.ChannelID, "err", err)
	return false
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
	// Post before marking the turn bridged: a failure in between repeats the
	// delivery (at worst a duplicate reply) instead of losing the answer.
	if err := s.agentSays(ctx, link, text, &conversationID); err != nil {
		return err
	}
	if err := s.repo.SetBridgedIndex(ctx, link.ChannelID, turnEnd); err != nil {
		return err
	}
	s.forwardAfterTurn(ctx, link)
	return nil
}

// agentSays posts text into the direct message as the agent.
// Only a failure to store it is an error: once stored it is not retried.
func (s *Service) agentSays(ctx context.Context, link *chatdom.AgentLink, text string, run *uuid.UUID) error {
	content := blocksOf(text)
	m := &chatdom.Message{ChannelID: link.ChannelID, AuthorID: link.AgentID, Content: content, ContentText: text, RunID: run}
	if err := s.repo.CreateMessage(ctx, m); err != nil {
		slog.Warn("chat: agent message", "channel_id", link.ChannelID, "err", err)
		return err
	}
	if full, err := s.repo.FindMessage(ctx, link.ChannelID, m.ID, link.AgentID); err == nil {
		s.notifyMessage(ctx, chatdom.EventMessageNew, uuid.Nil, full, link.AgentID)
	}
	return nil
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
