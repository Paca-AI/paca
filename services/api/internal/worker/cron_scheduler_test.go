package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/google/uuid"
)

// fakeCronRepo implements automationGraphReader with just enough behavior to
// drive CronScheduler.tick and the executeRun call it makes on a match — a
// single fixed automation/candidate list, no-op run bookkeeping.
type fakeCronRepo struct {
	candidates      []automationdom.CronCandidate
	automation      *automationdom.Automation
	recordFireCalls []time.Time
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
func (f *fakeCronRepo) FindNodeByID(context.Context, uuid.UUID) (*automationdom.Node, error) {
	return nil, automationdom.ErrNodeNotFound
}
func (f *fakeCronRepo) LoadGraph(context.Context, uuid.UUID) (*automationdom.Graph, error) {
	return &automationdom.Graph{Automation: f.automation}, nil
}
func (f *fakeCronRepo) CreateRun(context.Context, *automationdom.Run) error { return nil }
func (f *fakeCronRepo) UpdateRun(context.Context, *automationdom.Run) error { return nil }
func (f *fakeCronRepo) CreateRunStep(context.Context, *automationdom.RunStep) error {
	return nil
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
	defer client.Close()

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
	defer client.Close()

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
	defer client.Close()

	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 0 {
		t.Fatalf("expected a node with no cron_expression to be skipped, got %d fires", len(repo.recordFireCalls))
	}
}

func TestCronScheduler_Tick_LeaderLockPreventsDoubleFireWithinSameTTLWindow(t *testing.T) {
	targetTaskID := uuid.New()
	node := newCronNode("* * * * *", targetTaskID, time.Now().Add(-time.Hour))
	repo := &fakeCronRepo{
		candidates: []automationdom.CronCandidate{{Node: node, LastFiredAt: nil}},
		automation: &automationdom.Automation{ID: uuid.New(), ProjectID: uuid.New(), Status: automationdom.StatusActive},
	}
	scheduler, client := newTestCronScheduler(t, repo, &fakeCronTaskReader{task: &taskdom.Task{ID: targetTaskID}})
	defer client.Close()

	// Two ticks in immediate succession simulate two replicas (or one
	// replica ticking faster than the lock's TTL) racing for the same
	// window — only the first should actually acquire the lock and fire.
	scheduler.tick(context.Background())
	scheduler.tick(context.Background())

	if len(repo.recordFireCalls) != 1 {
		t.Fatalf("expected the leader lock to block the second tick's fire, got %d fires", len(repo.recordFireCalls))
	}
}
