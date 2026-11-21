package chatsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

// The automation node types chat contributes. Like the git.* ones they belong
// to core. Both triggers carry no task, so a run can reach call_api,
// trigger_ai_agent, wait and chat.send_message, and read the message through
// the {{event.text}}, {{event.author}}, {{event.channel}} … placeholders.
const (
	// TriggerMessagePosted fires for a message a person or service account
	// writes in a project channel (an agent's message never does, so agents
	// cannot set each other off).
	TriggerMessagePosted = "chat.message_posted"
	// TriggerMention fires for such a message that mentions someone; the
	// {{event.mentioned}} placeholder lists their names.
	TriggerMention = "chat.mention"
	// ActionSendMessage posts text in a channel as an agent of the project.
	ActionSendMessage = "chat.send_message"

	// AutomationOwner is the name the engine resolves these node types to.
	AutomationOwner = "core.chat"
)

// AutomationEmitter hands a chat event to the automation engine.
type AutomationEmitter interface {
	EmitAutomationEvent(ctx context.Context, topic string, payload map[string]any) error
}

// WithAutomationEmitter makes the service emit the chat automation triggers.
func (s *Service) WithAutomationEmitter(e AutomationEmitter) *Service {
	s.automation = e
	return s
}

// emitAutomation emits the triggers a stored message raises, best effort: an
// automation that cannot be queued must not fail the post.
func (s *Service) emitAutomation(ctx context.Context, projectID uuid.UUID, ch *chatdom.Channel, m *chatdom.Message, mentioned []uuid.UUID) {
	if s.automation == nil || projectID == uuid.Nil || ch.ArchivedAt != nil {
		return
	}
	if kind, ok, err := s.repo.AccountKind(ctx, m.AuthorID); err != nil || !ok || kind == chatdom.AccountAgent {
		return
	}
	payload := map[string]any{
		"project_id": projectID.String(), "channel_id": ch.ID.String(), "channel": ch.Name, "channel_kind": ch.Kind,
		"message_id": m.ID.String(), "author_id": m.AuthorID.String(), "author": m.AuthorName, "text": m.ContentText,
	}
	if m.ThreadRootID != nil {
		payload["thread_root_id"] = m.ThreadRootID.String()
	}
	// Private channels stay out of automations: the message was not written for
	// whoever may later read a run's log.
	if ch.Kind != chatdom.KindPublic {
		return
	}
	emit := func(topic string, p map[string]any) {
		if err := s.automation.EmitAutomationEvent(ctx, topic, p); err != nil {
			slog.Warn("chat: emitting automation event", "topic", topic, "channel_id", ch.ID, "err", err)
		}
	}
	emit(TriggerMessagePosted, payload)
	if len(mentioned) > 0 {
		names := mentionNames(m.Content)
		p := map[string]any{}
		for k, v := range payload {
			p[k] = v
		}
		p["mentioned"] = strings.Join(names, ", ")
		emit(TriggerMention, p)
	}
}

// mentionNames returns the names the content mentions, in order.
func mentionNames(content []byte) []string {
	var blocks []struct {
		Content []struct {
			Type  string         `json:"type"`
			Props map[string]any `json:"props"`
		} `json:"content"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		for _, n := range b.Content {
			if n.Type == "teamMention" {
				if name, _ := n.Props["name"].(string); name != "" {
					out = append(out, name)
				}
			}
		}
	}
	return out
}

// AutomationNodeTypes describes the chat node types for the automation palette.
func AutomationNodeTypes() (triggers, conditions, actions []plugindom.AutomationNodeManifest) {
	triggers = []plugindom.AutomationNodeManifest{
		{Type: TriggerMessagePosted, Label: "Chat: Message Posted", EventTopic: TriggerMessagePosted, Taskless: true},
		{Type: TriggerMention, Label: "Chat: Someone Mentioned", EventTopic: TriggerMention, Taskless: true},
	}
	cfg, _ := json.Marshal(map[string]any{
		"type":     "object",
		"required": []string{"member_id", "channel_id", "text"},
		"properties": map[string]any{
			"member_id":  map[string]any{"type": "string", "title": "Post as (agent member)"},
			"channel_id": map[string]any{"type": "string", "title": "Channel ID (or {{event.channel_id}})"},
			"text":       map[string]any{"type": "string", "title": "Message", "format": "textarea"},
			"thread":     map[string]any{"type": "string", "title": "Reply in thread of message ID (optional, e.g. {{event.message_id}})"},
		},
	})
	actions = []plugindom.AutomationNodeManifest{{Type: ActionSendMessage, Label: "Chat: Send Message", ConfigSchema: cfg}}
	return triggers, nil, actions
}

// RunAutomationAction runs a chat action node: it posts the (already rendered)
// text as the chosen agent. Result shape matches the other core nodes.
func (s *Service) RunAutomationAction(ctx context.Context, raw []byte) ([]byte, error) {
	var n struct {
		NodeType  string          `json:"node_type"`
		Config    json.RawMessage `json:"config"`
		ProjectID uuid.UUID       `json:"project_id"`
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		return nil, fmt.Errorf("chat automation: decode node: %w", err)
	}
	err := s.sendAsAgent(ctx, n.NodeType, n.ProjectID, n.Config)
	res := map[string]any{"applied": err == nil}
	if err != nil {
		res["error"] = err.Error()
	}
	return json.Marshal(res)
}

func (s *Service) sendAsAgent(ctx context.Context, nodeType string, projectID uuid.UUID, config json.RawMessage) error {
	if nodeType != ActionSendMessage {
		return fmt.Errorf("unknown action %q", nodeType)
	}
	var cfg struct {
		MemberID  string `json:"member_id"`
		ChannelID string `json:"channel_id"`
		Text      string `json:"text"`
		Thread    string `json:"thread"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}
	memberID, err := uuid.Parse(strings.TrimSpace(cfg.MemberID))
	if err != nil {
		return errors.New("member_id is not valid")
	}
	channelID, err := uuid.Parse(strings.TrimSpace(cfg.ChannelID))
	if err != nil {
		return errors.New("channel_id is not valid")
	}
	text := strings.TrimSpace(cfg.Text)
	if text == "" {
		return chatdom.ErrMessageEmpty
	}
	var thread *uuid.UUID
	if t := strings.TrimSpace(cfg.Thread); t != "" {
		id, err := uuid.Parse(t)
		if err != nil {
			return errors.New("thread is not a message id")
		}
		thread = &id
	}
	agent, ok, err := s.repo.AgentAccountOfMember(ctx, projectID, memberID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("the member is not an agent of this project")
	}
	// The channel must be one of this project's, whatever id the template gave.
	if _, err := s.repo.FindChannel(ctx, projectID, channelID, agent); err != nil {
		return err
	}
	if _, err := s.PostMessage(ctx, projectID, channelID, agent, blocksOf(text), thread); err != nil {
		return err
	}
	return nil
}
