package worker

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

type fakeTaskCreator struct {
	created []taskdom.CreateTaskInput
}

func (f *fakeTaskCreator) CreateTask(_ context.Context, in taskdom.CreateTaskInput) (*taskdom.Task, error) {
	f.created = append(f.created, in)
	return &taskdom.Task{ID: uuid.New(), Title: in.Title}, nil
}

func TestResolveTargetTasks_Siblings_ExcludesSelfAndHandlesNoParent(t *testing.T) {
	parentID := uuid.New()
	self := &taskdom.Task{ID: uuid.New(), ParentTaskID: &parentID}
	sib1 := &taskdom.Task{ID: uuid.New(), ParentTaskID: &parentID}
	sib2 := &taskdom.Task{ID: uuid.New(), ParentTaskID: &parentID}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{
		children: map[uuid.UUID][]*taskdom.Task{parentID: {sib1, self, sib2}},
	}}
	target := &automationdom.TaskTarget{Kind: automationdom.TaskTargetSiblings}

	got, err := c.resolveTargetTasks(context.Background(), uuid.New(), self, target)
	if err != nil || len(got) != 2 || got[0] != sib1 || got[1] != sib2 {
		t.Fatalf("expected [sib1 sib2], got %v, %v", got, err)
	}

	got, err = c.resolveTargetTasks(context.Background(), uuid.New(), &taskdom.Task{ID: uuid.New()}, target)
	if err != nil || len(got) != 0 {
		t.Fatalf("expected no siblings for a parentless task, got %v, %v", got, err)
	}
	if !automationdom.TaskTargetSiblings.MultiValued() {
		t.Fatal("siblings must be multi-valued")
	}
}

func TestApplyCreateTask_RendersTitleAsChildOfParent(t *testing.T) {
	creator := &fakeTaskCreator{}
	parent := &taskdom.Task{ID: uuid.New(), Title: "Login page"}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{}, taskCreator: creator}
	prio := 2

	applied, err := c.applyCreateTask(context.Background(), uuid.New(), parent, parent,
		&automationdom.TaskCreate{Title: "{{task.title}} - Review", Importance: &prio, Tags: []string{"auto"}},
		"auto", map[string]string{"task.title": parent.Title})
	if err != nil || !applied {
		t.Fatalf("expected applied, got %v, %v", applied, err)
	}
	if len(creator.created) != 1 {
		t.Fatalf("expected 1 create, got %d", len(creator.created))
	}
	in := creator.created[0]
	if in.Title != "Login page - Review" || in.ParentTaskID == nil || *in.ParentTaskID != parent.ID || in.Importance != 2 {
		t.Fatalf("unexpected input: %+v", in)
	}
}

func TestApplyCreateTask_PassesEveryConfiguredField(t *testing.T) {
	creator := &fakeTaskCreator{}
	parent := &taskdom.Task{ID: uuid.New()}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{}, taskCreator: creator}
	typeID, statusID, sprintID, reporterID, assignee := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pts := 5
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	due := start.Add(48 * time.Hour)
	desc := json.RawMessage(`[{"type":"paragraph"}]`)

	_, err := c.applyCreateTask(context.Background(), uuid.New(), parent, parent, &automationdom.TaskCreate{
		Title: "t", TaskTypeID: &typeID, StatusID: &statusID, SprintID: &sprintID, Description: desc,
		StoryPoints: &pts, AssigneeIDs: []uuid.UUID{assignee}, ReporterID: &reporterID,
		CustomFields: map[string]any{"k": "v"}, StartDate: &start, DueDate: &due, Tags: []string{"x"},
	}, "a", nil)
	if err != nil || len(creator.created) != 1 {
		t.Fatalf("unexpected: %v %d", err, len(creator.created))
	}
	in := creator.created[0]
	if in.TaskTypeID != &typeID || in.StatusID != &statusID || in.SprintID != &sprintID || in.ReporterID != &reporterID ||
		string(in.Description) != string(desc) || *in.StoryPoints != 5 || in.AssigneeIDs[0] != assignee ||
		in.CustomFields["k"] != "v" || !in.StartDate.Equal(start) || !in.DueDate.Equal(due) || in.Tags[0] != "x" {
		t.Fatalf("fields not passed through: %+v", in)
	}
}

func TestApplyCreateTask_IdempotentWhenChildWithSameTitleExists(t *testing.T) {
	creator := &fakeTaskCreator{}
	parent := &taskdom.Task{ID: uuid.New(), Title: "Login page"}
	c := &AutomationConsumer{taskCreator: creator, taskRepo: &fakeAutomationTaskReader{
		children: map[uuid.UUID][]*taskdom.Task{parent.ID: {{ID: uuid.New(), Title: "Login page - Review"}}},
	}}
	applied, err := c.applyCreateTask(context.Background(), uuid.New(), parent, parent,
		&automationdom.TaskCreate{Title: "{{task.title}} - Review"}, "auto", map[string]string{"task.title": parent.Title})
	if err != nil || applied || len(creator.created) != 0 {
		t.Fatalf("expected no-op, got applied=%v err=%v created=%d", applied, err, len(creator.created))
	}
}

func TestApplyCreateTask_Errors(t *testing.T) {
	parent := &taskdom.Task{ID: uuid.New()}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{}}
	if _, err := c.applyCreateTask(context.Background(), uuid.New(), parent, parent, &automationdom.TaskCreate{Title: "x"}, "a", nil); err == nil {
		t.Fatal("expected error when no task creator is configured")
	}
	c.taskCreator = &fakeTaskCreator{}
	if _, err := c.applyCreateTask(context.Background(), uuid.New(), parent, parent, &automationdom.TaskCreate{Title: "  "}, "a", nil); err == nil {
		t.Fatal("expected error for blank title")
	}
	if _, err := c.applyCreateTask(context.Background(), uuid.New(), parent, parent, nil, "a", nil); err == nil {
		t.Fatal("expected error for nil config")
	}
}
