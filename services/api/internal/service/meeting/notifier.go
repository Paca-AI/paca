package meetingsvc

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/events"
)

// Realtime announcements. Like the other chat announcements they carry ids
// only: clients refetch, and the fetch is authorized.
const (
	EventAssistantChanged = "chat.assistant.changed"
	EventProposalChanged  = "chat.proposal.changed"
)

type publisher interface {
	Publish(ctx context.Context, channel string, payload any) error
}

// PubSubNotifier publishes assistant announcements to the room of the channel
// a call belongs to.
type PubSubNotifier struct{ pub publisher }

// NewPubSubNotifier returns a notifier over pub.
func NewPubSubNotifier(pub publisher) *PubSubNotifier { return &PubSubNotifier{pub: pub} }

func (n *PubSubNotifier) publish(ctx context.Context, typ string, payload map[string]any) {
	if err := n.pub.Publish(ctx, events.ChannelRealtime, map[string]any{"type": typ, "payload": payload}); err != nil {
		slog.Warn("meeting: publish realtime announcement", "type", typ, "err", err)
	}
}

// AssistantChanged implements meetingdom.Notifier.
func (n *PubSubNotifier) AssistantChanged(ctx context.Context, channelID uuid.UUID) {
	n.publish(ctx, EventAssistantChanged, map[string]any{"channel_id": channelID.String()})
}

// ProposalChanged implements meetingdom.Notifier. A proposal made outside a
// channel has no room to announce to.
func (n *PubSubNotifier) ProposalChanged(ctx context.Context, channelID *uuid.UUID, targetID, proposalID uuid.UUID) {
	if channelID == nil {
		return
	}
	n.publish(ctx, EventProposalChanged, map[string]any{
		"channel_id": channelID.String(), "proposal_id": proposalID.String(), "target_id": targetID.String(),
	})
}
