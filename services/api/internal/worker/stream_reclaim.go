package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Defaults for a consumer group's stale-message reclaim. The idle threshold is
// also the retry spacing: a message a handler declined to ack is redelivered
// no sooner than this after its last delivery, so maxDeliveries attempts span
// roughly maxDeliveries x reclaimMinIdle.
const (
	reclaimMinIdle      = 60 * time.Second
	reclaimInterval     = 30 * time.Second
	reclaimBatch        = 50
	reclaimMaxDelivered = 5
	// deadLetterMaxLen caps a dead-letter stream (approximate trim), so it
	// cannot grow without bound: nothing else in this codebase trims a stream.
	deadLetterMaxLen = 1000
)

// streamReclaimer redelivers entries a consumer group has delivered but nobody
// acknowledged, and parks the ones that keep failing.
//
// Without it an entry left unacked (a handler that returned an error, or a pod
// that died mid-handle) is only retried by the same consumer name's startup
// replay. Consumer names derive from the hostname, which changes with every
// pod, so the entry owned by a dead pod's name was never retried at all.
// XAUTOCLAIM reassigns any entry that has been idle long enough, whoever owns
// it, so one live consumer picks it up.
//
// Delivery is at-least-once: an entry whose handler finished its side effect
// but died before the ack is handled again, so handlers must tolerate that.
type streamReclaimer struct {
	client        *redis.Client
	log           *slog.Logger
	stream        string
	group         string
	consumer      string
	deadStream    string
	minIdle       time.Duration
	maxDeliveries int64
	batch         int64

	lastRun time.Time

	// readFailures counts, per entry id, the deliveries seen while XPENDING could
	// not be read, so a persistent read failure cannot make an entry immortal.
	readFailures map[string]int64
}

func newStreamReclaimer(client *redis.Client, log *slog.Logger, stream, group, consumer string) *streamReclaimer {
	return &streamReclaimer{
		client:        client,
		log:           log,
		stream:        stream,
		group:         group,
		consumer:      consumer,
		deadStream:    stream + ".dead",
		minIdle:       reclaimMinIdle,
		maxDeliveries: reclaimMaxDelivered,
		batch:         reclaimBatch,
		readFailures:  map[string]int64{},
	}
}

// due reports whether enough time has passed since the last run, and if so
// records this as the run. Called once per read-loop iteration, which is at
// most one read block apart, so no extra goroutine or timer is needed.
func (r *streamReclaimer) due(now time.Time) bool {
	if now.Sub(r.lastRun) < reclaimInterval {
		return false
	}
	r.lastRun = now
	return true
}

// run claims every entry idle for at least minIdle and hands each to handle,
// or dead-letters it once it has been delivered maxDeliveries times. handle
// is responsible for acking on success, exactly as for a normal read.
func (r *streamReclaimer) run(ctx context.Context, handle func(redis.XMessage)) {
	start := "0-0"
	for {
		msgs, next, err := r.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   r.stream,
			Group:    r.group,
			Consumer: r.consumer,
			MinIdle:  r.minIdle,
			Start:    start,
			Count:    r.batch,
		}).Result()
		if err != nil {
			// NOGROUP is recovered by the read loop's own handling.
			r.log.Warn("stream reclaim: xautoclaim failed", "stream", r.stream, "group", r.group, "err", err)
			return
		}
		for _, msg := range msgs {
			deliveries := r.deliveries(ctx, msg.ID)
			if deliveries > r.maxDeliveries {
				r.deadLetter(ctx, msg, deliveries, "delivered too many times without being acknowledged")
				continue
			}
			r.log.Warn("stream reclaim: redelivering unacked entry", "stream", r.stream, "group", r.group, "id", msg.ID, "deliveries", deliveries)
			handle(msg)
		}
		if next == "" || next == "0-0" {
			return
		}
		start = next
	}
}

// deliveries returns how many times the group has delivered id, counting the
// claim that just happened. When XPENDING cannot be read it falls back to a
// local count of the claims this reclaimer has made of the entry, so the
// dead-letter cap still holds.
func (r *streamReclaimer) deliveries(ctx context.Context, id string) int64 {
	pending, err := r.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: r.stream,
		Group:  r.group,
		Start:  id,
		End:    id,
		Count:  1,
	}).Result()
	if err != nil || len(pending) == 0 {
		if err != nil {
			r.log.Error("stream reclaim: could not read delivery count; counting locally", "stream", r.stream, "id", id, "err", err)
		}
		r.readFailures[id]++
		return r.readFailures[id]
	}
	delete(r.readFailures, id)
	return pending[0].RetryCount
}

// deadLetter copies a poison entry to the dead-letter stream with enough
// context to diagnose and replay it, then acks it so it stops being retried.
// The copy is written first: if it fails the entry stays pending and is tried
// again on the next run, so an entry is never dropped without a record.
func (r *streamReclaimer) deadLetter(ctx context.Context, msg redis.XMessage, deliveries int64, reason string) {
	delete(r.readFailures, msg.ID)
	values := make(map[string]any, len(msg.Values)+5)
	for k, v := range msg.Values {
		values[k] = v
	}
	values["dead_source_stream"] = r.stream
	values["dead_source_id"] = msg.ID
	values["dead_group"] = r.group
	values["dead_deliveries"] = deliveries
	values["dead_reason"] = reason
	if err := r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: r.deadStream,
		MaxLen: deadLetterMaxLen,
		Approx: true,
		Values: values,
	}).Err(); err != nil {
		r.log.Error("stream reclaim: could not dead-letter entry; leaving it pending", "stream", r.stream, "id", msg.ID, "err", err)
		return
	}
	if err := r.client.XAck(ctx, r.stream, r.group, msg.ID).Err(); err != nil {
		r.log.Warn("stream reclaim: xack after dead-letter failed", "stream", r.stream, "id", msg.ID, "err", err)
	}
	r.log.Error("stream reclaim: entry dead-lettered", "stream", r.stream, "group", r.group, "id", msg.ID, "deliveries", deliveries, "reason", reason, "dead_stream", r.deadStream)
}

// recordDead copies an entry that is being given up on to the dead-letter
// stream without touching its pending state; the caller acks it. Unlike
// deadLetter it does not ack, so the caller keeps control of the entry's fate.
// It returns an error when the record could not be written, and then the caller
// must not ack: an entry given up on with no record is lost without a trace.
func (r *streamReclaimer) recordDead(ctx context.Context, msg redis.XMessage, reason string) error {
	values := make(map[string]any, len(msg.Values)+4)
	for k, v := range msg.Values {
		values[k] = v
	}
	values["dead_source_stream"] = r.stream
	values["dead_source_id"] = msg.ID
	values["dead_group"] = r.group
	values["dead_reason"] = reason
	if err := r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: r.deadStream,
		MaxLen: deadLetterMaxLen,
		Approx: true,
		Values: values,
	}).Err(); err != nil {
		r.log.Warn("stream reclaim: could not record dead entry", "stream", r.stream, "id", msg.ID, "err", err)
		return err
	}
	return nil
}
