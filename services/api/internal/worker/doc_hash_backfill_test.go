package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// fakeHashStore holds document bodies and the hashes filled in so far.
type fakeHashStore struct {
	bodies []json.RawMessage
	hashes []string // "" = not backfilled yet
	calls  int
	failOn int // the call number that fails (0 = none)
}

func (f *fakeHashStore) BackfillContentHashes(_ context.Context, limit int, hash func(json.RawMessage) string) (int, error) {
	f.calls++
	if f.calls == f.failOn {
		return 0, errors.New("database unreachable")
	}
	n := 0
	for i, body := range f.bodies {
		if f.hashes[i] != "" {
			continue
		}
		if n == limit {
			break
		}
		f.hashes[i] = hash(body)
		n++
	}
	return n, nil
}

func newFakeHashStore(n int) *fakeHashStore {
	f := &fakeHashStore{hashes: make([]string, n)}
	for i := 0; i < n; i++ {
		f.bodies = append(f.bodies, json.RawMessage(`[{"id":"x","type":"paragraph","content":[{"type":"text","text":"doc `+string(rune('a'+i%26))+`","styles":{}}]}]`))
	}
	return f
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestDocHashBackfill_FillsAndStops(t *testing.T) {
	store := newFakeHashStore(450)
	b := NewDocHashBackfill(store, quietLogger())

	filled, err := b.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if filled != 450 {
		t.Fatalf("filled %d rows, want 450", filled)
	}
	for i, h := range store.hashes {
		if h != docdom.ContentHash(store.bodies[i]) {
			t.Fatalf("row %d has hash %q, want the content hash of its body", i, h)
		}
	}
	// 200 + 200 + 50, then the empty batch that ends the loop.
	if store.calls != 4 {
		t.Fatalf("made %d batch calls, want 4", store.calls)
	}

	// Nothing left: one query, then done.
	store.calls = 0
	if filled, err := b.Run(context.Background()); err != nil || filled != 0 || store.calls != 1 {
		t.Fatalf("second run: filled %d, calls %d, err %v; want 0, 1, nil", filled, store.calls, err)
	}
}

func TestDocHashBackfill_StartRetriesAfterAnErrorThenExits(t *testing.T) {
	store := newFakeHashStore(250)
	store.failOn = 2
	b := NewDocHashBackfill(store, quietLogger()).WithRetryDelay(time.Millisecond)

	b.Start(context.Background())
	select {
	case <-b.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the backfill did not finish")
	}
	for i, h := range store.hashes {
		if h == "" {
			t.Fatalf("row %d was never backfilled", i)
		}
	}
	b.Stop() // after it finished on its own: must not block or panic
}

func TestDocHashBackfill_StopEndsAFailingLoop(t *testing.T) {
	store := newFakeHashStore(10)
	store.failOn = 1
	b := NewDocHashBackfill(store, quietLogger()).WithRetryDelay(time.Hour)

	b.Start(context.Background())
	stopped := make(chan struct{})
	go func() { b.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not end the retry wait")
	}
}
