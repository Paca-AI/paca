package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/events"
)

// fakeDeliveryRepo hands out a fixed set of due deliveries and records the
// outcomes the dispatcher returned for them.
type fakeDeliveryRepo struct {
	due      []notificationdom.PendingDelivery
	outcomes []notificationdom.DeliveryOutcome
	prunes   int
}

func (r *fakeDeliveryRepo) ClaimDue(_ context.Context, _ notificationdom.DeliveryChannel, _ time.Time, _ int, deliver func([]notificationdom.PendingDelivery) []notificationdom.DeliveryOutcome) (int, error) {
	r.outcomes = append(r.outcomes, deliver(r.due)...)
	return len(r.due), nil
}

func (r *fakeDeliveryRepo) Prune(context.Context, time.Time, time.Time) (int64, error) {
	r.prunes++
	return 0, nil
}

type recordingPublisher struct {
	err       error
	published []any
}

func (p *recordingPublisher) Publish(_ context.Context, channel string, payload any) error {
	if channel != events.ChannelRealtime {
		return errors.New("wrong channel " + channel)
	}
	if p.err != nil {
		return p.err
	}
	p.published = append(p.published, payload)
	return nil
}

func dueDelivery(attempts int, createdAt time.Time) notificationdom.PendingDelivery {
	return notificationdom.PendingDelivery{
		ID:       uuid.New(),
		Channel:  notificationdom.DeliveryChannelRealtime,
		Attempts: attempts,
		Notification: notificationdom.Notification{
			ID: uuid.New(), RecipientAccountID: uuid.New(), Type: notificationdom.NotificationTypeAssigned,
			ProjectID: uuid.New(), CreatedAt: createdAt,
		},
	}
}

func newTestDispatcher(repo *fakeDeliveryRepo, pub *recordingPublisher, now time.Time) *NotificationDeliveryDispatcher {
	d := NewNotificationDeliveryDispatcher(repo, pub, discardLogger())
	d.now = func() time.Time { return now }
	return d
}

func TestDispatcher_PublishesDueDeliveryAndMarksItSent(t *testing.T) {
	now := time.Now()
	d := dueDelivery(0, now.Add(-time.Minute))
	repo := &fakeDeliveryRepo{due: []notificationdom.PendingDelivery{d}}
	pub := &recordingPublisher{}

	n, err := newTestDispatcher(repo, pub, now).Tick(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("Tick = %d, %v; want 1, nil", n, err)
	}
	if len(pub.published) != 1 {
		t.Fatalf("expected one realtime publish, got %d", len(pub.published))
	}
	msg := pub.published[0].(map[string]any)
	if msg["type"] != events.TopicNotificationCreated {
		t.Fatalf("published type = %v, want %s", msg["type"], events.TopicNotificationCreated)
	}
	payload := msg["payload"].(map[string]any)
	if payload["id"] != d.Notification.ID || payload["recipient_account_id"] != d.Notification.RecipientAccountID {
		t.Fatalf("payload does not identify the notification and its recipient: %v", payload)
	}
	if len(repo.outcomes) != 1 || repo.outcomes[0].ID != d.ID || repo.outcomes[0].Err != nil {
		t.Fatalf("expected a success outcome for the delivery, got %+v", repo.outcomes)
	}
}

func TestDispatcher_RetriesWithBackoffOnPublishFailure(t *testing.T) {
	now := time.Now()
	repo := &fakeDeliveryRepo{due: []notificationdom.PendingDelivery{dueDelivery(0, now.Add(-time.Minute))}}
	pub := &recordingPublisher{err: errors.New("valkey down")}

	if _, err := newTestDispatcher(repo, pub, now).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	o := repo.outcomes[0]
	if o.Err == nil || o.Failed {
		t.Fatalf("a first failure must be retried, got %+v", o)
	}
	if want := now.Add(deliveryBackoffBase); !o.NextAttemptAt.Equal(want) {
		t.Fatalf("NextAttemptAt = %v, want %v", o.NextAttemptAt, want)
	}
}

func TestDispatcher_GivesUpAfterMaxAttempts(t *testing.T) {
	now := time.Now()
	repo := &fakeDeliveryRepo{due: []notificationdom.PendingDelivery{dueDelivery(deliveryMaxAttempts-1, now.Add(-time.Minute))}}
	pub := &recordingPublisher{err: errors.New("valkey down")}

	if _, err := newTestDispatcher(repo, pub, now).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if o := repo.outcomes[0]; !o.Failed || o.Err == nil {
		t.Fatalf("the last allowed attempt failing must end the delivery, got %+v", o)
	}
}

func TestDispatcher_ExpiresStaleNotificationWithoutPublishing(t *testing.T) {
	now := time.Now()
	repo := &fakeDeliveryRepo{due: []notificationdom.PendingDelivery{dueDelivery(0, now.Add(-deliveryMaxAge-time.Minute))}}
	pub := &recordingPublisher{}

	if _, err := newTestDispatcher(repo, pub, now).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(pub.published) != 0 {
		t.Fatal("a notification past deliveryMaxAge must not be announced")
	}
	if o := repo.outcomes[0]; !o.Failed || !errors.Is(o.Err, errDeliveryExpired) {
		t.Fatalf("expected an expired, failed outcome, got %+v", o)
	}
}

func TestDispatcher_PrunesAtMostOncePerGap(t *testing.T) {
	now := time.Now()
	repo := &fakeDeliveryRepo{}
	d := newTestDispatcher(repo, &recordingPublisher{}, now)

	_, _ = d.Tick(context.Background())
	_, _ = d.Tick(context.Background())
	if repo.prunes != 1 {
		t.Fatalf("expected one prune within the gap, got %d", repo.prunes)
	}
	d.now = func() time.Time { return now.Add(deliveryPruneGap) }
	_, _ = d.Tick(context.Background())
	if repo.prunes != 2 {
		t.Fatalf("expected a second prune after the gap, got %d", repo.prunes)
	}
}

func TestDeliveryBackoff_DoublesUpToCap(t *testing.T) {
	cases := map[int]time.Duration{
		1: deliveryBackoffBase,
		2: 2 * deliveryBackoffBase,
		3: 4 * deliveryBackoffBase,
		9: deliveryBackoffCap,
	}
	for attempts, want := range cases {
		if got := deliveryBackoff(attempts); got != want {
			t.Errorf("deliveryBackoff(%d) = %v, want %v", attempts, got, want)
		}
	}
}

func TestDispatcher_SummaryCountsAndResetsEachWindow(t *testing.T) {
	now := time.Now()
	d := newTestDispatcher(&fakeDeliveryRepo{}, &recordingPublisher{}, now)
	d.sent, d.retried, d.failed, d.expired = 5, 2, 1, 0

	if _, ok := d.takeSummary(now); ok {
		t.Fatal("the first call only opens the window")
	}
	if _, ok := d.takeSummary(now.Add(deliverySummaryEvery - time.Second)); ok {
		t.Fatal("a summary must wait for the whole window")
	}
	s, ok := d.takeSummary(now.Add(deliverySummaryEvery))
	if !ok || s != (deliverySummary{sent: 5, retried: 2, failed: 1}) {
		t.Fatalf("summary = %+v ok=%v", s, ok)
	}
	if _, ok := d.takeSummary(now.Add(2 * deliverySummaryEvery)); ok {
		t.Fatal("a quiet window must produce no summary, and the counters must have reset")
	}
}

func TestDispatcher_CountsWhatItDid(t *testing.T) {
	now := time.Now()
	repo := &fakeDeliveryRepo{due: []notificationdom.PendingDelivery{
		dueDelivery(0, now.Add(-time.Minute)),                     // sent
		dueDelivery(0, now.Add(-deliveryMaxAge-time.Minute)),      // expired
		dueDelivery(deliveryMaxAttempts-1, now.Add(-time.Minute)), // failed for good
	}}
	pub := &recordingPublisher{}
	d := newTestDispatcher(repo, pub, now)
	if _, err := d.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The first and third publish; make the third fail by flipping the error.
	if d.sent+d.expired+d.failed+d.retried != 3 {
		t.Fatalf("counters sent=%d expired=%d failed=%d retried=%d; every delivery must be counted once", d.sent, d.expired, d.failed, d.retried)
	}
	if d.expired != 1 {
		t.Fatalf("expired = %d, want 1", d.expired)
	}
}
