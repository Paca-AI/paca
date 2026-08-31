package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/api/internal/events"
)

const (
	environmentCommandConsumerGroup = "api.environment_commands"
	environmentCommandReadBlock     = 5 * time.Second
	environmentCommandReadCount     = 10
	// environmentCommandConcurrency bounds how many queued commands run()
	// (the live read loop, not processPending's own one-time startup drain)
	// dispatches at once — see run()'s own doc comment for why this became
	// necessary once create/start/restart-ports started blocking on a
	// multi-minute wait for agent-runner's reply (aiAgentProvisionHTTPTimeout,
	// environmentsvc's own doc comment) instead of a fast HTTP round trip.
	// Deliberately not tied to agent-runner's own
	// config.Settings.EnvironmentProvisionConcurrency (env-configurable,
	// default 5): that one bounds concurrent SandboxMgr work (the actual
	// Pod/container operations), this one bounds concurrent BRPop waiters
	// on this side. Sizing this above agent-runner's own budget is fine and
	// intentional — the excess just queues waiting on agent-runner's
	// semaphore instead of this one, which costs nothing but a few idle
	// goroutines — so the two are allowed to drift independently rather
	// than needing to match.
	environmentCommandConcurrency = 10
)

// environmentLifecycleExecutor is the minimal environmentsvc.Service
// surface this consumer needs to actually execute a previously-queued
// create/start/stop command — see environmentsvc.Service.ExecuteStart's
// own doc comment for why these methods aren't part of
// environmentdom.Service (and so aren't reachable through the
// notificationdom.Service-style full-domain-interface pattern other
// consumers in this package use).
type environmentLifecycleExecutor interface {
	ExecuteCreate(ctx context.Context, environmentID uuid.UUID) error
	ExecuteStart(ctx context.Context, environmentID uuid.UUID) error
	ExecuteStop(ctx context.Context, environmentID uuid.UUID) error
}

// EnvironmentCommandConsumer reads queued environment lifecycle commands —
// "create", "start", and "stop" — from StreamEnvironmentCommands and
// executes them. This is the asynchronous counterpart to
// environmentsvc.Service.CreateEnvironment/StartEnvironment/
// StopEnvironment, which only validate and queue: moving the actual
// (potentially slow) agent-runner call here means the HTTP request that
// triggered it returns immediately regardless of how long provisioning/
// starting/stopping the backing container/Pod takes, instead of holding
// the connection open for it — which, past the server's own WriteTimeout,
// showed up to callers as a 502 for a start that was actually still
// succeeding server-side a few seconds later.
type EnvironmentCommandConsumer struct {
	client       *redis.Client
	svc          environmentLifecycleExecutor
	log          *slog.Logger
	consumerName string // unique per instance, derived from hostname
	stopCh       chan struct{}
	doneCh       chan struct{}
	// sem bounds concurrent handle calls dispatched from run()'s live read
	// loop — see environmentCommandConcurrency's own doc comment.
	sem chan struct{}
	// envLocks serializes handle calls for the same environment_id (across
	// concurrent goroutines now that run() dispatches them instead of
	// calling handle inline) — see handle's own doc comment for why.
	envLocks *environmentLocks
	// wg tracks handle goroutines dispatched by run() (never processPending's
	// own synchronous calls, which are always finished long before Stop
	// could run) so Stop can give them a real chance to finish instead of
	// the process exiting out from under them — see Stop's own doc comment.
	wg sync.WaitGroup
}

// NewEnvironmentCommandConsumer creates a consumer that is ready to be
// started. The consumer name is derived from the hostname so it is unique
// per pod/instance.
func NewEnvironmentCommandConsumer(client *redis.Client, svc environmentLifecycleExecutor, log *slog.Logger) *EnvironmentCommandConsumer {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = uuid.New().String()
	}
	return &EnvironmentCommandConsumer{
		client:       client,
		svc:          svc,
		log:          log,
		consumerName: fmt.Sprintf("%s.%s", environmentCommandConsumerGroup, hostname),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		sem:          make(chan struct{}, environmentCommandConcurrency),
		envLocks:     newEnvironmentLocks(),
	}
}

// Start creates the consumer group if needed, then begins reading from the
// stream in a background goroutine. Call Stop to drain and exit cleanly.
func (c *EnvironmentCommandConsumer) Start(ctx context.Context) {
	// "0": first-ever creation processes any command that arrived before
	// the group existed (see ensureGroup's own doc comment on the
	// distinction from the NOGROUP recovery path in run() below).
	if err := c.ensureGroup(ctx, "0"); err != nil {
		c.log.Warn("environment command consumer: could not create consumer group, will retry on first read", "err", err)
	}

	go c.run()
}

// ensureGroup creates the consumer group at startID if it doesn't already
// exist. MKSTREAM ensures the stream key is created if it doesn't exist
// yet. startID is "0" only for Start's own first-ever creation — the
// NOGROUP recovery path in run() passes "$" instead, since recreating at
// "0" there would redeliver every command still retained on the stream
// (including ones already executed before the group vanished) with no
// idempotency guard: re-executing an old "start" is at worst a harmless
// no-op against an already-running environment, but re-executing an old
// "stop" is not — the environment could have been legitimately restarted
// in the meantime, and a stale replay would stop it again out from under
// whoever just started it. "$" (from now) avoids both concerns.
func (c *EnvironmentCommandConsumer) ensureGroup(ctx context.Context, startID string) error {
	err := c.client.XGroupCreateMkStream(ctx, events.StreamEnvironmentCommands, environmentCommandConsumerGroup, startID).Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// environmentCommandDrainTimeout bounds how long Stop will wait for
// in-flight handle goroutines before giving up on them — see Stop's own
// doc comment for why it waits at all. Deliberately NOT the full remaining
// shutdown budget bootstrap.App.Shutdown's caller passes down to
// a.server.Shutdown(ctx) (10s as of main.go) — Stop takes no ctx and isn't
// given a share of that deadline, so a slow drain here can never crowd out
// the HTTP server's own graceful drain, which runs right after every
// consumer's Stop returns. Kept short enough to leave that server drain a
// real remainder of a typical shutdown budget, long enough that a
// same-environment BRPop wait that's already close to done (agent-runner's
// own operations are usually fast — see aiAgentProvisionHTTPTimeout's own
// doc comment) gets to actually finish rather than being cut off on
// principle. If bootstrap.App.Shutdown's own budget is ever changed, revisit
// this alongside it.
const environmentCommandDrainTimeout = 5 * time.Second

// Stop signals the consumer to stop reading new messages, then waits — up
// to environmentCommandDrainTimeout — for handle goroutines run() already
// dispatched to finish, so a routine graceful shutdown (a rolling deploy,
// most commonly) gives an already-accepted create/start/restart-ports
// command a real chance to receive agent-runner's reply and persist its
// outcome, instead of the process exiting out from under it the instant
// stopCh closes. That exit-out-from-under-it failure mode is exactly what
// an earlier version of this method allowed: closing stopCh and waiting
// only on doneCh (closed by run() as soon as its own read loop returns)
// said nothing about the goroutines that loop had already fired off via "go
// c.handle(msg)" — with create/start/restart-ports now blocking on a
// multi-minute BRPop (see callEnvironmentCommand's own doc comment) rather
// than a sub-second HTTP call, that gap was big enough to hit on an
// ordinary deploy, not just a hard kill.
//
// If the deadline arrives first, still-running handlers are abandoned
// mid-flight (not cancelled — handle's own ctx is context.Background(), by
// design, so a caller giving up can't abort an in-flight backend mutation
// any more than agent-runner's own consumer lets that happen — see that
// consumer's identical reasoning). Their stream messages stay unacked in
// this consumer's own PEL; because consumerName is hostname-derived, a
// replacement pod started under a new hostname will NOT automatically pick
// those up (this package has no XAUTOCLAIM-style reclaim of another
// consumer's PEL) — logged here so that's visible rather than silent.
func (c *EnvironmentCommandConsumer) Stop() {
	close(c.stopCh)
	<-c.doneCh

	drained := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(drained)
	}()

	select {
	case <-drained:
	case <-time.After(environmentCommandDrainTimeout):
		c.log.Warn("environment command consumer: drain timeout reached before all in-flight commands finished — remaining commands were abandoned mid-flight, their stream messages left unacked in this consumer's own PEL")
	}
}

// run is the main loop executed in a goroutine by Start.
//
// Dispatches each message into its own goroutine (bounded by sem) rather
// than calling handle inline: create/start/restart-ports now block on a
// multi-minute BRPop waiting for agent-runner's reply (see
// environmentsvc.Service.callEnvironmentCommand's own doc comment), where
// the old direct-HTTP path returned in well under a second in the common
// case. Handling messages one at a time here — fine when every call was a
// fast round trip — would otherwise let one legitimate-but-slow
// provisioning stall every later command on this stream for minutes,
// including a fast stop for a completely different environment. Not
// applied to processPending below: that only ever runs once, synchronously,
// at startup, draining an already-known-finite backlog — the "one at a
// time" behavior there was never the bottleneck this fixes.
func (c *EnvironmentCommandConsumer) run() {
	defer close(c.doneCh)
	c.log.Info("environment command consumer: started", "stream", events.StreamEnvironmentCommands)

	// On startup, replay any pending messages (PEL) that were delivered but
	// never acknowledged (e.g. after a crash mid-execution). "0" fetches
	// the backlog.
	c.processPending(context.Background())

	for {
		select {
		case <-c.stopCh:
			c.log.Info("environment command consumer: stopping")
			return
		default:
		}

		ctx, cancel := context.WithTimeout(context.Background(), environmentCommandReadBlock+time.Second)
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    environmentCommandConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamEnvironmentCommands, ">"},
			Count:    environmentCommandReadCount,
			Block:    environmentCommandReadBlock,
		}).Result()
		cancel()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			c.log.Error("environment command consumer: xreadgroup error", "err", err)
			if strings.Contains(err.Error(), "NOGROUP") {
				recoverCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				geErr := c.ensureGroup(recoverCtx, "$")
				cancel()
				if geErr != nil {
					c.log.Warn("environment command consumer: failed to recreate consumer group", "err", geErr)
				}
			}
			time.Sleep(2 * time.Second)
			continue
		}

		for _, stream := range msgs {
			for _, msg := range stream.Messages {
				// Add before the goroutine starts, not inside handle itself:
				// Stop's wg.Wait() must never observe a count of 0 while a
				// just-dispatched handle is still merely queued to run, which
				// an Add from inside the new goroutine couldn't guarantee.
				c.wg.Add(1)
				go func(msg redis.XMessage) {
					defer c.wg.Done()
					c.handle(msg)
				}(msg)
			}
		}
	}
}

// processPending re-delivers and acknowledges any messages in the PEL that
// were not acked during a previous run. Loops in batches of
// environmentCommandReadCount rather than reading once: each XReadGroup
// call from ID "0" starts at the head of the PEL, so once a batch is
// handled (and acked — see handle's own doc comment) the next call
// naturally advances to whatever's left. Without the loop, a backlog of
// more than one batch would leave the excess pending indefinitely — run()
// switches to reading only new messages (">") right after this returns, so
// nothing else would ever revisit them until the next process restart.
func (c *EnvironmentCommandConsumer) processPending(ctx context.Context) {
	for {
		msgs, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    environmentCommandConsumerGroup,
			Consumer: c.consumerName,
			Streams:  []string{events.StreamEnvironmentCommands, "0"},
			Count:    environmentCommandReadCount,
		}).Result()
		if err != nil && err != redis.Nil {
			c.log.Warn("environment command consumer: could not read pending messages", "err", err)
			return
		}
		delivered := 0
		for _, stream := range msgs {
			for _, msg := range stream.Messages {
				c.handle(msg)
				delivered++
			}
		}
		if delivered < environmentCommandReadCount {
			return
		}
	}
}

// handle deserialises one stream message and executes the command it
// carries. Always acks, regardless of outcome: ExecuteCreate/ExecuteStart/
// ExecuteStop already persist StatusError with the failure reason
// directly onto the environment row on failure, so there is nothing left
// in the PEL worth retrying automatically — a user sees the error status
// and retries
// explicitly, which queues a fresh command.
//
// Called from both run() (concurrently, one goroutine per message) and
// processPending (synchronously, in a loop) — acquires sem and envLocks
// unconditionally either way, since doing so under processPending's
// already-sequential calls is harmless (no contention, negligible
// overhead), and keeping the acquisition inside handle itself means
// neither caller has to know which concurrency model it's running under.
// The environment_id lock specifically guards against two commands for
// the very same environment now running on different goroutines at once
// (only possible via run(), never processPending) — ExecuteCreate/
// ExecuteStart/ExecuteStop each re-check the row's current status before
// acting, but that check-then-act is only race-free if two such checks
// for the same row can never interleave; mirrors agent-runner's
// messaging.Consumer serializing trigger handling per conversation_id for
// the identical reason.
func (c *EnvironmentCommandConsumer) handle(msg redis.XMessage) {
	ctx := context.Background()

	eventType, _ := msg.Values["type"].(string)
	raw, ok := msg.Values["payload"].(string)
	if !ok {
		c.log.Warn("environment command consumer: message has no payload field", "id", msg.ID)
		c.ack(ctx, msg.ID)
		return
	}

	var p environmentCommandPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		c.log.Warn("environment command consumer: failed to decode payload", "id", msg.ID, "err", err)
		c.ack(ctx, msg.ID)
		return
	}
	environmentID, err := uuid.Parse(p.EnvironmentID)
	if err != nil {
		c.log.Warn("environment command consumer: invalid environment_id", "id", msg.ID, "environment_id", p.EnvironmentID)
		c.ack(ctx, msg.ID)
		return
	}

	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	unlock := c.envLocks.Lock(environmentID)
	defer unlock()

	switch eventType {
	case events.TopicEnvironmentCreate:
		if err := c.svc.ExecuteCreate(ctx, environmentID); err != nil {
			c.log.Error("environment command consumer: create failed", "environment_id", environmentID, "err", err)
		}
	case events.TopicEnvironmentStart:
		if err := c.svc.ExecuteStart(ctx, environmentID); err != nil {
			c.log.Error("environment command consumer: start failed", "environment_id", environmentID, "err", err)
		}
	case events.TopicEnvironmentStop:
		if err := c.svc.ExecuteStop(ctx, environmentID); err != nil {
			c.log.Error("environment command consumer: stop failed", "environment_id", environmentID, "err", err)
		}
	default:
		c.log.Warn("environment command consumer: unknown event type", "id", msg.ID, "type", eventType)
	}

	c.ack(ctx, msg.ID)
}

func (c *EnvironmentCommandConsumer) ack(ctx context.Context, id string) {
	if err := c.client.XAck(ctx, events.StreamEnvironmentCommands, environmentCommandConsumerGroup, id).Err(); err != nil {
		c.log.Warn("environment command consumer: xack failed", "id", id, "err", err)
	}
}

// environmentCommandPayload mirrors the JSON shape both
// environmentsvc.Service.StartEnvironment and StopEnvironment publish to
// StreamEnvironmentCommands — every command here concerns exactly one
// environment, so environment_id is all either one carries.
type environmentCommandPayload struct {
	EnvironmentID string `json:"environment_id"`
}

// environmentLocks is a per-environment-ID mutex: independent environments
// never contend with each other, but two commands for the same one are
// serialized — see handle's own doc comment for why run()'s new
// goroutine-per-message dispatch needs this. Functionally identical to
// agent-runner's internal/convlock.Locks (a separate Go module, so
// duplicated here rather than shared) for the same reason that package
// exists: ref-counted so it never grows unbounded with environment-ID
// history — only IDs with a lock actually held (or queued) have an entry.
type environmentLocks struct {
	mu    sync.Mutex
	locks map[uuid.UUID]*refCountedMutex
}

type refCountedMutex struct {
	mu   sync.Mutex
	refs int
}

func newEnvironmentLocks() *environmentLocks {
	return &environmentLocks{locks: make(map[uuid.UUID]*refCountedMutex)}
}

// Lock blocks until key's lock is free, then returns an unlock function the
// caller must call exactly once (typically via defer) to release it.
func (l *environmentLocks) Lock(key uuid.UUID) (unlock func()) {
	l.mu.Lock()
	e, ok := l.locks[key]
	if !ok {
		e = &refCountedMutex{}
		l.locks[key] = e
	}
	e.refs++
	l.mu.Unlock()

	e.mu.Lock()

	return func() {
		e.mu.Unlock()
		l.mu.Lock()
		e.refs--
		if e.refs == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
}
