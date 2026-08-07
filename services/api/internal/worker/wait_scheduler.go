package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
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

// releaseLockScript deletes leaderKey only if it still holds this holder's
// own token — a plain Del (used by DueDateScheduler/CronScheduler, where a
// lost lock is harmless) would let a replica whose lock already expired and
// was re-acquired by someone else delete that new holder's lock instead of
// its own. Here that distinction matters (see WaitScheduler's docstring), so
// release is compare-and-delete rather than unconditional.
var releaseLockScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
else
	return 0
end
`)

// renewLockScript extends leaderKey's TTL only if it still holds this
// holder's own token, atomically with the check — same compare-then-act
// requirement as releaseLockScript, so a holder whose lock already expired
// (and was possibly reclaimed by another replica) can't accidentally revive
// or extend a lock it no longer owns.
var renewLockScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("pexpire", KEYS[1], ARGV[2])
else
	return 0
end
`)

// WaitScheduler periodically polls for wait action nodes whose configured
// duration has elapsed and resumes their paused graph walk. Same
// leader-lock/ticker shape as DueDateScheduler/CronScheduler — here the lock
// is a correctness requirement, not just an optimization: ListDueDelays no
// longer deletes on read (see its docstring), so without the lock two
// replicas could both list and resume the same due delay concurrently.
//
// Unlike DueDateScheduler/CronScheduler's plain SETNX-then-unconditional-Del
// lock (fine there, since a lost lock is merely "harmless" — see
// DueDateScheduler's docstring), this lock is held across a loop of
// resumeAfterDelay calls whose total duration isn't bounded by the tick
// interval — a backlog of due delays, or one slow call_api HTTP call, can
// keep a tick running well past the lock's TTL. So the lock here carries a
// per-acquisition token (see acquireLock/renewLock/releaseLock): release is
// compare-and-delete rather than unconditional (a holder whose TTL already
// expired must not delete a different replica's freshly-acquired lock), and
// the lock is renewed before processing each delay in the loop rather than
// held statically for the whole tick — so a long backlog keeps extending its
// own lease instead of running out from under itself.
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

// acquireLock attempts to take leaderKey with a fresh, per-acquisition
// token, returning ("", false) if another replica already holds it.
func (s *WaitScheduler) acquireLock(ctx context.Context) (string, bool) {
	token := uuid.New().String()
	err := s.client.SetArgs(ctx, s.leaderKey, token, redis.SetArgs{TTL: s.interval * 2, Mode: "NX"}).Err()
	if errors.Is(err, redis.Nil) {
		return "", false // another replica holds the lock this tick
	}
	if err != nil {
		s.log.Warn("wait scheduler: leader lock error", "err", err)
		return "", false
	}
	return token, true
}

// renewLock extends leaderKey's TTL, but only while token is still the
// current holder — see renewLockScript. Returns false if the lock was lost
// (expired and possibly reclaimed by another replica), signaling the caller
// to stop processing rather than continue unprotected.
func (s *WaitScheduler) renewLock(ctx context.Context, token string) bool {
	n, err := renewLockScript.Run(ctx, s.client, []string{s.leaderKey}, token, (s.interval * 2).Milliseconds()).Int()
	if err != nil {
		s.log.Warn("wait scheduler: renew leader lock", "err", err)
		return false
	}
	return n != 0
}

// releaseLock deletes leaderKey, but only while token is still the current
// holder — see releaseLockScript.
func (s *WaitScheduler) releaseLock(ctx context.Context, token string) {
	if err := releaseLockScript.Run(ctx, s.client, []string{s.leaderKey}, token).Err(); err != nil {
		s.log.Warn("wait scheduler: release leader lock", "err", err)
	}
}

func (s *WaitScheduler) tick(ctx context.Context) {
	token, ok := s.acquireLock(ctx)
	if !ok {
		return
	}
	defer s.releaseLock(ctx, token)

	delays, err := s.consumer.repo.ListDueDelays(ctx)
	if err != nil {
		s.log.Error("wait scheduler: list due delays", "err", err)
		return
	}
	for _, delay := range delays {
		// Renewed before every delay (not just once per tick) so a long
		// backlog — or one slow resumeAfterDelay call — keeps the lease
		// alive instead of racing its own TTL. A lost lock here means
		// another replica may already be processing (or about to process)
		// the remainder of this tick's list, so stop rather than risk
		// double-resuming.
		if !s.renewLock(ctx, token) {
			s.log.Warn("wait scheduler: lost leader lock mid-tick, stopping early", "remaining", len(delays))
			return
		}
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
