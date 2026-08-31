package messaging

// Stream/channel keys mirror services/api's internal/events/topics.go —
// see that file for the authoritative list. Must stay byte-identical:
// these are literal Valkey key names every service reads/writes.
const (
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

	// StreamAgentEnvironmentCommands is written by services/api's
	// environmentsvc.Service for its 3 environment-lifecycle calls that
	// actually wait on a Pod/container becoming ready — create, start, and
	// restart-ports (see EnvironmentCommand* below) — and consumed here by
	// EnvironmentCommandConsumer. Its other 6 calls (stop, delete,
	// folders, browse, ssh-keys/sync, port-forwards/assign) stay on direct
	// HTTP against this service's existing /internal/environments/*
	// endpoints — each is a single fast, bounded operation with no
	// readiness-wait loop, so HTTP already fits fine.
	//
	// Must stay byte-identical with services/api's own copy in
	// internal/events/topics.go — see that file's doc comment.
	StreamAgentEnvironmentCommands = "paca:agent:environment_commands"
)

// EnvironmentReplyKey returns the Valkey list key this service RPushes its
// reply to a StreamAgentEnvironmentCommands entry onto, and the key the
// original api-side caller BRPops from — one key per request, expired
// shortly after this service pushes to it so an orphan (caller already
// gave up, or crashed before popping) doesn't linger. A list, not a
// Pub/Sub channel: list values persist until popped, so there's no
// "subscriber must already be listening" race — whether this service's
// RPush lands before or after the caller's BRPop call starts, the caller
// sees it either way.
//
// Must stay byte-identical with services/api's own copy in
// internal/events/topics.go.
func EnvironmentReplyKey(requestID string) string {
	return "paca:agent:environment_reply:" + requestID
}

// Environment command types — must stay byte-identical with services/api's
// own copy in internal/events/topics.go. See that file's doc comment for
// why these are deliberately distinct from TopicEnvironmentCreate/Start/
// Stop (an unrelated stream/consumer pair).
const (
	EnvironmentCommandCreate       = "create"
	EnvironmentCommandStart        = "start"
	EnvironmentCommandRestartPorts = "restart_ports"
)
