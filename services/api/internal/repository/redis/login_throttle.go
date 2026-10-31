package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	loginFailPrefix = "login:fail:"
	loginLockPrefix = "login:lock:"
)

// failScript counts a failure and, at max, starts the lockout and resets the
// counter, all in one step so concurrent guesses can't slip past the cap.
// It returns the lockout in milliseconds when this failure tripped it, else 0.
var failScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[2]) end
if n >= tonumber(ARGV[1]) then
	redis.call('SET', KEYS[2], 1, 'PX', ARGV[3])
	redis.call('DEL', KEYS[1])
	return tonumber(ARGV[3])
end
return 0
`)

// LoginThrottle limits wrong-password sign-ins per username in Redis. It keys
// on the name as typed (not on whether the account exists), so the lockout
// can't be used to tell real usernames from invented ones.
type LoginThrottle struct {
	client      *redis.Client
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
}

// NewLoginThrottle locks a username for lockout after maxAttempts failures
// within window.
func NewLoginThrottle(client *redis.Client, maxAttempts int, window, lockout time.Duration) *LoginThrottle {
	return &LoginThrottle{client: client, maxAttempts: maxAttempts, window: window, lockout: lockout}
}

// Check returns how long key remains locked, or 0 when it is not.
func (t *LoginThrottle) Check(ctx context.Context, key string) (time.Duration, error) {
	ttl, err := t.client.PTTL(ctx, loginLockPrefix+key).Result()
	if err != nil {
		return 0, fmt.Errorf("login throttle: check: %w", err)
	}
	if ttl < 0 { // -2 no key, -1 no expiry
		return 0, nil
	}
	return ttl, nil
}

// Fail records a wrong password and returns the lockout it started, if any.
func (t *LoginThrottle) Fail(ctx context.Context, key string) (time.Duration, error) {
	ms, err := failScript.Run(ctx, t.client, []string{loginFailPrefix + key, loginLockPrefix + key},
		t.maxAttempts, t.window.Milliseconds(), t.lockout.Milliseconds()).Int64()
	if err != nil {
		return 0, fmt.Errorf("login throttle: fail: %w", err)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// Reset forgets earlier failures after a correct password.
func (t *LoginThrottle) Reset(ctx context.Context, key string) error {
	if err := t.client.Del(ctx, loginFailPrefix+key).Err(); err != nil {
		return fmt.Errorf("login throttle: reset: %w", err)
	}
	return nil
}
