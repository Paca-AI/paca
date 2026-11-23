package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/events"
)

const (
	deliveryDispatchInterval = 5 * time.Second
	deliveryDispatchBatch    = 100

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
// Safe to run on every API replica: deliveries are claimed with
// FOR UPDATE SKIP LOCKED.
type NotificationDeliveryDispatcher struct {
	repo notificationdom.DeliveryRepository
	log  *slog.Logger

	// channel is the delivery channel this dispatcher serves and send does one
	// attempt on it. prunes marks the one dispatcher that also prunes finished
	// deliveries of every channel.
	channel notificationdom.DeliveryChannel
	send    func(ctx context.Context, n *notificationdom.Notification) error
	prunes  bool

	interval  time.Duration
	batch     int
	now       func() time.Time
	lastPrune time.Time
	stopCh    chan struct{}
	doneCh    chan struct{}

	// Counters since the last summary line (only the dispatcher's goroutine
	// touches them).
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
	return d
}

func newDeliveryDispatcher(repo notificationdom.DeliveryRepository, log *slog.Logger, channel notificationdom.DeliveryChannel) *NotificationDeliveryDispatcher {
	return &NotificationDeliveryDispatcher{
		repo: repo, log: log, channel: channel,
		interval: deliveryDispatchInterval, batch: deliveryDispatchBatch, now: time.Now,
		stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
}

// Start polls in a background goroutine; Stop ends it.
func (d *NotificationDeliveryDispatcher) Start(ctx context.Context) {
	go d.run(ctx)
}

// Stop ends the poll loop and waits for it.
func (d *NotificationDeliveryDispatcher) Stop() {
	close(d.stopCh)
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

// Tick claims one batch of due realtime deliveries and attempts each. It
// returns how many it claimed.
func (d *NotificationDeliveryDispatcher) Tick(ctx context.Context) (int, error) {
	if d.prunes {
		d.prune(ctx)
	}
	now := d.now()
	defer d.logSummary(now)
	return d.repo.ClaimDue(ctx, d.channel, now, d.batch,
		func(pending []notificationdom.PendingDelivery) []notificationdom.DeliveryOutcome {
			outcomes := make([]notificationdom.DeliveryOutcome, 0, len(pending))
			for _, p := range pending {
				outcomes = append(outcomes, d.deliver(ctx, p, now))
			}
			return outcomes
		})
}

func (d *NotificationDeliveryDispatcher) deliver(ctx context.Context, p notificationdom.PendingDelivery, now time.Time) notificationdom.DeliveryOutcome {
	n := p.Notification
	if now.Sub(n.CreatedAt) > deliveryMaxAge {
		d.log.Info("notification delivery dispatcher: expired", "channel", d.channel, "notification_id", n.ID, "age", now.Sub(n.CreatedAt).Round(time.Second))
		d.expired++
		return notificationdom.DeliveryOutcome{ID: p.ID, Err: errDeliveryExpired, Failed: true}
	}

	err := d.send(ctx, &n)
	if err == nil {
		d.sent++
		return notificationdom.DeliveryOutcome{ID: p.ID}
	}

	attempts := p.Attempts + 1
	if attempts >= deliveryMaxAttempts {
		d.failed++
		d.log.Error("notification delivery dispatcher: giving up", "channel", d.channel, "notification_id", n.ID, "attempts", attempts, "err", err)
		return notificationdom.DeliveryOutcome{ID: p.ID, Err: err, Failed: true}
	}
	d.retried++
	d.log.Warn("notification delivery dispatcher: send failed; will retry", "channel", d.channel, "notification_id", n.ID, "attempts", attempts, "err", err)
	return notificationdom.DeliveryOutcome{ID: p.ID, Err: err, NextAttemptAt: now.Add(deliveryBackoff(attempts))}
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
