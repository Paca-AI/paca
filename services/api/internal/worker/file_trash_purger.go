package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	filedom "github.com/Paca-AI/api/internal/domain/file"
)

const (
	fileTrashPurgerLeaderKey = "files:trash_purger:leader"
	fileTrashPurgerInterval  = time.Hour
	// fileTrashPurgeBatch bounds the trashed subtrees removed per tick, so a
	// large backlog is worked off over several ticks instead of one long one.
	fileTrashPurgeBatch = 200
)

// ExpiredTrashPurger permanently removes trashed files older than a cutoff.
type ExpiredTrashPurger interface {
	PurgeExpired(ctx context.Context, before time.Time, limit int) (int, error)
}

// FileTrashPurger permanently deletes file-manager items (and their stored
// bytes) once they have sat in the trash for filedom.TrashRetention. Like the
// other schedulers, only the replica holding a short-lived Redis lock works on
// a given tick.
type FileTrashPurger struct {
	client    *redis.Client
	purger    ExpiredTrashPurger
	interval  time.Duration
	retention time.Duration
	leaderKey string
	log       *slog.Logger
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewFileTrashPurger returns a purger that ticks hourly.
func NewFileTrashPurger(client *redis.Client, purger ExpiredTrashPurger, log *slog.Logger) *FileTrashPurger {
	return &FileTrashPurger{
		client: client, purger: purger, interval: fileTrashPurgerInterval,
		retention: filedom.TrashRetention, leaderKey: fileTrashPurgerLeaderKey, log: log,
		stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
}

// Start begins ticking in a background goroutine.
func (p *FileTrashPurger) Start(_ context.Context) { go p.run() }

// Stop signals the purger to stop and waits for it to exit.
func (p *FileTrashPurger) Stop() {
	close(p.stopCh)
	<-p.doneCh
}

func (p *FileTrashPurger) run() {
	defer close(p.doneCh)
	p.log.Info("file trash purger: started", "interval", p.interval, "retention", p.retention)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			p.log.Info("file trash purger: stopping")
			return
		case <-ticker.C:
			p.tick(context.Background())
		}
	}
}

func (p *FileTrashPurger) tick(ctx context.Context) {
	err := p.client.SetArgs(ctx, p.leaderKey, "1", redis.SetArgs{TTL: p.interval / 2, Mode: "NX"}).Err()
	if errors.Is(err, redis.Nil) {
		return // another replica holds the lock this tick
	}
	if err != nil {
		p.log.Warn("file trash purger: leader lock error", "err", err)
		return
	}
	n, err := p.purger.PurgeExpired(ctx, time.Now().Add(-p.retention), fileTrashPurgeBatch)
	if err != nil {
		p.log.Error("file trash purger: purge", "err", err)
		return
	}
	if n > 0 {
		p.log.Info("file trash purger: removed expired files", "objects", n)
	}
}
