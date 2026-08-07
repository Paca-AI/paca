package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	waitSchedulerLeaderKey = "automation:wait_scheduler:leader"
	// waitSchedulerInterval is much shorter than DueDateScheduler's one
	// minute: a wait node's configured duration can itself be just a couple
	// of minutes, and a full minute of imprecision on top of that would be
	// visibly wrong.
	waitSchedulerInterval = 15 * time.Second
)

// WaitScheduler periodically polls for wait action nodes whose configured
// duration has elapsed and resumes their paused graph walk. Same
// leader-lock/ticker shape as DueDateScheduler/CronScheduler — here the lock
// is a correctness requirement, not just an optimization: ListDueDelays no
// longer deletes on read (see its docstring), so without the lock two
// replicas could both list and resume the same due delay concurrently. The
// lock is held for the whole tick, including every resumeAfterDelay call
// below, not just the list step.
type WaitScheduler struct {
	client    *redis.Client
	consumer  *AutomationConsumer
	interval  time.Duration
	leaderKey string
	log       *slog.Logger
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewWaitScheduler creates a scheduler that resumes due delays through
// consumer's graph-walk engine.
func NewWaitScheduler(client *redis.Client, consumer *AutomationConsumer, log *slog.Logger) *WaitScheduler {
	return &WaitScheduler{
		client:    client,
		consumer:  consumer,
		interval:  waitSchedulerInterval,
		leaderKey: waitSchedulerLeaderKey,
		log:       log,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// WithInterval overrides the poll interval (used by tests to avoid waiting
// on the real cadence).
func (s *WaitScheduler) WithInterval(d time.Duration) *WaitScheduler {
	s.interval = d
	return s
}

// WithLeaderKey overrides the Redis leader-lock key — see
// DueDateScheduler.WithLeaderKey's docstring for why e2e tests need this
// (parallel tests sharing one physical Redis instance).
func (s *WaitScheduler) WithLeaderKey(key string) *WaitScheduler {
	s.leaderKey = key
	return s
}

// Start begins polling in a background goroutine, stopping cleanly if
// either ctx is canceled or Stop is called explicitly.
func (s *WaitScheduler) Start(ctx context.Context) {
	go s.run(ctx)
}

// Stop signals the scheduler to stop and waits for the goroutine to exit.
func (s *WaitScheduler) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

func (s *WaitScheduler) run(ctx context.Context) {
	defer close(s.doneCh)
	s.log.Info("wait scheduler: started", "interval", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			s.log.Info("wait scheduler: stopping")
			return
		case <-ctx.Done():
			s.log.Info("wait scheduler: stopping (context canceled)")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *WaitScheduler) tick(ctx context.Context) {
	err := s.client.SetArgs(ctx, s.leaderKey, "1", redis.SetArgs{TTL: s.interval * 2, Mode: "NX"}).Err()
	if errors.Is(err, redis.Nil) {
		return // another replica holds the lock this tick
	}
	if err != nil {
		s.log.Warn("wait scheduler: leader lock error", "err", err)
		return
	}
	defer func() {
		if delErr := s.client.Del(ctx, s.leaderKey).Err(); delErr != nil {
			s.log.Warn("wait scheduler: release leader lock", "err", delErr)
		}
	}()

	delays, err := s.consumer.repo.ListDueDelays(ctx)
	if err != nil {
		s.log.Error("wait scheduler: list due delays", "err", err)
		return
	}
	for _, delay := range delays {
		if err := s.consumer.resumeAfterDelay(ctx, delay); err != nil {
			s.log.Error("wait scheduler: resume after delay", "run_id", delay.RunID, "node_id", delay.NodeID, "err", err)
			continue // row left in place — retried next tick, see ListDueDelays
		}
		// Only delete once the walk has actually resumed — see
		// ListDueDelays' docstring.
		if err := s.consumer.repo.DeletePendingDelay(ctx, delay.ID); err != nil {
			s.log.Error("wait scheduler: delete pending delay", "id", delay.ID, "err", err)
			continue // don't finalize on top of a delete that may not have taken
		}
		// Must run after the delete above, not before — finalizeRunIfDone
		// counts this same table, so it would still see this now-resolved
		// delay as outstanding otherwise (see resumeAfterDelay's docstring).
		if err := s.consumer.finalizeRunIfDone(ctx, delay.RunID); err != nil {
			s.log.Error("wait scheduler: finalize run", "run_id", delay.RunID, "err", err)
		}
	}
}
