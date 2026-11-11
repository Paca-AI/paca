package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/domain/changeoutbox"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

const (
	// changeReconcilerGrace is how old an outbox row must be before it is
	// looked at: a change is announced just after its commit, so a row this
	// young is almost certainly still being announced.
	changeReconcilerGrace = 30 * time.Second
	// changeReconcilerSlack tolerates clock difference between the database
	// that stamps a row and the process that stamps the announcement. It only
	// matters for an announcement that carries no row version to compare.
	changeReconcilerSlack = 2 * time.Second

	// changeReconcilerMaxAge bounds the outbox: a row this old is dropped
	// unhandled. It is the change log's own retention (messaging.ChangeLogTTL):
	// past it the log holds nothing to compare against, and a client that was
	// away that long resets anyway. Without it a long Valkey outage would let
	// the table grow with every write.
	changeReconcilerMaxAge   = messaging.ChangeLogTTL
	changeReconcilerPruneGap = time.Minute

	changeReconcilerInterval = 5 * time.Second
	changeReconcilerBatch    = 1000
)

// ChangeLogReader says when an entity's last whole-entity change was announced
// on the realtime change log. *messaging.Publisher implements it.
type ChangeLogReader interface {
	LastLogged(ctx context.Context, projectID string, entityIDs []string) (map[string]messaging.Logged, error)
}

// ChangeRepair is what a loader returns for an entity: found says whether it
// still exists (a deleted one is announced as deleted), snapshot is what a
// client patches its cache with (nil when none is attached), and topic names
// the project-scoped event to announce.
type ChangeRepair struct {
	Found    bool
	Snapshot any
}

// ChangeLoader loads an entity's current state for a repair.
type ChangeLoader func(ctx context.Context, id uuid.UUID) (ChangeRepair, error)

// ChangeReconciler closes the one gap of the realtime change feed that the
// feed itself cannot: a change is announced just after its database commit, so
// a process that dies in between commits a change nobody hears of. Triggers
// record each change of tasks and sprints in change_outbox inside the
// committing transaction (migration 000083); the reconciler looks at every row
// once it is old enough that its announcement should be done, checks whether
// the entity was announced since, and announces it (with its current state)
// when it was not. Rows are deleted once handled, so the table stays small.
//
// The announcement made here is a plain "<entity>.updated" or ".deleted" with
// no actor, no diff and no activity entry: it repairs delivery to live clients,
// not the activity log. A client that gets one refetches what a change of that
// entity could have moved, which is what it does for any change it cannot
// place. It also makes visible changes no code path announces one by one, such
// as the tasks a completed sprint moves.
type ChangeReconciler struct {
	outbox  changeoutbox.Repository
	log     ChangeLogReader
	sink    events.RealtimeSink
	loaders map[string]ChangeLoader
	logger  *slog.Logger

	interval  time.Duration
	grace     time.Duration
	maxAge    time.Duration
	lastPrune time.Time
	now       func() time.Time
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewChangeReconciler returns a reconciler. loaders maps an outbox entity type
// (changeoutbox.EntityTask, ...) to the function that loads it.
func NewChangeReconciler(outbox changeoutbox.Repository, log ChangeLogReader, sink events.RealtimeSink, loaders map[string]ChangeLoader, logger *slog.Logger) *ChangeReconciler {
	return &ChangeReconciler{
		outbox: outbox, log: log, sink: sink, loaders: loaders, logger: logger,
		interval: changeReconcilerInterval, grace: changeReconcilerGrace, maxAge: changeReconcilerMaxAge, now: time.Now,
		stopCh: make(chan struct{}), doneCh: make(chan struct{}),
	}
}

// WithTiming overrides the poll interval and grace period (tests).
func (c *ChangeReconciler) WithTiming(interval, grace time.Duration) *ChangeReconciler {
	c.interval, c.grace = interval, grace
	return c
}

// Start polls in a background goroutine; Stop ends it.
func (c *ChangeReconciler) Start(ctx context.Context) {
	go c.run(ctx)
}

// Stop ends the poll loop and waits for it.
func (c *ChangeReconciler) Stop() {
	close(c.stopCh)
	<-c.doneCh
}

func (c *ChangeReconciler) run(ctx context.Context) {
	defer close(c.doneCh)
	t := time.NewTicker(c.interval)
	defer t.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-t.C:
			// Drain: keep claiming while a full batch comes back.
			for {
				n, err := c.Tick(ctx)
				if err != nil {
					c.logger.Warn("change reconciler: tick failed", "err", err)
					break
				}
				if n < changeReconcilerBatch {
					break
				}
			}
		}
	}
}

// Tick claims one batch of old enough rows and reconciles them. It returns how
// many rows it handled.
func (c *ChangeReconciler) Tick(ctx context.Context) (int, error) {
	c.prune(ctx)
	olderThan := c.now().Add(-c.grace)
	return c.outbox.Claim(ctx, olderThan, changeReconcilerBatch, func(rows []changeoutbox.Row) error {
		return c.reconcile(ctx, rows)
	})
}

// prune drops rows older than maxAge, at most once a minute. It runs before
// the claim so it still bounds the table while claiming fails every tick (the
// change log unreachable).
func (c *ChangeReconciler) prune(ctx context.Context) {
	now := c.now()
	if now.Sub(c.lastPrune) < changeReconcilerPruneGap {
		return
	}
	c.lastPrune = now
	n, err := c.outbox.Prune(ctx, now.Add(-c.maxAge))
	if err != nil {
		c.logger.Warn("change reconciler: prune failed", "err", err)
		return
	}
	if n > 0 {
		c.logger.Warn("change reconciler: dropped outbox rows that were never reconciled", "rows", n, "older_than", c.maxAge)
	}
}

type entityKey struct {
	project uuid.UUID
	kind    string
	id      uuid.UUID
}

func (c *ChangeReconciler) reconcile(ctx context.Context, rows []changeoutbox.Row) error {
	// One decision per entity: the newest row (highest id, the order Claim
	// reads them in) says what state it is in.
	latest := map[entityKey]changeoutbox.Row{}
	order := make([]entityKey, 0, len(rows))
	for _, r := range rows {
		k := entityKey{r.ProjectID, r.EntityType, r.EntityID}
		prev, seen := latest[k]
		if !seen {
			order = append(order, k)
		}
		if !seen || r.ID > prev.ID {
			latest[k] = r
		}
	}

	// What was announced, per project, in one read each.
	byProject := map[uuid.UUID][]string{}
	for _, k := range order {
		byProject[k.project] = append(byProject[k.project], k.id.String())
	}
	logged := map[uuid.UUID]map[string]messaging.Logged{}
	for project, ids := range byProject {
		m, err := c.log.LastLogged(ctx, project.String(), ids)
		if err != nil {
			// Valkey unreachable: nothing is deleted, the rows are claimed again.
			return err
		}
		logged[project] = m
	}

	for _, k := range order {
		if announced(latest[k], logged[k.project][k.id.String()]) {
			continue
		}
		c.repair(ctx, k)
	}
	return nil
}

// announced reports whether the logged change of an entity covers the database
// change in row. By row version when both sides have one: the log carried a state
// at least as new as the row, with no clock involved. Otherwise by time: it was
// logged after the change (within a small slack for clock difference).
func announced(row changeoutbox.Row, l messaging.Logged) bool {
	if l.AtMs == 0 && l.Version == 0 {
		return false // never logged
	}
	if l.Version == messaging.DeletedVersion {
		return true
	}
	if l.Version != messaging.UnknownVersion && row.Version != nil {
		return l.Version >= *row.Version
	}
	return l.AtMs >= row.CreatedAt.Add(-changeReconcilerSlack).UnixMilli()
}

// repair announces an entity whose change was never announced. A loader error
// is logged and dropped: retrying a row that cannot be loaded would loop forever.
func (c *ChangeReconciler) repair(ctx context.Context, k entityKey) {
	load, ok := c.loaders[k.kind]
	if !ok {
		return
	}
	state, err := load(ctx, k.id)
	if err != nil {
		c.logger.Warn("change reconciler: could not load entity", "kind", k.kind, "id", k.id, "err", err)
		return
	}
	events.PublishRealtime(ctx, c.sink, repairEvent(k, state))
	c.logger.Info("change reconciler: announced a change that was never announced", "kind", k.kind, "id", k.id, "project", k.project)
}

// repairEvent is the announcement for an entity found in the given state.
func repairEvent(k entityKey, state ChangeRepair) events.Event {
	id := k.id
	payload := map[string]any{"project_id": k.project.String(), "repair": true}
	e := events.Event{
		ProjectID: k.project, EntityID: &id, Origin: events.OriginSystem, Payload: payload,
	}
	suffix := "updated"
	if !state.Found {
		suffix = "deleted"
	} else {
		e.Snapshot = state.Snapshot
	}
	switch k.kind {
	case changeoutbox.EntityTask:
		e.Topic, e.EntityType, e.TaskID = "task."+suffix, events.EntityTask, &id
		payload["task_id"] = id.String()
	case changeoutbox.EntitySprint:
		e.Topic, e.EntityType = "sprint."+suffix, events.EntitySprint
		payload["sprint_id"] = id.String()
	}
	return e
}
