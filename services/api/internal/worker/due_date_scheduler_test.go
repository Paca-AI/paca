package worker

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// fakeDueDateRepo implements automationGraphReader with just enough behavior
// to drive DueDateScheduler.tick and the executeRun call it makes on a
// match — a single fixed automation/candidate list, no-op run bookkeeping.
type fakeDueDateRepo struct {
	candidates      []automationdom.DueDateCandidate
	automation      *automationdom.Automation
	recordFireCalls []uuid.UUID
}

func (f *fakeDueDateRepo) ListEnabledTriggerNodesByType(context.Context, uuid.UUID, automationdom.TriggerType) ([]*automationdom.Node, error) {
	return nil, nil
}
func (f *fakeDueDateRepo) ListPredecessorTriggersWatching(context.Context, uuid.UUID) ([]*automationdom.Node, error) {
	return nil, nil
}
func (f *fakeDueDateRepo) FindAutomationByNodeID(context.Context, uuid.UUID) (*automationdom.Automation, error) {
	return f.automation, nil
}
func (f *fakeDueDateRepo) FindAutomationByID(context.Context, uuid.UUID) (*automationdom.Automation, error) {
	return f.automation, nil
}
func (f *fakeDueDateRepo) FindNodeByID(context.Context, uuid.UUID) (*automationdom.Node, error) {
	return nil, automationdom.ErrNodeNotFound
}
func (f *fakeDueDateRepo) LoadGraph(context.Context, uuid.UUID) (*automationdom.Graph, error) {
	return &automationdom.Graph{Automation: f.automation}, nil
}
func (f *fakeDueDateRepo) CreateRun(context.Context, *automationdom.Run) error { return nil }
func (f *fakeDueDateRepo) UpdateRun(context.Context, *automationdom.Run) error { return nil }
func (f *fakeDueDateRepo) CreateRunStep(context.Context, *automationdom.RunStep) error {
	return nil
}
func (f *fakeDueDateRepo) ListRunStepsByRun(context.Context, uuid.UUID) ([]*automationdom.RunStep, error) {
	return nil, nil
}
func (f *fakeDueDateRepo) CreatePendingAgentWait(context.Context, *automationdom.PendingAgentWait) error {
	return nil
}
func (f *fakeDueDateRepo) FindPendingAgentWait(context.Context, uuid.UUID) (*automationdom.PendingAgentWait, error) {
	return nil, nil
}
func (f *fakeDueDateRepo) DeletePendingAgentWait(context.Context, uuid.UUID) error {
	return nil
}
func (f *fakeDueDateRepo) CountPendingAgentWaits(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakeDueDateRepo) DeletePendingAgentWaitAndCountRemaining(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakeDueDateRepo) CreatePendingDelay(context.Context, *automationdom.PendingDelay) error {
	return nil
}
func (f *fakeDueDateRepo) ListDueDelays(context.Context) ([]*automationdom.PendingDelay, error) {
	return nil, nil
}
func (f *fakeDueDateRepo) DeletePendingDelay(context.Context, uuid.UUID) error {
	return nil
}
func (f *fakeDueDateRepo) CountPendingDelays(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (f *fakeDueDateRepo) ListDueDateCandidates(context.Context) ([]automationdom.DueDateCandidate, error) {
	return f.candidates, nil
}
func (f *fakeDueDateRepo) RecordDueDateFire(_ context.Context, _, _, taskID uuid.UUID) error {
	f.recordFireCalls = append(f.recordFireCalls, taskID)
	return nil
}
func (f *fakeDueDateRepo) ListCronCandidates(context.Context) ([]automationdom.CronCandidate, error) {
	return nil, nil
}
func (f *fakeDueDateRepo) RecordCronFire(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

type fakeDueDateTaskReader struct {
	task *taskdom.Task
}

func (f *fakeDueDateTaskReader) FindTaskByID(context.Context, uuid.UUID) (*taskdom.Task, error) {
	return f.task, nil
}
func (f *fakeDueDateTaskReader) FindTaskStatusByID(context.Context, uuid.UUID) (*taskdom.TaskStatus, error) {
	return nil, nil
}
func (f *fakeDueDateTaskReader) ListChildTasks(context.Context, uuid.UUID, uuid.UUID) ([]*taskdom.Task, error) {
	return nil, nil
}
func (f *fakeDueDateTaskReader) ListTaskLinks(context.Context, uuid.UUID) ([]*taskdom.TaskLink, error) {
	return nil, nil
}

func newTestDueDateScheduler(t *testing.T, repo *fakeDueDateRepo, taskReader automationTaskReader) (*DueDateScheduler, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	consumer := NewAutomationConsumer(client, repo, taskReader, &fakeTaskUpdater{}, nil, nil, discardLogger())
	return NewDueDateScheduler(client, consumer, discardLogger()), client
}

func newDueDateNode() *automationdom.Node {
	return &automationdom.Node{
		ID:   uuid.New(),
		Kind: automationdom.KindTrigger,
		Type: string(automationdom.TriggerDueDateReached),
	}
}

func TestDueDateScheduler_Tick_FiresDueCandidate(t *testing.T) {
	taskID := uuid.New()
	node := newDueDateNode()
	repo := &fakeDueDateRepo{
		candidates: []automationdom.DueDateCandidate{{Node: node, TaskID: taskID}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestDueDateScheduler(t, repo, &fakeDueDateTaskReader{task: &taskdom.Task{ID: taskID}})
	defer func() { _ = client.Close() }()

	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 1 || repo.recordFireCalls[0] != taskID {
		t.Fatalf("expected exactly one fire recorded for task %s, got %v", taskID, repo.recordFireCalls)
	}
}

func TestDueDateScheduler_Tick_SkipsWhenAnotherReplicaHoldsTheLock(t *testing.T) {
	taskID := uuid.New()
	node := newDueDateNode()
	repo := &fakeDueDateRepo{
		candidates: []automationdom.DueDateCandidate{{Node: node, TaskID: taskID}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestDueDateScheduler(t, repo, &fakeDueDateTaskReader{task: &taskdom.Task{ID: taskID}})
	defer func() { _ = client.Close() }()

	// Simulate another replica already holding the lock for this tick.
	if err := client.SetArgs(context.Background(), dueDateSchedulerLeaderKey, "1", redis.SetArgs{TTL: time.Minute, Mode: "NX"}).Err(); err != nil {
		t.Fatalf("setup: seed leader lock: %v", err)
	}

	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 0 {
		t.Fatalf("expected tick to skip while another replica holds the lock, got %d fires", len(repo.recordFireCalls))
	}
}

func TestDueDateScheduler_Tick_ReleasesLockAfterProcessingSoTheNextTickCanFire(t *testing.T) {
	taskID := uuid.New()
	node := newDueDateNode()
	repo := &fakeDueDateRepo{
		candidates: []automationdom.DueDateCandidate{{Node: node, TaskID: taskID}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestDueDateScheduler(t, repo, &fakeDueDateTaskReader{task: &taskdom.Task{ID: taskID}})
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
