// Package notificationhealthsvc tells an administrator whether notifications
// are getting through: the delivery outbox's backlog and failures, how long
// delivery takes, and what the stream consumers have given up on.
package notificationhealthsvc

import (
	"context"
	"fmt"
	"time"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
)

// Thresholds. The dispatchers tick every few seconds, so a delivery that has
// been due for minutes is stuck, not busy.
const (
	// overdueFailing is how long the oldest overdue delivery may have waited
	// before delivery counts as stalled rather than late.
	overdueFailing = 10 * time.Minute
	// pendingDegraded and pendingFailing are how long a stream entry may sit
	// delivered-but-unacknowledged. The reclaimer redelivers after a minute, so
	// minutes of idleness mean its consumer is gone or stuck.
	pendingDegraded = 5 * time.Minute
	pendingFailing  = 15 * time.Minute
	// slowP95 is a creation-to-delivery 95th percentile worth flagging.
	slowP95 = 60.0 // seconds
)

// Service implements notificationdom.HealthService.
type Service struct {
	deliveries notificationdom.DeliveryStatsRepository
	streams    notificationdom.StreamInspector
	now        func() time.Time
}

var _ notificationdom.HealthService = (*Service)(nil)

// New returns a Service. streams may be nil (no Valkey): stream health is then
// simply not reported.
func New(deliveries notificationdom.DeliveryStatsRepository, streams notificationdom.StreamInspector) *Service {
	return &Service{deliveries: deliveries, streams: streams, now: time.Now}
}

// Health implements notificationdom.HealthService.
func (s *Service) Health(ctx context.Context) (*notificationdom.Health, error) {
	now := s.now()
	channels, err := s.deliveries.DeliveryStats(ctx, now)
	if err != nil {
		return nil, err
	}
	h := &notificationdom.Health{Status: notificationdom.HealthOK, CheckedAt: now, Channels: channels}
	if s.streams != nil {
		if h.Streams, err = s.streams.StreamHealth(ctx, now); err != nil {
			return nil, err
		}
	}

	raise := func(to notificationdom.HealthStatus, reason string) {
		h.Reasons = append(h.Reasons, reason)
		if to == notificationdom.HealthFailing || h.Status == notificationdom.HealthOK {
			h.Status = to
		}
	}

	for _, c := range h.Channels {
		if c.Overdue > 0 {
			var waited time.Duration
			if c.OldestOverdue != nil {
				waited = now.Sub(*c.OldestOverdue).Round(time.Second)
			}
			if waited >= overdueFailing {
				raise(notificationdom.HealthFailing, fmt.Sprintf("%s: %d deliveries are overdue, the oldest by %s; the dispatcher looks stalled", c.Channel, c.Overdue, waited))
			} else {
				raise(notificationdom.HealthDegraded, fmt.Sprintf("%s: %d deliveries are overdue", c.Channel, c.Overdue))
			}
		}
		if c.FailedLastHour > 0 {
			raise(notificationdom.HealthDegraded, fmt.Sprintf("%s: %d deliveries failed in the last hour", c.Channel, c.FailedLastHour))
		}
		if c.LatencyP95 != nil && *c.LatencyP95 > slowP95 {
			raise(notificationdom.HealthDegraded, fmt.Sprintf("%s: the slowest 5%% of deliveries took over %.0fs", c.Channel, *c.LatencyP95))
		}
	}
	for _, st := range h.Streams {
		switch {
		case st.OldestPendingIdle >= pendingFailing:
			raise(notificationdom.HealthFailing, fmt.Sprintf("%s: entries have been unacknowledged for %s; its consumer looks stuck", st.Stream, st.OldestPendingIdle.Round(time.Second)))
		case st.OldestPendingIdle >= pendingDegraded:
			raise(notificationdom.HealthDegraded, fmt.Sprintf("%s: entries have been unacknowledged for %s", st.Stream, st.OldestPendingIdle.Round(time.Second)))
		}
		if st.DeadLastHour > 0 {
			raise(notificationdom.HealthDegraded, fmt.Sprintf("%s: %d entries were given up on in the last hour (see %s.dead)", st.Stream, st.DeadLastHour, st.Stream))
		}
	}
	return h, nil
}
