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

func TestChangeLog_CarriesTheClientOpOfTheRequest(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	var order []string
	sink := &logSink{order: &order}
	ctx := events.WithClientOp(context.Background(), "op-123.abc_XYZ")
	events.FanoutTo(ctx, sink, events.Event{
		Topic: "task.updated", Payload: map[string]any{}, ProjectID: project,
		EntityType: events.EntityTask, EntityID: &task,
	})
	events.PublishRealtime(ctx, sink, events.Event{
		Topic: "view.reordered", Payload: map[string]any{}, ProjectID: project,
		EntityType: events.EntityView,
	})
	for i, c := range sink.changes {
		if c["client_op_id"] != "op-123.abc_XYZ" {
			t.Errorf("change %d client_op_id = %v", i, c["client_op_id"])
		}
	}
	if len(sink.changes) != 2 {
		t.Fatalf("logged %d changes, want 2", len(sink.changes))
	}
}

func TestClientOp_OnlySafeTokensAreKept(t *testing.T) {
	for _, bad := range []string{"", "has space", "semi;colon", "new\nline", strings.Repeat("a", 65), "ünï"} {
		if got := events.ClientOp(events.WithClientOp(context.Background(), bad)); got != "" {
			t.Errorf("%q was kept as %q", bad, got)
		}
	}
	if got := events.ClientOp(events.WithClientOp(context.Background(), "ok-1")); got != "ok-1" {
		t.Errorf("a valid id was dropped: %q", got)
	}
	// An event without a client op, outside a request, carries none.
	var order []string
	sink := &logSink{order: &order}
	events.FanoutTo(context.Background(), sink, events.Event{Topic: "task.created", Payload: map[string]any{}, ProjectID: uuid.New(), EntityType: events.EntityTask})
	if _, has := sink.changes[0]["client_op_id"]; has {
		t.Error("an event with no client op must not carry the field")
	}
}

func TestChangeLog_MarksOnlyWholeEntityChanges(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	var order []string
	sink := &logSink{order: &order}
	for _, topic := range []string{"task.updated", "task.comment.added", "task.link.added", "doc.updated", "sprint.completed"} {
		events.FanoutTo(context.Background(), sink, events.Event{
			Topic: topic, Payload: map[string]any{}, ProjectID: project,
			EntityType: events.EntityTask, EntityID: &task, TaskID: &task,
		})
	}
	want := map[string]bool{"task.updated": true, "doc.updated": true, "sprint.completed": true}
	for _, c := range sink.changes {
		topic := c["type"].(string)
		_, marked := c[messaging.MarkField]
		if marked != want[topic] {
			t.Errorf("%s marked = %v, want %v", topic, marked, want[topic])
		}
	}
}

func TestChangeLog_MarksTheVersionOfTheSnapshot_AndADeleteAsFinal(t *testing.T) {
	project, task := uuid.New(), uuid.New()
	var order []string
	sink := &logSink{order: &order}
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.updated", Payload: map[string]any{}, ProjectID: project,
		EntityType: events.EntityTask, EntityID: &task, TaskID: &task,
		Snapshot: map[string]any{"id": task.String(), "version": 7},
	})
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.updated", Payload: map[string]any{}, ProjectID: project,
		EntityType: events.EntityTask, EntityID: &task, TaskID: &task,
	})
	events.FanoutTo(context.Background(), sink, events.Event{
		Topic: "task.deleted", Payload: map[string]any{}, ProjectID: project,
		EntityType: events.EntityTask, EntityID: &task, TaskID: &task,
	})
	want := []int64{7, messaging.UnknownVersion, messaging.DeletedVersion}
	for i, c := range sink.changes {
		if got := c[messaging.MarkVersionField]; got != want[i] {
			t.Errorf("change %d version = %v, want %d", i, got, want[i])
		}
	}
}
