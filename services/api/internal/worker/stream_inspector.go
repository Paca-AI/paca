package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/events"
)

// notificationStreams are the consumer groups a notification depends on, each
// with the dead-letter stream the reclaimer parks its poison entries on.
var notificationStreams = []struct{ stream, group string }{
	{events.StreamTaskAssignments, notificationConsumerGroup},
	{events.StreamPluginEvents, pluginEventConsumerGroup},
	{events.StreamAgentEvents, questionConsumerGroup},
}

// StreamInspector reports how the streams notifications travel on are doing:
// what is waiting unacknowledged, for how long, and what was given up on.
type StreamInspector struct {
	client *redis.Client
}

var _ notificationdom.StreamInspector = (*StreamInspector)(nil)

// NewStreamInspector returns an inspector over client.
func NewStreamInspector(client *redis.Client) *StreamInspector {
	return &StreamInspector{client: client}
}

// StreamHealth implements notificationdom.StreamInspector. A stream or group
// that does not exist yet is reported as empty, not as an error: a deployment
// that has not seen an event of that kind has neither.
func (i *StreamInspector) StreamHealth(ctx context.Context, now time.Time) ([]notificationdom.StreamHealth, error) {
	out := make([]notificationdom.StreamHealth, 0, len(notificationStreams))
	for _, s := range notificationStreams {
		h := notificationdom.StreamHealth{Stream: s.stream, Group: s.group}

		if p, err := i.client.XPending(ctx, s.stream, s.group).Result(); err == nil {
			h.Pending = p.Count
		}
		if h.Pending > 0 {
			if ext, err := i.client.XPendingExt(ctx, &redis.XPendingExtArgs{
				Stream: s.stream, Group: s.group, Start: "-", End: "+", Count: 50,
			}).Result(); err == nil {
				for _, e := range ext {
					if e.Idle > h.OldestPendingIdle {
						h.OldestPendingIdle = e.Idle
					}
				}
			}
		}

		dead := s.stream + ".dead"
		if n, err := i.client.XLen(ctx, dead).Result(); err == nil {
			h.DeadTotal = n
		}
		if h.DeadTotal > 0 {
			since := fmt.Sprintf("%d-0", now.Add(-time.Hour).UnixMilli())
			if recent, err := i.client.XRange(ctx, dead, since, "+").Result(); err == nil {
				h.DeadLastHour = int64(len(recent))
			}
		}
		out = append(out, h)
	}
	return out, nil
}
