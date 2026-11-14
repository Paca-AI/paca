package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

const (
	docHashBackfillBatch      = 200
	docHashBackfillRetryDelay = time.Minute
)

// DocHashStore fills in missing document content hashes, one batch per call.
type DocHashStore interface {
	// BackfillContentHashes stores hash(content) on up to limit documents
	// whose content_hash is NULL, soft-deleted ones included, and returns how
	// many it filled. It must not change seq or updated_at. It may skip rows
	// that are locked; zero means none could be filled.
	BackfillContentHashes(ctx context.Context, limit int, hash func(content json.RawMessage) string) (int, error)
}

// DocHashBackfill fills documents.content_hash on the rows written before the
// column existed (migration 000087 left them NULL: the hash needs the Go
// normalizer). A NULL hash is not wrong, the service computes it from the body
// when it needs it, but list queries and the change feed have no body to
// compute it from. The backfill runs once per process start, in the
// background, and exits when no NULL hash is left; rows written after the
// migration always carry a hash, so there is nothing to poll for.
//
// It does not show up in the change feed: the store writes the hash without
// moving seq or updated_at, because a document's hash is the same before and
// after it is stored.
type DocHashBackfill struct {
	store      DocHashStore
	logger     *slog.Logger
	retryDelay time.Duration

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewDocHashBackfill returns a backfill over store.
func NewDocHashBackfill(store DocHashStore, logger *slog.Logger) *DocHashBackfill {
	return &DocHashBackfill{
		store: store, logger: logger, retryDelay: docHashBackfillRetryDelay,
		stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
}

// WithRetryDelay overrides how long Start waits after a failed batch (tests).
func (b *DocHashBackfill) WithRetryDelay(d time.Duration) *DocHashBackfill {
	b.retryDelay = d
	return b
}

// Run fills batches until none is left and returns how many rows it filled.
// It stops at the first error.
func (b *DocHashBackfill) Run(ctx context.Context) (int, error) {
	total := 0
	for {
		n, err := b.store.BackfillContentHashes(ctx, docHashBackfillBatch, docdom.ContentHash)
		total += n
		if err != nil {
			return total, err
		}
		// Only an empty batch says nothing is left: a short one may have
		// skipped rows another transaction was holding.
		if n == 0 {
			return total, nil
		}
		select {
		case <-b.stopCh:
			return total, nil
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
	}
}

// Start runs the backfill in a background goroutine. A failed batch is retried
// after the retry delay; the goroutine exits once a run completes or Stop is
// called.
func (b *DocHashBackfill) Start(ctx context.Context) {
	go func() {
		defer close(b.doneCh)
		for {
			n, err := b.Run(ctx)
			if n > 0 {
				b.logger.Info("doc hash backfill: stored content hashes", "rows", n)
			}
			if err == nil {
				return
			}
			b.logger.Warn("doc hash backfill: batch failed, will retry", "err", err, "retry_in", b.retryDelay)
			t := time.NewTimer(b.retryDelay)
			select {
			case <-b.stopCh:
				t.Stop()
				return
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}()
}

// Done is closed when the goroutine started by Start has exited.
func (b *DocHashBackfill) Done() <-chan struct{} { return b.doneCh }

// Stop ends the backfill started by Start and waits for it. It is safe to call
// after the backfill finished on its own.
func (b *DocHashBackfill) Stop() {
	b.stopOnce.Do(func() { close(b.stopCh) })
	<-b.doneCh
}
