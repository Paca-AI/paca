package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// fakeCronRepo implements automationGraphReader with just enough behavior to
// drive CronScheduler.tick and the executeRun call it makes on a match — a
// single fixed automation/candidate list, no-op run bookkeeping.
type fakeCronRepo struct {
	candidates      []automationdom.CronCandidate
	automation      *automationdom.Automation
	recordFireCalls []time.Time
	// createdRunSteps captures every CreateRunStep call, in order — used by
	// tests that need to observe a walk's outcome now that walker no longer
	// carries an in-memory failed flag (see automation_consumer_test.go's
	// nil-task defense-in-depth tests).
	createdRunSteps []*automationdom.RunStep
}

func (f *fakeCronRepo) ListEnabledTriggerNodesByType(context.Context, uuid.UUID, automationdom.TriggerType) ([]*automationdom.Node, error) {
	return nil, nil
}
func (f *fakeCronRepo) ListPredecessorTriggersWatching(context.Context, uuid.UUID) ([]*automationdom.Node, error) {
	return nil, nil
}
func (f *fakeCronRepo) FindAutomationByNodeID(context.Context, uuid.UUID) (*automationdom.Automation, error) {
	return f.automation, nil
}
func (f *fakeCronRepo) FindAutomationByID(context.Context, uuid.UUID) (*automationdom.Automation, error) {
	return f.automation, nil
}
func (f *fakeCronRepo) FindNodeByID(context.Context, uuid.UUID) (*automationdom.Node, error) {
	return nil, automationdom.ErrNodeNotFound
}
func (f *fakeCronRepo) LoadGraph(context.Context, uuid.UUID) (*automationdom.Graph, error) {
	return &automationdom.Graph{Automation: f.automation}, nil
}
func (f *fakeCronRepo) CreateRun(context.Context, *automationdom.Run) error { return nil }
func (f *fakeCronRepo) UpdateRun(context.Context, *automationdom.Run) error { return nil }
func (f *fakeCronRepo) CreateRunStep(_ context.Context, s *automationdom.RunStep) error {
	f.createdRunSteps = append(f.createdRunSteps, s)
	return nil
}
func (f *fakeCronRepo) ListRunStepsByRun(context.Context, uuid.UUID) ([]*automationdom.RunStep, error) {
	return nil, nil
}
func (f *fakeCronRepo) CreatePendingAgentWait(context.Context, *automationdom.PendingAgentWait) error {
	return nil
}
func (f *fakeCronRepo) ClaimPendingAgentWait(context.Context, uuid.UUID) (*automationdom.PendingAgentWait, error) {
	return nil, nil
}
func (f *fakeCronRepo) CountPendingAgentWaits(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakeCronRepo) CreatePendingDelay(context.Context, *automationdom.PendingDelay) error {
	return nil
}
func (f *fakeCronRepo) ClaimDueDelays(context.Context) ([]*automationdom.PendingDelay, error) {
	return nil, nil
}
func (f *fakeCronRepo) CountPendingDelays(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakeCronRepo) ListDueDateCandidates(context.Context) ([]automationdom.DueDateCandidate, error) {
	return nil, nil
}
func (f *fakeCronRepo) RecordDueDateFire(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (f *fakeCronRepo) ListCronCandidates(context.Context) ([]automationdom.CronCandidate, error) {
	return f.candidates, nil
}
func (f *fakeCronRepo) RecordCronFire(_ context.Context, _, _ uuid.UUID, firedAt time.Time) error {
	f.recordFireCalls = append(f.recordFireCalls, firedAt)
	return nil
}

type fakeCronTaskReader struct {
	task *taskdom.Task
}

func (f *fakeCronTaskReader) FindTaskByID(context.Context, uuid.UUID) (*taskdom.Task, error) {
	return f.task, nil
}
func (f *fakeCronTaskReader) FindTaskStatusByID(context.Context, uuid.UUID) (*taskdom.TaskStatus, error) {
	return nil, nil
}
func (f *fakeCronTaskReader) ListChildTasks(context.Context, uuid.UUID, uuid.UUID) ([]*taskdom.Task, error) {
	return nil, nil
}
func (f *fakeCronTaskReader) ListTaskLinks(context.Context, uuid.UUID) ([]*taskdom.TaskLink, error) {
	return nil, nil
}

func newTestCronScheduler(t *testing.T, repo *fakeCronRepo, taskReader automationTaskReader) (*CronScheduler, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	consumer := NewAutomationConsumer(client, repo, taskReader, &fakeTaskUpdater{}, nil, nil, discardLogger())
	return NewCronScheduler(client, consumer, discardLogger()), client
}

func newCronNode(cronExpr string, targetTaskID uuid.UUID, createdAt time.Time) *automationdom.Node {
	cfg, _ := json.Marshal(automationdom.TriggerConfig{CronExpression: cronExpr, TargetTaskID: &targetTaskID})
	return &automationdom.Node{
		ID:        uuid.New(),
		Kind:      automationdom.KindTrigger,
		Type:      string(automationdom.TriggerCron),
		Config:    cfg,
		CreatedAt: createdAt,
	}
}

func TestCronScheduler_Tick_FiresOverdueNodeExactlyOnce(t *testing.T) {
	targetTaskID := uuid.New()
	// Every minute, but never fired and created 24h ago: many hundreds of
	// missed occurrences on this schedule's own grid.
	node := newCronNode("* * * * *", targetTaskID, time.Now().Add(-24*time.Hour))
	repo := &fakeCronRepo{
		candidates: []automationdom.CronCandidate{{Node: node, LastFiredAt: nil}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestCronScheduler(t, repo, &fakeCronTaskReader{task: &taskdom.Task{ID: targetTaskID}})
	defer func() { _ = client.Close() }()

	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 1 {
		t.Fatalf("expected the burst of missed occurrences to collapse into exactly one fire, got %d", len(repo.recordFireCalls))
	}
	if repo.recordFireCalls[0].After(time.Now()) {
		t.Fatalf("expected the recorded fire time to be at or before now, got %v", repo.recordFireCalls[0])
	}
}

func TestCronScheduler_Tick_SkipsNodeNotYetDue(t *testing.T) {
	targetTaskID := uuid.New()
	// Once a year (Jan 1st) — anchored to "now", the next occurrence is
	// necessarily far in the future, so this tick must not fire.
	node := newCronNode("0 0 1 1 *", targetTaskID, time.Now())
	repo := &fakeCronRepo{
		candidates: []automationdom.CronCandidate{{Node: node, LastFiredAt: nil}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestCronScheduler(t, repo, &fakeCronTaskReader{task: &taskdom.Task{ID: targetTaskID}})
	defer func() { _ = client.Close() }()

	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 0 {
		t.Fatalf("expected a not-yet-due node to be skipped, got %d fires", len(repo.recordFireCalls))
	}
}

func TestCronScheduler_Tick_SkipsMisconfiguredNode(t *testing.T) {
	targetTaskID := uuid.New()
	// Missing cron_expression — same defensive skip predecessor_done uses
	// for a missing target_task_id.
	cfg, _ := json.Marshal(automationdom.TriggerConfig{TargetTaskID: &targetTaskID})
	node := &automationdom.Node{ID: uuid.New(), Kind: automationdom.KindTrigger, Type: string(automationdom.TriggerCron), Config: cfg, CreatedAt: time.Now().Add(-time.Hour)}
	repo := &fakeCronRepo{
		candidates: []automationdom.CronCandidate{{Node: node, LastFiredAt: nil}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestCronScheduler(t, repo, &fakeCronTaskReader{task: &taskdom.Task{ID: targetTaskID}})
	defer func() { _ = client.Close() }()

	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 0 {
		t.Fatalf("expected a node with no cron_expression to be skipped, got %d fires", len(repo.recordFireCalls))
	}
}

func TestCronScheduler_Tick_SkipsWhenAnotherReplicaHoldsTheLock(t *testing.T) {
	targetTaskID := uuid.New()
	node := newCronNode("* * * * *", targetTaskID, time.Now().Add(-time.Hour))
	repo := &fakeCronRepo{
		candidates: []automationdom.CronCandidate{{Node: node, LastFiredAt: nil}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestCronScheduler(t, repo, &fakeCronTaskReader{task: &taskdom.Task{ID: targetTaskID}})
	defer func() { _ = client.Close() }()

	// Simulate another replica already holding the lock for this tick.
	if err := client.SetArgs(context.Background(), cronSchedulerLeaderKey, "1", redis.SetArgs{TTL: time.Minute, Mode: "NX"}).Err(); err != nil {
		t.Fatalf("setup: seed leader lock: %v", err)
	}

	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 0 {
		t.Fatalf("expected tick to skip while another replica holds the lock, got %d fires", len(repo.recordFireCalls))
	}
}

func TestCronScheduler_Tick_ReleasesLockAfterProcessingSoTheNextTickCanFire(t *testing.T) {
	targetTaskID := uuid.New()
	node := newCronNode("* * * * *", targetTaskID, time.Now().Add(-time.Hour))
	repo := &fakeCronRepo{
		candidates: []automationdom.CronCandidate{{Node: node, LastFiredAt: nil}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestCronScheduler(t, repo, &fakeCronTaskReader{task: &taskdom.Task{ID: targetTaskID}})
	defer func() { _ = client.Close() }()

	// Two sequential ticks, one interval apart, is exactly what production
	// looks like with a single replica: the lock's TTL (2x interval) must
	// not survive past the tick that acquired it, or every subsequent real
	// tick would find the key still present and skip forever.
	scheduler.tick(context.Background())
	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 2 {
		t.Fatalf("expected the lock to be released after each tick so the next tick can also fire, got %d fires", len(repo.recordFireCalls))
	}
}
