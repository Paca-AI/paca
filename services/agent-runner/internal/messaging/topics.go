package messaging

// Stream/channel keys mirror services/api's internal/events/topics.go —
// see that file for the authoritative list. Must stay byte-identical:
// these are literal Valkey key names every service reads/writes.
const (
	// StreamAgentTurnRequests carries authoritative, turn-first project chat
	// execution requests emitted from services/api's PostgreSQL outbox.
	StreamAgentTurnRequests = "paca:agent:turn_requests"
	StreamAgentTurnControls = "paca:agent:turn_controls"

	// StreamAgentTriggers is written by services/api and consumed here.
	// Also carries stop/pause/heartbeat control messages for an
	// already-running conversation, distinguished from a new-conversation
	// trigger by the stream entry's own "type" field — see decode.go's
	// decodeTrigger and the Consumer's routing.
	StreamAgentTriggers = "paca:agent:triggers"
	// StreamAgentEvents is a durable log this service appends every
	// conversation event to. Confirmed (by reading the real Python source,
	// not the docs) that nothing in services/api currently consumes this;
	// it exists purely as an event history. Live UI updates go through
	// ChannelRealtime instead, below — don't conflate the two.
	StreamAgentEvents = "paca:agent:events"
	// ChannelRealtime is the Valkey Pub/Sub channel services/realtime
	// subscribes to for immediate WebSocket fan-out — matches
	// events.ChannelRealtime in services/api. Every individual conversation
	// event AND every status transition gets published here, not just
	// terminal ones — see Publisher.PublishRealtime.
	ChannelRealtime = "paca.events"
	// StreamAgentConversationStatus is a durable stream a conversation's
	// terminal status (finished/failed/stopped — never "paused", which
	// isn't terminal) is appended to. Consumed by services/api's automation
	// engine to resume a graph walk paused at a trigger_ai_agent node once
	// this conversation finishes — unlike ChannelRealtime's fire-and-forget
	// pub/sub, this survives the automation engine not being connected at
	// the exact moment it's published (including across its own restart).
	StreamAgentConversationStatus = "paca:agent:conversation_status"
)
