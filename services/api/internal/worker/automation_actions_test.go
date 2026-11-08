package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

type fakeActivityRec struct{ recorded []taskdom.RecordActivityInput }

func (f *fakeActivityRec) RecordActivity(_ context.Context, in taskdom.RecordActivityInput) error {
	f.recorded = append(f.recorded, in)
	return nil
}

type fakeLinkCreator struct {
	created []taskdom.CreateTaskLinkInput
	err     error
}

func (f *fakeLinkCreator) CreateTaskLink(_ context.Context, in taskdom.CreateTaskLinkInput) (*taskdom.TaskLink, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.created = append(f.created, in)
	return &taskdom.TaskLink{}, nil
}

type fakeNotifier struct {
	got []notificationdom.NotifyAutomationInput
}

func (f *fakeNotifier) NotifyAutomation(_ context.Context, in notificationdom.NotifyAutomationInput) error {
	f.got = append(f.got, in)
	return nil
}

func ptr[T any](v T) *T { return &v }

func TestUpdateTask_TagsAddKeepsExistingAndRemoveKeepsRest(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	task := &taskdom.Task{ID: uuid.New(), Tags: []string{"bug", "ui"}}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, task,
		&automationdom.TaskFieldUpdate{Tags: []string{"review"}, TagsMode: automationdom.ListModeAdd}, "a", nil)
	if err != nil || !applied {
		t.Fatalf("add: applied=%v err=%v", applied, err)
	}
	got := *updater.lastInput.Tags
	if len(got) != 3 || got[0] != "bug" || got[2] != "review" {
		t.Fatalf("add should keep bug,ui and append review, got %v", got)
	}

	task = &taskdom.Task{ID: uuid.New(), Tags: []string{"bug", "ui"}}
	applied, err = c.applyUpdateTask(context.Background(), uuid.New(), task, task,
		&automationdom.TaskFieldUpdate{Tags: []string{"ui"}, TagsMode: automationdom.ListModeRemove}, "a", nil)
	if err != nil || !applied {
		t.Fatalf("remove: applied=%v err=%v", applied, err)
	}
	if got := *updater.lastInput.Tags; len(got) != 1 || got[0] != "bug" {
		t.Fatalf("remove should leave bug, got %v", got)
	}

	// Removing the only tag empties the list.
	task = &taskdom.Task{ID: uuid.New(), Tags: []string{"ui"}}
	if _, err = c.applyUpdateTask(context.Background(), uuid.New(), task, task,
		&automationdom.TaskFieldUpdate{Tags: []string{"ui"}, TagsMode: automationdom.ListModeRemove}, "a", nil); err != nil {
		t.Fatal(err)
	}
	if got := *updater.lastInput.Tags; len(got) != 0 {
		t.Fatalf("expected empty tags, got %v", got)
	}
}

func TestUpdateTask_AssigneesAddIsIdempotent(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	a, b := uuid.New(), uuid.New()
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: []uuid.UUID{a}}
	upd := &automationdom.TaskFieldUpdate{AssigneeIDs: []uuid.UUID{b}, AssigneesMode: automationdom.ListModeAdd}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, task, upd, "a", nil)
	if err != nil || !applied {
		t.Fatalf("expected applied, got %v %v", applied, err)
	}
	if got := *updater.lastInput.AssigneeIDs; len(got) != 2 || got[0] != a || got[1] != b {
		t.Fatalf("expected [a b], got %v", got)
	}
	// Second run: b is already assigned (the in-memory task was updated).
	applied, err = c.applyUpdateTask(context.Background(), uuid.New(), task, task, upd, "a", nil)
	if err != nil || applied {
		t.Fatalf("expected no-op the second time, got %v %v", applied, err)
	}
}

func TestUpdateTask_ClearFields(t *testing.T) {
	updater := &fakeTaskUpdater{}
	c := newTestConsumer(updater)
	sprint, rep := uuid.New(), uuid.New()
	due := time.Now()
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: []uuid.UUID{uuid.New()}, SprintID: &sprint, ReporterID: &rep, DueDate: &due, StoryPoints: ptr(3)}

	applied, err := c.applyUpdateTask(context.Background(), uuid.New(), task, task,
		&automationdom.TaskFieldUpdate{Clear: []string{"assignee_ids", "sprint_id", "due_date", "story_points"}}, "a", nil)
	if err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	in := updater.lastInput
	if len(*in.AssigneeIDs) != 0 || *in.SprintID != nil || *in.DueDate != nil || *in.StoryPoints != nil {
		t.Fatalf("expected fields cleared, got %+v", in)
	}
	if in.ReporterID != nil {
		t.Fatal("reporter was not asked to clear")
	}
	if task.SprintID != nil || task.DueDate != nil || len(task.AssigneeIDs) != 0 {
		t.Fatal("in-memory task should reflect the clear")
	}
	// Clearing an already-empty field is a no-op.
	applied, err = c.applyUpdateTask(context.Background(), uuid.New(), task, task,
		&automationdom.TaskFieldUpdate{Clear: []string{"sprint_id", "due_date"}}, "a", nil)
	if err != nil || applied {
		t.Fatalf("expected no-op, got %v %v", applied, err)
	}
}

func TestMaterializeFields_DynamicFromParentReporterAndNow(t *testing.T) {
	parentID := uuid.New()
	parentAssignee, parentSprint, reporter := uuid.New(), uuid.New(), uuid.New()
	parentDue := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	parent := &taskdom.Task{ID: parentID, AssigneeIDs: []uuid.UUID{parentAssignee}, SprintID: &parentSprint, DueDate: &parentDue}
	walk := &taskdom.Task{ID: uuid.New(), ParentTaskID: &parentID, ReporterID: &reporter}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{byID: map[uuid.UUID]*taskdom.Task{parentID: parent}}}

	got, err := c.materializeFields(context.Background(), walk, nil, &automationdom.TaskFieldUpdate{
		Dynamic: map[string]automationdom.DynamicValue{
			"assignee_ids": {Source: "parent"},
			"sprint_id":    {Source: "parent"},
			"reporter_id":  {Source: "task", Field: "reporter_id"},
			"due_date":     {Source: "parent", OffsetDays: ptr(-2)},
			"start_date":   {Source: "now", OffsetDays: ptr(1)},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.AssigneeIDs) != 1 || got.AssigneeIDs[0] != parentAssignee || *got.SprintID != parentSprint || *got.ReporterID != reporter {
		t.Fatalf("unexpected dynamic ids: %+v", got)
	}
	if !got.DueDate.Equal(parentDue.AddDate(0, 0, -2)) {
		t.Fatalf("due date offset wrong: %v", got.DueDate)
	}
	if d := time.Until(*got.StartDate); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("start date should be ~tomorrow, got %v", got.StartDate)
	}

	// assignee ← reporter cross-field, and errors for nonsense.
	got, err = c.materializeFields(context.Background(), walk, nil, &automationdom.TaskFieldUpdate{
		Dynamic: map[string]automationdom.DynamicValue{"assignee_ids": {Source: "task", Field: "reporter_id"}},
	}, nil)
	if err != nil || len(got.AssigneeIDs) != 1 || got.AssigneeIDs[0] != reporter {
		t.Fatalf("assignee from reporter: %+v %v", got, err)
	}
	if _, err = c.materializeFields(context.Background(), walk, nil, &automationdom.TaskFieldUpdate{
		Dynamic: map[string]automationdom.DynamicValue{"importance": {Source: "task", Field: "due_date"}},
	}, nil); err == nil {
		t.Fatal("expected an incompatible-field error")
	}

	// No parent: dynamic parent value is a no-op, not an error.
	orphan := &taskdom.Task{ID: uuid.New()}
	got, err = c.materializeFields(context.Background(), orphan, nil, &automationdom.TaskFieldUpdate{
		Dynamic: map[string]automationdom.DynamicValue{"sprint_id": {Source: "parent"}},
	}, nil)
	if err != nil || got.SprintID != nil {
		t.Fatalf("expected untouched sprint, got %+v %v", got, err)
	}
}

func TestMaterializeFields_RendersDescriptionAndCustomFieldsWithoutMutatingInput(t *testing.T) {
	c := &AutomationConsumer{}
	in := &automationdom.TaskFieldUpdate{
		Description:  json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"Review {{task.title}}"}]}]`),
		CustomFields: map[string]any{"ref": "{{task.title}}", "n": 3},
	}
	got, err := c.materializeFields(context.Background(), nil, nil, in, map[string]string{"task.title": `Say "hi"`})
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomFields["ref"] != `Say "hi"` || got.CustomFields["n"] != 3 {
		t.Fatalf("custom fields: %v", got.CustomFields)
	}
	var doc []map[string]any
	if err := json.Unmarshal(got.Description, &doc); err != nil {
		t.Fatalf("description must stay valid JSON: %v", err)
	}
	text := doc[0]["content"].([]any)[0].(map[string]any)["text"]
	if text != `Review Say "hi"` {
		t.Fatalf("description text: %v", text)
	}
	if in.CustomFields["ref"] != "{{task.title}}" {
		t.Fatal("the stored config must not be mutated")
	}
}

func TestApplyAddComment(t *testing.T) {
	rec := &fakeActivityRec{}
	c := &AutomationConsumer{activityRec: rec}
	task := &taskdom.Task{ID: uuid.New()}
	applied, err := c.applyAddComment(context.Background(), uuid.New(), task, &automationdom.CommentAction{Text: "Done: {{task.title}}\nBye"}, "auto", map[string]string{"task.title": "X"})
	if err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	first := rec.recorded[0]
	if first.ActivityType != taskdom.ActivityTypeComment || first.Origin != taskdom.OriginAutomation || first.ActorID != nil {
		t.Fatalf("unexpected comment activity: %+v", first)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(first.Content, &blocks); err != nil || len(blocks) != 2 {
		t.Fatalf("expected two paragraphs, got %s (%v)", first.Content, err)
	}
	if _, err := c.applyAddComment(context.Background(), uuid.New(), task, &automationdom.CommentAction{Text: "  "}, "auto", nil); err == nil {
		t.Fatal("blank comment must error")
	}
}

func TestApplyNotify_ResolvesRolesDedupesAndSkipsEmpty(t *testing.T) {
	n := &fakeNotifier{}
	a, b, r, parentOnly := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	parentID := uuid.New()
	parent := &taskdom.Task{ID: parentID, AssigneeIDs: []uuid.UUID{b, parentOnly}}
	c := &AutomationConsumer{notifier: n, taskRepo: &fakeAutomationTaskReader{byID: map[uuid.UUID]*taskdom.Task{parentID: parent}}}
	task := &taskdom.Task{ID: uuid.New(), AssigneeIDs: []uuid.UUID{a, b}, ReporterID: &r, ParentTaskID: &parentID}

	applied, err := c.applyNotify(context.Background(), uuid.New(), task, &automationdom.NotifyAction{
		Recipients: []string{"assignees", "reporter", "parent_assignees"}, MemberIDs: []uuid.UUID{a},
		Message: "{{task.title}} moved",
	}, map[string]string{"task.title": "T"})
	if err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if len(n.got) != 4 { // a, b, r, parentOnly — b and a are not repeated
		t.Fatalf("expected 4 unique recipients, got %d", len(n.got))
	}
	if n.got[0].Message != "T moved" || n.got[0].TaskID != task.ID {
		t.Fatalf("unexpected notification: %+v", n.got[0])
	}

	n.got = nil
	applied, err = c.applyNotify(context.Background(), uuid.New(), &taskdom.Task{ID: uuid.New()}, &automationdom.NotifyAction{Recipients: []string{"assignees"}, Message: "x"}, nil)
	if err != nil || applied || len(n.got) != 0 {
		t.Fatalf("no recipients should be a no-op, got %v %v", applied, err)
	}
}

func TestLinkTasks_DirectionMappingAndDuplicates(t *testing.T) {
	lc := &fakeLinkCreator{}
	c := &AutomationConsumer{linkCreator: lc}
	from, to := uuid.New(), uuid.New()
	cases := []struct {
		typ           string
		wantType      taskdom.LinkType
		wantSrcIsFrom bool
	}{
		{"blocks", taskdom.LinkTypeBlocks, true},
		{"is_blocked_by", taskdom.LinkTypeBlocks, false},
		{"relates_to", taskdom.LinkTypeRelatesTo, true},
		{"duplicates", taskdom.LinkTypeDuplicates, true},
		{"is_duplicated_by", taskdom.LinkTypeDuplicates, false},
	}
	for _, tc := range cases {
		lc.created = nil
		ok, err := c.linkTasks(context.Background(), uuid.New(), from, to, tc.typ)
		if err != nil || !ok || len(lc.created) != 1 {
			t.Fatalf("%s: ok=%v err=%v", tc.typ, ok, err)
		}
		got := lc.created[0]
		if got.LinkType != tc.wantType || (got.SourceTaskID == from) != tc.wantSrcIsFrom {
			t.Fatalf("%s: unexpected link %+v", tc.typ, got)
		}
	}
	lc.err = taskdom.ErrTaskLinkDuplicate
	ok, err := c.linkTasks(context.Background(), uuid.New(), from, to, "blocks")
	if err != nil || ok {
		t.Fatalf("an existing link is a no-op, got %v %v", ok, err)
	}
	if _, err := c.linkTasks(context.Background(), uuid.New(), from, to, "nope"); err == nil {
		t.Fatal("unknown type must error")
	}
}

func TestTriggerMatches_FieldChanged(t *testing.T) {
	c := newTestConsumer(nil)
	node := &automationdom.Node{Kind: automationdom.KindTrigger, Type: string(automationdom.TriggerFieldChanged), Config: json.RawMessage(`{"field":"sprint"}`)}
	if !c.triggerMatches(node, &taskdom.Task{}, nil, []string{"title", "sprint"}) {
		t.Fatal("sprint change should match")
	}
	if c.triggerMatches(node, &taskdom.Task{}, nil, []string{"title"}) {
		t.Fatal("an unrelated change must not match")
	}
}

func TestApplyCreateTask_DynamicAndLinkToParent(t *testing.T) {
	creator := &fakeTaskCreator{}
	lc := &fakeLinkCreator{}
	parentAssignee := uuid.New()
	parent := &taskdom.Task{ID: uuid.New(), Title: "P", AssigneeIDs: []uuid.UUID{parentAssignee}}
	c := &AutomationConsumer{taskRepo: &fakeAutomationTaskReader{}, taskCreator: creator, linkCreator: lc}

	applied, err := c.applyCreateTask(context.Background(), uuid.New(), parent, parent, &automationdom.TaskCreate{
		Title:   "{{task.title}} - QA",
		Dynamic: map[string]automationdom.DynamicValue{"assignee_ids": {Source: "task"}},
		Link:    "blocks",
	}, "a", map[string]string{"task.title": "P"})
	if err != nil || !applied {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	if got := creator.created[0].AssigneeIDs; len(got) != 1 || got[0] != parentAssignee {
		t.Fatalf("assignee should be inherited from the parent, got %v", got)
	}
	if len(lc.created) != 1 || lc.created[0].TargetTaskID != parent.ID || lc.created[0].LinkType != taskdom.LinkTypeBlocks {
		t.Fatalf("new task should block its parent: %+v", lc.created)
	}
}

func TestRenderAgentMessage_EventNotice(t *testing.T) {
	long := strings.Repeat("x", 5000)
	for _, tmpl := range []string{"Look: {{event.title}}", "Look: {{ event.title }}"} {
		got := renderAgentMessage(tmpl, map[string]string{"event.title": long})
		if !strings.HasPrefix(got, "Note: text taken from an external event") {
			t.Fatalf("%q: notice not prepended", tmpl)
		}
		if !strings.Contains(sanitizeAgentMessage(got), "untrusted") {
			t.Fatalf("%q: notice lost after sanitizing", tmpl)
		}
	}
	if got := renderAgentMessage("Look: {{task.title}}", map[string]string{"task.title": "t"}); got != "Look: t" {
		t.Fatalf("unexpected notice for non-event template: %q", got)
	}
}
