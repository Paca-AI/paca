package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/events"
)

const (
	deliveryDispatchInterval = 5 * time.Second
	deliveryDispatchBatch    = 100
	// deliveryLease is how long a leased delivery is hidden from other replicas
	// while it is being sent. Longer than any one send can take, so a delivery is
	// only picked up twice when its sender died.
	deliveryLease = time.Minute

	// Push is an outbound HTTPS call per subscription (up to ten seconds each),
	// so it leases fewer deliveries at a time, a bit longer, and sends them from a
	// small pool instead of one after another.
	pushDispatchBatch = 25
	pushLease         = 3 * time.Minute
	pushConcurrency   = 8

	// deliveryMaxAttempts bounds retries. With deliveryBackoffBase doubling up
	// to deliveryBackoffCap, eight attempts span roughly ten minutes — long
	// enough to ride out a Valkey restart or failover.
	deliveryMaxAttempts = 8
	deliveryBackoffBase = 15 * time.Second
	deliveryBackoffCap  = 5 * time.Minute

	// deliveryMaxAge is how old a notification may be and still be announced.
	// A realtime push exists to make an open client update now; hours later it
	// would only be a chime out of nowhere, and the client's own catch-up
	// (refetch on reconnect, periodic refetch) already shows the notification.
	deliveryMaxAge = 15 * time.Minute

	// deliverySummaryEvery is how often a dispatcher that did anything logs what
	// it did: the one place to see, in the logs, that deliveries are being
	// retried or given up on.
	deliverySummaryEvery = 5 * time.Minute

	deliverySentRetention   = 3 * 24 * time.Hour
	deliveryFailedRetention = 30 * 24 * time.Hour
	deliveryPruneGap        = time.Hour
)

var errDeliveryExpired = errors.New("notification too old to deliver")

// realtimeDeliveryPublisher is the part of *messaging.Publisher the dispatcher
// uses.
type realtimeDeliveryPublisher interface {
	Publish(ctx context.Context, channel string, payload any) error
}

// NotificationDeliveryDispatcher retries realtime announcements that did not
// go out. Creating a notification also records a pending realtime delivery in
// the same transaction; the creating request publishes right away and marks it
// sent, so in the normal case this finds nothing to do. It steps in when that
// did not happen — the publish failed, or the process died between the insert
// and the publish — and re-publishes with exponential backoff until it
// succeeds, runs out of attempts, or the notification is too old to be worth
// announcing.
//
// What it repairs: a notification the API created but never managed to hand to
// Valkey. What it cannot repair: an event that Valkey accepted but that a
// client's socket was not connected to receive (Pub/Sub keeps nothing); the
// web client's refetch-on-reconnect and periodic refetch cover that case, with
// the database as the source of truth.
//
// Delivery is at-least-once, so a notification may be announced twice (for
// example when the fast-path publish succeeded but its mark-sent update was
// lost); the client treats the event as "refetch", so a repeat is harmless.
//
// Safe to run on every API replica: deliveries are leased with
// FOR UPDATE SKIP LOCKED in a short transaction, so no replica holds a lock or
// a database connection while a send is in flight, and a replica that dies
// mid-send only delays its deliveries until their lease runs out.
type NotificationDeliveryDispatcher struct {
	repo notificationdom.DeliveryRepository
	log  *slog.Logger

	// channel is the delivery channel this dispatcher serves and send does one
	// attempt on it. prunes marks the one dispatcher that also prunes finished
	// deliveries of every channel.
	channel notificationdom.DeliveryChannel
	send    func(ctx context.Context, n *notificationdom.Notification) error
	prunes  bool

	interval    time.Duration
	batch       int
	lease       time.Duration
	concurrency int
	now         func() time.Time
	lastPrune   time.Time
	stopCh      chan struct{}
	doneCh      chan struct{}
	cancel      context.CancelFunc

	// Counters since the last summary line (only the dispatcher's goroutine
	// touches them, after a batch's sends have finished).
	sent, retried, failed, expired int
	lastSummary                    time.Time
}

// deliverySummary is one summary window's counters.
type deliverySummary struct{ sent, retried, failed, expired int }

// takeSummary returns the counters since the last summary and resets them, or
// ok=false when the window is still open or nothing happened in it.
func (d *NotificationDeliveryDispatcher) takeSummary(now time.Time) (deliverySummary, bool) {
	if d.lastSummary.IsZero() {
		d.lastSummary = now
	}
	if now.Sub(d.lastSummary) < deliverySummaryEvery {
		return deliverySummary{}, false
	}
	d.lastSummary = now
	s := deliverySummary{d.sent, d.retried, d.failed, d.expired}
	d.sent, d.retried, d.failed, d.expired = 0, 0, 0, 0
	return s, s != (deliverySummary{})
}

// logSummary writes the window's summary, at Warn when anything was given up
// on or expired so it can be alerted on from the logs.
func (d *NotificationDeliveryDispatcher) logSummary(now time.Time) {
	s, ok := d.takeSummary(now)
	if !ok {
		return
	}
	level := slog.LevelInfo
	if s.failed > 0 || s.expired > 0 {
		level = slog.LevelWarn
	}
	d.log.Log(context.Background(), level, "notification delivery dispatcher: summary",
		"channel", d.channel, "window", deliverySummaryEvery.String(),
		"sent", s.sent, "retried", s.retried, "failed", s.failed, "expired", s.expired)
}

// NewNotificationDeliveryDispatcher returns a dispatcher for the realtime
// channel, ready to Start.
func NewNotificationDeliveryDispatcher(repo notificationdom.DeliveryRepository, pub realtimeDeliveryPublisher, log *slog.Logger) *NotificationDeliveryDispatcher {
	d := newDeliveryDispatcher(repo, log, notificationdom.DeliveryChannelRealtime)
	d.prunes = true
	d.send = func(ctx context.Context, n *notificationdom.Notification) error {
		return pub.Publish(ctx, events.ChannelRealtime, map[string]any{
			"type":    events.TopicNotificationCreated,
			"payload": notificationdom.RealtimeCreatedPayload(n),
		})
	}
	return d
}

// PushSender sends one notification as a Web Push to the recipient's browsers.
// It returns an error only for a failure worth retrying.
type PushSender interface {
	Send(ctx context.Context, n *notificationdom.Notification) error
}

// NewNotificationPushDispatcher returns a dispatcher for the push channel. It
// retries and expires exactly like the realtime one; see its doc comment.
func NewNotificationPushDispatcher(repo notificationdom.DeliveryRepository, sender PushSender, log *slog.Logger) *NotificationDeliveryDispatcher {
	d := newDeliveryDispatcher(repo, log, notificationdom.DeliveryChannelPush)
	d.send = sender.Send
	d.batch, d.lease, d.concurrency = pushDispatchBatch, pushLease, pushConcurrency
	return d
}

func newDeliveryDispatcher(repo notificationdom.DeliveryRepository, log *slog.Logger, channel notificationdom.DeliveryChannel) *NotificationDeliveryDispatcher {
	return &NotificationDeliveryDispatcher{
		repo: repo, log: log, channel: channel,
		interval: deliveryDispatchInterval, batch: deliveryDispatchBatch, lease: deliveryLease, concurrency: 1,
		now: time.Now, stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
}

// Start polls in a background goroutine; Stop ends it.
func (d *NotificationDeliveryDispatcher) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)
	go d.run(ctx)
}

// Stop ends the poll loop and waits for it. Sends in flight are cancelled, so a
// slow push service cannot hold up shutdown; their deliveries are left leased
// and are picked up again when the lease runs out.
func (d *NotificationDeliveryDispatcher) Stop() {
	close(d.stopCh)
	if d.cancel != nil {
		d.cancel()
	}
	<-d.doneCh
}

func (d *NotificationDeliveryDispatcher) run(ctx context.Context) {
	defer close(d.doneCh)
	d.log.Info("notification delivery dispatcher: started", "channel", d.channel)
	t := time.NewTicker(d.interval)
	defer t.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			// Drain: keep claiming while a full batch comes back.
			for {
				n, err := d.Tick(ctx)
				if err != nil {
					d.log.Warn("notification delivery dispatcher: tick failed", "err", err)
					break
				}
				if n < d.batch {
					break
				}
			}
		}
	}
}

// Tick leases one batch of due deliveries, attempts each (outside any database
// transaction, from a small pool when the channel is slow), and records the
// outcomes. It returns how many it leased.
func (d *NotificationDeliveryDispatcher) Tick(ctx context.Context) (int, error) {
	if d.prunes {
		d.prune(ctx)
	}
	now := d.now()
	defer d.logSummary(now)

	pending, err := d.repo.LeaseDue(ctx, d.channel, now, d.batch, d.lease)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	outcomes := d.deliverAll(ctx, pending, now)
	d.count(outcomes)

	// Recorded even when the dispatcher is stopping: what was sent should not be
	// sent again. A short deadline keeps shutdown prompt.
	recCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := d.repo.RecordOutcomes(recCtx, outcomes); err != nil {
		return len(pending), err
	}
	return len(pending), nil
}

// deliverAll attempts every leased delivery, up to d.concurrency at a time, and
// returns one outcome per delivery that was actually decided. A delivery whose
// send was cut short by shutdown has no outcome: it stays leased and is
// retried when the lease runs out, without costing an attempt.
func (d *NotificationDeliveryDispatcher) deliverAll(ctx context.Context, pending []notificationdom.PendingDelivery, now time.Time) []notificationdom.DeliveryOutcome {
	results := make([]*notificationdom.DeliveryOutcome, len(pending))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(d.concurrency, 1))
	for i, p := range pending {
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, p notificationdom.PendingDelivery) {
			defer wg.Done()
			defer func() { <-sem }()
			if o, ok := d.deliver(ctx, p, now); ok {
				results[i] = &o
			}
		}(i, p)
	}
	wg.Wait()

	outcomes := make([]notificationdom.DeliveryOutcome, 0, len(pending))
	for _, r := range results {
		if r != nil {
			outcomes = append(outcomes, *r)
		}
	}
	return outcomes
}

// count adds a batch's outcomes to the summary counters.
func (d *NotificationDeliveryDispatcher) count(outcomes []notificationdom.DeliveryOutcome) {
	for _, o := range outcomes {
		switch {
		case o.Err == nil:
			d.sent++
		case errors.Is(o.Err, errDeliveryExpired):
			d.expired++
		case o.Failed:
			d.failed++
		default:
			d.retried++
		}
	}
}

// deliver attempts one delivery. ok is false when the attempt was abandoned
// because the dispatcher is shutting down.
func (d *NotificationDeliveryDispatcher) deliver(ctx context.Context, p notificationdom.PendingDelivery, now time.Time) (notificationdom.DeliveryOutcome, bool) {
	n := p.Notification
	if now.Sub(n.CreatedAt) > deliveryMaxAge {
		d.log.Info("notification delivery dispatcher: expired", "channel", d.channel, "notification_id", n.ID, "age", now.Sub(n.CreatedAt).Round(time.Second))
		return notificationdom.DeliveryOutcome{ID: p.ID, Err: errDeliveryExpired, Failed: true}, true
	}

	err := d.send(ctx, &n)
	if err == nil {
		return notificationdom.DeliveryOutcome{ID: p.ID}, true
	}
	if ctx.Err() != nil {
		return notificationdom.DeliveryOutcome{}, false
	}

	attempts := p.Attempts + 1
	if attempts >= deliveryMaxAttempts {
		d.log.Error("notification delivery dispatcher: giving up", "channel", d.channel, "notification_id", n.ID, "attempts", attempts, "err", err)
		return notificationdom.DeliveryOutcome{ID: p.ID, Err: err, Failed: true}, true
	}
	d.log.Warn("notification delivery dispatcher: send failed; will retry", "channel", d.channel, "notification_id", n.ID, "attempts", attempts, "err", err)
	return notificationdom.DeliveryOutcome{ID: p.ID, Err: err, NextAttemptAt: now.Add(deliveryBackoff(attempts))}, true
}

// deliveryBackoff is the wait after the given number of failed attempts.
func deliveryBackoff(attempts int) time.Duration {
	wait := deliveryBackoffBase
	for i := 1; i < attempts && wait < deliveryBackoffCap; i++ {
		wait *= 2
	}
	return min(wait, deliveryBackoffCap)
}

// prune drops finished deliveries past their retention, at most once per
// deliveryPruneGap. Best-effort: failure only means the table is a little
// bigger until the next pass.
func (d *NotificationDeliveryDispatcher) prune(ctx context.Context) {
	now := d.now()
	if now.Sub(d.lastPrune) < deliveryPruneGap {
		return
	}
	d.lastPrune = now
	if n, err := d.repo.Prune(ctx, now.Add(-deliverySentRetention), now.Add(-deliveryFailedRetention)); err != nil {
		d.log.Warn("notification delivery dispatcher: prune failed", "err", err)
	} else if n > 0 {
		d.log.Info("notification delivery dispatcher: pruned finished deliveries", "count", n)
	}
}
