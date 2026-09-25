// Package activitysvc is the one place activity is recorded and read.
//
// Every domain service records what happened through a Recorder, which hands
// the entry to events.Fanout (activity stream → worker.ActivityConsumer →
// activities table, plus realtime and optionally plugins). Service reads the
// log back — the project feed, per-entity timelines — and owns comments, the
// only entries written synchronously and edited afterwards.
package activitysvc

import (
	"context"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

// Entry is one thing that happened, on its way to the activity log.
type Entry struct {
	ProjectID  uuid.UUID
	EntityType events.EntityType
	// EntityID is what the entry is about; uuid.Nil for none.
	EntityID uuid.UUID
	// Topic is the event type, e.g. "sprint.completed". It becomes the
	// stored activity_type and the realtime message type.
	Topic string
	// Payload is the message body realtime clients and stream readers
	// receive; it must be JSON-marshalable.
	Payload any

	// ActorID/ActorAgentID/Origin attribute the entry. Left all unset, they
	// are taken from the request context (see events.WithRequestActor), and
	// an entry made outside a request is a system one.
	ActorID      *uuid.UUID
	ActorAgentID *uuid.UUID
	Origin       events.Origin

	// Plugins also delivers the entry to the plugin event stream. Only the
	// task and doc events plugins subscribe to set it.
	Plugins bool
}

// Recorder records activity entries. The zero value of every implementation
// here is safe to call and records nothing.
type Recorder interface {
	Record(ctx context.Context, e Entry)
}

// Discard is a Recorder that records nothing — the default for a service
// constructed without one.
var Discard Recorder = &FanoutRecorder{}

// FanoutRecorder records through events.Fanout.
type FanoutRecorder struct {
	sink events.Sink
}

// NewRecorder returns a Recorder publishing through p. A nil p (tests, or a
// deployment without Valkey) returns a Recorder that records nothing.
func NewRecorder(p *messaging.Publisher) *FanoutRecorder {
	if p == nil {
		return &FanoutRecorder{}
	}
	return &FanoutRecorder{sink: p}
}

// NewRecorderTo is NewRecorder for a caller holding the publisher behind its
// own interface. A nil sink records nothing.
func NewRecorderTo(sink events.Sink) *FanoutRecorder {
	return &FanoutRecorder{sink: sink}
}

// Record fans e out. Errors are swallowed inside Fanout: a messaging failure
// must never fail the request that caused the entry.
func (r *FanoutRecorder) Record(ctx context.Context, e Entry) {
	if r == nil || r.sink == nil {
		return
	}
	ev := events.Event{
		Topic:            e.Topic,
		Payload:          e.Payload,
		ProjectID:        e.ProjectID,
		EntityType:       e.EntityType,
		ActorID:          e.ActorID,
		ActorAgentID:     e.ActorAgentID,
		Origin:           e.Origin,
		SkipPluginEvents: !e.Plugins,
	}
	if e.EntityID != uuid.Nil {
		id := e.EntityID
		ev.EntityID = &id
		if e.EntityType == events.EntityTask {
			ev.TaskID = &id
		}
	}
	events.FanoutTo(ctx, r.sink, ev)
}
