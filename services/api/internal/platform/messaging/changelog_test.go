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

func TestChangeHead_ReportsTheHeadAndNothingBeforeTheFirstChange(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()
	p := NewPublisher(client, testLogger())
	ctx := context.Background()
	if _, _, ok := p.ChangeHead(ctx, "p"); ok {
		t.Fatal("a project with no change has no head")
	}
	_, _ = p.AppendChange(ctx, "p", map[string]any{"type": "t"})
	ref, _ := p.AppendChange(ctx, "p", map[string]any{"type": "t"})
	seq, epoch, ok := p.ChangeHead(ctx, "p")
	if !ok || seq != 2 || epoch != ref.Epoch {
		t.Fatalf("head = %d/%d ok=%v, want 2/%d", seq, epoch, ok, ref.Epoch)
	}
	var nilPub *Publisher
	if _, _, ok := nilPub.ChangeHead(ctx, "p"); ok {
		t.Error("a nil publisher has no head")
	}
}

func TestAppendChange_RecordsWhenAndAtWhatVersionAnEntityWasLastLogged(t *testing.T) {
	client, cleanup := newTestRedisClient(t)
	defer cleanup()
	p := NewPublisher(client, testLogger())
	ctx := context.Background()

	if _, err := p.AppendChange(ctx, "p", map[string]any{"type": "task.updated", MarkField: "task-1", MarkVersionField: int64(4)}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AppendChange(ctx, "p", map[string]any{"type": "task.updated", MarkField: "task-3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AppendChange(ctx, "p", map[string]any{"type": "task.deleted", MarkField: "task-4", MarkVersionField: DeletedVersion}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AppendChange(ctx, "p", map[string]any{"type": "task.comment.added"}); err != nil {
		t.Fatal(err)
	}
	got, err := p.LastLogged(ctx, "p", []string{"task-1", "task-2", "task-3", "task-4"})
	if err != nil {
		t.Fatal(err)
	}
	if got["task-1"].AtMs == 0 || got["task-1"].Version != 4 {
		t.Errorf("task-1 = %+v, want a time and version 4", got["task-1"])
	}
	if got["task-3"].Version != UnknownVersion {
		t.Errorf("task-3 logged without a version must say so, got %+v", got["task-3"])
	}
	if got["task-4"].Version != DeletedVersion {
		t.Errorf("task-4 = %+v, want the deleted version", got["task-4"])
	}
	if _, has := got["task-2"]; has {
		t.Errorf("task-2 was never logged, got %v", got)
	}
	_, stream := ChangeLogKeys("p")
	entries, _ := client.XRange(ctx, stream, "-", "+").Result()
	for _, e := range entries {
		for _, k := range []string{MarkField, MarkVersionField} {
			if _, has := e.Values[k]; has {
				t.Errorf("%s must not be stored in the entry: %v", k, e.Values)
			}
		}
	}
	if ttl := client.TTL(ctx, changeLogLastKey("p")).Val(); ttl <= 0 {
		t.Errorf("the index must expire, ttl = %v", ttl)
	}
}
