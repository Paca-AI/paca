package chatsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
)

// contextMessages is how many earlier messages an agent is shown with a mention.
const contextMessages = 8

// linkAttempts is how often the run link is written before it is given up on.
const linkAttempts = 3

// WithAgentTrigger lets an @mention of an agent in a project channel start a
// run of that agent (see chat_agent_runs).
func (s *Service) WithAgentTrigger(t chatdom.AgentTrigger) *Service {
	s.trigger = t
	return s
}

// triggerMentionedAgents starts a run for every agent the message mentions.
// The write that stored the message already succeeded and nothing here may
// undo it: a mention that cannot start a run (the author may not use the
// agent, the agent is not in the project) is logged and skipped, as comment
// mentions are.
//
// Only people and service accounts start runs. An agent's own message never
// does, so two agents cannot call each other in a loop.
func (s *Service) triggerMentionedAgents(ctx context.Context, projectID uuid.UUID, ch *chatdom.Channel, m *chatdom.Message, mentioned []uuid.UUID) {
	if s.trigger == nil || projectID == uuid.Nil || len(mentioned) == 0 || ch.ArchivedAt != nil {
		return
	}
	if kind, ok, err := s.repo.AccountKind(ctx, m.AuthorID); err != nil || !ok || kind == chatdom.AccountAgent {
		return
	}
	authorMember, ok, err := s.repo.ProjectMemberID(ctx, projectID, m.AuthorID)
	if err != nil || !ok {
		return
	}
	var transcript []*chatdom.Message
	loaded := false
	for _, id := range mentioned {
		if id == m.AuthorID {
			continue
		}
		if kind, ok, err := s.repo.AccountKind(ctx, id); err != nil || !ok || kind != chatdom.AccountAgent {
			continue
		}
		// The agent must be a member of this project, as for a task comment.
		if _, ok, err := s.repo.ProjectMemberID(ctx, projectID, id); err != nil || !ok {
			continue
		}
		if !loaded {
			transcript, loaded = s.contextFor(ctx, ch, m), true
		}
		convID, err := s.trigger.TriggerChannelMention(ctx, projectID, id, authorMember, channelMentionPrompt(ch, projectID, m, transcript))
		if err != nil {
			slog.WarnContext(ctx, "chat: agent mention not run", "channel_id", ch.ID, "agent_id", id, "err", err)
			continue
		}
		root := m.ID
		if m.ThreadRootID != nil {
			root = *m.ThreadRootID
		}
		link := &chatdom.ChannelAgentRun{
			ConversationID: convID, ChannelID: ch.ID, SourceMessageID: m.ID, ThreadRootID: root, AgentID: id, RequesterID: m.AuthorID,
		}
		// Without this row the run's answer is never posted, so a failed write
		// is retried before it is given up on.
		var linkErr error
		for attempt := 0; attempt < linkAttempts; attempt++ {
			if linkErr = s.repo.CreateAgentRun(ctx, link); linkErr == nil {
				break
			}
		}
		if linkErr != nil {
			slog.ErrorContext(ctx, "chat: link agent run", "conversation_id", convID, "err", linkErr)
		}
	}
}

// contextFor returns the messages just before m, oldest first, for the agent to
// read with the mention. A private channel gets none: the agent has not been
// let into it, and the person only chose to share the message they wrote.
func (s *Service) contextFor(ctx context.Context, ch *chatdom.Channel, m *chatdom.Message) []*chatdom.Message {
	if ch.Kind != chatdom.KindPublic {
		return nil
	}
	f := chatdom.MessageFilter{BeforeSeq: &m.Seq, Limit: contextMessages, ThreadRootID: m.ThreadRootID}
	msgs, _, err := s.repo.ListMessages(ctx, ch.ID, m.AuthorID, f)
	if err != nil {
		return nil
	}
	if m.ThreadRootID != nil {
		// A reply is read with the message it replies to.
		if root, err := s.repo.FindMessage(ctx, ch.ID, *m.ThreadRootID, m.AuthorID); err == nil {
			msgs = append([]*chatdom.Message{root}, msgs...)
		}
	}
	out := msgs[:0:0]
	for _, x := range msgs {
		if x.DeletedAt == nil && x.ContentText != "" {
			out = append(out, x)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// channelMentionPrompt is what the agent is told: who mentioned it, where, what
// they wrote and what came before, and that its final answer is posted back.
func channelMentionPrompt(ch *chatdom.Channel, projectID uuid.UUID, m *chatdom.Message, before []*chatdom.Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s mentioned you in #%s.\n\n", m.AuthorName, ch.Name)
	if len(before) > 0 {
		b.WriteString("Earlier messages:\n")
		for _, x := range before {
			fmt.Fprintf(&b, "- %s: %s\n", x.AuthorName, x.ContentText)
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "Their message:\n%s\n\n", m.ContentText)
	b.WriteString("Do what they ask. Your final answer is posted as a reply in that thread, so write it for the people in the channel and keep it short.\n")
	fmt.Fprintf(&b, "(project %s, channel %s, message %s)", projectID, ch.ID, m.ID)
	return b.String()
}

// OnChannelRunEnd posts the answer of a run that a channel mention started, as
// a reply in the mention's thread from the agent's own account, once its turn
// ends. A run no channel started, and a turn already posted, do nothing.
func (s *Service) OnChannelRunEnd(ctx context.Context, conversationID uuid.UUID, turnEnd int) error {
	run, err := s.repo.FindAgentRun(ctx, conversationID)
	if err != nil || run == nil || run.Posted {
		return err
	}
	claimed, err := s.repo.ClaimAgentRunPost(ctx, conversationID)
	if err != nil {
		return err
	}
	if !claimed {
		// Another delivery is posting it. Not acked as done: if that worker
		// dies, this one is retried and takes the claim over once its lease ends.
		return errReplyInFlight
	}
	release := func() {
		if rerr := s.repo.ReleaseAgentRunPost(ctx, conversationID); rerr != nil {
			slog.Error("chat: release agent run post", "conversation_id", conversationID, "err", rerr)
		}
	}
	text, err := s.repo.AgentReply(ctx, conversationID, -1, turnEnd)
	if err != nil {
		release()
		return err
	}
	if text == "" {
		text = "I'm done, but I have nothing to add in words. Open the run to see what I did."
	}
	source, err := s.repo.FindMessage(ctx, run.ChannelID, run.SourceMessageID, run.AgentID)
	if err != nil {
		release()
		return err
	}
	content := replyBlocks(run.RequesterID, source.AuthorName, text)
	m := &chatdom.Message{
		ChannelID: run.ChannelID, AuthorID: run.AgentID, Content: content,
		ContentText: "@" + source.AuthorName + " " + text, ThreadRootID: &run.ThreadRootID, RunID: &conversationID,
	}
	if err := s.repo.CreateMessage(ctx, m); err != nil {
		release()
		return err
	}
	// The reply is stored: record it so a repeated turn_end adds nothing. If
	// this write fails the claim's lease runs out and the delivery is retried,
	// which can post the reply twice; that is better than losing it.
	if err := s.repo.MarkAgentRunPosted(ctx, conversationID); err != nil {
		slog.ErrorContext(ctx, "chat: mark agent run posted", "conversation_id", conversationID, "err", err)
	}
	_ = s.repo.RecordMessageEffects(ctx, m, []uuid.UUID{run.RequesterID})
	if full, err := s.repo.FindMessage(ctx, run.ChannelID, m.ID, run.AgentID); err == nil {
		s.notifyMessage(ctx, chatdom.EventMessageNew, run.ProjectID, full, run.AgentID)
	}
	return nil
}

// errReplyInFlight: another delivery of the same turn_end holds the claim.
var errReplyInFlight = errors.New("chat: agent reply is being posted by another delivery")

// replyBlocks is text as block content, the first paragraph opening with a
// mention of the person who asked so the answer reaches their Inbox.
func replyBlocks(requester uuid.UUID, name, text string) []byte {
	type node struct {
		Type   string         `json:"type"`
		Text   string         `json:"text,omitempty"`
		Styles map[string]any `json:"styles,omitempty"`
		Props  map[string]any `json:"props,omitempty"`
	}
	type block struct {
		Type    string `json:"type"`
		Content []node `json:"content"`
	}
	var blocks []block
	for i, para := range strings.Split(strings.TrimSpace(text), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		var content []node
		if i == 0 {
			content = append(content, node{Type: "teamMention", Props: map[string]any{"id": requester.String(), "name": name}}, node{Type: "text", Text: " ", Styles: map[string]any{}})
		}
		content = append(content, node{Type: "text", Text: para, Styles: map[string]any{}})
		blocks = append(blocks, block{Type: "paragraph", Content: content})
	}
	out, _ := json.Marshal(blocks)
	return out
}
