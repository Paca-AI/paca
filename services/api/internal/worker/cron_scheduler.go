package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

const (
	cronSchedulerLeaderKey = "automation:cron_scheduler:leader"
	cronSchedulerInterval  = time.Minute
)

// CronScheduler periodically polls for cron trigger nodes whose schedule has
// come due, and fires them through the same graph-walk engine the stream
// consumer uses. Structurally identical to DueDateScheduler (same
// leader-locked ticker, same reason for living in the worker package — see
// due_date_scheduler.go's header comment) — the only real difference is the
// per-tick "is it due" check: due-date is a one-shot event per (node, task),
// cron is a recurring schedule anchored to its own last-fired timestamp.
type CronScheduler struct {
	client   *redis.Client
	consumer *AutomationConsumer
	interval time.Duration
	log      *slog.Logger
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewCronScheduler creates a scheduler that fires due cron nodes through
// consumer's graph-walk engine.
func NewCronScheduler(client *redis.Client, consumer *AutomationConsumer, log *slog.Logger) *CronScheduler {
	return &CronScheduler{
		client:   client,
		consumer: consumer,
		interval: cronSchedulerInterval,
		log:      log,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// WithInterval overrides the poll interval (used by tests to avoid waiting
// on the real one-minute cadence).
func (s *CronScheduler) WithInterval(d time.Duration) *CronScheduler {
	s.interval = d
	return s
}

// Start begins polling in a background goroutine. Call Stop to drain and
// exit cleanly.
func (s *CronScheduler) Start(ctx context.Context) {
	go s.run()
}

// Stop signals the scheduler to stop and waits for the goroutine to exit.
func (s *CronScheduler) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

func (s *CronScheduler) run() {
	defer close(s.doneCh)
	s.log.Info("cron scheduler: started", "interval", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			s.log.Info("cron scheduler: stopping")
			return
		case <-ticker.C:
			s.tick(context.Background())
		}
	}
}

func (s *CronScheduler) tick(ctx context.Context) {
	err := s.client.SetArgs(ctx, cronSchedulerLeaderKey, "1", redis.SetArgs{TTL: s.interval * 2, Mode: "NX"}).Err()
	if errors.Is(err, redis.Nil) {
		return // another replica holds the lock this tick
	}
	if err != nil {
		s.log.Warn("cron scheduler: leader lock error", "err", err)
		return
	}

	candidates, err := s.consumer.repo.ListCronCandidates(ctx)
	if err != nil {
		s.log.Error("cron scheduler: list candidates", "err", err)
		return
	}
	now := time.Now().UTC()
	for _, cand := range candidates {
		var cfg automationdom.TriggerConfig
		if err := json.Unmarshal(cand.Node.Config, &cfg); err != nil || cfg.CronExpression == "" {
			continue // misconfigured node — same defensive skip as predecessor_done
		}
		schedule, err := cron.ParseStandard(cfg.CronExpression)
		if err != nil {
			s.log.Warn("cron scheduler: invalid cron expression", "node_id", cand.Node.ID, "err", err)
			continue
		}

		anchor := cand.Node.CreatedAt
		if cand.LastFiredAt != nil {
			anchor = *cand.LastFiredAt
		}
		next := schedule.Next(anchor)
		if next.After(now) {
			continue // not due yet
		}
		// Collapse a burst of missed occurrences (e.g. the scheduler was
		// down for an hour on a 5-minute cron) into ONE fire instead of one
		// per missed tick — walk forward on the schedule's own grid until
		// reaching the most recent occurrence that's still <= now.
		for {
			following := schedule.Next(next)
			if following.After(now) {
				break
			}
			next = following
		}

		automation, err := s.consumer.repo.FindAutomationByNodeID(ctx, cand.Node.ID)
		if err != nil {
			s.log.Error("cron scheduler: find automation", "node_id", cand.Node.ID, "err", err)
			continue
		}
		// Recorded before executing, and as the schedule's own grid time
		// (not wall-clock "now"): the at-most-once trade-off mirrors
		// due-date's (avoids double-firing across a leader-lock handoff),
		// and anchoring to grid time rather than processing time keeps a
		// fixed-interval cron (e.g. "every hour on the hour") from
		// drifting later with each tick's own scheduling jitter.
		if err := s.consumer.repo.RecordCronFire(ctx, automation.ID, cand.Node.ID, next); err != nil {
			s.log.Error("cron scheduler: record fire", "node_id", cand.Node.ID, "err", err)
			continue
		}
		// TargetTaskID is optional — nil means this node may only reach
		// call_api actions downstream (validateTaskReachability enforces
		// that at edge-creation/node-update/activate time).
		var targetTask *taskdom.Task
		if cfg.TargetTaskID != nil {
			targetTask, err = s.consumer.taskRepo.FindTaskByID(ctx, *cfg.TargetTaskID)
			if err != nil {
				s.log.Error("cron scheduler: find target task", "node_id", cand.Node.ID, "err", err)
				continue
			}
		}
		if err := s.consumer.executeRun(ctx, automation.ProjectID, cand.Node, targetTask); err != nil {
			s.log.Error("cron scheduler: run failed", "node_id", cand.Node.ID, "err", err)
		}
	}
}
