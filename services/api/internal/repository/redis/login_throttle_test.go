package redis

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestLoginThrottle_Redis(t *testing.T) {
	url := os.Getenv("PACA_TEST_REDIS_URL")
	if url == "" {
		t.Skip("PACA_TEST_REDIS_URL not set; skipping Redis-backed test")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opts)
	// Pair cap 3, so the all-address cap is 15.
	th := NewLoginThrottle(c, 3, time.Minute, 30*time.Second)
	ctx := t.Context()
	key := "test-" + uuid.NewString()
	const attacker, owner = "198.51.100.1", "203.0.113.7"
	t.Cleanup(func() {
		c.Del(ctx, loginFailPrefix+pairKey(key, attacker), loginLockPrefix+pairKey(key, attacker),
			loginFailPrefix+pairKey(key, owner), loginLockPrefix+pairKey(key, owner),
			loginUserFailPrefix+key, loginUserLockPrefix+key, loginKnownPrefix+key)
	})

	// One address locks itself out after the cap, and only itself.
	for i := 0; i < 2; i++ {
		if wait, err := th.Fail(ctx, key, attacker); err != nil || wait != 0 {
			t.Fatalf("fail %d: %v %v", i, wait, err)
		}
	}
	if wait, _ := th.Check(ctx, key, attacker); wait != 0 {
		t.Fatal("locked too early")
	}
	if wait, err := th.Fail(ctx, key, attacker); err != nil || wait != 30*time.Second {
		t.Fatalf("third failure should lock for 30s: %v %v", wait, err)
	}
	if wait, _ := th.Check(ctx, key, attacker); wait <= 0 || wait > 30*time.Second {
		t.Fatalf("Check wait = %v", wait)
	}
	if wait, _ := th.Check(ctx, key, owner); wait != 0 {
		t.Fatalf("another address must not be locked by the attacker's failures: %v", wait)
	}

	// Reset clears a pair's count.
	_, _ = th.Fail(ctx, key, owner)
	if err := th.Reset(ctx, key, owner); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Exists(ctx, loginFailPrefix+pairKey(key, owner)).Result(); n != 0 {
		t.Fatal("reset left the counter")
	}
}

func TestLoginThrottle_Redis_DistributedGuessingSparesKnownAddresses(t *testing.T) {
	url := os.Getenv("PACA_TEST_REDIS_URL")
	if url == "" {
		t.Skip("PACA_TEST_REDIS_URL not set; skipping Redis-backed test")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opts)
	th := NewLoginThrottle(c, 2, time.Minute, 30*time.Second) // all-address cap 10
	ctx := t.Context()
	key := "test-" + uuid.NewString()
	t.Cleanup(func() {
		keys := []string{loginUserFailPrefix + key, loginUserLockPrefix + key, loginKnownPrefix + key}
		for i := 0; i < 10; i++ {
			keys = append(keys, loginFailPrefix+pairKey(key, ipN(i)), loginLockPrefix+pairKey(key, ipN(i)))
		}
		c.Del(ctx, keys...)
	})

	const owner = "203.0.113.7"
	if err := th.Trust(ctx, key, owner); err != nil {
		t.Fatal(err)
	}
	// Ten addresses, one wrong guess each: never a pair lock, but the tenth
	// failure trips the all-address cap.
	var lastWait time.Duration
	for i := 0; i < 10; i++ {
		lastWait, err = th.Fail(ctx, key, ipN(i))
		if err != nil {
			t.Fatal(err)
		}
	}
	if lastWait != 30*time.Second {
		t.Fatalf("an unfamiliar address tripping the cap should be told to wait: %v", lastWait)
	}
	if wait, _ := th.Check(ctx, key, ipN(11)); wait <= 0 {
		t.Fatal("unfamiliar addresses must be locked out while the cap is in force")
	}
	if wait, _ := th.Check(ctx, key, owner); wait != 0 {
		t.Fatalf("the account owner's known address must keep working: %v", wait)
	}
}

func ipN(i int) string { return fmt.Sprintf("198.51.100.%d", i) }
