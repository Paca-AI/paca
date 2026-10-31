package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Per (username, client address): failures and the lock they cause.
	loginFailPrefix = "login:fail:"
	loginLockPrefix = "login:lock:"
	// Per username across all addresses.
	loginUserFailPrefix = "login:ufail:"
	loginUserLockPrefix = "login:ulock:"
	// Addresses a username has fully signed in from.
	loginKnownPrefix = "login:known:"

	// userMaxFactor scales the per-address cap to the all-address cap.
	userMaxFactor = 5
	// knownTTL is how long a signed-in address stays trusted after its last
	// sign-in; knownMax bounds the set so it can't grow without limit.
	knownTTL = 30 * 24 * time.Hour
	knownMax = 20
)

// failScript counts a failure against the pair and the username and, on
// reaching either cap, starts that lockout and resets that counter, all in one
// step so concurrent guesses can't slip past. It returns {pairMs, userMs}: the
// lockout each counter started in milliseconds, else 0.
var failScript = redis.NewScript(`
local function bump(fail, lock, max, window, lockout)
	local n = redis.call('INCR', fail)
	if n == 1 then redis.call('PEXPIRE', fail, window) end
	if n >= tonumber(max) then
		redis.call('SET', lock, 1, 'PX', lockout)
		redis.call('DEL', fail)
		return tonumber(lockout)
	end
	return 0
end
local pair = bump(KEYS[1], KEYS[2], ARGV[1], ARGV[3], ARGV[4])
local user = bump(KEYS[3], KEYS[4], ARGV[2], ARGV[3], ARGV[4])
return {pair, user}
`)

// LoginThrottle limits wrong-password sign-ins in Redis. It keys on the name
// as typed (not on whether the account exists), so a lockout can't be used to
// tell real usernames from invented ones. See domainauth.LoginThrottle for how
// the two scopes keep it from being used to lock a victim out.
type LoginThrottle struct {
	client      *redis.Client
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
}

// NewLoginThrottle locks a (username, address) pair for lockout after
// maxAttempts failures within window, and unfamiliar addresses out of a
// username after 5x that many failures from anywhere.
func NewLoginThrottle(client *redis.Client, maxAttempts int, window, lockout time.Duration) *LoginThrottle {
	return &LoginThrottle{client: client, maxAttempts: maxAttempts, window: window, lockout: lockout}
}

func pairKey(key, ip string) string { return key + "|" + ip }

func (t *LoginThrottle) known(ctx context.Context, key, ip string) (bool, error) {
	return t.client.SIsMember(ctx, loginKnownPrefix+key, ip).Result()
}

// remaining is the TTL of a lock key, 0 when absent.
func (t *LoginThrottle) remaining(ctx context.Context, lockKey string) (time.Duration, error) {
	ttl, err := t.client.PTTL(ctx, lockKey).Result()
	if err != nil {
		return 0, err
	}
	if ttl < 0 { // -2 no key, -1 no expiry
		return 0, nil
	}
	return ttl, nil
}

// Check returns how long key remains locked for ip, or 0 when it is not.
func (t *LoginThrottle) Check(ctx context.Context, key, ip string) (time.Duration, error) {
	wait, err := t.remaining(ctx, loginLockPrefix+pairKey(key, ip))
	if err != nil {
		return 0, fmt.Errorf("login throttle: check: %w", err)
	}
	userWait, err := t.remaining(ctx, loginUserLockPrefix+key)
	if err != nil {
		return 0, fmt.Errorf("login throttle: check: %w", err)
	}
	if userWait > wait {
		ok, err := t.known(ctx, key, ip)
		if err != nil {
			return 0, fmt.Errorf("login throttle: check: %w", err)
		}
		if !ok {
			wait = userWait
		}
	}
	return wait, nil
}

// Fail records a wrong password and returns the lockout it started for ip, if any.
func (t *LoginThrottle) Fail(ctx context.Context, key, ip string) (time.Duration, error) {
	res, err := failScript.Run(ctx, t.client,
		[]string{loginFailPrefix + pairKey(key, ip), loginLockPrefix + pairKey(key, ip), loginUserFailPrefix + key, loginUserLockPrefix + key},
		t.maxAttempts, t.maxAttempts*userMaxFactor, t.window.Milliseconds(), t.lockout.Milliseconds()).Int64Slice()
	if err != nil || len(res) != 2 {
		return 0, fmt.Errorf("login throttle: fail: %w", err)
	}
	wait := time.Duration(res[0]) * time.Millisecond
	if userWait := time.Duration(res[1]) * time.Millisecond; userWait > 0 && wait == 0 {
		ok, err := t.known(ctx, key, ip)
		if err != nil {
			return 0, fmt.Errorf("login throttle: fail: %w", err)
		}
		if !ok {
			wait = userWait
		}
	}
	return wait, nil
}

// Reset forgets the pair's earlier failures after a correct password.
func (t *LoginThrottle) Reset(ctx context.Context, key, ip string) error {
	if err := t.client.Del(ctx, loginFailPrefix+pairKey(key, ip)).Err(); err != nil {
		return fmt.Errorf("login throttle: reset: %w", err)
	}
	return nil
}

// Trust records ip as an address key has fully signed in from.
func (t *LoginThrottle) Trust(ctx context.Context, key, ip string) error {
	k := loginKnownPrefix + key
	pipe := t.client.Pipeline()
	pipe.SAdd(ctx, k, ip)
	pipe.Expire(ctx, k, knownTTL)
	card := pipe.SCard(ctx, k)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("login throttle: trust: %w", err)
	}
	if card.Val() > knownMax {
		_ = t.client.SPop(ctx, k).Err()
	}
	return nil
}
