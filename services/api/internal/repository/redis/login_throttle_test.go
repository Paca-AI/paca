package redis

import (
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
	th := NewLoginThrottle(c, 3, time.Minute, 30*time.Second)
	ctx := t.Context()
	key := "test-" + uuid.NewString()
	t.Cleanup(func() { c.Del(ctx, loginFailPrefix+key, loginLockPrefix+key) })

	for i := 0; i < 2; i++ {
		if wait, err := th.Fail(ctx, key); err != nil || wait != 0 {
			t.Fatalf("fail %d: %v %v", i, wait, err)
		}
	}
	if wait, _ := th.Check(ctx, key); wait != 0 {
		t.Fatal("locked too early")
	}
	if wait, err := th.Fail(ctx, key); err != nil || wait != 30*time.Second {
		t.Fatalf("third failure should lock for 30s: %v %v", wait, err)
	}
	if wait, _ := th.Check(ctx, key); wait <= 0 || wait > 30*time.Second {
		t.Fatalf("Check wait = %v", wait)
	}
	// Reset clears the count of a different, unlocked key.
	other := key + "-b"
	t.Cleanup(func() { c.Del(ctx, loginFailPrefix+other) })
	_, _ = th.Fail(ctx, other)
	if err := th.Reset(ctx, other); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Exists(ctx, loginFailPrefix+other).Result(); n != 0 {
		t.Fatal("reset left the counter")
	}
}
