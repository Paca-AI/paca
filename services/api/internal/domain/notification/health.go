package notificationdom

import (
	"context"
	"time"
)

// ErrorCount is how many failed deliveries share a reason.
type ErrorCount struct {
	Error string
	Count int64
}

// ChannelStats is the delivery outbox as it stands for one channel.
type ChannelStats struct {
	Channel DeliveryChannel
	// Totals by status across what has not been pruned yet.
	Pending, Sent, Failed int64
	// Retrying is the pending deliveries that already failed at least once.
	Retrying int64
	// Overdue counts pending deliveries that were due more than a couple of
	// minutes ago: the dispatcher ticks every few seconds, so these are stuck or
	// the dispatcher is down. OldestOverdue is when the oldest became due.
	Overdue       int64
	OldestOverdue *time.Time
	// The last hour.
	SentLastHour, FailedLastHour int64
	// Seconds from a notification's creation to its delivery being sent, over
	// the last hour; nil when nothing was sent.
	LatencyP50, LatencyP95 *float64
	// TopErrors are the commonest reasons deliveries failed in the last hour.
	TopErrors []ErrorCount
}

// DeliveryStatsRepository reads the delivery outbox's health.
type DeliveryStatsRepository interface {
	// DeliveryStats returns one entry per channel that has any delivery.
	DeliveryStats(ctx context.Context, now time.Time) ([]ChannelStats, error)
}

// StreamHealth is one consumer group's state on one stream.
type StreamHealth struct {
	Stream string
	Group  string
	// Pending is entries delivered to the group and not yet acknowledged, and
	// OldestPendingIdle how long the longest-waiting one has gone unattended.
	Pending           int64
	OldestPendingIdle time.Duration
	// DeadLastHour and DeadTotal count entries given up on and parked on the
	// stream's dead-letter stream.
	DeadLastHour, DeadTotal int64
}

// StreamInspector reads the health of the streams notifications travel on.
type StreamInspector interface {
	StreamHealth(ctx context.Context, now time.Time) ([]StreamHealth, error)
}

// HealthStatus summarises delivery health.
type HealthStatus string

const (
	// HealthOK means nothing needs attention.
	HealthOK HealthStatus = "ok"
	// HealthDegraded means something is failing or late but deliveries still flow.
	HealthDegraded HealthStatus = "degraded"
	// HealthFailing means delivery looks stalled.
	HealthFailing HealthStatus = "failing"
)

// Health is the notification pipeline's health, for an administrator.
type Health struct {
	Status HealthStatus
	// Reasons say why the status is not ok, in plain words.
	Reasons   []string
	CheckedAt time.Time
	Channels  []ChannelStats
	Streams   []StreamHealth
}

// HealthService reports notification delivery health.
type HealthService interface {
	Health(ctx context.Context) (*Health, error)
}
