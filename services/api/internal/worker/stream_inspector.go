package worker

import (
	"context"
	"fmt"
	"strings"
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

// missingGroup reports whether err only says the stream or its consumer group
// does not exist yet, which is not a failure: a deployment that has not seen an
// event of that kind has neither. Anything else (Valkey down, bad credentials,
// a timeout) is a failure and must not read as "nothing is pending".
func missingGroup(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "NOGROUP") || strings.Contains(err.Error(), "no such key"))
}

// StreamHealth implements notificationdom.StreamInspector. A stream or group
// that does not exist yet is reported as empty; any other error is returned.
func (i *StreamInspector) StreamHealth(ctx context.Context, now time.Time) ([]notificationdom.StreamHealth, error) {
	out := make([]notificationdom.StreamHealth, 0, len(notificationStreams))
	for _, s := range notificationStreams {
		h := notificationdom.StreamHealth{Stream: s.stream, Group: s.group}

		p, err := i.client.XPending(ctx, s.stream, s.group).Result()
		switch {
		case err == nil:
			h.Pending = p.Count
		case !missingGroup(err):
			return nil, fmt.Errorf("stream health: xpending %s: %w", s.stream, err)
		}
		if h.Pending > 0 {
			ext, err := i.client.XPendingExt(ctx, &redis.XPendingExtArgs{
				Stream: s.stream, Group: s.group, Start: "-", End: "+", Count: 50,
			}).Result()
			if err != nil && !missingGroup(err) {
				return nil, fmt.Errorf("stream health: xpending ext %s: %w", s.stream, err)
			}
			for _, e := range ext {
				if e.Idle > h.OldestPendingIdle {
					h.OldestPendingIdle = e.Idle
				}
			}
		}

		dead := s.stream + ".dead"
		n, err := i.client.XLen(ctx, dead).Result()
		if err != nil {
			return nil, fmt.Errorf("stream health: xlen %s: %w", dead, err)
		}
		h.DeadTotal = n
		if h.DeadTotal > 0 {
			since := fmt.Sprintf("%d-0", now.Add(-time.Hour).UnixMilli())
			recent, err := i.client.XRange(ctx, dead, since, "+").Result()
			if err != nil {
				return nil, fmt.Errorf("stream health: xrange %s: %w", dead, err)
			}
			h.DeadLastHour = int64(len(recent))
		}
		out = append(out, h)
	}
	return out, nil
}
