package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	agentTurnExpiryInterval = 5 * time.Second
	agentTurnExpiryBatch    = 100
)

type dueTurnExpirer interface {
	ExpireDueTurns(ctx context.Context, limit int) (int, error)
}

// AgentTurnExpiryScheduler is the DB-backed recovery path for a lost stream
// delivery, crashed runner, or disappeared ACP bridge. Multiple API replicas
// may run it concurrently; repository row locks and terminal checks arbitrate.
type AgentTurnExpiryScheduler struct {
	repo   dueTurnExpirer
	log    *slog.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewAgentTurnExpiryScheduler constructs the authoritative deadline worker.
func NewAgentTurnExpiryScheduler(repo dueTurnExpirer, log *slog.Logger) *AgentTurnExpiryScheduler {
	return &AgentTurnExpiryScheduler{repo: repo, log: log}
}

// Start begins deadline processing until the parent is canceled.
func (s *AgentTurnExpiryScheduler) Start(parent context.Context) {
	if s == nil || s.repo == nil || s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run(ctx)
	}()
}

// Stop cancels deadline processing and waits for the worker to exit.
func (s *AgentTurnExpiryScheduler) Stop() {
	if s == nil || s.cancel == nil {
		return
	}
	s.cancel()
	s.wg.Wait()
	s.cancel = nil
}

func (s *AgentTurnExpiryScheduler) run(ctx context.Context) {
	ticker := time.NewTicker(agentTurnExpiryInterval)
	defer ticker.Stop()
	for {
		if err := s.expireAll(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("expire due agent turns failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *AgentTurnExpiryScheduler) expireAll(ctx context.Context) error {
	for {
		count, err := s.repo.ExpireDueTurns(ctx, agentTurnExpiryBatch)
		if err != nil {
			return err
		}
		if count < agentTurnExpiryBatch {
			return nil
		}
	}
}
