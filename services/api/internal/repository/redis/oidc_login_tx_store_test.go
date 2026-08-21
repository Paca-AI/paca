package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client, mr
}

func TestOIDCLoginTxStore_RoundTrip(t *testing.T) {
	client, _ := newTestRedis(t)
	store := NewOIDCLoginTxStore(client)
	ctx := context.Background()

	payload := []byte(`{"nonce":"n1","verifier":"v1"}`)
	if err := store.Save(ctx, "state-1", payload, time.Minute); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := store.Consume(ctx, "state-1")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("expected %q, got %q", payload, got)
	}
}

func TestOIDCLoginTxStore_SingleUse(t *testing.T) {
	client, _ := newTestRedis(t)
	store := NewOIDCLoginTxStore(client)
	ctx := context.Background()

	_ = store.Save(ctx, "state-1", []byte("{}"), time.Minute)

	if got, err := store.Consume(ctx, "state-1"); err != nil || got == nil {
		t.Fatalf("first consume: got %q, err %v", got, err)
	}
	// Second use of the same state: no payload, no error — unknown, expired
	// and used states are deliberately indistinguishable.
	if got, err := store.Consume(ctx, "state-1"); err != nil || got != nil {
		t.Fatalf("second consume: got %q, err %v", got, err)
	}
}

func TestOIDCLoginTxStore_TTLExpires(t *testing.T) {
	client, mr := newTestRedis(t)
	store := NewOIDCLoginTxStore(client)
	ctx := context.Background()

	_ = store.Save(ctx, "state-1", []byte("{}"), time.Minute)
	mr.FastForward(2 * time.Minute)

	if got, err := store.Consume(ctx, "state-1"); err != nil || got != nil {
		t.Fatalf("expected expired state to yield no payload, got %q, err %v", got, err)
	}
}

func TestOIDCLoginTxStore_UnknownState(t *testing.T) {
	client, _ := newTestRedis(t)
	store := NewOIDCLoginTxStore(client)

	if got, err := store.Consume(context.Background(), "never-seen"); err != nil || got != nil {
		t.Fatalf("expected (nil, nil), got %q, %v", got, err)
	}
}
