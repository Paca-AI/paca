package redis

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
)

const (
	sessionPrefix     = "session:"
	sessionUserPrefix = "session:user:"
)

// SessionStore is the Redis registry of live sessions: one hash per session
// plus, per user, a sorted set of session ids scored by last activity.
//
// Both expire with the session's refresh token, so an abandoned session
// disappears on its own; ListByUser also drops index entries whose hash has
// already expired.
type SessionStore struct {
	client *redis.Client
}

// NewSessionStore returns a SessionStore backed by client.
func NewSessionStore(client *redis.Client) *SessionStore {
	return &SessionStore{client: client}
}

// extendExpiry raises KEYS[1]'s expiry to ARGV[1] milliseconds unless it
// already lasts longer. PTTL is -1 for a key without an expiry, which counts
// as shorter, so a freshly created index always gets one.
var extendExpiry = redis.NewScript(`
local cur = redis.call('PTTL', KEYS[1])
if cur < tonumber(ARGV[1]) then
	return redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return 0`)

// validID reports whether id could be a session id. Ids are UUID family ids;
// anything else is rejected before it is spliced into a key, where it could
// otherwise land in the per-user index namespace ("user:<uuid>").
func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

func sessionKey(id string) string { return sessionPrefix + id }

func sessionUserKey(userID uuid.UUID) string { return sessionUserPrefix + userID.String() }

// Save implements domainauth.SessionStore.
func (s *SessionStore) Save(ctx context.Context, sess domainauth.Session, ttl time.Duration) error {
	key := sessionKey(sess.ID)
	idx := sessionUserKey(sess.UserID)
	last := sess.LastActiveAt.UnixMilli()

	pipe := s.client.TxPipeline()
	// created_at, user_id and remember_me are fixed at the first save; later
	// saves (a refresh) only move the volatile fields.
	pipe.HSetNX(ctx, key, "created_at", sess.CreatedAt.UnixMilli())
	pipe.HSetNX(ctx, key, "user_id", sess.UserID.String())
	pipe.HSetNX(ctx, key, "remember_me", boolStr(sess.RememberMe))
	// The sign-in's IP and User-Agent stay as first recorded, so a hijacked
	// session refreshing from elsewhere cannot overwrite where it began.
	pipe.HSetNX(ctx, key, "ip", sess.IP)
	pipe.HSetNX(ctx, key, "user_agent", sess.UserAgent)
	pipe.HSet(ctx, key, "last_ip", sess.IP, "last_active_at", last)
	pipe.Expire(ctx, key, ttl)
	pipe.ZAdd(ctx, idx, redis.Z{Score: float64(last), Member: sess.ID})
	// The index must outlive its longest-lived member. Done server-side in the
	// same transaction so concurrent saves with different TTLs (remember-me
	// or not) can only ever extend it, never shorten it.
	extendExpiry.Eval(ctx, pipe, []string{idx}, ttl.Milliseconds())
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session store: save %q: %w", sess.ID, err)
	}
	return nil
}

// Get implements domainauth.SessionStore.
func (s *SessionStore) Get(ctx context.Context, id string) (*domainauth.Session, error) {
	if !validID(id) {
		return nil, domainauth.ErrSessionNotFound
	}
	m, err := s.client.HGetAll(ctx, sessionKey(id)).Result()
	if err != nil {
		return nil, fmt.Errorf("session store: get %q: %w", id, err)
	}
	if len(m) == 0 {
		return nil, domainauth.ErrSessionNotFound
	}
	return decodeSession(id, m)
}

// ListByUser implements domainauth.SessionStore.
func (s *SessionStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]domainauth.Session, error) {
	idx := sessionUserKey(userID)
	ids, err := s.client.ZRangeArgs(ctx, redis.ZRangeArgs{Key: idx, Start: 0, Stop: -1, Rev: true}).Result()
	if err != nil {
		return nil, fmt.Errorf("session store: list %s: %w", userID, err)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	cmds := make([]*redis.MapStringStringCmd, len(ids))
	pipe := s.client.Pipeline()
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, sessionKey(id))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("session store: list %s: %w", userID, err)
	}

	out := make([]domainauth.Session, 0, len(ids))
	var stale []any
	for i, id := range ids {
		m := cmds[i].Val()
		if len(m) == 0 {
			stale = append(stale, id)
			continue
		}
		sess, err := decodeSession(id, m)
		if err != nil || sess.UserID != userID {
			stale = append(stale, id)
			continue
		}
		out = append(out, *sess)
	}
	if len(stale) > 0 {
		// Best effort: a failure only leaves the dead entries for next time.
		_ = s.client.ZRem(ctx, idx, stale...).Err()
	}
	return out, nil
}

// Delete implements domainauth.SessionStore.
func (s *SessionStore) Delete(ctx context.Context, id string) error {
	if !validID(id) {
		return nil
	}
	key := sessionKey(id)
	uid, err := s.client.HGet(ctx, key, "user_id").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("session store: delete %q: %w", id, err)
	}
	pipe := s.client.TxPipeline()
	pipe.Del(ctx, key)
	if uid != "" {
		pipe.ZRem(ctx, sessionUserPrefix+uid, id)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session store: delete %q: %w", id, err)
	}
	return nil
}

func decodeSession(id string, m map[string]string) (*domainauth.Session, error) {
	uid, err := uuid.Parse(m["user_id"])
	if err != nil {
		return nil, fmt.Errorf("session store: session %q has a bad user id", id)
	}
	return &domainauth.Session{
		ID:           id,
		UserID:       uid,
		IP:           m["ip"],
		UserAgent:    m["user_agent"],
		LastIP:       m["last_ip"],
		RememberMe:   m["remember_me"] == "1",
		CreatedAt:    millis(m["created_at"]),
		LastActiveAt: millis(m["last_active_at"]),
	}, nil
}

func millis(v string) time.Time {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(n)
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
