package messaging

import (
	"context"
	"strconv"
	"sync"
	"testing"
)

func TestAppendChange_AllocatesGaplessSeqPerProject(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()
	p := NewPublisher(client, testLogger())
	ctx := context.Background()

	a1, err := p.AppendChange(ctx, "p1", map[string]any{"type": "task.created"})
	if err != nil {
		t.Fatalf("AppendChange: %v", err)
	}
	a2, _ := p.AppendChange(ctx, "p1", map[string]any{"type": "task.updated"})
	b1, _ := p.AppendChange(ctx, "p2", map[string]any{"type": "task.created"})

	if a1.Seq != 1 || a2.Seq != 2 {
		t.Errorf("p1 seqs = %d, %d; want 1, 2", a1.Seq, a2.Seq)
	}
	if b1.Seq != 1 {
		t.Errorf("p2 seq = %d, want 1: counters are per project", b1.Seq)
	}
	if a1.Epoch == 0 || a1.Epoch != a2.Epoch {
		t.Errorf("epoch must be set and stable, got %d and %d", a1.Epoch, a2.Epoch)
	}

	_, stream := ChangeLogKeys("p1")
	entries, err := client.XRange(ctx, stream, "-", "+").Result()
	if err != nil || len(entries) != 2 {
		t.Fatalf("stream entries = %v, err %v; want 2", len(entries), err)
	}
	if entries[1].ID != "2-0" {
		t.Errorf("second entry id = %s, want 2-0 so seq maps straight to an id", entries[1].ID)
	}
	if entries[1].Values["seq"] != "2" || entries[1].Values["type"] != "task.updated" {
		t.Errorf("second entry = %v", entries[1].Values)
	}
}

func TestAppendChange_ConcurrentWritersKeepStreamOrderEqualToSeqOrder(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()
	p := NewPublisher(client, testLogger())
	ctx := context.Background()

	const n = 40
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.AppendChange(ctx, "p", map[string]any{"type": "t"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	_, stream := ChangeLogKeys("p")
	entries, _ := client.XRange(ctx, stream, "-", "+").Result()
	if len(entries) != n {
		t.Fatalf("got %d entries, want %d", len(entries), n)
	}
	for i, e := range entries {
		if want := strconv.Itoa(i + 1); e.Values["seq"] != want {
			t.Fatalf("entry %d has seq %v, want %s", i, e.Values["seq"], want)
		}
	}
}

func TestAppendChange_ExpiresStreamButKeepsCounter(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()
	p := NewPublisher(client, testLogger())
	ctx := context.Background()
	if _, err := p.AppendChange(ctx, "p", map[string]any{"type": "t"}); err != nil {
		t.Fatal(err)
	}
	meta, stream := ChangeLogKeys("p")
	if ttl := client.TTL(ctx, stream).Val(); ttl <= 0 {
		t.Errorf("stream ttl = %v, want it to expire", ttl)
	}
	if ttl := client.TTL(ctx, meta).Val(); ttl > 0 {
		t.Errorf("meta ttl = %v: the counter must not expire", ttl)
	}
}

func TestAppendChange_NilPublisher(t *testing.T) {
	var p *Publisher
	if _, err := p.AppendChange(context.Background(), "p", nil); err == nil {
		t.Error("a nil publisher must report an error")
	}
}
