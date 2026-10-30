package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
)

// ---------------------------------------------------------------------------
// Fake task service
// ---------------------------------------------------------------------------

type fakeTaskSvc struct {
	mu                    sync.RWMutex
	tasks                 map[uuid.UUID]*taskdom.Task
	types                 map[uuid.UUID]*taskdom.TaskType
	lastProjectID         uuid.UUID
	lastFilter            taskdom.TaskFilter
	customFields          []*taskdom.CustomFieldDefinition
	lastAssignedMemberIDs []uuid.UUID
	lastColumnsBy         string
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

func (f *fakeTaskSvc) ListDistinctTags(_ context.Context, _ uuid.UUID) ([]string, error) {
	return nil, nil
}

func (f *fakeTaskSvc) ListAssignedTasks(_ context.Context, memberIDs []uuid.UUID, limit int, _ *string) ([]*taskdom.Task, bool, error) {
	f.mu.Lock()
	f.lastAssignedMemberIDs = memberIDs
	f.mu.Unlock()

	f.mu.RLock()
	defer f.mu.RUnlock()
	want := make(map[uuid.UUID]bool, len(memberIDs))
	for _, id := range memberIDs {
		want[id] = true
	}
	var out []*taskdom.Task
	for _, t := range f.tasks {
		for _, a := range t.AssigneeIDs {
			if want[a] {
				cp := *t
				out = append(out, &cp)
				break
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, false, nil
}

func (f *fakeTaskSvc) CountOpenTasksByProjects(_ context.Context, _ []uuid.UUID) (int64, error) {
	return 0, nil
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
		AssigneeIDs:  in.AssigneeIDs,
		ReporterID:   in.ReporterID,
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
	applyFakeTaskUpdate(t, in)
	cp := *t
	return &cp, nil
}

func (f *fakeTaskSvc) UpdateTaskAtomic(_ context.Context, _, id uuid.UUID, decide func(current *taskdom.Task) (taskdom.UpdateTaskInput, bool)) (*taskdom.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tasks[id]
	if !ok {
		return nil, taskdom.ErrTaskNotFound
	}
	current := *t
	in, ok := decide(&current)
	if !ok {
		return &current, nil
	}
	applyFakeTaskUpdate(t, in)
	cp := *t
	return &cp, nil
}

// applyFakeTaskUpdate mutates t with whatever subset of fields this fake
// supports — shared by UpdateTask and UpdateTaskAtomic so both apply
// identical (if intentionally partial) field handling.
func applyFakeTaskUpdate(t *taskdom.Task, in taskdom.UpdateTaskInput) {
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

func (f *fakeTaskSvc) ListTaskLinks(_ context.Context, _, _ uuid.UUID, _ *string, _ int) ([]*taskdom.TaskLink, bool, int, error) {
	return []*taskdom.TaskLink{}, false, 0, nil
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
	pageCursor *string
	pageLimit  int
	nextCursor *string
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

func (f *fakeActivitySvc) ListActivities(_ context.Context, _, taskID uuid.UUID, cursor *string, limit int) ([]*taskdom.Activity, *string, error) {
	f.pageCursor, f.pageLimit = cursor, limit
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []*taskdom.Activity
	for _, a := range f.activities {
		if a.TaskID == taskID && a.DeletedAt == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, f.nextCursor, nil
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

func (f *fakeActivitySvc) UpdateComment(_ context.Context, id uuid.UUID, _ uuid.UUID, actorID uuid.UUID, content json.RawMessage) (*taskdom.Activity, error) {
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

func (f *fakeActivitySvc) DeleteComment(_ context.Context, id uuid.UUID, _ uuid.UUID, actorID uuid.UUID) error {
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

func (f *fakeViewSvcTask) ListViews(_ context.Context, _, _ uuid.UUID) ([]*sprintdom.SprintView, error) {
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

func (f *fakeViewSvcTask) ReorderViews(_ context.Context, _, _ uuid.UUID, _ []uuid.UUID) error {
	return nil
}

func (f *fakeViewSvcTask) ReorderProjectViews(_ context.Context, _ uuid.UUID, _ sprintdom.ViewContext, _ []uuid.UUID) error {
	return nil
}

func (f *fakeViewSvcTask) SetUserViewConfig(_ context.Context, _, _, _ uuid.UUID, _ sprintdom.ViewConfig) (*sprintdom.SprintView, error) {
	return nil, nil
}

func (f *fakeViewSvcTask) ClearUserViewConfig(_ context.Context, _, _, _ uuid.UUID) (*sprintdom.SprintView, error) {
	return nil, nil
}

func (f *fakeViewSvcTask) OverlayUserConfigs(_ context.Context, _ uuid.UUID, _ []*sprintdom.SprintView) error {
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
		r.Get("/tasks/count", h.CountTasks)
		r.Get("/tasks/columns", h.ListTaskColumns)
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

func TestTaskHandler_CountTasks_ParsesFiltersAndReturnsTotal(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()
	sprintID := uuid.New()
	statusID := uuid.New()

	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks/count?sprint_id=%s&status_ids=%s", projectID, sprintID, statusID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"total_count":0`) {
		t.Fatalf("expected total_count in body, got %s", w.Body.String())
	}
}

func TestTaskHandler_CountTasks_RejectsInvalidFilter(t *testing.T) {
	r := buildTaskHandlerRouter(newFakeTaskSvc())
	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks/count?sprint_id=nope", uuid.New()), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// include=assignees,reporter embeds the people behind assignee_ids /
// reporter_id with one batched members lookup per project; without it the
// response is unchanged.
func TestTaskHandler_ListTasks_IncludeEmbedsAssigneesAndReporter(t *testing.T) {
	projectID := uuid.New()
	alice, bob, gone := uuid.New(), uuid.New(), uuid.New()
	taskSvc := newFakeTaskSvc()
	taskID := uuid.New()
	taskSvc.tasks[taskID] = &taskdom.Task{
		ID: taskID, ProjectID: projectID, Title: "t",
		AssigneeIDs: []uuid.UUID{alice, gone}, ReporterID: &bob,
	}
	var calls int
	var gotFilter projectdom.MemberFilter
	projectSvc := &mockProjectSvc{
		listMembers: func(_ context.Context, pid uuid.UUID, _ int, _ *string, f projectdom.MemberFilter) ([]*projectdom.ProjectMember, bool, error) {
			calls++
			gotFilter = f
			return []*projectdom.ProjectMember{
				{ID: alice, ProjectID: pid, UserID: uuid.New(), Username: "alice", FullName: "Alice", Description: "secret note"},
				{ID: bob, ProjectID: pid, UserID: uuid.New(), Username: "bob", FullName: "Bob"},
			}, false, nil
		},
	}
	h := handler.NewTaskHandler(taskSvc, &fakeViewSvcTask{}, newFakeActivitySvc(),
		handler.WithTaskAssignedProjectService(projectSvc))
	r := chi.NewRouter()
	r.Get("/projects/{projectId}/tasks", h.ListTasks)

	type listResp struct {
		Data struct {
			Items []struct {
				AssigneeIDs []string         `json:"assignee_ids"`
				Assignees   []map[string]any `json:"assignees"`
				Reporter    map[string]any   `json:"reporter"`
			} `json:"items"`
		} `json:"data"`
	}
	get := func(q string) listResp {
		w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks%s", projectID, q), nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d: %s", q, w.Code, w.Body.String())
		}
		var out listResp
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	plain := get("")
	if calls != 0 || len(plain.Data.Items) != 1 || plain.Data.Items[0].Assignees != nil || plain.Data.Items[0].Reporter != nil {
		t.Fatalf("without include: calls=%d resp=%+v", calls, plain.Data.Items)
	}

	got := get("?include=assignees,reporter")
	it := got.Data.Items[0]
	if calls != 1 || !gotFilter.SkipRoles || len(gotFilter.AccountIDs) != 3 {
		t.Fatalf("expected one batched lookup of 3 ids without roles, calls=%d filter=%+v", calls, gotFilter)
	}
	if len(it.Assignees) != 1 || it.Assignees[0]["username"] != "alice" {
		t.Fatalf("assignees = %v (the removed member must be left out)", it.Assignees)
	}
	if len(it.AssigneeIDs) != 2 {
		t.Fatalf("assignee_ids must stay intact, got %v", it.AssigneeIDs)
	}
	if it.Reporter == nil || it.Reporter["username"] != "bob" {
		t.Fatalf("reporter = %v", it.Reporter)
	}
	if it.Assignees[0]["description"] != "" {
		t.Fatalf("the member's description must not be exposed: %v", it.Assignees[0])
	}

	if w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks?include=owner", projectID), nil); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown include: got %d", w.Code)
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
		{FieldKey: "priority", FieldType: taskdom.FieldTypeSelect, Options: []taskdom.CustomFieldOption{{Value: "Low"}, {Value: "High"}}},
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
		{FieldKey: "priority", FieldType: taskdom.FieldTypeSelect, Options: []taskdom.CustomFieldOption{{Value: "Low"}, {Value: "High"}}},
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

// ---------------------------------------------------------------------------
// ListAssignedToMe
// ---------------------------------------------------------------------------

func buildAssignedToMeRouter(taskSvc *fakeTaskSvc, projectSvc *mockProjectSvc, userID uuid.UUID) chi.Router {
	h := handler.NewTaskHandler(taskSvc, &fakeViewSvcTask{}, newFakeActivitySvc(),
		handler.WithTaskAssignedProjectService(projectSvc))
	r := chi.NewRouter()
	r.Use(claimsMiddleware(userID.String()))
	r.Get("/users/me/tasks", h.ListAssignedToMe)
	return r
}

func TestTaskHandler_ListAssignedToMe_ResolvesMemberIDsAcrossProjects(t *testing.T) {
	userID := uuid.New()
	projectA := &projectdom.Project{ID: uuid.New(), Name: "A"}
	projectB := &projectdom.Project{ID: uuid.New(), Name: "B"}
	memberInA := uuid.New()
	memberInB := uuid.New()

	projectSvc := &mockProjectSvc{
		listAccessible: func(_ context.Context, uid uuid.UUID, _, _ int) ([]*projectdom.Project, int64, error) {
			if uid != userID {
				t.Fatalf("unexpected userID passed to ListAccessible: %s", uid)
			}
			return []*projectdom.Project{projectA, projectB}, 2, nil
		},
		listAllMembers: func(_ context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
			switch projectID {
			case projectA.ID:
				return []*projectdom.ProjectMember{
					{ID: memberInA, ProjectID: projectA.ID, AccountID: userID},
					{ID: uuid.New(), ProjectID: projectA.ID, AccountID: uuid.New()},
				}, nil
			case projectB.ID:
				return []*projectdom.ProjectMember{
					{ID: memberInB, ProjectID: projectB.ID, AccountID: userID},
				}, nil
			}
			return nil, nil
		},
	}

	taskSvc := newFakeTaskSvc()
	// Assigned to the caller (via their per-project member ID) — should be returned.
	_, _ = taskSvc.CreateTask(context.Background(), taskdom.CreateTaskInput{
		ProjectID: projectA.ID, Title: "Mine in A", AssigneeIDs: []uuid.UUID{memberInA},
	})
	_, _ = taskSvc.CreateTask(context.Background(), taskdom.CreateTaskInput{
		ProjectID: projectB.ID, Title: "Mine in B", AssigneeIDs: []uuid.UUID{memberInB},
	})
	// Assigned to someone else — should NOT be returned.
	_, _ = taskSvc.CreateTask(context.Background(), taskdom.CreateTaskInput{
		ProjectID: projectA.ID, Title: "Not mine", AssigneeIDs: []uuid.UUID{uuid.New()},
	})

	r := buildAssignedToMeRouter(taskSvc, projectSvc, userID)
	w := doTaskRequest(r, http.MethodGet, "/users/me/tasks", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(env.Data.Items) != 2 {
		t.Fatalf("expected 2 assigned tasks, got %d: %+v", len(env.Data.Items), env.Data.Items)
	}

	gotMemberIDs := taskSvc.lastAssignedMemberIDs
	if len(gotMemberIDs) != 2 {
		t.Fatalf("expected 2 resolved member ids, got %+v", gotMemberIDs)
	}
	want := map[uuid.UUID]bool{memberInA: true, memberInB: true}
	for _, id := range gotMemberIDs {
		if !want[id] {
			t.Errorf("unexpected member id resolved: %s", id)
		}
	}
}

func TestTaskHandler_ListAssignedToMe_NoProjectsReturnsEmptyList(t *testing.T) {
	userID := uuid.New()
	projectSvc := &mockProjectSvc{
		listAccessible: func(context.Context, uuid.UUID, int, int) ([]*projectdom.Project, int64, error) {
			return nil, 0, nil
		},
	}
	taskSvc := newFakeTaskSvc()
	r := buildAssignedToMeRouter(taskSvc, projectSvc, userID)

	w := doTaskRequest(r, http.MethodGet, "/users/me/tasks", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var env struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(env.Data.Items) != 0 {
		t.Fatalf("expected empty items, got %+v", env.Data.Items)
	}
}

func TestTaskHandler_ListAssignedToMe_UnauthenticatedReturns401(t *testing.T) {
	h := handler.NewTaskHandler(newFakeTaskSvc(), &fakeViewSvcTask{}, newFakeActivitySvc())
	r := chi.NewRouter()
	r.Get("/users/me/tasks", h.ListAssignedToMe)

	w := doTaskRequest(r, http.MethodGet, "/users/me/tasks", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
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

// TestTaskHandler_UpdateTask_AssignmentModeOnlyRecordsActivity is a
// regression test for the report that switching a task to Auto assignment
// mode never resulted in an assignment: TaskAutoAssignConsumer only reacts
// to task.updated activity events on events.StreamActivities, and
// UpdateTask only publishes one when taskChangedFields finds at least one
// change. Flipping an already-unassigned task's assignment_mode to "auto"
// (assignee_ids stays empty, so it isn't itself a "change") used to compute
// zero changes and thus never publish anything — silently stranding the
// task in Auto mode with no consumer ever waking up to resolve an assignee.
func TestTaskHandler_UpdateTask_AssignmentModeOnlyRecordsActivity(t *testing.T) {
	svc := newFakeTaskSvc()
	actSvc := newFakeActivitySvc()
	r := buildTaskHandlerRouterWithActivity(svc, actSvc)
	projectID := uuid.New()
	actorID := uuid.New()

	createW := doTaskRequestWithActor(r, http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID),
		map[string]any{"title": "Needs an owner"},
		actorID,
	)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", createW.Code, createW.Body.String())
	}
	taskID := decodeTaskID(t, createW.Body.Bytes())

	patchW := doTaskRequestWithActor(r, http.MethodPatch,
		fmt.Sprintf("/projects/%s/tasks/%s", projectID, taskID),
		map[string]any{"assignment_mode": "auto", "assignee_ids": []string{}},
		actorID,
	)
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d: %s", patchW.Code, patchW.Body.String())
	}

	activities, _, err := actSvc.ListActivities(context.Background(), projectID, uuid.MustParse(taskID), nil, 100)
	if err != nil {
		t.Fatalf("unexpected error listing activities: %v", err)
	}
	found := false
	for _, a := range activities {
		if a.ActivityType == taskdom.ActivityTypeTaskUpdated {
			var payload struct {
				Changes []taskdom.FieldChange `json:"changes"`
			}
			if err := json.Unmarshal(a.Content, &payload); err == nil {
				for _, ch := range payload.Changes {
					if ch.Field == "assignment_mode" {
						found = true
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("expected a task.updated activity recording the assignment_mode change, found none — TaskAutoAssignConsumer would never be woken up for this task")
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

func TestTaskHandler_ListTaskActivities_PagedMode(t *testing.T) {
	act := newFakeActivitySvc()
	older := "older-page"
	act.nextCursor = &older
	r := buildTaskHandlerRouterWithActivity(newFakeTaskSvc(), act)
	path := fmt.Sprintf("/projects/%s/tasks/%s/activities", uuid.New(), uuid.New())

	w := doTaskRequest(r, http.MethodGet, path+"?page_size=5&cursor=abc", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("got %d: %s", w.Code, w.Body.String())
	}
	if act.pageLimit != 5 || act.pageCursor == nil || *act.pageCursor != "abc" {
		t.Fatalf("limit/cursor = %d/%v", act.pageLimit, act.pageCursor)
	}
	if !strings.Contains(w.Body.String(), `"next_cursor":"older-page"`) {
		t.Fatalf("missing next_cursor: %s", w.Body.String())
	}
	for _, q := range []string{"page_size=0", "page_size=101"} {
		if w := doTaskRequest(r, http.MethodGet, path+"?"+q, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", q, w.Code)
		}
	}
	// page_size is optional: the listing is always paginated.
	act.pageLimit = 0
	if w := doTaskRequest(r, http.MethodGet, path, nil); w.Code != http.StatusOK || act.pageLimit != 30 {
		t.Fatalf("default page size: %d limit=%d", w.Code, act.pageLimit)
	}
}

// ---------------------------------------------------------------------------
// Reporter defaulting / validation
// ---------------------------------------------------------------------------

func buildReporterRouter(svc *fakeTaskSvc, repo *fakeMemberRepo, userID uuid.UUID) chi.Router {
	h := handler.NewTaskHandler(svc, &fakeViewSvcTask{}, newFakeActivitySvc(),
		handler.WithTaskMemberRepository(repo))
	r := chi.NewRouter()
	r.Use(claimsMiddleware(userID.String()))
	r.Post("/projects/{projectId}/tasks", h.CreateTask)
	return r
}

func TestTaskHandler_CreateTask_DefaultsReporterToCallerMember(t *testing.T) {
	userID, projectID, memberID := uuid.New(), uuid.New(), uuid.New()
	svc := newFakeTaskSvc()
	repo := &fakeMemberRepo{findMember: func(_ context.Context, pid, aid uuid.UUID) (*projectdom.ProjectMember, error) {
		if pid != projectID || aid != userID {
			t.Fatalf("unexpected lookup %s/%s", pid, aid)
		}
		return &projectdom.ProjectMember{ID: memberID, ProjectID: projectID}, nil
	}}
	w := doTaskRequest(buildReporterRouter(svc, repo, userID), http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", projectID), map[string]any{"title": "T"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), memberID.String()) {
		t.Fatalf("reporter not defaulted to %s: %s", memberID, w.Body.String())
	}
}

func TestTaskHandler_CreateTask_NonMemberCallerGetsNoReporter(t *testing.T) {
	repo := &fakeMemberRepo{findMember: func(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
		return nil, projectdom.ErrMemberNotFound
	}}
	w := doTaskRequest(buildReporterRouter(newFakeTaskSvc(), repo, uuid.New()), http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", uuid.New()), map[string]any{"title": "T"})
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_CreateTask_MemberLookupFailureSurfaces(t *testing.T) {
	repo := &fakeMemberRepo{findMember: func(context.Context, uuid.UUID, uuid.UUID) (*projectdom.ProjectMember, error) {
		return nil, errors.New("db down")
	}}
	w := doTaskRequest(buildReporterRouter(newFakeTaskSvc(), repo, uuid.New()), http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", uuid.New()), map[string]any{"title": "T"})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTaskHandler_CreateTask_ReporterFromOtherProjectReturns400(t *testing.T) {
	repo := &fakeMemberRepo{findMemberByID: func(_ context.Context, id uuid.UUID) (*projectdom.ProjectMember, error) {
		return &projectdom.ProjectMember{ID: id, ProjectID: uuid.New()}, nil
	}}
	w := doTaskRequest(buildReporterRouter(newFakeTaskSvc(), repo, uuid.New()), http.MethodPost,
		fmt.Sprintf("/projects/%s/tasks", uuid.New()),
		map[string]any{"title": "T", "reporter_id": uuid.New().String()})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func (f *fakeTaskSvc) ListGroupColumns(_ context.Context, _ uuid.UUID, filter taskdom.TaskFilter, by string) ([]taskdom.GroupColumn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastColumnsBy, f.lastFilter = by, filter
	if by == "assignee" {
		// One more than the cap: the list was cut short.
		cols := make([]taskdom.GroupColumn, taskdom.MaxGroupColumns+1)
		for i := range cols {
			cols[i] = taskdom.GroupColumn{Key: fmt.Sprint(i), Label: fmt.Sprint(i)}
		}
		return cols, nil
	}
	return []taskdom.GroupColumn{{Key: "a", Label: "a", Count: 2}}, nil
}

func TestTaskHandler_ListTasks_ParsesGroup(t *testing.T) {
	svc := newFakeTaskSvc()
	svc.customFields = []*taskdom.CustomFieldDefinition{{FieldKey: "size", FieldType: "select"}}
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks?group_by=size&group_key=S", projectID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if g := svc.lastFilter.Group; g == nil || g.By != "size" || g.Key != "S" || g.FieldType != "select" {
		t.Fatalf("group = %+v; want size/S with the field's type resolved server-side", g)
	}

	for _, q := range []string{"group_by=tags", "group_by=nope&group_key=x"} {
		w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks?%s", projectID, q), nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", q, w.Code, w.Body.String())
		}
	}
}

func TestTaskHandler_ListTaskColumns(t *testing.T) {
	svc := newFakeTaskSvc()
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks/columns?column_by=tags&search=x", projectID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"key":"a"`) || !strings.Contains(w.Body.String(), `"count":2`) {
		t.Fatalf("unexpected body %s", w.Body.String())
	}
	if svc.lastColumnsBy != "tags" || svc.lastFilter.Search == nil || *svc.lastFilter.Search != "x" {
		t.Fatalf("by=%q filter=%+v; want tags narrowed by the search", svc.lastColumnsBy, svc.lastFilter)
	}

	for _, q := range []string{"", "column_by=status", "column_by=importance", "column_by=nope"} {
		w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks/columns?%s", projectID, q), nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%q: expected 400, got %d: %s", q, w.Code, w.Body.String())
		}
	}
}

func TestTaskHandler_ListTaskColumns_CustomFieldAndTruncation(t *testing.T) {
	svc := newFakeTaskSvc()
	svc.customFields = []*taskdom.CustomFieldDefinition{{FieldKey: "pts", FieldType: "number"}}
	r := buildTaskHandlerRouter(svc)
	projectID := uuid.New()

	w := doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks/columns?column_by=pts", projectID), nil)
	if w.Code != http.StatusOK || svc.lastColumnsBy != "pts" || !strings.Contains(w.Body.String(), `"truncated":false`) {
		t.Fatalf("custom field: code %d by=%q body %s", w.Code, svc.lastColumnsBy, w.Body.String())
	}

	w = doTaskRequest(r, http.MethodGet, fmt.Sprintf("/projects/%s/tasks/columns?column_by=assignee", projectID), nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"truncated":true`) {
		t.Fatalf("expected truncated list, got %d: %s", w.Code, w.Body.String())
	}
	if n := strings.Count(w.Body.String(), `"key"`); n != taskdom.MaxGroupColumns {
		t.Errorf("returned %d columns, want %d", n, taskdom.MaxGroupColumns)
	}
}
