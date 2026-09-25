package events

import (
	"context"
	"encoding/json"
	"maps"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/messaging"
)

// EntityType names the kind of thing an activity happened to. It is the
// discriminator the project-wide activity feed filters on, and it is
// deliberately a small closed set rather than a free-form string: adding a
// value here means adding a case to the feed's renderer and its filters.
type EntityType string

// The EntityType values — one per kind of thing the activity log records.
const (
	EntityTask        EntityType = "task"
	EntityDoc         EntityType = "doc"
	EntitySprint      EntityType = "sprint"
	EntityView        EntityType = "view"
	EntityAutomation  EntityType = "automation"
	EntityEnvironment EntityType = "environment"
	EntityMember      EntityType = "member"
	// EntityProject covers the project itself — its settings and the task
	// schema (types, statuses, custom fields) defined on it.
	EntityProject EntityType = "project"
	EntityRole    EntityType = "role"
	EntityAgent   EntityType = "agent"
)

// Origin mirrors taskdom.Origin, kept as a string here so this package stays
// free of a domain import — events is imported by the services that own the
// domains, so a domain import here would invert that dependency.
type Origin string

// Origin constants mirroring taskdom's, for producers in this package and for
// producers that would otherwise hard-code the string.
const (
	OriginUser       Origin = "user"
	OriginAgent      Origin = "agent"
	OriginAutomation Origin = "automation"
	OriginJev        Origin = "jev"
	OriginAnnotation Origin = "annotation"
	OriginSystem     Origin = "system"
)

// Event is a single thing that happened in a project, on its way to every
// listener that cares about it.
type Event struct {
	// Topic is the event type — "task.updated", "sprint.completed", etc. It
	// becomes the stream entry's "type" field and the realtime message's
	// "type", and services/realtime routes it to a room by prefix (see
	// eventNamespace in services/realtime/src/permissions.ts). A topic with
	// no matching prefix rule is silently dropped before it reaches any
	// client, so a new topic needs a routing rule there too.
	Topic string

	// Payload is the message body. It must be JSON-marshalable, and it is
	// the same body every destination receives — the stream entry's
	// "payload" field and the realtime message's "payload" field. Its shape
	// is entirely the producer's; Fanout never inspects or rewrites it, so
	// existing consumers of a given stream see exactly the bytes they saw
	// before.
	Payload any

	// ProjectID scopes the event to a project, and is written as its own
	// stream field so a consumer can resolve the project without decoding
	// the payload.
	ProjectID uuid.UUID

	// EntityType/EntityID identify what the event happened to, for the
	// project activity log. EntityID is nil for events with no single
	// subject (e.g. a bulk sprint completion moving many tasks).
	EntityType EntityType
	EntityID   *uuid.UUID

	// TaskID is set when the event concerns a specific task, letting the
	// project feed link back to it. Nil for non-task events.
	TaskID *uuid.UUID

	// ActorID is the authenticated user's UUID; ActorAgentID the acting
	// agent's. Both nil for system-driven changes, which is exactly why
	// Origin exists.
	ActorID      *uuid.UUID
	ActorAgentID *uuid.UUID

	// Origin identifies what caused the event. Empty is normalised to
	// OriginSystem. It travels as its own stream field so a consumer can
	// tell a system-caused change from a human one — the automation engine's
	// self-trigger guard depends on it.
	Origin Origin

	// SkipPluginEvents suppresses the StreamPluginEvents append. Set it for
	// events a plugin could not meaningfully act on — plugin dispatch is
	// best-effort fan-out, and every event appended there costs every
	// subscribed plugin a callback.
	SkipPluginEvents bool
}

// orSystem returns the origin, normalised so consumers never see an empty
// string.
func (e Event) orSystem() Origin {
	if e.Origin == "" {
		return OriginSystem
	}
	return e.Origin
}

// streamFields renders the event as a Valkey stream entry: "type" and
// "payload" exactly as Publisher.Append has always written them, plus the
// routing and attribution fields as siblings.
//
// Siblings rather than fields nested inside the payload, deliberately: the
// payload keeps whatever shape its producer gave it, so a consumer that
// decodes "payload" into its own struct is unaffected by any of this. That is
// what lets the task listeners on StreamActivities keep decoding the same
// task payload they always have.
func (e Event) streamFields() (map[string]any, bool) {
	body, err := json.Marshal(e.Payload)
	if err != nil {
		return nil, false
	}
	fields := map[string]any{
		"type":        e.Topic,
		"payload":     string(body),
		"project_id":  e.ProjectID.String(),
		"origin":      string(e.orSystem()),
		"entity_type": string(e.EntityType),
	}
	if e.EntityID != nil {
		fields["entity_id"] = e.EntityID.String()
	}
	if e.TaskID != nil {
		fields["task_id"] = e.TaskID.String()
	}
	if e.ActorID != nil {
		fields["actor_id"] = e.ActorID.String()
	}
	if e.ActorAgentID != nil {
		fields["actor_agent_id"] = e.ActorAgentID.String()
	}
	return fields, true
}

// Fanout is the single place an event enters the outside world. Every producer
// — the task and doc activity services, the sprint and view services, the
// automation engine, the annotation flow — calls this, and nothing else
// publishes directly.
//
// # Listeners are peers, not stages
//
// Fanout copies the event to each destination below. It does not call a
// listener, wait on one, or hand one listener's output to the next: every
// destination has exactly one independent reader group, and those readers
// never know about each other. The activity log and realtime both learn about
// a task update by reading the same event, in parallel, and neither is
// downstream of the other. Nothing here may be reordered into a pipeline.
//
// The destinations, each with the reader that owns it — one group per
// listener, so adding a listener means adding a group, not editing a caller:
//
//   - StreamActivities, read by four unrelated groups: api.activity_writer
//     (persists every entry, whatever the entity, to the activities table),
//     api.task_autofill, api.task_auto_assign, and api.automation_engine
//     (matches trigger nodes). The last three act on task entries only.
//   - StreamPluginEvents, read by api.plugin_dispatcher, unless the event opts
//     out.
//   - ChannelRealtime, read by services/realtime, which pushes to connected
//     sockets. Pub/Sub rather than a stream deliberately: realtime runs a
//     single replica in every shipped deployment, so fan-out-to-all is already
//     the correct delivery, and its traffic includes high-frequency ephemeral
//     events from agent-runner (per-token, per-tool-call). Making those
//     durable would persist events that are worthless a second later — and
//     nothing in this codebase trims a stream.
//
// Valkey is therefore the only fan-out point in the system. To add a listener,
// give it its own consumer group on StreamActivities; do not add a call to it
// here.
//
// Every event is persisted, including those whose write path already inserted
// the row itself (comments, which must return it synchronously): the writer
// keys the row on the payload's own "id", so the stream copy is a no-op.
//
// Errors are swallowed deliberately: a messaging failure must not fail the
// HTTP request that caused the event, and no caller can meaningfully recover
// from one. A nil publisher is a no-op, which is how tests and any deployment
// without Valkey behave.
func Fanout(ctx context.Context, publisher *messaging.Publisher, e Event) {
	if publisher == nil {
		return
	}
	FanoutTo(ctx, publisher, e)
}

// Sink is what Fanout writes to. *messaging.Publisher is the real one; the
// interface exists for callers that hold the publisher behind their own
// interface (the plugin runtime).
type Sink interface {
	Publish(ctx context.Context, channel string, payload any) error
	AppendFlat(ctx context.Context, stream string, fields map[string]any) error
}

// FanoutTo is Fanout for a caller holding a Sink. The sink must be non-nil.
func FanoutTo(ctx context.Context, publisher Sink, e Event) {
	e = withContextActor(ctx, e)

	fields, ok := e.streamFields()
	if !ok {
		// The payload could not be marshalled — nothing downstream could use
		// it either, so drop the whole event rather than publish a partial
		// one. Marshalling a caller-constructed payload should not fail; this
		// is a programming error, not a runtime condition.
		return
	}

	appendFields(ctx, publisher, StreamActivities, fields)
	if !e.SkipPluginEvents {
		appendFields(ctx, publisher, StreamPluginEvents, fields)
	}

	_ = publisher.Publish(ctx, ChannelRealtime, map[string]any{
		"type":    e.Topic,
		"payload": e.Payload,
	})
}

// appendFields writes one stream entry, copying the map because AppendFlat
// hands it to the Redis client and callers reuse the same map across several
// destinations.
func appendFields(ctx context.Context, publisher Sink, stream string, fields map[string]any) {
	entry := make(map[string]any, len(fields))
	maps.Copy(entry, fields)
	_ = publisher.AppendFlat(ctx, stream, entry)
}
