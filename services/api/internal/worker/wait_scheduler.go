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
// leader-lock/ticker shape as DueDateScheduler/CronScheduler, though here
// the lock is purely an optimization rather than a correctness requirement:
// ClaimDueDelays' DELETE ... RETURNING already makes claiming a due delay
// atomic and safe under concurrent replicas on its own — the lock just saves
// every replica from redundantly polling the same (usually empty) query on
// every tick.
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

// Start begins polling in a background goroutine. Call Stop to drain and
// exit cleanly.
func (s *WaitScheduler) Start(ctx context.Context) {
	go s.run()
}

// Stop signals the scheduler to stop and waits for the goroutine to exit.
func (s *WaitScheduler) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

func (s *WaitScheduler) run() {
	defer close(s.doneCh)
	s.log.Info("wait scheduler: started", "interval", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			s.log.Info("wait scheduler: stopping")
			return
		case <-ticker.C:
			s.tick(context.Background())
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

	delays, err := s.consumer.repo.ClaimDueDelays(ctx)
	if err != nil {
		s.log.Error("wait scheduler: claim due delays", "err", err)
		return
	}
	for _, delay := range delays {
		if err := s.consumer.resumeAfterDelay(ctx, delay); err != nil {
			s.log.Error("wait scheduler: resume after delay", "run_id", delay.RunID, "node_id", delay.NodeID, "err", err)
		}
	}
}
