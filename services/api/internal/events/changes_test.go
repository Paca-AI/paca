package events_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

// logSink is a Sink that also keeps the change log.
type logSink struct {
	captureSink
	changes []map[string]any
	seq     int64
	fail    bool
	order   *[]string
}

func (l *logSink) AppendChange(_ context.Context, _ string, fields map[string]any) (messaging.ChangeRef, error) {
	if l.fail {
		return messaging.ChangeRef{}, errors.New("down")
	}
	*l.order = append(*l.order, "log")
	l.seq++
	l.changes = append(l.changes, fields)
	return messaging.ChangeRef{Seq: l.seq, Epoch: 99}, nil
}

func (l *logSink) Publish(ctx context.Context, ch string, p any) error {
	*l.order = append(*l.order, "publish")
	return l.captureSink.Publish(ctx, ch, p)
}

func TestFanout_LogsTheChangeBeforeThePubSubMessageAndStampsItsSeq(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	var order []string
	sink := &logSink{order: &order}
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.updated", Payload: map[string]any{"id": task.String()},
		ProjectID: project, EntityType: events.EntityTask, EntityID: &task,
	})

	if len(sink.changes) != 1 {
		t.Fatalf("logged %d changes, want 1", len(sink.changes))
	}
	c := sink.changes[0]
	if c["op"] != "update" || c["type"] != "task.updated" || c["project_id"] != project.String() {
		t.Errorf("entry = %v", c)
	}
	for _, k := range []string{"id", "ts", "authz", "payload", "entity_id"} {
		if c[k] == nil || c[k] == "" {
			t.Errorf("entry has no %q: %v", k, c)
		}
	}
	if len(order) != 2 || order[0] != "log" || order[1] != "publish" {
		t.Errorf("order = %v, want the log before the publish", order)
	}
	msg := sink.realtime[0].(map[string]any)
	if msg["seq"] != int64(1) || msg["epoch"] != int64(99) {
		t.Errorf("message seq/epoch = %v/%v, want 1/99", msg["seq"], msg["epoch"])
	}
}

func TestPublishRealtime_IsLoggedToo(t *testing.T) {
	project := uuid.New()
	var order []string
	sink := &logSink{order: &order}
	events.PublishRealtime(context.Background(), sink, events.Event{
		Topic: "view.reordered", ProjectID: project, EntityType: events.EntityView,
		Payload: map[string]any{},
	})
	if len(sink.changes) != 1 || sink.changes[0]["op"] != "move" {
		t.Errorf("changes = %v", sink.changes)
	}
}

func TestChangeLog_SkipsWhatIsNotAProjectRoomEvent(t *testing.T) {
	project := uuid.New()
	var order []string
	sink := &logSink{order: &order}
	events.PublishRealtime(context.Background(), sink, events.Event{
		Topic: "notification.created", ProjectID: project, Payload: map[string]any{},
	})
	if len(sink.changes) != 0 {
		t.Errorf("a recipient-routed notification must not enter the project log: %v", sink.changes)
	}
	if _, has := sink.realtime[0].(map[string]any)["seq"]; has {
		t.Error("an unlogged message must carry no seq")
	}
}

func TestChangeLog_FailureStillDeliversTheLegacyMessage(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	var order []string
	sink := &logSink{order: &order, fail: true}
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.created", Payload: map[string]any{}, ProjectID: project,
		EntityType: events.EntityTask, EntityID: &task,
	})
	if len(sink.realtime) != 1 {
		t.Fatalf("published %d messages, want 1", len(sink.realtime))
	}
	if _, has := sink.realtime[0].(map[string]any)["seq"]; has {
		t.Error("a message whose change was not logged must carry no seq")
	}
}

func TestChangeLog_SinkWithoutALogIsUnchanged(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	sink := &captureSink{}
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.created", Payload: map[string]any{}, ProjectID: project,
		EntityType: events.EntityTask, EntityID: &task,
	})
	if len(sink.realtime) != 1 {
		t.Fatalf("published %d messages, want 1", len(sink.realtime))
	}
}

func TestChangeLog_CarriesTheSnapshotOnlyInTheChange(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	var order []string
	sink := &logSink{order: &order}
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.updated", Payload: map[string]any{"id": "a"},
		ProjectID: project, EntityType: events.EntityTask, EntityID: &task,
		Snapshot: map[string]any{"id": task.String(), "title": "New"},
	})
	got, _ := sink.changes[0]["entity"].(string)
	if got == "" || !strings.Contains(got, `"title":"New"`) {
		t.Errorf("entity = %q, want the snapshot as JSON", got)
	}
	// The legacy message and the stream payload keep their shape.
	msg := sink.realtime[0].(map[string]any)
	if _, has := msg["entity"]; has {
		t.Error("the Pub/Sub message must not carry the snapshot")
	}
	if strings.Contains(sink.changes[0]["payload"].(string), "New") {
		t.Error("the snapshot must not leak into the payload")
	}
}

func TestChangeLog_DropsAnOversizedSnapshot(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	var order []string
	sink := &logSink{order: &order}
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.updated", Payload: map[string]any{}, ProjectID: project,
		EntityType: events.EntityTask, EntityID: &task,
		Snapshot: map[string]any{"blob": strings.Repeat("x", events.MaxSnapshotBytes+1)},
	})
	if _, has := sink.changes[0]["entity"]; has {
		t.Error("a snapshot over MaxSnapshotBytes must be left out")
	}
}
