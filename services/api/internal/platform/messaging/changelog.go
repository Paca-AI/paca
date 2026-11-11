package messaging

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// The change log is the replayable record of what happened in a project, kept
// in one capped Valkey Stream per project. Unlike the Pub/Sub channel, a
// reader that was away can catch up from the last sequence number it applied
// (see services/realtime/README.md, "Sync protocol").
//
// Each project has two keys, sharing a hash tag so they live in one cluster
// slot and one script can touch both:
//
//	paca:changes:{<project>}:meta    hash: seq (last allocated), epoch (ms)
//	paca:changes:{<project>}:stream  the entries, each carrying seq and epoch
//
// seq is allocated and appended in a single script, so the stream order is
// exactly the seq order even with concurrent writers. The entry's stream ID is
// "<seq>-0", so a reader turns a cursor into a range ("(<cursor>-0" to "+")
// without scanning. epoch changes when the
// counter is lost (a flushed Valkey), which tells a reader its cursor means
// nothing any more.
const (
	// ChangeLogMaxLen caps a project's stream (approximate trim).
	ChangeLogMaxLen = 10_000
	// ChangeLogTTL drops the stream of a project nobody has written to for
	// this long. The counter is kept: it is a few bytes, and losing it would
	// restart seq at 1 under a client that still holds an older cursor.
	ChangeLogTTL = 7 * 24 * time.Hour
)

// ChangeRef says where an appended change landed.
type ChangeRef struct {
	Seq   int64
	Epoch int64
}

// ChangeLogKeys returns the meta and stream keys of a project's change log.
func ChangeLogKeys(projectID string) (meta, stream string) {
	base := "paca:changes:{" + projectID + "}"
	return base + ":meta", base + ":stream"
}

// appendChangeScript allocates the next seq, appends the entry with it, trims
// and refreshes the expiry. KEYS: meta, stream. ARGV: maxlen, ttl seconds,
// now (ms, the epoch for a new counter), then the entry's field/value pairs.
var appendChangeScript = redis.NewScript(`
local meta, stream = KEYS[1], KEYS[2]
if redis.call('HEXISTS', meta, 'epoch') == 0 then
  redis.call('HSET', meta, 'epoch', ARGV[3])
end
local seq = redis.call('HINCRBY', meta, 'seq', 1)
local epoch = redis.call('HGET', meta, 'epoch')
local args = {stream, 'MAXLEN', '~', ARGV[1], seq .. '-0', 'seq', seq, 'epoch', epoch}
for i = 4, #ARGV do args[#args + 1] = ARGV[i] end
redis.call('XADD', unpack(args))
redis.call('EXPIRE', stream, ARGV[2])
return {seq, epoch}
`)

// AppendChange appends fields to the project's change log and returns the seq
// and epoch it was given. The fields must not include "seq" or "epoch".
func (p *Publisher) AppendChange(ctx context.Context, projectID string, fields map[string]any) (ChangeRef, error) {
	if p == nil || p.client == nil {
		return ChangeRef{}, errors.New("messaging: publisher not initialized")
	}
	meta, stream := ChangeLogKeys(projectID)
	argv := make([]any, 0, 3+2*len(fields))
	argv = append(argv,
		ChangeLogMaxLen,
		int64(ChangeLogTTL/time.Second),
		time.Now().UnixMilli(),
	)
	for k, v := range fields {
		argv = append(argv, k, v)
	}
	res, err := appendChangeScript.Run(ctx, p.client, []string{meta, stream}, argv...).Slice()
	if err != nil {
		return ChangeRef{}, fmt.Errorf("messaging: append change to %q: %w", stream, err)
	}
	if len(res) != 2 {
		return ChangeRef{}, fmt.Errorf("messaging: append change: unexpected reply %v", res)
	}
	seq, ok := res[0].(int64)
	if !ok {
		return ChangeRef{}, fmt.Errorf("messaging: append change: seq %v is not an integer", res[0])
	}
	var epoch int64
	switch v := res[1].(type) {
	case string:
		epoch, _ = strconv.ParseInt(v, 10, 64)
	case int64:
		epoch = v
	}
	return ChangeRef{Seq: seq, Epoch: epoch}, nil
}
