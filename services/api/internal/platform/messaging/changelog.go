package messaging

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
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

// changeLogLastKey is a hash entity id -> the time (ms) the entity's last
// whole-entity change was logged. The change reconciler reads it to tell a
// database change that was announced from one whose announcement was lost.
func changeLogLastKey(projectID string) string {
	return "paca:changes:{" + projectID + "}:last"
}

// MarkField is the key of the field of an AppendChange map that names the
// entity whose change this is, for the "last logged" index. It is read and
// removed before the entry is stored.
const MarkField = "_mark"

// MarkVersionField carries, next to MarkField, the row version the logged state
// of the entity has (its snapshot's version), or UnknownVersion when the change
// carries none, or DeletedVersion for a delete (which any later row is covered
// by). Read and removed like MarkField.
const MarkVersionField = "_markver"

// UnknownVersion marks a logged change whose row version is not known.
const UnknownVersion int64 = -1

// DeletedVersion marks the delete of an entity: nothing can follow it.
const DeletedVersion int64 = math.MaxInt64

// appendChangeScript allocates the next seq, appends the entry with it, trims
// and refreshes the expiry. KEYS: meta, stream, last. ARGV: maxlen, ttl
// seconds, now (ms, the epoch for a new counter and the logged time), the id of
// the entity to record as just logged ("" for none), the row version of the
// logged state, then the entry's field/value pairs. The index holds
// "<ms>:<version>" per entity.
var appendChangeScript = redis.NewScript(`
local meta, stream, last = KEYS[1], KEYS[2], KEYS[3]
if redis.call('HEXISTS', meta, 'epoch') == 0 then
  redis.call('HSET', meta, 'epoch', ARGV[3])
end
local seq = redis.call('HINCRBY', meta, 'seq', 1)
local epoch = redis.call('HGET', meta, 'epoch')
local args = {stream, 'MAXLEN', '~', ARGV[1], seq .. '-0', 'seq', seq, 'epoch', epoch}
for i = 6, #ARGV do args[#args + 1] = ARGV[i] end
redis.call('XADD', unpack(args))
redis.call('EXPIRE', stream, ARGV[2])
if ARGV[4] ~= '' then
  -- Concurrent writers can log out of order: never let an older state
  -- replace a newer one (a delete, math.MaxInt64, always wins).
  local keep = false
  local prev = redis.call('HGET', last, ARGV[4])
  if prev then
    local pv = tonumber(string.match(prev, ':(-?%d+)$'))
    local nv = tonumber(ARGV[5])
    if pv and nv and nv >= 0 and pv > nv then keep = true end
  end
  if not keep then
    redis.call('HSET', last, ARGV[4], ARGV[3] .. ':' .. ARGV[5])
  end
  redis.call('EXPIRE', last, ARGV[2])
end
return {seq, epoch}
`)

// AppendChange appends fields to the project's change log and returns the seq
// and epoch it was given. The fields must not include "seq" or "epoch".
func (p *Publisher) AppendChange(ctx context.Context, projectID string, fields map[string]any) (ChangeRef, error) {
	if p == nil || p.client == nil {
		return ChangeRef{}, errors.New("messaging: publisher not initialized")
	}
	meta, stream := ChangeLogKeys(projectID)
	mark, _ := fields[MarkField].(string)
	markVer, ok := fields[MarkVersionField].(int64)
	if !ok {
		markVer = UnknownVersion
	}
	argv := make([]any, 0, 5+2*len(fields))
	argv = append(argv,
		ChangeLogMaxLen,
		int64(ChangeLogTTL/time.Second),
		time.Now().UnixMilli(),
		mark,
		markVer,
	)
	for k, v := range fields {
		if k == MarkField || k == MarkVersionField {
			continue
		}
		argv = append(argv, k, v)
	}
	res, err := appendChangeScript.Run(ctx, p.client, []string{meta, stream, changeLogLastKey(projectID)}, argv...).Slice()
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

// ChangeHead returns the project's current change-log head: the last allocated
// seq and the log's epoch. ok is false when the project has no log yet or the
// read fails; callers treat that as "no head to report".
func (p *Publisher) ChangeHead(ctx context.Context, projectID string) (seq, epoch int64, ok bool) {
	if p == nil || p.client == nil {
		return 0, 0, false
	}
	meta, _ := ChangeLogKeys(projectID)
	vals, err := p.client.HMGet(ctx, meta, "seq", "epoch").Result()
	if err != nil || len(vals) != 2 || vals[0] == nil || vals[1] == nil {
		return 0, 0, false
	}
	seq, err1 := strconv.ParseInt(fmt.Sprint(vals[0]), 10, 64)
	epoch, err2 := strconv.ParseInt(fmt.Sprint(vals[1]), 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return seq, epoch, true
}

// Logged says when an entity's last whole-entity change was logged and the row
// version of the state it carried.
type Logged struct {
	AtMs    int64
	Version int64 // UnknownVersion, DeletedVersion or a row version
}

// LastLogged returns, for each entity id, its last whole-entity change in the
// project; an id never logged (or whose record expired) is absent.
func (p *Publisher) LastLogged(ctx context.Context, projectID string, entityIDs []string) (map[string]Logged, error) {
	out := make(map[string]Logged, len(entityIDs))
	if len(entityIDs) == 0 {
		return out, nil
	}
	if p == nil || p.client == nil {
		return nil, errors.New("messaging: publisher not initialized")
	}
	vals, err := p.client.HMGet(ctx, changeLogLastKey(projectID), entityIDs...).Result()
	if err != nil {
		return nil, fmt.Errorf("messaging: read last logged: %w", err)
	}
	for i, v := range vals {
		if v == nil {
			continue
		}
		at, ver, found := strings.Cut(fmt.Sprint(v), ":")
		ms, err1 := strconv.ParseInt(at, 10, 64)
		if err1 != nil {
			continue
		}
		version := UnknownVersion
		if found {
			if n, err2 := strconv.ParseInt(ver, 10, 64); err2 == nil {
				version = n
			}
		}
		out[entityIDs[i]] = Logged{AtMs: ms, Version: version}
	}
	return out, nil
}
