package chatsvc

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	chatdom "github.com/Paca-AI/api/internal/domain/chat"
	"github.com/Paca-AI/api/internal/events"
)

// publisher is the part of messaging.Publisher the notifier uses.
type publisher interface {
	Publish(ctx context.Context, channel string, payload any) error
}

// PubSubNotifier publishes announcements on the realtime Pub/Sub channel.
// They never enter the project change log: message traffic would crowd out
// task and doc changes there (see docs/architecture/chat-and-meetings.md).
type PubSubNotifier struct{ pub publisher }

// NewPubSubNotifier returns a notifier over pub.
func NewPubSubNotifier(pub publisher) *PubSubNotifier { return &PubSubNotifier{pub: pub} }

type wireAuthz struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
}

type wireMessage struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
	// RecipientIDs are routed to the accounts' own rooms and are never part
	// of what a client receives.
	RecipientIDs []string `json:"recipient_ids,omitempty"`
	// Authz is set on channel lifecycle events: realtime checks each candidate
	// socket against it, as it does for project events.
	Authz *wireAuthz `json:"authz,omitempty"`
}

// Notify implements chatdom.Notifier.
func (n *PubSubNotifier) Notify(ctx context.Context, ev chatdom.Notification) {
	payload := map[string]any{
		"channel_id": ev.ChannelID.String(),
		"seq":        ev.Seq,
		"actor_id":   ev.ActorID.String(),
	}
	// A direct message has no project.
	if ev.ProjectID != uuid.Nil {
		payload["project_id"] = ev.ProjectID.String()
	}
	if ev.MessageID != nil {
		payload["message_id"] = ev.MessageID.String()
	}
	if ev.ThreadRootID != nil {
		payload["thread_root_id"] = ev.ThreadRootID.String()
	}
	msg := wireMessage{Type: ev.Type, Payload: payload}
	for _, r := range ev.Recipients {
		msg.RecipientIDs = append(msg.RecipientIDs, r.String())
	}
	if ev.Type == chatdom.EventChannelChanged {
		msg.Authz = &wireAuthz{Action: "channels:read", Resource: "project/" + ev.ProjectID.String() + "/channel/" + ev.ChannelID.String()}
	}
	// A lost announcement costs a client one refetch (after_seq), so this is
	// logged, not returned.
	if err := n.pub.Publish(ctx, events.ChannelRealtime, msg); err != nil {
		slog.Warn("chat: publish realtime announcement", "type", ev.Type, "channel_id", ev.ChannelID, "err", err)
	}
}
