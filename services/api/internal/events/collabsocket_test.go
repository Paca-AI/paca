package events_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/events"
)

func TestCollabSocket_OnlySafeTokensAreKept(t *testing.T) {
	for _, bad := range []string{"", "has space", "semi;colon", "new\nline", strings.Repeat("a", 65), "ünï"} {
		if got := events.CollabSocket(events.WithCollabSocket(context.Background(), bad)); got != "" {
			t.Errorf("%q was kept as %q", bad, got)
		}
	}
	if got := events.CollabSocket(events.WithCollabSocket(context.Background(), "Ab3_-xY9zQ1w2e3r4t5y")); got != "Ab3_-xY9zQ1w2e3r4t5y" {
		t.Errorf("a valid socket id was dropped: %q", got)
	}
}

func TestRealtimeEnvelope_CarriesTheCollabSocketOfTheRequest(t *testing.T) {
	project, doc := uuid.New(), uuid.New()
	var order []string
	sink := &logSink{order: &order}
	ctx := events.WithCollabSocket(context.Background(), "sock-1")
	events.FanoutTo(ctx, sink, events.Event{
		Topic: "doc.updated", Payload: map[string]any{"document_id": doc.String()},
		ProjectID: project, EntityType: events.EntityDoc, EntityID: &doc,
	})
	if len(sink.realtime) != 1 {
		t.Fatalf("published %d messages, want 1", len(sink.realtime))
	}
	msg := sink.realtime[0].(map[string]any)
	if msg["collab_socket"] != "sock-1" {
		t.Errorf("collab_socket = %v, want sock-1", msg["collab_socket"])
	}
	// The socket only matters to live delivery: it is not persisted with the
	// event's payload or in the change log.
	if p, _ := msg["payload"].(map[string]any); p["collab_socket"] != nil {
		t.Error("collab_socket must not be added to the payload")
	}
	for _, c := range sink.changes {
		if _, has := c["collab_socket"]; has {
			t.Error("collab_socket must not enter the change log")
		}
	}
}

func TestRealtimeEnvelope_HasNoCollabSocketOutsideABrowserEditor(t *testing.T) {
	var order []string
	sink := &logSink{order: &order}
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "doc.updated", Payload: map[string]any{}, ProjectID: uuid.New(), EntityType: events.EntityDoc,
	})
	if _, has := sink.realtime[0].(map[string]any)["collab_socket"]; has {
		t.Error("a write without X-Collab-Socket must not carry collab_socket")
	}
}
