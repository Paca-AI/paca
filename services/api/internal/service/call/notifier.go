package callsvc

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/events"
)

// EventCallChanged says who is in a channel's call changed. Like the other
// chat announcements it carries no content: clients refetch the call.
const EventCallChanged = "chat.call.changed"

type publisher interface {
	Publish(ctx context.Context, channel string, payload any) error
}

// PubSubNotifier publishes call announcements on the realtime channel, to the
// room of the channel the call belongs to.
type PubSubNotifier struct{ pub publisher }

// NewPubSubNotifier returns a notifier over pub.
func NewPubSubNotifier(pub publisher) *PubSubNotifier { return &PubSubNotifier{pub: pub} }

// CallChanged implements calldom.Notifier.
func (n *PubSubNotifier) CallChanged(ctx context.Context, channelID uuid.UUID) {
	msg := map[string]any{"type": EventCallChanged, "payload": map[string]any{"channel_id": channelID.String()}}
	if err := n.pub.Publish(ctx, events.ChannelRealtime, msg); err != nil {
		slog.Warn("calls: publish realtime announcement", "channel_id", channelID, "err", err)
	}
}
