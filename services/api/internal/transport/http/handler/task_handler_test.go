package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

// ---------------------------------------------------------------------------
// Fake task service
// ---------------------------------------------------------------------------

type fakeTaskSvc struct {
	mu            sync.RWMutex
	tasks         map[uuid.UUID]*taskdom.Task
	types         map[uuid.UUID]*taskdom.TaskType
	lastProjectID uuid.UUID
	lastFilter    taskdom.TaskFilter
	customFields  []*taskdom.CustomFieldDefinition
}

func newFakeTaskSvc() *fakeTaskSvc {
	return &fakeTaskSvc{
		tasks: make(map[uuid.UUID]*taskdom.Task),
		types: make(map[uuid.UUID]*taskdom.TaskType),
	}
}

// -- TaskTypeService --

func (f *fakeTaskSvc) ListTaskTypes(_ context.Context, _ uuid.UUID) ([]*taskdom.TaskType, error) {
	return nil, nil
}

func (f *fakeTaskSvc) GetTaskType(_ context.Context, _ uuid.UUID) (*taskdom.TaskType, error) {
	return nil, taskdom.ErrTypeNotFound
}

func (f *fakeTaskSvc) CreateTaskType(_ context.Context, in taskdom.CreateTaskTypeInput) (*taskdom.TaskType, error) {
	if in.Name == "" {
		return nil, taskdom.ErrTypeNameInvalid
	}
	now := time.Now()
	t := &taskdom.TaskType{ID: uuid.New(), ProjectID: in.ProjectID, Name: in.Name, CreatedAt: now, UpdatedAt: now}
	f.mu.Lock()
	f.types[t.ID] = t
	f.mu.Unlock()
	return t, nil
}

func (f *fakeTaskSvc) UpdateTaskType(_ context.Context, _, _ uuid.UUID, _ taskdom.UpdateTaskTypeInput) (*taskdom.TaskType, error) {
	return nil, taskdom.ErrTypeNotFound
}

func (f *fakeTaskSvc) DeleteTaskType(_ context.Context, _, _ uuid.UUID) error { return nil }

func (f *fakeTaskSvc) SetDefaultTaskType(_ context.Context, _, _ uuid.UUID) (*taskdom.TaskType, error) {
	return &taskdom.TaskType{}, nil
}

// -- TaskStatusService --

func (f *fakeTaskSvc) ListTaskStatuses(_ context.Context, _ uuid.UUID) ([]*taskdom.TaskStatus, error) {
	return nil, nil
}

func (f *fakeTaskSvc) GetTaskStatus(_ context.Context, _ uuid.UUID) (*taskdom.TaskStatus, error) {
	return nil, taskdom.ErrStatusNotFound
}

func (f *fakeTaskSvc) CreateTaskStatus(_ context.Context, in taskdom.CreateTaskStatusInput) (*taskdom.TaskStatus, error) {
	if !taskdom.ValidStatusCategories[in.Category] {
		return nil, taskdom.ErrStatusCategoryInvalid
	}
	now := time.Now()
	s := &taskdom.TaskStatus{ID: uuid.New(), ProjectID: in.ProjectID, Name: in.Name, Category: in.Category, CreatedAt: now, UpdatedAt: now}
	return s, nil
}

func (f *fakeTaskSvc) UpdateTaskStatus(_ context.Context, _, _ uuid.UUID, _ taskdom.UpdateTaskStatusInput) (*taskdom.TaskStatus, error) {
	return nil, taskdom.ErrStatusNotFound
}

func (f *fakeTaskSvc) DeleteTaskStatus(_ context.Context, _, _ uuid.UUID) error { return nil }

func (f *fakeTaskSvc) SetDefaultTaskStatus(_ context.Context, _, _ uuid.UUID) (*taskdom.TaskStatus, error) {
	return &taskdom.TaskStatus{}, nil
}

func (f *fakeTaskSvc) ReorderTaskStatuses(_ context.Context, _ uuid.UUID, _ []uuid.UUID) error {
	return nil
}

// -- TaskService --

func (f *fakeTaskSvc) ListTasks(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, _ int, _ taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	f.mu.Lock()
	f.lastProjectID = projectID
	f.lastFilter = filter
	f.mu.Unlock()
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*taskdom.Task
	for _, t := range f.tasks {
		if t.ProjectID != projectID {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	return out, false, nil
}

func (f *fakeTaskSvc) CountTasks(_ context.Context, projectID uuid.UUID, _ taskdom.TaskFilter) (int64, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var count int64
	for _, t := range f.tasks {
		if t.ProjectID == projectID {
			count++
		}
	}
	return count, nil
}

func (f *fakeTaskSvc) SumTaskField(_ context.Context, projectID uuid.UUID, _ taskdom.TaskFilter, fieldKey string) (float64, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var sum float64
	for _, t := range f.tasks {
		if t.ProjectID != projectID {
			continue
		}
		if fieldKey == "story_points" && t.StoryPoints != nil {
			sum += float64(*t.StoryPoints)
		}
	}
	return sum, nil
}

func (f *fakeTaskSvc) GetTask(_ context.Context, _, id uuid.UUID) (*taskdom.Task, error) {
	f.mu.RLock()
	t, ok := f.tasks[id]
	f.mu.RUnlock()
	if !ok {
		return nil, taskdom.ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeTaskSvc) GetTaskByNumber(_ context.Context, projectID uuid.UUID, taskNumber int64) (*taskdom.Task, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, t := range f.tasks {
		if t.ProjectID == projectID && t.TaskNumber == taskNumber {
			cp := *t
			return &cp, nil
		}
	}
	return nil, taskdom.ErrTaskNotFound
}

func (f *fakeTaskSvc) CreateTask(_ context.Context, in taskdom.CreateTaskInput) (*taskdom.Task, error) {
	if in.Title == "" {
		return nil, taskdom.ErrTaskTitleInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	taskNum := int64(len(f.tasks) + 1)
	now := time.Now()
	t := &taskdom.Task{
		ID:           uuid.New(),
		ProjectID:    in.ProjectID,
		TaskNumber:   taskNum,
		Title:        in.Title,
		SprintID:     in.SprintID,
		StatusID:     in.StatusID,
		CustomFields: map[string]any{},
		Tags:         []string{},
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	f.tasks[t.ID] = t
	return t, nil
}

func (f *fakeTaskSvc) UpdateTask(_ context.Context, _, id uuid.UUID, in taskdom.UpdateTaskInput) (*taskdom.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, taskdom.ErrTaskNotFound
	}
	if in.StatusID != nil {
		t.StatusID = *in.StatusID
	}
	if in.SprintID != nil {
		t.SprintID = *in.SprintID
	}
	if in.TaskTypeID != nil {
		t.TaskTypeID = *in.TaskTypeID
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	cp := *t
	return &cp, nil
}

func (f *fakeTaskSvc) DeleteTask(_ context.Context, _, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.tasks[id]; !ok {
		return taskdom.ErrTaskNotFound
	}
	delete(f.tasks, id)
	return nil
}

// -- CustomFieldDefinitionService --

func (f *fakeTaskSvc) ListCustomFieldDefinitions(_ context.Context, _ uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.customFields, nil
}

func (f *fakeTaskSvc) GetCustomFieldDefinition(_ context.Context, _, _ uuid.UUID) (*taskdom.CustomFieldDefinition, error) {
	return nil, taskdom.ErrCustomFieldNotFound
}

func (f *fakeTaskSvc) CreateCustomFieldDefinition(_ context.Context, _ taskdom.CreateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error) {
	return nil, nil
}

func (f *fakeTaskSvc) UpdateCustomFieldDefinition(_ context.Context, _, _ uuid.UUID, _ taskdom.UpdateCustomFieldDefinitionInput) (*taskdom.CustomFieldDefinition, error) {
	return nil, taskdom.ErrCustomFieldNotFound
}

func (f *fakeTaskSvc) DeleteCustomFieldDefinition(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

// --- TaskLinkService stubs --------------------------------------------------

func (f *fakeTaskSvc) ListTaskLinks(_ context.Context, _, _ uuid.UUID) ([]*taskdom.TaskLink, error) {
	return []*taskdom.TaskLink{}, nil
}

func (f *fakeTaskSvc) CreateTaskLink(_ context.Context, in taskdom.CreateTaskLinkInput) (*taskdom.TaskLink, error) {
	return &taskdom.TaskLink{
		ID:           uuid.New(),
		SourceTaskID: in.SourceTaskID,
		TargetTaskID: in.TargetTaskID,
		LinkType:     in.LinkType,
		CreatedBy:    in.CreatedBy,
	}, nil
}

func (f *fakeTaskSvc) DeleteTaskLink(_ context.Context, _, _, _ uuid.UUID) error { return nil }

// ---------------------------------------------------------------------------
// Fake activity service
// ---------------------------------------------------------------------------

type fakeActivitySvc struct {
	mu         sync.RWMutex
	activities map[uuid.UUID]*taskdom.Activity
}

func newFakeActivitySvc() *fakeActivitySvc {
	return &fakeActivitySvc{activities: make(map[uuid.UUID]*taskdom.Activity)}
}

func (f *fakeActivitySvc) RecordActivity(_ context.Context, in taskdom.RecordActivityInput) error {
	a := &taskdom.Activity{
		ID:           uuid.New(),
		TaskID:       in.TaskID,
		ActorID:      in.ActorID,
		ActivityType: in.ActivityType,
		Content:      in.Content,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.mu.Lock()
	f.activities[a.ID] = a
	f.mu.Unlock()
	return nil
}

func (f *fakeActivitySvc) ListActivities(_ context.Context, taskID uuid.UUID) ([]*taskdom.Activity, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*taskdom.Activity
	for _, a := range f.activities {
		if a.TaskID == taskID && a.DeletedAt == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeActivitySvc) AddComment(_ context.Context, in taskdom.AddCommentInput) (*taskdom.Activity, error) {
	if fakeIsContentEmpty(in.Content) || !fakeIsContentTypeValid(in.Content) {
		return nil, taskdom.ErrCommentContentInvalid
	}
	now := time.Now()
	a := &taskdom.Activity{
		ID:           uuid.New(),
		TaskID:       in.TaskID,
		ActorID:      &in.ActorID,
		ActivityType: taskdom.ActivityTypeComment,
		Content:      in.Content,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	f.mu.Lock()
	f.activities[a.ID] = a
	f.mu.Unlock()
	return a, nil
}

func (f *fakeActivitySvc) UpdateComment(_ context.Context, id uuid.UUID, _ uuid.UUID, actorID uuid.UUID, _ *uuid.UUID, content json.RawMessage) (*taskdom.Activity, error) {
	if fakeIsContentEmpty(content) || !fakeIsContentTypeValid(content) {
		return nil, taskdom.ErrCommentContentInvalid
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.activities[id]
	if !ok {
		return nil, taskdom.ErrActivityNotFound
	}
	if a.ActivityType != taskdom.ActivityTypeComment {
		return nil, taskdom.ErrActivityNotAComment
	}
	if a.ActorID == nil || *a.ActorID != actorID {
		return nil, taskdom.ErrActivityForbidden
	}
	a.Content = content
	a.UpdatedAt = time.Now()
	cp := *a
	return &cp, nil
}

func (f *fakeActivitySvc) DeleteComment(_ context.Context, id uuid.UUID, _ uuid.UUID, actorID uuid.UUID, _ *uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.activities[id]
	if !ok {
		return taskdom.ErrActivityNotFound
	}
	if a.ActivityType != taskdom.ActivityTypeComment {
		return taskdom.ErrActivityNotAComment
	}
	if a.ActorID == nil || *a.ActorID != actorID {
		return taskdom.ErrActivityForbidden
	}
	now := time.Now()
	a.DeletedAt = &now
	return nil
}

type fakeViewSvcTask struct{}

func (f *fakeViewSvcTask) ListViews(_ context.Context, _ uuid.UUID) ([]*sprintdom.SprintView, error) {
	return nil, nil
}

func (f *fakeViewSvcTask) ListProjectViews(_ context.Context, _ uuid.UUID, _ sprintdom.ViewContext) ([]*sprintdom.SprintView, error) {
	return nil, nil
}

func (f *fakeViewSvcTask) GetView(_ context.Context, _, _ uuid.UUID) (*sprintdom.SprintView, error) {
	return nil, sprintdom.ErrViewNotFound
}

func (f *fakeViewSvcTask) CreateView(_ context.Context, _ sprintdom.CreateViewInput) (*sprintdom.SprintView, error) {
	return nil, nil
}

func (f *fakeViewSvcTask) UpdateView(_ context.Context, _, _ uuid.UUID, _ sprintdom.UpdateViewInput) (*sprintdom.SprintView, error) {
	return nil, sprintdom.ErrViewNotFound
}

func (f *fakeViewSvcTask) DeleteView(_ context.Context, _, _ uuid.UUID) error { return nil }

func (f *fakeViewSvcTask) MoveTask(_ context.Context, _, _ uuid.UUID, _ sprintdom.MoveTaskInput) error {
	return nil
}

func (f *fakeViewSvcTask) BulkMoveTasks(_ context.Context, _, _ uuid.UUID, _ []sprintdom.MoveTaskInput) error {
	return nil
}

func (f *fakeViewSvcTask) ListTaskPositions(_ context.Context, _, _ uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
	return nil, nil
}

func (f *fakeViewSvcTask) ReorderViews(_ context.Context, _ uuid.UUID, _ []uuid.UUID) error {
	return nil
}

func (f *fakeViewSvcTask) ReorderProjectViews(_ context.Context, _ uuid.UUID, _ sprintdom.ViewContext, _ []uuid.UUID) error {
	return nil
}

// ---------------------------------------------------------------------------
// Router helper
// ---------------------------------------------------------------------------

func buildTaskHandlerRouter(svc *fakeTaskSvc) chi.Router {
	return buildTaskHandlerRouterWithActivity(svc, newFakeActivitySvc())
}

func buildTaskHandlerRouterWithActivity(svc *fakeTaskSvc, actSvc *fakeActivitySvc) chi.Router {
	h := handler.NewTaskHandler(svc, &fakeViewSvcTask{}, actSvc)
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Get("/task-types", h.ListTaskTypes)
		r.Post("/task-types", h.CreateTaskType)
		r.Patch("/task-types/{typeId}", h.UpdateTaskType)
		r.Delete("/task-types/{typeId}", h.DeleteTaskType)
		r.Get("/tasks", h.ListTasks)
		r.Post("/tasks", h.CreateTask)
		r.Get("/tasks/by-number/{taskNumber}", h.GetTaskByNumber)
		r.Get("/tasks/{taskId}", h.GetTask)
		r.Patch("/tasks/{taskId}", h.UpdateTask)
		r.Delete("/tasks/{taskId}", h.DeleteTask)
		r.Get("/tasks/{taskId}/activities", h.ListTaskActivities)
		r.Post("/tasks/{taskId}/activities/comments", h.AddComment)
		r.Patch("/tasks/{taskId}/activities/comments/{commentId}", h.UpdateComment)
		r.Delete("/tasks/{taskId}/activities/comments/{commentId}", h.DeleteComment)
	})
	return r
}

func doTaskRequest(r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeTaskID(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatalf("missing id in response: %s", body)
	}
	return id
}

func decodeTaskField(t *testing.T, body []byte, field string) any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return env.Data[field]
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestTaskHandler_ListTasks_UsesUnifiedFilters(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)

	projectID := uuid.New()
	sprintID := uuid.New()
	statusID := uuid.New()
	assigneeID := uuid.New()
	taskType1 := uuid.New()
	taskType2 := uuid.New()

	path := fmt.Sprintf(
		"/projects/%s/tasks?sprint_id=%s&status_ids=%s&assignee_ids=%s&task_type_ids=%s,%s",
		projectID,
		sprintID,
		statusID,
		assigneeID,
		taskType1,
		taskType2,
	)
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastProjectID != projectID {
		t.Fatalf("expected project id %s, got %s", projectID, svc.lastProjectID)
	}
	if svc.lastFilter.SprintID == nil || *svc.lastFilter.SprintID != sprintID {
		t.Fatalf("expected sprint filter %s, got %+v", sprintID, svc.lastFilter.SprintID)
	}
	if len(svc.lastFilter.StatusIDs) != 1 || svc.lastFilter.StatusIDs[0] != statusID {
		t.Fatalf("unexpected status ids: %+v", svc.lastFilter.StatusIDs)
	}
	if len(svc.lastFilter.AssigneeIDs) != 1 || svc.lastFilter.AssigneeIDs[0] != assigneeID {
		t.Fatalf("unexpected assignee ids: %+v", svc.lastFilter.AssigneeIDs)
	}
	if len(svc.lastFilter.TaskTypeIDs) != 2 || svc.lastFilter.TaskTypeIDs[0] != taskType1 || svc.lastFilter.TaskTypeIDs[1] != taskType2 {
		t.Fatalf("unexpected task type ids: %+v", svc.lastFilter.TaskTypeIDs)
	}
}

func TestTaskHandler_ListTasks_SprintIDsTakePriority(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	sprintID1 := uuid.New()
	sprintID2 := uuid.New()

	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks?sprint_id=null&sprint_ids=%s,%s", projectID, sprintID1, sprintID2), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if svc.lastFilter.BacklogOnly {
		t.Fatal("expected BacklogOnly=false when sprint_ids are provided")
	}
	if len(svc.lastFilter.SprintIDs) != 2 || svc.lastFilter.SprintIDs[0] != sprintID1 || svc.lastFilter.SprintIDs[1] != sprintID2 {
		t.Fatalf("unexpected sprint ids: %+v", svc.lastFilter.SprintIDs)
	}
}

func TestTaskHandler_ListTasks_SprintNullMeansBacklog(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks?sprint_id=null", projectID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !svc.lastFilter.BacklogOnly {
		t.Fatal("expected BacklogOnly=true when sprint_id=null")
	}
}

func TestTaskHandler_ListTasks_AssigneeNullAndIDsTogether(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	assigneeID := uuid.New()

	path := fmt.Sprintf("/projects/%s/tasks?assignee_id=null&assignee_ids=%s", projectID, assigneeID)
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !svc.lastFilter.AssigneeNull {
		t.Fatal("expected AssigneeNull=true when assignee_id=null")
	}
	if len(svc.lastFilter.AssigneeIDs) != 1 || svc.lastFilter.AssigneeIDs[0] != assigneeID {
		t.Fatalf("expected AssigneeIDs=[%s], got %+v", assigneeID, svc.lastFilter.AssigneeIDs)
	}
}

func TestTaskHandler_ListTasks_TaskTypeIDsDriveFiltering(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	taskTypeID := uuid.New()

	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks?task_type_ids=%s", projectID, taskTypeID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(svc.lastFilter.TaskTypeIDs) != 1 || svc.lastFilter.TaskTypeIDs[0] != taskTypeID {
		t.Fatalf("unexpected task type ids: %+v", svc.lastFilter.TaskTypeIDs)
	}
}

func TestTaskHandler_ListTasks_CustomFieldFilters_SelectResolvesFieldType(t *testing.T) {
	svc := newFakeTaskSvc()
	svc.customFields = []*taskdom.CustomFieldDefinition{
		{FieldKey: "priority", FieldType: taskdom.FieldTypeSelect, Options: []string{"Low", "High"}},
	}
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	raw := `{"priority":{"values":["High"]}}`
	path := fmt.Sprintf("/projects/%s/tasks?custom_field_filters=%s", projectID, url.QueryEscape(raw))
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got, ok := svc.lastFilter.CustomFieldFilters["priority"]
	if !ok {
		t.Fatalf("expected CustomFieldFilters to include %q, got %+v", "priority", svc.lastFilter.CustomFieldFilters)
	}
	want := taskdom.CustomFieldFilterQuery{FieldType: "select", Values: []string{"High"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CustomFieldFilters[%q] = %+v, want %+v", "priority", got, want)
	}
}

func TestTaskHandler_ListTasks_CustomFieldFilters_NumberRange(t *testing.T) {
	svc := newFakeTaskSvc()
	svc.customFields = []*taskdom.CustomFieldDefinition{
		{FieldKey: "effort", FieldType: taskdom.FieldTypeNumber},
	}
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	raw := `{"effort":{"min":2,"max":8}}`
	path := fmt.Sprintf("/projects/%s/tasks?custom_field_filters=%s", projectID, url.QueryEscape(raw))
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	got, ok := svc.lastFilter.CustomFieldFilters["effort"]
	if !ok {
		t.Fatalf("expected CustomFieldFilters to include %q, got %+v", "effort", svc.lastFilter.CustomFieldFilters)
	}
	if got.FieldType != "number" || got.Min == nil || *got.Min != 2 || got.Max == nil || *got.Max != 8 {
		t.Fatalf("CustomFieldFilters[%q] = %+v, want FieldType=number Min=2 Max=8", "effort", got)
	}
}

func TestTaskHandler_ListTasks_CustomFieldFilters_UnknownFieldKeyIgnored(t *testing.T) {
	svc := newFakeTaskSvc()
	svc.customFields = []*taskdom.CustomFieldDefinition{
		{FieldKey: "priority", FieldType: taskdom.FieldTypeSelect, Options: []string{"Low", "High"}},
	}
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	raw := `{"deleted_field":{"values":["x"]}}`
	path := fmt.Sprintf("/projects/%s/tasks?custom_field_filters=%s", projectID, url.QueryEscape(raw))
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(svc.lastFilter.CustomFieldFilters) != 0 {
		t.Fatalf("expected stale field key to be ignored, got %+v", svc.lastFilter.CustomFieldFilters)
	}
}

func TestTaskHandler_ListTasks_CustomFieldFilters_InvalidJSONReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	path := fmt.Sprintf("/projects/%s/tasks?custom_field_filters=%s", projectID, url.QueryEscape("not-json"))
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_ListTasks_DateRangeFilters(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	path := fmt.Sprintf(
		"/projects/%s/tasks?start_date_after=2024-01-01&start_date_before=2024-06-01&due_date_after=2024-02-01&due_date_before=2024-07-01",
		projectID,
	)
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f := svc.lastFilter
	if f.StartDateAfter == nil || *f.StartDateAfter != "2024-01-01" {
		t.Errorf("expected StartDateAfter=2024-01-01, got %+v", f.StartDateAfter)
	}
	if f.StartDateBefore == nil || *f.StartDateBefore != "2024-06-01" {
		t.Errorf("expected StartDateBefore=2024-06-01, got %+v", f.StartDateBefore)
	}
	if f.DueDateAfter == nil || *f.DueDateAfter != "2024-02-01" {
		t.Errorf("expected DueDateAfter=2024-02-01, got %+v", f.DueDateAfter)
	}
	if f.DueDateBefore == nil || *f.DueDateBefore != "2024-07-01" {
		t.Errorf("expected DueDateBefore=2024-07-01, got %+v", f.DueDateBefore)
	}
}

func TestTaskHandler_ListTasks_InvalidDateFilterReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	path := fmt.Sprintf("/projects/%s/tasks?due_date_after=not-a-date", projectID)
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_ListTasks_StoryPointsRangeFilter(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	path := fmt.Sprintf("/projects/%s/tasks?story_points_min=2&story_points_max=8", projectID)
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	f := svc.lastFilter
	if f.StoryPointsMin == nil || *f.StoryPointsMin != 2 {
		t.Errorf("expected StoryPointsMin=2, got %+v", f.StoryPointsMin)
	}
	if f.StoryPointsMax == nil || *f.StoryPointsMax != 8 {
		t.Errorf("expected StoryPointsMax=8, got %+v", f.StoryPointsMax)
	}
}

func TestTaskHandler_ListTasks_InvalidStoryPointsFilterReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	path := fmt.Sprintf("/projects/%s/tasks?story_points_min=abc", projectID)
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_ListTasks_ImportanceRangesFilter(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	raw := `[{"min":1,"max":19},{"min":100,"max":2147483647}]`
	path := fmt.Sprintf("/projects/%s/tasks?importance_ranges=%s", projectID, url.QueryEscape(raw))
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	want := []taskdom.IntRange{{Min: 1, Max: 19}, {Min: 100, Max: 2147483647}}
	if !reflect.DeepEqual(svc.lastFilter.ImportanceRanges, want) {
		t.Fatalf("ImportanceRanges = %+v, want %+v", svc.lastFilter.ImportanceRanges, want)
	}
}

func TestTaskHandler_ListTasks_ImportanceRanges_InvalidJSONReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	path := fmt.Sprintf("/projects/%s/tasks?importance_ranges=%s", projectID, url.QueryEscape("not-json"))
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_ListTasks_ImportanceRanges_MinGreaterThanMaxReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	raw := `[{"min":50,"max":10}]`
	path := fmt.Sprintf("/projects/%s/tasks?importance_ranges=%s", projectID, url.QueryEscape(raw))
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_ListTasks_TagsFilter(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	path := fmt.Sprintf("/projects/%s/tasks?tags=urgent,%s", projectID, url.QueryEscape("needs review"))
	w := doTaskRequest(r, http.MethodGet, path, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	want := []string{"urgent", "needs review"}
	if !reflect.DeepEqual(svc.lastFilter.Tags, want) {
		t.Fatalf("Tags = %+v, want %+v", svc.lastFilter.Tags, want)
	}
}

func TestTaskHandler_ListTasks_ResponseIncludesTotalCount(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	// Pre-populate 3 tasks for the project.
	for i := 0; i < 3; i++ {
		_, _ = svc.CreateTask(context.Background(), taskdom.CreateTaskInput{
			ProjectID: projectID,
			Title:     fmt.Sprintf("Task %d", i),
		})
	}

	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks", projectID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			TotalCount float64 `json:"total_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if env.Data.TotalCount != 3 {
		t.Errorf("expected total_count=3, got %v", env.Data.TotalCount)
	}
}

func TestTaskHandler_CreateTask_Returns201(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "New Task"},
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	id := decodeTaskID(t, w.Body.Bytes())
	if id == "" {
		t.Error("expected non-empty id in response")
	}
}

func TestTaskHandler_CreateTask_EmptyTitleReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": ""},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTaskHandler_CreateTask_StringDescriptionReturns400 guards against
// GitHub issue #233: a plain string description (instead of a BlockNote
// block array) must be rejected up front, not stored and later crash the
// web app's `.map()`-based block renderers.
func TestTaskHandler_CreateTask_StringDescriptionReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "New Task", "description": "just a plain string"},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTaskHandler_UpdateTask_StringDescriptionReturns400 mirrors the create
// case above for PATCH /projects/:projectId/tasks/:taskId.
func TestTaskHandler_UpdateTask_StringDescriptionReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	createW := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "New Task"},
	)
	taskID := decodeTaskID(t, createW.Body.Bytes())

	w := doTaskRequest(r, http.MethodPatch,
		fmt.Sprintf("/projects/%s/tasks/%s", projectID, taskID),
		map[string]any{"description": "just a plain string"},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_GetTask_Returns200(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	// Create first
	createW := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "Test Task"},
	)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: got %d", createW.Code)
	}
	taskID := decodeTaskID(t, createW.Body.Bytes())

	// Get by ID
	getW := doTaskRequest(r, http.MethodGet,
		fmt.Sprintf("/projects/%s/tasks/%s", projectID, taskID),
		nil,
	)
	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	gotID := decodeTaskID(t, getW.Body.Bytes())
	if gotID != taskID {
		t.Errorf("expected id %s, got %s", taskID, gotID)
	}
}

func TestTaskHandler_GetTask_NotFoundReturns404(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodGet,
		fmt.Sprintf("/projects/%s/tasks/%s", projectID, uuid.New()),
		nil,
	)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestTaskHandler_UpdateTask_StatusOnlyPreservesSprintID verifies that a PATCH
// with only status_id does not clear other fields (the partial-update bug fix).
func TestTaskHandler_UpdateTask_StatusOnlyPreservesSprintID(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	sprintID := uuid.New()
	statusID := uuid.New()

	// Create a task assigned to a sprint.
	createW := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{
			"title":     "Sprint Task",
			"sprint_id": sprintID.String(),
		},
	)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: got %d", createW.Code)
	}
	taskID := decodeTaskID(t, createW.Body.Bytes())

	// PATCH with only status_id — sprint_id must not be cleared.
	patchW := doTaskRequest(r, http.MethodPatch,
		fmt.Sprintf("/projects/%s/tasks/%s", projectID, taskID),
		map[string]any{"status_id": statusID.String()},
	)
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", patchW.Code, patchW.Body.String())
	}
	gotSprintID := decodeTaskField(t, patchW.Body.Bytes(), "sprint_id")
	if gotSprintID != sprintID.String() {
		t.Errorf("expected sprint_id %s to be preserved, got %v", sprintID, gotSprintID)
	}
}

// TestTaskHandler_UpdateTask_NullSprintIDClearsField verifies that sending
// sprint_id=null explicitly removes the sprint assignment.
func TestTaskHandler_UpdateTask_NullSprintIDClearsField(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	sprintID := uuid.New()

	createW := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "Sprint Task", "sprint_id": sprintID.String()},
	)
	taskID := decodeTaskID(t, createW.Body.Bytes())

	// Send sprint_id: null explicitly.
	body := []byte(`{"sprint_id": null}`)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPatch,
		fmt.Sprintf("/projects/%s/tasks/%s", projectID, taskID),
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	gotSprintID := decodeTaskField(t, w.Body.Bytes(), "sprint_id")
	if gotSprintID != nil {
		t.Errorf("expected sprint_id=nil after explicit null, got %v", gotSprintID)
	}
}

func TestTaskHandler_DeleteTask_Returns200(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	createW := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "Delete Me"},
	)
	taskID := decodeTaskID(t, createW.Body.Bytes())

	delW := doTaskRequest(r, http.MethodDelete,
		fmt.Sprintf("/projects/%s/tasks/%s", projectID, taskID),
		nil,
	)
	if delW.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", delW.Code, delW.Body.String())
	}
}

func TestTaskHandler_DeleteTask_NotFoundReturns404(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodDelete,
		fmt.Sprintf("/projects/%s/tasks/%s", projectID, uuid.New()),
		nil,
	)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_CreateTaskType_Returns201(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/task-types", projectID),
		map[string]any{"name": "Bug"},
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_InvalidTaskID_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodGet,
		fmt.Sprintf("/projects/%s/tasks/not-a-uuid", projectID),
		nil,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid task id, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Task Number handler tests
// ---------------------------------------------------------------------------

func TestTaskHandler_CreateTask_ResponseContainsTaskNumber(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "Numbered Task"},
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	taskNum := decodeTaskField(t, w.Body.Bytes(), "task_number")
	if taskNum == nil {
		t.Fatal("expected task_number in response")
	}
	if taskNum.(float64) < 1 {
		t.Errorf("expected task_number >= 1, got %v", taskNum)
	}
}

func TestTaskHandler_GetTaskByNumber_Returns200(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	// Create task first to get a task number.
	createW := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "Task by number"},
	)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: got %d", createW.Code)
	}
	taskNum := decodeTaskField(t, createW.Body.Bytes(), "task_number").(float64)
	originalID := decodeTaskID(t, createW.Body.Bytes())

	// Look up by task number.
	getW := doTaskRequest(r, http.MethodGet,
		fmt.Sprintf("/projects/%s/tasks/by-number/%d", projectID, int64(taskNum)),
		nil,
	)
	if getW.Code != http.StatusOK {
		t.Fatalf("get by number: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	gotID := decodeTaskID(t, getW.Body.Bytes())
	if gotID != originalID {
		t.Errorf("expected id %s, got %s", originalID, gotID)
	}
}

func TestTaskHandler_GetTaskByNumber_NotFoundReturns404(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	w := doTaskRequest(r, http.MethodGet,
		fmt.Sprintf("/projects/%s/tasks/by-number/9999", projectID),
		nil,
	)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_GetTaskByNumber_InvalidNumberReturns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	w := doTaskRequest(r, http.MethodGet,
		fmt.Sprintf("/projects/%s/tasks/by-number/not-a-number", projectID),
		nil,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid task number, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Helpers for actor-aware requests
// ---------------------------------------------------------------------------

func doTaskRequestWithActor(r chi.Router, method, path string, body any, actorID uuid.UUID) *httptest.ResponseRecorder {
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequestWithContext(middleware.WithActorID(context.Background(), actorID), method, path, buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// Activity handler tests
// ---------------------------------------------------------------------------

func TestActivityHandler_ListEmpty(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	taskID := uuid.New()

	w := doTaskRequest(r, http.MethodGet,
		fmt.Sprintf("/projects/%s/tasks/%s/activities", projectID, taskID),
		nil,
	)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivityHandler_AddComment(t *testing.T) {
	svc := newFakeTaskSvc()
	actSvc := newFakeActivitySvc()
	r := buildTaskHandlerRouterWithActivity(svc, actSvc)
	projectID := uuid.New()
	taskID := uuid.New()
	actorID := uuid.New()

	w := doTaskRequestWithActor(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments", projectID, taskID),
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "hello world"}}}}},
		actorID,
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivityHandler_AddComment_NoActor(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	taskID := uuid.New()

	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments", projectID, taskID),
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "hello world"}}}}},
	)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivityHandler_AddComment_EmptyText(t *testing.T) {
	svc := newFakeTaskSvc()
	actSvc := newFakeActivitySvc()
	r := buildTaskHandlerRouterWithActivity(svc, actSvc)
	projectID := uuid.New()
	taskID := uuid.New()
	actorID := uuid.New()

	w := doTaskRequestWithActor(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments", projectID, taskID),
		map[string]any{"content": []map[string]any{}},
		actorID,
	)
	if w.Code == http.StatusCreated {
		t.Fatalf("expected error, got 201")
	}
}

func TestActivityHandler_UpdateAndDeleteComment(t *testing.T) {
	svc := newFakeTaskSvc()
	actSvc := newFakeActivitySvc()
	r := buildTaskHandlerRouterWithActivity(svc, actSvc)
	projectID := uuid.New()
	taskID := uuid.New()
	actorID := uuid.New()

	w := doTaskRequestWithActor(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments", projectID, taskID),
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "original"}}}}},
		actorID,
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("add comment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	commentID := created.Data.ID

	w = doTaskRequestWithActor(r, http.MethodPatch,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments/%s", projectID, taskID, commentID),
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "updated"}}}}},
		actorID,
	)
	if w.Code != http.StatusOK {
		t.Fatalf("update comment: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	w = doTaskRequestWithActor(r, http.MethodDelete,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments/%s", projectID, taskID, commentID),
		nil,
		actorID,
	)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete comment: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivityHandler_UpdateComment_Forbidden(t *testing.T) {
	svc := newFakeTaskSvc()
	actSvc := newFakeActivitySvc()
	r := buildTaskHandlerRouterWithActivity(svc, actSvc)
	projectID := uuid.New()
	taskID := uuid.New()
	actorID := uuid.New()
	otherActor := uuid.New()

	w := doTaskRequestWithActor(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments", projectID, taskID),
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "mine"}}}}},
		actorID,
	)
	if w.Code != http.StatusCreated {
		t.Fatalf("add comment: expected 201, got %d", w.Code)
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)

	w = doTaskRequestWithActor(r, http.MethodPatch,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments/%s", projectID, taskID, created.Data.ID),
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "hacked"}}}}},
		otherActor,
	)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// fakeIsContentEmpty mirrors the production isContentEmpty logic to keep
// handler-test validation in sync with service-layer validation.
func fakeIsContentEmpty(content json.RawMessage) bool {
	if len(content) == 0 {
		return true
	}
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || trimmed == "[]" || trimmed == "null" {
		return true
	}
	var str string
	if json.Unmarshal([]byte(trimmed), &str) == nil {
		return strings.TrimSpace(str) == ""
	}
	return false
}

// fakeIsContentTypeValid mirrors the production isContentTypeValid logic.
func fakeIsContentTypeValid(content json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(content))
	var arr []any
	if json.Unmarshal([]byte(trimmed), &arr) == nil {
		return true
	}
	var legacy struct {
		Text string `json:"text"`
	}
	return json.Unmarshal([]byte(trimmed), &legacy) == nil && legacy.Text != ""
}

// ---------------------------------------------------------------------------
// Validation tests for additional routes not in buildTaskHandlerRouter
// ---------------------------------------------------------------------------

func buildTaskStatusRouter(svc *fakeTaskSvc) chi.Router {
	h := handler.NewTaskHandler(svc, &fakeViewSvcTask{}, newFakeActivitySvc())
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Post("/task-statuses", h.CreateTaskStatus)
	})
	return r
}

func buildCustomFieldRouter(svc *fakeTaskSvc) chi.Router {
	h := handler.NewTaskHandler(svc, &fakeViewSvcTask{}, newFakeActivitySvc())
	r := chi.NewRouter()
	r.Route("/projects/{projectId}", func(r chi.Router) {
		r.Post("/custom-fields", h.CreateCustomFieldDefinition)
	})
	return r
}

func TestTaskHandler_CreateTaskType_EmptyName_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/task-types", projectID),
		map[string]any{"name": ""},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_CreateTaskStatus_EmptyName_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskStatusRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/task-statuses", projectID),
		map[string]any{"name": "", "category": "todo"},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_CreateTaskStatus_EmptyCategory_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskStatusRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/task-statuses", projectID),
		map[string]any{"name": "In Progress", "category": ""},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_CreateCustomFieldDefinition_MissingFieldKey_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildCustomFieldRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/custom-fields", projectID),
		map[string]any{"field_key": "", "display_name": "Priority", "field_type": "text"},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing field_key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_CreateCustomFieldDefinition_MissingDisplayName_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildCustomFieldRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/custom-fields", projectID),
		map[string]any{"field_key": "priority", "display_name": "", "field_type": "text"},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing display_name, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_CreateCustomFieldDefinition_MissingFieldType_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildCustomFieldRouter(svc)
	projectID := uuid.New()
	w := doTaskRequest(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/custom-fields", projectID),
		map[string]any{"field_key": "priority", "display_name": "Priority", "field_type": ""},
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing field_type, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivityHandler_AddComment_NullContent_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	actSvc := newFakeActivitySvc()
	r := buildTaskHandlerRouterWithActivity(svc, actSvc)
	projectID := uuid.New()
	taskID := uuid.New()
	actorID := uuid.New()

	// content field absent → json.RawMessage is nil → len == 0 → handler returns 400
	w := doTaskRequestWithActor(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments", projectID, taskID),
		map[string]any{},
		actorID,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nil content, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivityHandler_UpdateComment_NullContent_Returns400(t *testing.T) {
	svc := newFakeTaskSvc()
	actSvc := newFakeActivitySvc()
	r := buildTaskHandlerRouterWithActivity(svc, actSvc)
	projectID := uuid.New()
	taskID := uuid.New()
	actorID := uuid.New()

	// First create a comment so we have an ID
	createW := doTaskRequestWithActor(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments", projectID, taskID),
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "original"}}}}},
		actorID,
	)
	if createW.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Update with null content → handler returns 400
	w := doTaskRequestWithActor(r, http.MethodPatch,
		fmt.Sprintf("/projects/%s/tasks/%s/activities/comments/%s", projectID, taskID, created.Data.ID),
		map[string]any{},
		actorID,
	)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nil content, got %d: %s", w.Code, w.Body.String())
	}
}
