package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	dueDateSchedulerLeaderKey = "automation:due_date_scheduler:leader"
	dueDateSchedulerInterval  = time.Minute
)

// DueDateScheduler periodically polls for due_date_reached trigger nodes
// whose watched tasks have just entered their configured offset window, and
// fires them through the same graph-walk engine the stream consumer uses.
// No trigger detection in this codebase was previously poll-based (every
// other trigger reacts to an activity-stream event), so due dates need this
// dedicated ticker rather than reusing AutomationConsumer's stream loop.
//
// Because the API can run as multiple horizontally-scaled replicas, every
// tick first tries to acquire a short-lived Redis SETNX leader lock: only
// the replica holding the lock actually polls and fires for that tick,
// preventing every replica from independently double-firing the same
// due-date candidates. A replica that doesn't hold the lock simply does
// nothing this tick — harmless, since the leader is presumably ticking on
// schedule.
type DueDateScheduler struct {
	client   *redis.Client
	consumer *AutomationConsumer
	interval time.Duration
	log      *slog.Logger
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewDueDateScheduler creates a scheduler that fires due-date candidates
// through consumer's graph-walk engine.
func NewDueDateScheduler(client *redis.Client, consumer *AutomationConsumer, log *slog.Logger) *DueDateScheduler {
	return &DueDateScheduler{
		client:   client,
		consumer: consumer,
		interval: dueDateSchedulerInterval,
		log:      log,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// WithInterval overrides the poll interval (used by tests to avoid waiting
// on the real one-minute cadence).
func (s *DueDateScheduler) WithInterval(d time.Duration) *DueDateScheduler {
	s.interval = d
	return s
}

// Start begins polling in a background goroutine. Call Stop to drain and
// exit cleanly.
func (s *DueDateScheduler) Start(ctx context.Context) {
	go s.run()
}

// Stop signals the scheduler to stop and waits for the goroutine to exit.
func (s *DueDateScheduler) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

func (s *DueDateScheduler) run() {
	defer close(s.doneCh)
	s.log.Info("due-date scheduler: started", "interval", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			s.log.Info("due-date scheduler: stopping")
			return
		case <-ticker.C:
			s.tick(context.Background())
		}
	}
}

func (s *DueDateScheduler) tick(ctx context.Context) {
	acquired, err := s.client.SetNX(ctx, dueDateSchedulerLeaderKey, "1", s.interval*2).Result()
	if err != nil {
		s.log.Warn("due-date scheduler: leader lock error", "err", err)
		return
	}
	if !acquired {
		return // another replica holds the lock this tick
	}

	candidates, err := s.consumer.repo.ListDueDateCandidates(ctx)
	if err != nil {
		s.log.Error("due-date scheduler: list candidates", "err", err)
		return
	}
	for _, cand := range candidates {
		automation, err := s.consumer.repo.FindAutomationByNodeID(ctx, cand.Node.ID)
		if err != nil {
			s.log.Error("due-date scheduler: find automation", "node_id", cand.Node.ID, "err", err)
			continue
		}
		// Recorded before executing: a due-date fire is a one-shot event, and
		// marking it fired up front avoids double-firing if two ticks
		// overlap right at a leader-lock handoff. Acceptable trade-off for a
		// scheduled trigger, unlike the stream-based triggers' true
		// at-least-once retry semantics.
		if err := s.consumer.repo.RecordDueDateFire(ctx, automation.ID, cand.Node.ID, cand.TaskID); err != nil {
			s.log.Error("due-date scheduler: record fire", "node_id", cand.Node.ID, "task_id", cand.TaskID, "err", err)
			continue
		}
		task, err := s.consumer.taskRepo.FindTaskByID(ctx, cand.TaskID)
		if err != nil {
			s.log.Error("due-date scheduler: find task", "task_id", cand.TaskID, "err", err)
			continue
		}
		if err := s.consumer.executeRun(ctx, automation.ProjectID, cand.Node, task); err != nil {
			s.log.Error("due-date scheduler: run failed", "node_id", cand.Node.ID, "err", err)
		}
	}
}
