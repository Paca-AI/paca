package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/jev"
)

// fakeAutofillTaskService is a minimal in-memory taskAutofillTaskService for
// exercising buildQuestions/applyAnswers without a database.
type fakeAutofillTaskService struct {
	tasks        map[uuid.UUID]*taskdom.Task
	taskTypes    []*taskdom.TaskType
	customFields []*taskdom.CustomFieldDefinition
	epics        []*taskdom.Task
	distinctTags []string
	updateErr    error
	lastUpdate   taskdom.UpdateTaskInput
}

func (f *fakeAutofillTaskService) GetTask(_ context.Context, _, id uuid.UUID) (*taskdom.Task, error) {
	return f.tasks[id], nil
}

func (f *fakeAutofillTaskService) UpdateTask(_ context.Context, _, _ uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	f.lastUpdate = in
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return &taskdom.Task{}, nil
}

func (f *fakeAutofillTaskService) ListTaskTypes(_ context.Context, _ uuid.UUID) ([]*taskdom.TaskType, error) {
	return f.taskTypes, nil
}

func (f *fakeAutofillTaskService) ListCustomFieldDefinitions(_ context.Context, _ uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	return f.customFields, nil
}

func (f *fakeAutofillTaskService) ListTasks(_ context.Context, _ uuid.UUID, _ taskdom.TaskFilter, _ int, _ taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	return f.epics, false, nil
}

func (f *fakeAutofillTaskService) ListDistinctTags(_ context.Context, _ uuid.UUID) ([]string, error) {
	return f.distinctTags, nil
}

// fakeActivityRecorder is a minimal in-memory taskActivityRecorder for
// asserting what both Jev consumers record — shared across
// task_autofill_consumer_test.go and task_auto_assign_consumer_test.go
// (same package).
type fakeActivityRecorder struct {
	recorded []taskdom.RecordActivityInput
}

func (f *fakeActivityRecorder) RecordActivity(_ context.Context, in taskdom.RecordActivityInput) error {
	f.recorded = append(f.recorded, in)
	return nil
}

func newTestAutofillConsumer(svc taskAutofillTaskService) *TaskAutofillConsumer {
	return &TaskAutofillConsumer{
		taskService: svc,
		// The consumer's default transport is SSRF-safe (netguard) and
		// rejects the loopback httptest.Servers these tests point
		// jev_base_url at — see WithHTTPClient.
		jevHTTPClient: &http.Client{Timeout: 5 * time.Second},
		log:           slog.Default(),
	}
}

func TestBuildQuestions_SkipsUserSetAndExcludedFields(t *testing.T) {
	svc := &fakeAutofillTaskService{
		taskTypes: []*taskdom.TaskType{
			{ID: uuid.New(), Name: "Bug"},
			{ID: uuid.New(), Name: "Feature"},
		},
		customFields: []*taskdom.CustomFieldDefinition{
			{FieldKey: "severity", DisplayName: "Severity", FieldType: taskdom.FieldTypeSelect,
				Options: []taskdom.CustomFieldOption{{Value: "low"}, {Value: "high"}}},
			{FieldKey: "is_regression", DisplayName: "Regression", FieldType: taskdom.FieldTypeBoolean},
			{FieldKey: "affected_areas", DisplayName: "Areas", FieldType: taskdom.FieldTypeMultiSelect,
				Options: []taskdom.CustomFieldOption{{Value: "web"}, {Value: "api"}}},
			{FieldKey: "notes", DisplayName: "Notes", FieldType: taskdom.FieldTypeText}, // excluded type
		},
	}
	c := newTestAutofillConsumer(svc)

	userSet := map[string]bool{"custom:is_regression": true}
	settings := projectdom.JevSettings{
		AutofillEnabled:        true,
		AutofillExcludedFields: []string{"task_type_id"},
	}

	questions := c.buildQuestions(context.Background(), uuid.New(), &taskdom.Task{}, userSet, settings)

	if _, ok := questions["importance"]; !ok {
		t.Error("expected an importance question")
	}
	if _, ok := questions["task_type_id"]; ok {
		t.Error("task_type_id is excluded by settings, should not be asked")
	}
	if _, ok := questions["custom:severity"]; !ok {
		t.Error("expected a custom:severity choice question")
	}
	if _, ok := questions["custom:is_regression"]; ok {
		t.Error("custom:is_regression is user-set, should not be asked")
	}
	if _, ok := questions["custom:affected_areas:web"]; !ok {
		t.Error("expected a per-option noul question for the multi_select field")
	}
	if _, ok := questions["custom:affected_areas:api"]; !ok {
		t.Error("expected a per-option noul question for the multi_select field")
	}
	if _, ok := questions["custom:notes"]; ok {
		t.Error("text custom fields should never be asked (unconstrained, Jev can't generate free text)")
	}
	if q := questions["custom:severity"]; q.Type != jev.TypeChoice {
		t.Errorf("expected select field to map to choice, got %v", q.Type)
	}
	if q := questions["importance"]; q.Type != jev.TypeScore {
		t.Errorf("expected importance to map to score, got %v", q.Type)
	}
}

func TestBuildQuestions_SingleTaskTypeSkipsChoice(t *testing.T) {
	svc := &fakeAutofillTaskService{
		taskTypes: []*taskdom.TaskType{{ID: uuid.New(), Name: "Task"}},
	}
	c := newTestAutofillConsumer(svc)
	questions := c.buildQuestions(context.Background(), uuid.New(), &taskdom.Task{}, nil, projectdom.DefaultJevSettings())
	if _, ok := questions["task_type_id"]; ok {
		t.Error("a project with only one task type has nothing to choose between — should not ask")
	}
}

func TestBuildQuestions_StoryPointsAsked(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	questions := c.buildQuestions(context.Background(), uuid.New(), &taskdom.Task{}, nil, projectdom.DefaultJevSettings())
	q, ok := questions["story_points"]
	if !ok {
		t.Fatal("expected a story_points question")
	}
	if q.Type != jev.TypeScore {
		t.Errorf("expected story_points to map to score, got %v", q.Type)
	}
}

func TestBuildQuestions_StoryPointsSkippedWhenUserSetOrExcluded(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	userSet := map[string]bool{"story_points": true}
	if _, ok := c.buildQuestions(context.Background(), uuid.New(), &taskdom.Task{}, userSet, projectdom.DefaultJevSettings())["story_points"]; ok {
		t.Error("story_points is user-set, should not be asked")
	}
	excluded := projectdom.JevSettings{AutofillEnabled: true, AutofillExcludedFields: []string{"story_points"}}
	if _, ok := c.buildQuestions(context.Background(), uuid.New(), &taskdom.Task{}, nil, excluded)["story_points"]; ok {
		t.Error("story_points is excluded by settings, should not be asked")
	}
}

func TestBuildQuestions_ParentEpicOfferedFromExistingEpics(t *testing.T) {
	epicTypeID := uuid.New()
	epicA, epicB := uuid.New(), uuid.New()
	svc := &fakeAutofillTaskService{
		taskTypes: []*taskdom.TaskType{{ID: epicTypeID, Name: "Epic", IsSystem: true}},
		epics: []*taskdom.Task{
			{ID: epicA, Title: "Platform revamp"},
			{ID: epicB, Title: "Mobile rollout"},
		},
	}
	c := newTestAutofillConsumer(svc)
	questions := c.buildQuestions(context.Background(), uuid.New(), &taskdom.Task{}, nil, projectdom.DefaultJevSettings())
	q, ok := questions["parent_task_id"]
	if !ok {
		t.Fatal("expected a parent_task_id question")
	}
	if q.Type != jev.TypeChoice {
		t.Errorf("expected parent_task_id to map to choice, got %v", q.Type)
	}
	criteria, ok := q.Criteria.(map[string]any)
	if !ok || len(criteria) != 2 {
		t.Fatalf("expected 2 epic candidates in criteria, got %v", q.Criteria)
	}
}

func TestBuildQuestions_SkipsParentEpicQuestionWhenTaskIsItselfAnEpic(t *testing.T) {
	epicTypeID := uuid.New()
	svc := &fakeAutofillTaskService{
		taskTypes: []*taskdom.TaskType{{ID: epicTypeID, Name: "Epic", IsSystem: true}},
		epics:     []*taskdom.Task{{ID: uuid.New(), Title: "Some other epic"}},
	}
	c := newTestAutofillConsumer(svc)
	task := &taskdom.Task{TaskTypeID: &epicTypeID}
	if _, ok := c.buildQuestions(context.Background(), uuid.New(), task, nil, projectdom.DefaultJevSettings())["parent_task_id"]; ok {
		t.Error("a task whose own type is Epic can't have a parent, should not be asked")
	}
}

func TestBuildQuestions_SkipsParentEpicQuestionWhenNoEpicTypeOrNoEpics(t *testing.T) {
	svc := &fakeAutofillTaskService{
		taskTypes: []*taskdom.TaskType{{ID: uuid.New(), Name: "Task"}},
	}
	c := newTestAutofillConsumer(svc)
	if _, ok := c.buildQuestions(context.Background(), uuid.New(), &taskdom.Task{}, nil, projectdom.DefaultJevSettings())["parent_task_id"]; ok {
		t.Error("project has no Epic task type, should not be asked")
	}
}

func TestBuildQuestions_TagsOfferOnlyAlreadyUsedValuesExcludingTaskOwn(t *testing.T) {
	svc := &fakeAutofillTaskService{
		distinctTags: []string{"backend", "frontend", "urgent"},
	}
	c := newTestAutofillConsumer(svc)
	task := &taskdom.Task{Tags: []string{"backend"}}
	questions := c.buildQuestions(context.Background(), uuid.New(), task, nil, projectdom.DefaultJevSettings())
	if _, ok := questions["tags:backend"]; ok {
		t.Error("task already has this tag, should not be asked about it again")
	}
	if _, ok := questions["tags:frontend"]; !ok {
		t.Error("expected a tags:frontend question")
	}
	if _, ok := questions["tags:urgent"]; !ok {
		t.Error("expected a tags:urgent question")
	}
}

func TestBuildQuestions_TagsNoopWhenProjectHasNoTags(t *testing.T) {
	svc := &fakeAutofillTaskService{distinctTags: nil}
	c := newTestAutofillConsumer(svc)
	questions := c.buildQuestions(context.Background(), uuid.New(), &taskdom.Task{}, nil, projectdom.DefaultJevSettings())
	for k := range questions {
		if strings.HasPrefix(k, "tags:") {
			t.Errorf("project has no existing tags, should not ask about any, got %q", k)
		}
	}
}

func TestApplyAnswers_ImportanceBelowThresholdLeftBlank(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	task := &taskdom.Task{}
	low := 0.3
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"importance": {Type: jev.TypeScore, Score: 4, Confidence: &low},
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), task, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.Importance != nil {
		t.Errorf("low-confidence importance should not be applied, got %v", *svc.lastUpdate.Importance)
	}
}

func TestApplyAnswers_ImportanceAppliedAtConfidence(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	task := &taskdom.Task{}
	high := 0.95
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"importance": {Type: jev.TypeScore, Score: 4, Confidence: &high}, // bucket 4 -> Critical -> 150
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), task, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.Importance == nil || *svc.lastUpdate.Importance != 150 {
		t.Fatalf("expected importance 150 (critical bucket), got %v", svc.lastUpdate.Importance)
	}
}

// TestApplyAnswers_RecordsActivityForAppliedChanges verifies autofilled
// changes are recorded as a task.updated activity (this consumer bypasses
// TaskHandler, which is the only place activity recording otherwise
// happens) — a regression test for the "auto-fill/auto-assign changes never
// show up in the activity feed" report.
func TestApplyAnswers_RecordsActivityForAppliedChanges(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	rec := &fakeActivityRecorder{}
	c := newTestAutofillConsumer(svc)
	c.activityRec = rec
	task := &taskdom.Task{Importance: 0}
	high := 0.95
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"importance": {Type: jev.TypeScore, Score: 4, Confidence: &high},
	}}

	taskID := uuid.New()
	if err := c.applyAnswers(context.Background(), uuid.New(), taskID, task, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.recorded) != 1 {
		t.Fatalf("expected exactly one recorded activity, got %d", len(rec.recorded))
	}
	got := rec.recorded[0]
	if got.TaskID != taskID || got.ActivityType != taskdom.ActivityTypeTaskUpdated {
		t.Fatalf("unexpected recorded activity: %+v", got)
	}
	if got.ActorID != nil || got.ActorAgentID != nil {
		t.Errorf("expected a system-attributed activity (nil actor), got ActorID=%v ActorAgentID=%v", got.ActorID, got.ActorAgentID)
	}
	var payload struct {
		Changes []taskdom.FieldChange `json:"changes"`
	}
	if err := json.Unmarshal(got.Content, &payload); err != nil {
		t.Fatalf("could not decode activity content: %v", err)
	}
	if len(payload.Changes) != 1 || payload.Changes[0].Field != "importance" {
		t.Fatalf("expected a single importance change, got %+v", payload.Changes)
	}
}

// TestApplyAnswers_NoRecordedActivityWhenNothingApplied verifies a no-op
// autofill (nothing confidently answered) never records an empty activity.
func TestApplyAnswers_NoRecordedActivityWhenNothingApplied(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	rec := &fakeActivityRecorder{}
	c := newTestAutofillConsumer(svc)
	c.activityRec = rec
	task := &taskdom.Task{}
	low := 0.3
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"importance": {Type: jev.TypeScore, Score: 4, Confidence: &low},
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), task, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.recorded) != 0 {
		t.Errorf("expected no recorded activity for a no-op autofill, got %d", len(rec.recorded))
	}
}

func TestApplyAnswers_MergesCustomFieldsWithoutClobberingExisting(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	task := &taskdom.Task{CustomFields: map[string]any{"already_set": "keep-me"}}
	high := 0.9
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"custom:severity":           {Type: jev.TypeChoice, Choice: "high", Confidence: &high},
		"custom:is_regression":      {Type: jev.TypeNoul, Noul: 0.9},
		"custom:affected_areas:web": {Type: jev.TypeNoul, Noul: 0.9},
		"custom:affected_areas:api": {Type: jev.TypeNoul, Noul: 0.1}, // below threshold, excluded
		"task_type_id":              {Type: jev.TypeChoice, Choice: "not-a-uuid", Confidence: &high},
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), task, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.CustomFields == nil {
		t.Fatal("expected CustomFields to be set")
	}
	got := *svc.lastUpdate.CustomFields
	if got["already_set"] != "keep-me" {
		t.Errorf("existing custom field value was clobbered: %v", got["already_set"])
	}
	if got["severity"] != "high" {
		t.Errorf("expected severity=high, got %v", got["severity"])
	}
	if got["is_regression"] != true {
		t.Errorf("expected is_regression=true, got %v", got["is_regression"])
	}
	if !reflect.DeepEqual(got["affected_areas"], []string{"web"}) {
		t.Errorf("expected affected_areas=[web] (api below threshold), got %v", got["affected_areas"])
	}
	if svc.lastUpdate.TaskTypeID != nil {
		t.Error("an invalid (non-UUID) task_type_id choice should not be applied")
	}
}

func TestApplyAnswers_NoConfidentAnswersIsNoop(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), &taskdom.Task{}, &jev.Response{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	zero := taskdom.UpdateTaskInput{}
	if !reflect.DeepEqual(svc.lastUpdate, zero) {
		t.Error("UpdateTask should not have been called when nothing is dirty")
	}
}

func TestApplyAnswers_StoryPointsAppliedAtConfidence(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	high := 0.9
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"story_points": {Type: jev.TypeScore, Score: 5, Confidence: &high}, // bucket 5 -> 8
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), &taskdom.Task{}, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.StoryPoints == nil || *svc.lastUpdate.StoryPoints == nil || **svc.lastUpdate.StoryPoints != 8 {
		t.Fatalf("expected story_points 8, got %v", svc.lastUpdate.StoryPoints)
	}
}

func TestApplyAnswers_StoryPointsBelowThresholdLeftBlank(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	low := 0.3
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"story_points": {Type: jev.TypeScore, Score: 5, Confidence: &low},
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), &taskdom.Task{}, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.StoryPoints != nil {
		t.Error("low-confidence story_points should not be applied")
	}
}

func TestApplyAnswers_ParentTaskIDAppliedAtConfidence(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	epicID := uuid.New()
	high := 0.9
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"parent_task_id": {Type: jev.TypeChoice, Choice: epicID.String(), Confidence: &high},
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), &taskdom.Task{}, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.ParentTaskID == nil || *svc.lastUpdate.ParentTaskID == nil || **svc.lastUpdate.ParentTaskID != epicID {
		t.Fatalf("expected parent_task_id %v, got %v", epicID, svc.lastUpdate.ParentTaskID)
	}
}

func TestApplyAnswers_ParentTaskIDInvalidChoiceIgnored(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	high := 0.9
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"parent_task_id": {Type: jev.TypeChoice, Choice: "not-a-uuid", Confidence: &high},
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), &taskdom.Task{}, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.ParentTaskID != nil {
		t.Error("an invalid (non-UUID) parent_task_id choice should not be applied")
	}
}

func TestApplyAnswers_TagsMergedWithExisting(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	task := &taskdom.Task{Tags: []string{"backend"}}
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"tags:urgent":   {Type: jev.TypeNoul, Noul: 0.9},
		"tags:frontend": {Type: jev.TypeNoul, Noul: 0.1}, // below threshold, excluded
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), task, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.Tags == nil {
		t.Fatal("expected Tags to be set")
	}
	got := *svc.lastUpdate.Tags
	if !reflect.DeepEqual(got, []string{"backend", "urgent"}) {
		t.Errorf("expected tags [backend urgent], got %v", got)
	}
}

func TestApplyAnswers_NoConfidentTagsIsNoop(t *testing.T) {
	svc := &fakeAutofillTaskService{}
	c := newTestAutofillConsumer(svc)
	task := &taskdom.Task{Tags: []string{"backend"}}
	resp := &jev.Response{Answers: map[string]jev.Answer{
		"tags:urgent": {Type: jev.TypeNoul, Noul: 0.1},
	}}

	if err := c.applyAnswers(context.Background(), uuid.New(), uuid.New(), task, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc.lastUpdate.Tags != nil {
		t.Error("no tag answer met threshold, Tags should not be set")
	}
}

func TestNearestBucket(t *testing.T) {
	cases := []struct {
		score float64
		want  int
	}{
		{-1, 0}, {0, 0}, {0.4, 0}, {0.6, 1}, {2.5, 3}, {4, 4}, {99, 4},
	}
	for _, c := range cases {
		if got := nearestBucket(c.score); got != c.want {
			t.Errorf("nearestBucket(%v) = %d, want %d", c.score, got, c.want)
		}
	}
}

func TestNearestStoryPointBucket(t *testing.T) {
	cases := []struct {
		score float64
		want  int
	}{
		{-1, 0}, {0, 0}, {0.4, 0}, {0.6, 1}, {6.5, 7}, {7, 7}, {99, 7},
	}
	for _, c := range cases {
		if got := nearestStoryPointBucket(c.score); got != c.want {
			t.Errorf("nearestStoryPointBucket(%v) = %d, want %d", c.score, got, c.want)
		}
	}
}

func TestExtractBlockNoteText(t *testing.T) {
	doc := json.RawMessage(`[{"type":"paragraph","content":[{"type":"text","text":"Hello"},{"type":"text","text":"world"}]}]`)
	got := extractBlockNoteText(doc)
	if got != "Hello world" {
		t.Errorf("expected %q, got %q", "Hello world", got)
	}
	if extractBlockNoteText(nil) != "" {
		t.Error("expected empty string for nil input")
	}
	if extractBlockNoteText(json.RawMessage(`not json`)) != "" {
		t.Error("expected empty string for invalid json, not a panic/error")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("short string should be unchanged, got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Errorf("expected truncation with ellipsis, got %q", got)
	}
}
