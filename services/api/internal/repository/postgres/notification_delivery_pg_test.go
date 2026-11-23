package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

type deliveryFx struct {
	db      *sqlx.DB
	repo    *NotificationRepository
	project uuid.UUID
	user    uuid.UUID
}

func newDeliveryFx(t *testing.T) *deliveryFx {
	t.Helper()
	db := newIAMPGTestDB(t)
	fx := newIAMFx(t, db)
	return &deliveryFx{db: db, repo: NewNotificationRepository(db), project: fx.project(), user: fx.user(false)}
}

func (f *deliveryFx) notify(t *testing.T) *notificationdom.Notification {
	t.Helper()
	n := &notificationdom.Notification{
		ID: uuid.New(), RecipientAccountID: f.user, Type: notificationdom.NotificationTypeAutomation,
		ProjectID: f.project, Message: "hi", CreatedAt: time.Now(),
	}
	if err := f.repo.Create(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	return n
}

// makeDue pulls the notification's delivery forward so LeaseDue sees it.
func (f *deliveryFx) makeDue(t *testing.T, id uuid.UUID) {
	t.Helper()
	pgExec(t, f.db, `UPDATE notification_deliveries SET next_attempt_at = NOW() - interval '1 second' WHERE notification_id = $1`, id)
}

type deliveryRow struct {
	Status    string  `db:"status"`
	Attempts  int     `db:"attempts"`
	LastError *string `db:"last_error"`
}

func (f *deliveryFx) delivery(t *testing.T, id uuid.UUID) deliveryRow {
	t.Helper()
	var d deliveryRow
	if err := f.db.GetContext(context.Background(), &d, `SELECT status, attempts, last_error FROM notification_deliveries WHERE notification_id = $1 AND channel = 'realtime'`, id); err != nil {
		t.Fatal(err)
	}
	return d
}

func (f *deliveryFx) lease(t *testing.T, channel notificationdom.DeliveryChannel, now time.Time, limit int) []notificationdom.PendingDelivery {
	t.Helper()
	got, err := f.repo.LeaseDue(context.Background(), channel, now, limit, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCreateNotification_AlsoCreatesPendingRealtimeDelivery(t *testing.T) {
	f := newDeliveryFx(t)
	n := f.notify(t)

	d := f.delivery(t, n.ID)
	if d.Status != "pending" || d.Attempts != 0 {
		t.Fatalf("new delivery = %+v, want pending with 0 attempts", d)
	}
	// Not due yet: the creating request's own publish gets first go.
	if got := f.lease(t, notificationdom.DeliveryChannelRealtime, time.Now(), 10); len(got) != 0 {
		t.Fatalf("LeaseDue before the grace = %d deliveries; want 0", len(got))
	}
}

func TestMarkDelivered_PreventsRedelivery(t *testing.T) {
	f := newDeliveryFx(t)
	n := f.notify(t)
	f.makeDue(t, n.ID)

	if err := f.repo.MarkDelivered(context.Background(), n.ID, notificationdom.DeliveryChannelRealtime); err != nil {
		t.Fatal(err)
	}
	if d := f.delivery(t, n.ID); d.Status != "sent" {
		t.Fatalf("status = %q, want sent", d.Status)
	}
	if got := f.lease(t, notificationdom.DeliveryChannelRealtime, time.Now(), 10); len(got) != 0 {
		t.Fatalf("a sent delivery must not be leased again, got %d", len(got))
	}
}

func TestRecordOutcomes_AppliesSuccessRetryAndFailure(t *testing.T) {
	f := newDeliveryFx(t)
	ctx := context.Background()
	ok, retry, dead := f.notify(t), f.notify(t), f.notify(t)
	for _, n := range []*notificationdom.Notification{ok, retry, dead} {
		f.makeDue(t, n.ID)
	}
	retryAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Microsecond)

	leased := f.lease(t, notificationdom.DeliveryChannelRealtime, time.Now(), 10)
	if len(leased) != 3 {
		t.Fatalf("leased %d deliveries, want 3", len(leased))
	}
	var outcomes []notificationdom.DeliveryOutcome
	for _, d := range leased {
		switch d.Notification.ID {
		case ok.ID:
			outcomes = append(outcomes, notificationdom.DeliveryOutcome{ID: d.ID})
		case retry.ID:
			outcomes = append(outcomes, notificationdom.DeliveryOutcome{ID: d.ID, Err: errors.New("valkey down"), NextAttemptAt: retryAt})
		case dead.ID:
			outcomes = append(outcomes, notificationdom.DeliveryOutcome{ID: d.ID, Err: errors.New("expired"), Failed: true, NextAttemptAt: time.Now()})
		}
	}
	if err := f.repo.RecordOutcomes(ctx, outcomes); err != nil {
		t.Fatal(err)
	}

	if d := f.delivery(t, ok.ID); d.Status != "sent" || d.Attempts != 1 {
		t.Fatalf("success outcome stored as %+v", d)
	}
	if d := f.delivery(t, retry.ID); d.Status != "pending" || d.Attempts != 1 || d.LastError == nil || *d.LastError != "valkey down" {
		t.Fatalf("retry outcome stored as %+v", d)
	}
	if d := f.delivery(t, dead.ID); d.Status != "failed" || d.Attempts != 1 {
		t.Fatalf("failure outcome stored as %+v", d)
	}
	// The retried one is not due again until its backoff passes.
	if again := f.lease(t, notificationdom.DeliveryChannelRealtime, time.Now(), 10); len(again) != 0 {
		t.Fatalf("nothing should be due, leased %d", len(again))
	}
	if err := f.repo.RecordOutcomes(ctx, nil); err != nil {
		t.Fatalf("recording nothing must be a no-op, got %v", err)
	}
}

func TestLeaseDue_HidesLeasedRowsFromOtherReplicasWithoutHoldingALock(t *testing.T) {
	f := newDeliveryFx(t)
	const total = 6
	for i := 0; i < total; i++ {
		f.makeDue(t, f.notify(t).ID)
	}
	ctx := context.Background()

	first, err := f.repo.LeaseDue(ctx, notificationdom.DeliveryChannelRealtime, time.Now(), 3, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// The first replica is still "sending". Its transaction is already over, so
	// no lock stands in the second one's way and no connection is pinned; the
	// lease alone keeps the two apart.
	var pinned int
	if err := f.db.GetContext(ctx, &pinned, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND state = 'idle in transaction'`); err != nil || pinned != 0 {
		t.Fatalf("idle-in-transaction connections = %d (%v); a lease must not hold a transaction open while sending", pinned, err)
	}
	second, err := f.repo.LeaseDue(ctx, notificationdom.DeliveryChannelRealtime, time.Now(), 10, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if len(first) != 3 || len(second) != total-3 {
		t.Fatalf("leases = %d and %d, want 3 and %d", len(first), len(second), total-3)
	}
	seen := map[uuid.UUID]bool{}
	for _, d := range append(first, second...) {
		if seen[d.ID] {
			t.Fatalf("delivery %s was leased twice", d.ID)
		}
		seen[d.ID] = true
	}
}

func TestLeaseDue_AnExpiredLeaseMakesTheDeliveryDueAgain(t *testing.T) {
	f := newDeliveryFx(t)
	n := f.notify(t)
	f.makeDue(t, n.ID)
	ctx := context.Background()

	if got, err := f.repo.LeaseDue(ctx, notificationdom.DeliveryChannelRealtime, time.Now(), 10, time.Minute); err != nil || len(got) != 1 {
		t.Fatalf("first lease = %d, %v; want 1", len(got), err)
	}
	if got := f.lease(t, notificationdom.DeliveryChannelRealtime, time.Now(), 10); len(got) != 0 {
		t.Fatalf("a leased delivery must not be handed out again while the lease holds, got %d", len(got))
	}
	// The sender died: the lease runs out and another replica takes over, with
	// no attempt spent.
	got := f.lease(t, notificationdom.DeliveryChannelRealtime, time.Now().Add(2*time.Minute), 10)
	if len(got) != 1 || got[0].Attempts != 0 {
		t.Fatalf("after the lease = %+v; want the same delivery with 0 attempts", got)
	}
}

func TestPruneDeliveries_KeepsPendingAndRecentRows(t *testing.T) {
	f := newDeliveryFx(t)
	ctx := context.Background()
	oldSent, oldFailed, recentSent, pending := f.notify(t), f.notify(t), f.notify(t), f.notify(t)
	set := func(id uuid.UUID, status string, age time.Duration) {
		pgExec(t, f.db, `UPDATE notification_deliveries SET status = $2, updated_at = $3 WHERE notification_id = $1`, id, status, time.Now().Add(-age))
	}
	set(oldSent.ID, "sent", 10*24*time.Hour)
	set(oldFailed.ID, "failed", 60*24*time.Hour)
	set(recentSent.ID, "sent", time.Hour)
	set(pending.ID, "pending", 90*24*time.Hour)

	n, err := f.repo.Prune(ctx, time.Now().Add(-3*24*time.Hour), time.Now().Add(-30*24*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("Prune = %d, %v; want 2, nil", n, err)
	}
	var left int
	if err := f.db.GetContext(ctx, &left, `SELECT COUNT(*) FROM notification_deliveries`); err != nil || left != 2 {
		t.Fatalf("rows left = %d, %v; want the recent sent and the pending one", left, err)
	}
}

func TestDeletingNotificationCascadesToDeliveries(t *testing.T) {
	f := newDeliveryFx(t)
	n := f.notify(t)
	pgExec(t, f.db, `DELETE FROM notifications WHERE id = $1`, n.ID)
	var left int
	if err := f.db.GetContext(context.Background(), &left, `SELECT COUNT(*) FROM notification_deliveries WHERE notification_id = $1`, n.ID); err != nil || left != 0 {
		t.Fatalf("deliveries left = %d, %v; want 0", left, err)
	}
}

func TestDeliveryStats_CountsBacklogFailuresAndLatency(t *testing.T) {
	f := newDeliveryFx(t)
	ctx := context.Background()
	now := time.Now()

	mk := func() uuid.UUID { return f.notify(t).ID }
	setDelivery := func(id uuid.UUID, set string, args ...any) {
		pgExec(t, f.db, `UPDATE notification_deliveries SET `+set+` WHERE notification_id = $1 AND channel = 'realtime'`, append([]any{id}, args...)...)
	}

	// Two sent in the last hour, one with a known latency of 4s.
	fast, slow := mk(), mk()
	pgExec(t, f.db, `UPDATE notifications SET created_at = $2 WHERE id = $1`, fast, now.Add(-10*time.Second))
	pgExec(t, f.db, `UPDATE notifications SET created_at = $2 WHERE id = $1`, slow, now.Add(-30*time.Second))
	setDelivery(fast, `status='sent', sent_at=$2, updated_at=$2`, now.Add(-6*time.Second))
	setDelivery(slow, `status='sent', sent_at=$2, updated_at=$2`, now.Add(-10*time.Second))
	// One sent two days ago: counted in the total, not in the last hour or latency.
	old := mk()
	setDelivery(old, `status='sent', sent_at=$2, updated_at=$2`, now.Add(-48*time.Hour))
	// Two failures in the last hour for the same reason, one for another.
	for i := 0; i < 2; i++ {
		setDelivery(mk(), `status='failed', last_error='valkey down', updated_at=$2`, now.Add(-5*time.Minute))
	}
	setDelivery(mk(), `status='failed', last_error='timeout', updated_at=$2`, now.Add(-5*time.Minute))
	// A retrying delivery that is not yet due, and an overdue one.
	setDelivery(mk(), `attempts=2, next_attempt_at=$2`, now.Add(time.Hour))
	overdue := mk()
	setDelivery(overdue, `next_attempt_at=$2`, now.Add(-9*time.Minute))
	// A delivery only a few seconds late is not overdue.
	setDelivery(mk(), `next_attempt_at=$2`, now.Add(-20*time.Second))

	stats, err := f.repo.DeliveryStats(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].Channel != notificationdom.DeliveryChannelRealtime {
		t.Fatalf("stats = %+v; want one realtime entry", stats)
	}
	s := stats[0]
	if s.Sent != 3 || s.SentLastHour != 2 {
		t.Errorf("sent = %d (%d in the last hour), want 3 (2)", s.Sent, s.SentLastHour)
	}
	if s.Failed != 3 || s.FailedLastHour != 3 {
		t.Errorf("failed = %d (%d in the last hour), want 3 (3)", s.Failed, s.FailedLastHour)
	}
	// pending: retrying + overdue + not-overdue-yet; the other creations above
	// (fast/slow/old/failed) are not pending.
	if s.Pending != 3 || s.Retrying != 1 {
		t.Errorf("pending = %d retrying = %d, want 3 and 1", s.Pending, s.Retrying)
	}
	if s.Overdue != 1 || s.OldestOverdue == nil || now.Sub(*s.OldestOverdue) < 8*time.Minute {
		t.Errorf("overdue = %d oldest = %v, want exactly the 9-minute-late one", s.Overdue, s.OldestOverdue)
	}
	if s.LatencyP50 == nil || s.LatencyP95 == nil || *s.LatencyP50 < 3.9 || *s.LatencyP95 > 20.1 || *s.LatencyP95 < *s.LatencyP50 {
		t.Errorf("latency p50/p95 = %v / %v, want between the two sends' 4s and 20s", s.LatencyP50, s.LatencyP95)
	}
	if len(s.TopErrors) != 2 || s.TopErrors[0].Error != "valkey down" || s.TopErrors[0].Count != 2 {
		t.Errorf("top errors = %+v, want valkey down x2 first", s.TopErrors)
	}
}

func TestDeliveryStats_EmptyIsNotAnError(t *testing.T) {
	f := newDeliveryFx(t)
	stats, err := f.repo.DeliveryStats(context.Background(), time.Now())
	if err != nil || len(stats) != 0 {
		t.Fatalf("stats = %+v, err = %v; no deliveries means no channels", stats, err)
	}
}
