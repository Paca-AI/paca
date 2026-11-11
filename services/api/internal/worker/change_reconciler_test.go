package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/domain/changeoutbox"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

type fakeOutbox struct {
	rows    []changeoutbox.Row
	deleted int
}

func (f *fakeOutbox) Claim(_ context.Context, olderThan time.Time, limit int, handle func([]changeoutbox.Row) error) (int, error) {
	var claimed []changeoutbox.Row
	for _, r := range f.rows {
		if r.CreatedAt.Before(olderThan) && len(claimed) < limit {
			claimed = append(claimed, r)
		}
	}
	if len(claimed) == 0 {
		return 0, nil
	}
	if err := handle(claimed); err != nil {
		return 0, err // nothing deleted
	}
	left := f.rows[:0:0]
	for _, r := range f.rows {
		if !r.CreatedAt.Before(olderThan) {
			left = append(left, r)
		}
	}
	f.deleted += len(f.rows) - len(left)
	f.rows = left
	return len(claimed), nil
}

type fakeLog struct {
	last map[string]messaging.Logged
	err  error
}

func (f *fakeLog) LastLogged(_ context.Context, _ string, ids []string) (map[string]messaging.Logged, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]messaging.Logged{}
	for _, id := range ids {
		if v, ok := f.last[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

type sentMessage struct {
	channel string
	msg     map[string]any
}

type fakeSink struct{ msgs []sentMessage }

func (f *fakeSink) Publish(_ context.Context, channel string, payload any) error {
	if m, ok := payload.(map[string]any); ok {
		f.msgs = append(f.msgs, sentMessage{channel, m})
	}
	return nil
}

type reconcilerFixture struct {
	r      *ChangeReconciler
	outbox *fakeOutbox
	log    *fakeLog
	sink   *fakeSink
	now    time.Time
	loads  []uuid.UUID
	state  map[uuid.UUID]ChangeRepair
	loadEr error
}

func newReconcilerFixture() *reconcilerFixture {
	f := &reconcilerFixture{
		outbox: &fakeOutbox{}, log: &fakeLog{last: map[string]messaging.Logged{}}, sink: &fakeSink{},
		now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), state: map[uuid.UUID]ChangeRepair{},
	}
	load := func(_ context.Context, id uuid.UUID) (ChangeRepair, error) {
		f.loads = append(f.loads, id)
		if f.loadEr != nil {
			return ChangeRepair{}, f.loadEr
		}
		return f.state[id], nil
	}
	f.r = NewChangeReconciler(f.outbox, f.log, f.sink, map[string]ChangeLoader{
		changeoutbox.EntityTask: load, changeoutbox.EntitySprint: load, changeoutbox.EntityDoc: load,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.r.now = func() time.Time { return f.now }
	return f
}

func (f *reconcilerFixture) row(kind string, project, id uuid.UUID, age time.Duration) changeoutbox.Row {
	r := changeoutbox.Row{ID: int64(len(f.outbox.rows) + 1), ProjectID: project, EntityType: kind, EntityID: id, Op: "update", CreatedAt: f.now.Add(-age)}
	f.outbox.rows = append(f.outbox.rows, r)
	return r
}

func TestChangeReconciler_LeavesAnAnnouncedChangeAlone(t *testing.T) {
	f := newReconcilerFixture()
	project, id := uuid.New(), uuid.New()
	r := f.row(changeoutbox.EntityTask, project, id, time.Minute)
	// A snapshot logged at the row's version, or with no version but just after it.
	f.log.last[id.String()] = messaging.Logged{AtMs: r.CreatedAt.Add(300 * time.Millisecond).UnixMilli(), Version: messaging.UnknownVersion}

	if n, err := f.r.Tick(context.Background()); err != nil || n != 1 {
		t.Fatalf("Tick = %d, %v", n, err)
	}
	if len(f.sink.msgs) != 0 || len(f.loads) != 0 {
		t.Errorf("an announced change must cost nothing: %d messages, %d loads", len(f.sink.msgs), len(f.loads))
	}
	if f.outbox.deleted != 1 {
		t.Error("the row must be deleted")
	}
}

func TestChangeReconciler_AnnouncesAChangeThatWasNeverAnnounced(t *testing.T) {
	f := newReconcilerFixture()
	project, id := uuid.New(), uuid.New()
	f.row(changeoutbox.EntityTask, project, id, time.Minute)
	f.state[id] = ChangeRepair{Found: true, Snapshot: map[string]any{"id": id.String(), "version": 7}}

	if _, err := f.r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.sink.msgs) != 1 {
		t.Fatalf("announced %d messages, want 1", len(f.sink.msgs))
	}
	msg := f.sink.msgs[0].msg
	if msg["type"] != "task.updated" {
		t.Errorf("type = %v", msg["type"])
	}
	payload := msg["payload"].(map[string]any)
	if payload["task_id"] != id.String() || payload["repair"] != true || payload["project_id"] != project.String() {
		t.Errorf("payload = %v", payload)
	}
}

func TestChangeReconciler_AnnouncesADeletedEntityAsDeleted(t *testing.T) {
	f := newReconcilerFixture()
	project := uuid.New()
	for kind, topic := range map[string]string{
		changeoutbox.EntityTask: "task.deleted", changeoutbox.EntitySprint: "sprint.deleted", changeoutbox.EntityDoc: "doc.deleted",
	} {
		f.sink.msgs = nil
		f.outbox.rows = nil
		id := uuid.New()
		f.row(kind, project, id, time.Minute) // state[id] is the zero value: not found
		if _, err := f.r.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(f.sink.msgs) != 1 || f.sink.msgs[0].msg["type"] != topic {
			t.Errorf("%s: messages = %v, want one %s", kind, f.sink.msgs, topic)
		}
	}
}

func TestChangeReconciler_OnlyAnnouncementsSinceTheChangeCount(t *testing.T) {
	f := newReconcilerFixture()
	project, id := uuid.New(), uuid.New()
	r := f.row(changeoutbox.EntityTask, project, id, time.Minute)
	// Announced a minute before the change: that was an earlier change.
	f.log.last[id.String()] = messaging.Logged{AtMs: r.CreatedAt.Add(-time.Minute).UnixMilli(), Version: messaging.UnknownVersion}
	f.state[id] = ChangeRepair{Found: true}
	if _, err := f.r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.sink.msgs) != 1 {
		t.Errorf("an older announcement must not hide a lost one, announced %d", len(f.sink.msgs))
	}
}

func TestChangeReconciler_OneAnnouncementPerEntityHoweverManyRows(t *testing.T) {
	f := newReconcilerFixture()
	project, id := uuid.New(), uuid.New()
	for i := 0; i < 5; i++ {
		f.row(changeoutbox.EntityTask, project, id, time.Minute+time.Duration(i)*time.Second)
	}
	f.state[id] = ChangeRepair{Found: true}
	if _, err := f.r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.sink.msgs) != 1 || len(f.loads) != 1 {
		t.Errorf("announced %d, loaded %d; want 1 each", len(f.sink.msgs), len(f.loads))
	}
	if f.outbox.deleted != 5 {
		t.Errorf("deleted %d rows, want 5", f.outbox.deleted)
	}
}

func TestChangeReconciler_DoesNotTouchRowsStillWithinTheGracePeriod(t *testing.T) {
	f := newReconcilerFixture()
	f.row(changeoutbox.EntityTask, uuid.New(), uuid.New(), 5*time.Second)
	if n, err := f.r.Tick(context.Background()); err != nil || n != 0 {
		t.Fatalf("Tick = %d, %v; a young row is not claimed", n, err)
	}
	if len(f.outbox.rows) != 1 {
		t.Error("a young row must stay")
	}
}

func TestChangeReconciler_KeepsTheRowsWhenTheLogCannotBeRead(t *testing.T) {
	f := newReconcilerFixture()
	f.row(changeoutbox.EntityTask, uuid.New(), uuid.New(), time.Minute)
	f.log.err = errors.New("valkey down")
	if _, err := f.r.Tick(context.Background()); err == nil {
		t.Fatal("a log read failure must fail the tick so the rows are claimed again")
	}
	if len(f.outbox.rows) != 1 || len(f.sink.msgs) != 0 {
		t.Error("nothing may be deleted or announced when the check could not be made")
	}
}

func TestChangeReconciler_DropsARowItCannotLoadInsteadOfLoopingOnIt(t *testing.T) {
	f := newReconcilerFixture()
	f.row(changeoutbox.EntityTask, uuid.New(), uuid.New(), time.Minute)
	f.loadEr = errors.New("db error")
	if _, err := f.r.Tick(context.Background()); err != nil {
		t.Fatalf("a loader error must not fail the tick: %v", err)
	}
	if len(f.outbox.rows) != 0 || len(f.sink.msgs) != 0 {
		t.Error("the unloadable row is dropped and nothing announced")
	}
}

func TestChangeReconciler_ComparesRowVersionsWhenBothSidesHaveOne(t *testing.T) {
	v := func(n int64) *int64 { return &n }
	cases := []struct {
		name      string
		logged    messaging.Logged
		rowVer    *int64
		announced bool
	}{
		{"the log carried the row's version", messaging.Logged{AtMs: 1, Version: 3}, v(3), true},
		{"the log carried a newer version", messaging.Logged{AtMs: 1, Version: 5}, v(3), true},
		// A change landing right after an announcement of the same entity must
		// not hide behind it, whatever the clocks say.
		{"the row is newer than what was logged, a moment after it", messaging.Logged{AtMs: time.Now().UnixMilli(), Version: 2}, v(3), false},
		{"the log has a delete", messaging.Logged{AtMs: 1, Version: messaging.DeletedVersion}, v(9), true},
		{"never logged", messaging.Logged{}, v(1), false},
	}
	for _, c := range cases {
		row := changeoutbox.Row{Version: c.rowVer, CreatedAt: time.Now()}
		if got := announced(row, c.logged); got != c.announced {
			t.Errorf("%s: announced = %v, want %v", c.name, got, c.announced)
		}
	}
}
