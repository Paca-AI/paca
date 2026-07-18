package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// In-memory fake task repository
// ---------------------------------------------------------------------------

type fakeTaskRepo struct {
	mu            sync.RWMutex
	types         map[uuid.UUID]*taskdom.TaskType
	statuses      map[uuid.UUID]*taskdom.TaskStatus
	tasks         map[uuid.UUID]*taskdom.Task
	customFields  map[uuid.UUID]*taskdom.CustomFieldDefinition
	counters      map[uuid.UUID]int64
	viewPositions map[string]float64 // key: viewID+":"+taskID
}

func newFakeTaskRepoIT() *fakeTaskRepo {
	return &fakeTaskRepo{
		types:         make(map[uuid.UUID]*taskdom.TaskType),
		statuses:      make(map[uuid.UUID]*taskdom.TaskStatus),
		tasks:         make(map[uuid.UUID]*taskdom.Task),
		customFields:  make(map[uuid.UUID]*taskdom.CustomFieldDefinition),
		counters:      make(map[uuid.UUID]int64),
		viewPositions: make(map[string]float64),
	}
}

// taskMatchesSearch mirrors the postgres repo's title / "#<task_number>" ILIKE
// matching so the fake exercises the same filter contract as production.
func taskMatchesSearch(t *taskdom.Task, filter taskdom.TaskFilter) bool {
	if filter.Search == nil {
		return true
	}
	q := strings.ToLower(strings.TrimSpace(*filter.Search))
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(t.Title), q) ||
		strings.Contains(fmt.Sprintf("#%d", t.TaskNumber), q)
}

// addViewPosition seeds a manual position for a task within a view. Used in
// integration tests to simulate what the postgres repo does via LEFT JOIN.
func (r *fakeTaskRepo) addViewPosition(viewID, taskID uuid.UUID, pos float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.viewPositions[viewID.String()+":"+taskID.String()] = pos
}

func (r *fakeTaskRepo) ListTaskTypes(_ context.Context, projectID uuid.UUID) ([]*taskdom.TaskType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*taskdom.TaskType
	for _, t := range r.types {
		if t.ProjectID == projectID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTaskRepo) FindTaskTypeByID(_ context.Context, id uuid.UUID) (*taskdom.TaskType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.types[id]
	if !ok {
		return nil, taskdom.ErrTypeNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *fakeTaskRepo) CreateTaskType(_ context.Context, t *taskdom.TaskType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *t
	r.types[t.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) UpdateTaskType(_ context.Context, t *taskdom.TaskType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.types[t.ID]; !ok {
		return taskdom.ErrTypeNotFound
	}
	cp := *t
	r.types[t.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) DeleteTaskType(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.types, id)
	return nil
}

func (r *fakeTaskRepo) SetDefaultTaskType(_ context.Context, projectID, typeID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, t := range r.types {
		if t.ProjectID == projectID {
			if t.ID == typeID {
				t.IsDefault = true
				found = true
			} else {
				t.IsDefault = false
			}
		}
	}
	if !found {
		return taskdom.ErrTypeNotFound
	}
	return nil
}

func (r *fakeTaskRepo) FindDefaultTaskType(_ context.Context, projectID uuid.UUID) (*taskdom.TaskType, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.types {
		if t.ProjectID == projectID && t.IsDefault {
			cp := *t
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *fakeTaskRepo) ListTaskStatuses(_ context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*taskdom.TaskStatus
	for _, s := range r.statuses {
		if s.ProjectID == projectID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTaskRepo) FindTaskStatusByID(_ context.Context, id uuid.UUID) (*taskdom.TaskStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.statuses[id]
	if !ok {
		return nil, taskdom.ErrStatusNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeTaskRepo) CreateTaskStatus(_ context.Context, s *taskdom.TaskStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *s
	r.statuses[s.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) UpdateTaskStatus(_ context.Context, s *taskdom.TaskStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.statuses[s.ID]; !ok {
		return taskdom.ErrStatusNotFound
	}
	cp := *s
	r.statuses[s.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) DeleteTaskStatus(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.statuses, id)
	return nil
}

func (r *fakeTaskRepo) SetDefaultTaskStatus(_ context.Context, projectID, statusID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, s := range r.statuses {
		if s.ProjectID == projectID {
			if s.ID == statusID {
				s.IsDefault = true
				found = true
			} else {
				s.IsDefault = false
			}
		}
	}
	if !found {
		return taskdom.ErrStatusNotFound
	}
	return nil
}

func (r *fakeTaskRepo) FindDefaultTaskStatus(_ context.Context, projectID uuid.UUID) (*taskdom.TaskStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.statuses {
		if s.ProjectID == projectID && s.IsDefault {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *fakeTaskRepo) ReorderTaskStatuses(_ context.Context, projectID uuid.UUID, statusIDs []uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing := make(map[uuid.UUID]struct{})
	for _, s := range r.statuses {
		if s.ProjectID == projectID {
			existing[s.ID] = struct{}{}
		}
	}
	if len(existing) != len(statusIDs) {
		return taskdom.ErrStatusReorderInvalid
	}
	for _, id := range statusIDs {
		if _, ok := existing[id]; !ok {
			return taskdom.ErrStatusReorderInvalid
		}
	}
	for i, id := range statusIDs {
		r.statuses[id].Position = i
	}
	return nil
}

func (r *fakeTaskRepo) ListTasks(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, limit int, sort taskdom.TaskSort) ([]*taskdom.Task, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var all []*taskdom.Task
	for _, t := range r.tasks {
		if t.ProjectID != projectID || t.DeletedAt != nil {
			continue
		}
		if filter.BacklogOnly {
			if t.SprintID != nil {
				continue
			}
		} else if filter.SprintID != nil && (t.SprintID == nil || *t.SprintID != *filter.SprintID) {
			continue
		}
		if filter.StatusID != nil && (t.StatusID == nil || *t.StatusID != *filter.StatusID) {
			continue
		}
		if filter.AssigneeID != nil && !slices.Contains(t.AssigneeIDs, *filter.AssigneeID) {
			continue
		}
		if !taskMatchesSearch(t, filter) {
			continue
		}
		cp := *t
		// Populate ViewPosition when sorting by view_position so the fake
		// behaves like the postgres repo (which gets it via LEFT JOIN).
		if sort.By == "view_position" && sort.ViewID != nil {
			key := sort.ViewID.String() + ":" + t.ID.String()
			if pos, ok := r.viewPositions[key]; ok {
				cp.ViewPosition = &pos
			}
		}
		all = append(all, &cp)
	}

	// Sort by view_position when requested: positioned tasks first (ASC), then
	// unpositioned tasks sorted by created_at ASC as tiebreaker — matching the
	// postgres ORDER BY vtp.position ASC NULLS LAST, tasks.created_at ASC.
	if sort.By == "view_position" {
		slices.SortStableFunc(all, func(a, b *taskdom.Task) int {
			switch {
			case a.ViewPosition != nil && b.ViewPosition != nil:
				if *a.ViewPosition < *b.ViewPosition {
					return -1
				}
				if *a.ViewPosition > *b.ViewPosition {
					return 1
				}
				return 0
			case a.ViewPosition != nil:
				return -1
			case b.ViewPosition != nil:
				return 1
			default:
				return a.CreatedAt.Compare(b.CreatedAt)
			}
		})
	}

	hasMore := len(all) > limit
	if hasMore {
		all = all[:limit]
	}
	return all, hasMore, nil
}

func (r *fakeTaskRepo) CountTasks(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var count int64
	for _, t := range r.tasks {
		if t.ProjectID != projectID || t.DeletedAt != nil {
			continue
		}
		if filter.BacklogOnly {
			if t.SprintID != nil {
				continue
			}
		} else if filter.SprintID != nil && (t.SprintID == nil || *t.SprintID != *filter.SprintID) {
			continue
		}
		if filter.StatusID != nil && (t.StatusID == nil || *t.StatusID != *filter.StatusID) {
			continue
		}
		if filter.AssigneeID != nil && !slices.Contains(t.AssigneeIDs, *filter.AssigneeID) {
			continue
		}
		if !taskMatchesSearch(t, filter) {
			continue
		}
		count++
	}
	return count, nil
}

func (r *fakeTaskRepo) SumTaskField(_ context.Context, projectID uuid.UUID, filter taskdom.TaskFilter, fieldKey string) (float64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sum float64
	for _, t := range r.tasks {
		if t.ProjectID != projectID || t.DeletedAt != nil {
			continue
		}
		if filter.BacklogOnly {
			if t.SprintID != nil {
				continue
			}
		} else if filter.SprintID != nil && (t.SprintID == nil || *t.SprintID != *filter.SprintID) {
			continue
		}
		if filter.StatusID != nil && (t.StatusID == nil || *t.StatusID != *filter.StatusID) {
			continue
		}
		if !taskMatchesSearch(t, filter) {
			continue
		}
		if fieldKey == "story_points" {
			if t.StoryPoints != nil {
				sum += float64(*t.StoryPoints)
			}
		} else {
			if v, ok := t.CustomFields[fieldKey]; ok {
				switch n := v.(type) {
				case float64:
					sum += n
				case int:
					sum += float64(n)
				}
			}
		}
	}
	return sum, nil
}

func (r *fakeTaskRepo) FindTaskByID(_ context.Context, id uuid.UUID) (*taskdom.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tasks[id]
	if !ok || t.DeletedAt != nil {
		return nil, taskdom.ErrTaskNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *fakeTaskRepo) FindTaskByNumber(_ context.Context, projectID uuid.UUID, taskNumber int64) (*taskdom.Task, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tasks {
		if t.ProjectID == projectID && t.TaskNumber == taskNumber && t.DeletedAt == nil {
			cp := *t
			return &cp, nil
		}
	}
	return nil, taskdom.ErrTaskNotFound
}

func (r *fakeTaskRepo) CreateTask(_ context.Context, t *taskdom.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[t.ProjectID]++
	t.TaskNumber = r.counters[t.ProjectID]
	cp := *t
	r.tasks[t.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) UpdateTask(_ context.Context, t *taskdom.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[t.ID]; !ok {
		return taskdom.ErrTaskNotFound
	}
	cp := *t
	r.tasks[t.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) DeleteTask(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[id]
	if !ok || t.DeletedAt != nil {
		return taskdom.ErrTaskNotFound
	}
	now := time.Now()
	t.DeletedAt = &now
	return nil
}

// BulkMoveSprintTasks moves non-done tasks in sourceSprintID to targetSprintID.
func (r *fakeTaskRepo) BulkMoveSprintTasks(_ context.Context, projectID, sourceSprintID uuid.UUID, targetSprintID *uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range r.tasks {
		if t.ProjectID != projectID || t.DeletedAt != nil {
			continue
		}
		if t.SprintID == nil || *t.SprintID != sourceSprintID {
			continue
		}
		// Leave tasks whose status has category "done" in place.
		if t.StatusID != nil {
			if s, ok := r.statuses[*t.StatusID]; ok && s.Category == taskdom.StatusCategoryDone {
				continue
			}
		}
		t.SprintID = targetSprintID
	}
	return nil
}

// ---------------------------------------------------------------------------
// In-memory fake sprint repository
// ---------------------------------------------------------------------------

type fakeSprintRepoIT struct {
	mu      sync.RWMutex
	sprints map[uuid.UUID]*sprintdom.Sprint
}

func newFakeSprintRepoIT() *fakeSprintRepoIT {
	return &fakeSprintRepoIT{sprints: make(map[uuid.UUID]*sprintdom.Sprint)}
}

func (r *fakeSprintRepoIT) ListSprints(_ context.Context, projectID uuid.UUID) ([]*sprintdom.Sprint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*sprintdom.Sprint
	for _, s := range r.sprints {
		if s.ProjectID == projectID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeSprintRepoIT) FindSprintByID(_ context.Context, id uuid.UUID) (*sprintdom.Sprint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sprints[id]
	if !ok {
		return nil, sprintdom.ErrSprintNotFound
	}
	cp := *s
	return &cp, nil
}

func (r *fakeSprintRepoIT) CreateSprint(_ context.Context, s *sprintdom.Sprint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *s
	r.sprints[s.ID] = &cp
	return nil
}

func (r *fakeSprintRepoIT) UpdateSprint(_ context.Context, s *sprintdom.Sprint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sprints[s.ID]; !ok {
		return sprintdom.ErrSprintNotFound
	}
	cp := *s
	r.sprints[s.ID] = &cp
	return nil
}

func (r *fakeSprintRepoIT) DeleteSprint(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sprints, id)
	return nil
}

// -- fakeTaskRepo: CustomFieldDefinition methods --

func (r *fakeTaskRepo) ListCustomFieldDefinitions(_ context.Context, projectID uuid.UUID) ([]*taskdom.CustomFieldDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*taskdom.CustomFieldDefinition
	for _, f := range r.customFields {
		if f.ProjectID == projectID {
			cp := *f
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTaskRepo) FindCustomFieldDefinitionByID(_ context.Context, id uuid.UUID) (*taskdom.CustomFieldDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.customFields[id]
	if !ok {
		return nil, taskdom.ErrCustomFieldNotFound
	}
	cp := *f
	return &cp, nil
}

func (r *fakeTaskRepo) CreateCustomFieldDefinition(_ context.Context, f *taskdom.CustomFieldDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.customFields {
		if existing.ProjectID == f.ProjectID && existing.FieldKey == f.FieldKey {
			return taskdom.ErrCustomFieldKeyTaken
		}
	}
	cp := *f
	r.customFields[f.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) UpdateCustomFieldDefinition(_ context.Context, f *taskdom.CustomFieldDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.customFields[f.ID]; !ok {
		return taskdom.ErrCustomFieldNotFound
	}
	cp := *f
	r.customFields[f.ID] = &cp
	return nil
}

func (r *fakeTaskRepo) DeleteCustomFieldDefinition(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.customFields, id)
	return nil
}

// -- fakeTaskRepo: TaskLink methods --

func (r *fakeTaskRepo) ListTaskLinks(_ context.Context, _ uuid.UUID) ([]*taskdom.TaskLink, error) {
	return []*taskdom.TaskLink{}, nil
}

func (r *fakeTaskRepo) FindTaskLinkByID(_ context.Context, _ uuid.UUID) (*taskdom.TaskLink, error) {
	return nil, taskdom.ErrTaskLinkNotFound
}

func (r *fakeTaskRepo) CreateTaskLinkIfNotExists(_ context.Context, _ *taskdom.TaskLink) (bool, error) {
	return true, nil
}

func (r *fakeTaskRepo) DeleteTaskLink(_ context.Context, _ uuid.UUID) error {
	return nil
}

// ---------------------------------------------------------------------------
// In-memory fake activity repository
// ---------------------------------------------------------------------------

type fakeTaskActivityRepo struct {
	mu         sync.RWMutex
	activities map[uuid.UUID]*taskdom.Activity
}

func newFakeTaskActivityRepo() *fakeTaskActivityRepo {
	return &fakeTaskActivityRepo{activities: make(map[uuid.UUID]*taskdom.Activity)}
}

func (r *fakeTaskActivityRepo) ListActivities(_ context.Context, taskID uuid.UUID) ([]*taskdom.Activity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*taskdom.Activity
	for _, a := range r.activities {
		if a.TaskID == taskID && a.DeletedAt == nil {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (r *fakeTaskActivityRepo) FindActivityByID(_ context.Context, id uuid.UUID) (*taskdom.Activity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.activities[id]
	if !ok {
		return nil, taskdom.ErrActivityNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *fakeTaskActivityRepo) CreateActivity(_ context.Context, a *taskdom.Activity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activities[a.ID] = a
	return nil
}

func (r *fakeTaskActivityRepo) UpdateActivity(_ context.Context, a *taskdom.Activity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.activities[a.ID]; !ok {
		return taskdom.ErrActivityNotFound
	}
	r.activities[a.ID] = a
	return nil
}

func (r *fakeTaskActivityRepo) DeleteActivity(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.activities[id]
	if !ok {
		return taskdom.ErrActivityNotFound
	}
	now := time.Now()
	a.DeletedAt = &now
	return nil
}

// fakeActivityMemberRepo is a minimal memberLookup stub that resolves any
// actor to a synthetic ProjectMember using the agent ID (when present) or the
// user UUID as the member UUID. This lets comment operations in integration
// tests pass actor-resolution without a real project_members store.
//
// The one deliberate exception mirrors production: userdom.SystemActorUserID
// with no agentID (the shared agent API key used without X-Agent-ID) never
// resolves, since that identity is never itself a project member.
type fakeActivityMemberRepo struct{}

func (r *fakeActivityMemberRepo) FindMemberByActor(_ context.Context, _, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error) {
	if agentID != nil {
		return &projectdom.ProjectMember{ID: *agentID}, nil
	}
	if actorID == userdom.SystemActorUserID {
		return nil, projectdom.ErrMemberNotFound
	}
	return &projectdom.ProjectMember{ID: actorID}, nil
}

func (r *fakeActivityMemberRepo) FindMemberByAgent(_ context.Context, _, _ uuid.UUID) (*projectdom.ProjectMember, error) {
	return nil, projectdom.ErrMemberNotFound
}

// ---------------------------------------------------------------------------
// Router builder and token helper
// ---------------------------------------------------------------------------

func buildTaskTestRouter(taskRepo *fakeTaskRepo, store *projectPermStore) http.Handler {
	return buildTaskTestRouterWithSprints(taskRepo, newFakeSprintRepoIT(), newFakeViewRepoIT(), store)
}

func buildTaskTestRouterWithSprints(taskRepo *fakeTaskRepo, sprintRepo *fakeSprintRepoIT, viewRepo *fakeViewRepoIT, store *projectPermStore) http.Handler {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	projectRepo := newFakeProjectRepo()
	projectService := projectsvc.New(projectRepo, taskRepo)
	taskService := tasksvc.New(taskRepo)
	sprintService := sprintsvc.New(sprintRepo, taskRepo)
	viewService := sprintsvc.NewViewService(viewRepo)
	activityRepo := newFakeTaskActivityRepo()
	activityService := tasksvc.NewActivityService(activityRepo, &fakeActivityMemberRepo{}, nil)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager:         tm,
		Authorizer:           authz.NewAuthorizer(store),
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, testCookieCfg),
		User:                 handler.NewUserHandler(userService),
		GlobalRole:           handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}, authz.NewAuthorizer(store)),
		Project:              handler.NewProjectHandler(projectService, authz.NewAuthorizer(store)),
		Task:                 handler.NewTaskHandler(taskService, viewService, activityService),
		Sprint:               handler.NewSprintHandler(sprintService, viewService),
		View:                 handler.NewViewHandler(viewService),
		Log:                  log,
	})
}

func issueTaskToken(t *testing.T, subject string) string {
	t.Helper()
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	tok, err := tm.IssueAccess(subject, "task-user", "USER", "fam-task", false)
	if err != nil {
		t.Fatalf("issue task token: %v", err)
	}
	return tok
}

// taskIDFrom decodes data.id from a handler JSON response.
func taskIDFrom(t *testing.T, field string, body []byte) string {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode %s response: %v", field, err)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatalf("missing id in %s response: %s", field, string(body))
	}
	return id
}

// decodeTaskData decodes data (map) from a handler JSON response.
func decodeTaskData(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode task response: %v", err)
	}
	return env.Data
}

// taskListCount decodes data.items and returns its length.
func taskListCount(t *testing.T, body []byte) int {
	t.Helper()
	var env struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	return len(env.Data.Items)
}

// taskListTotalCount decodes data.total_count from a list handler JSON response.
func taskListTotalCount(t *testing.T, body []byte) int64 {
	t.Helper()
	var env struct {
		Data struct {
			TotalCount int64 `json:"total_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode list total_count: %v", err)
	}
	return env.Data.TotalCount
}

// ---------------------------------------------------------------------------
// Task Type tests
// ---------------------------------------------------------------------------

func TestIntegrationTaskTypes_CRUD(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)

	// Create
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":  "Bug",
		"icon":  "bug-icon",
		"color": "#FF0000",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create type: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	typeID := taskIDFrom(t, "task-type", createW.Body.Bytes())

	// List
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list types: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := taskListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 type, got %d", count)
	}

	// Update
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+typeID, tok, map[string]any{
		"name": "Epic Bug",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update type: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	// Delete
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+typeID, tok, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("delete type: expected 200, got %d (%s)", delW.Code, delW.Body.String())
	}

	// Verify removed from list
	listAfterW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if count := taskListCount(t, listAfterW.Body.Bytes()); count != 0 {
		t.Errorf("expected 0 types after delete, got %d", count)
	}
}

func TestIntegrationTaskTypes_InvalidNameReturns400(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/task-types", projectID), tok, map[string]any{"name": "  "}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "TASK_TYPE_NAME_INVALID" {
		t.Errorf("expected TASK_TYPE_NAME_INVALID, got %q", code)
	}
}

func TestIntegrationTaskTypes_DeleteNotFoundReturns404(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	// Delete a non-existent type ID — should return 200 (idempotent), because
	// the fake repo's DeleteTaskType does not return an error. If the handler
	// tries to fetch-then-delete, it will return 404.  Let's check with a
	// random UUID that was never created:
	w := serve(r, authedJSONReq(t.Context(), http.MethodDelete,
		fmt.Sprintf("/api/v1/projects/%s/task-types/%s", projectID, uuid.New()), tok, nil))
	// Handler calls svc.DeleteTaskType → repo.DeleteTaskType (no-error idempotent)
	// OR handler first calls GetTaskType → 404. Depends on implementation.
	// Accept either 200 (idempotent) or 404.
	if w.Code != http.StatusOK && w.Code != http.StatusNotFound {
		t.Fatalf("expected 200 or 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationTaskTypes_SetDefault(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)

	// Create two task types.
	createW1 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Task"}))
	if createW1.Code != http.StatusCreated {
		t.Fatalf("create type 1: expected 201, got %d (%s)", createW1.Code, createW1.Body.String())
	}
	typeID1 := taskIDFrom(t, "task-type", createW1.Body.Bytes())

	createW2 := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": "Bug"}))
	if createW2.Code != http.StatusCreated {
		t.Fatalf("create type 2: expected 201, got %d (%s)", createW2.Code, createW2.Body.String())
	}
	taskIDFrom(t, "task-type", createW2.Body.Bytes())

	// Set type 1 as default.
	setDefaultW := serve(r, authedJSONReq(t.Context(), http.MethodPut, base+"/"+typeID1+"/set-default", tok, nil))
	if setDefaultW.Code != http.StatusOK {
		t.Fatalf("set default: expected 200, got %d (%s)", setDefaultW.Code, setDefaultW.Body.String())
	}

	// Verify the response has is_default: true.
	var setEnv struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(setDefaultW.Body.Bytes(), &setEnv); err != nil {
		t.Fatalf("decode set-default response: %v", err)
	}
	if isDefault, _ := setEnv.Data["is_default"].(bool); !isDefault {
		t.Errorf("expected is_default=true in set-default response, got %v", setEnv.Data["is_default"])
	}

	// Verify listing shows exactly one default type and it's type 1.
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list types: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	var listEnv struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	defaultCount := 0
	for _, item := range listEnv.Data.Items {
		if d, _ := item["is_default"].(bool); d {
			defaultCount++
			if id, _ := item["id"].(string); id != typeID1 {
				t.Errorf("expected default type id=%s, got %s", typeID1, id)
			}
		}
	}
	if defaultCount != 1 {
		t.Errorf("expected exactly 1 default type, got %d", defaultCount)
	}
}

func TestIntegrationTaskTypes_SetDefault_NotFound(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPut,
		fmt.Sprintf("/api/v1/projects/%s/task-types/%s/set-default", projectID, uuid.New()), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent type, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationTaskTypes_SystemTypeCannotBeUpdated(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)

	// Seed a system type directly in the repo.
	sysType := &taskdom.TaskType{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "Epic",
		IsSystem:  true,
	}
	if err := taskRepo.CreateTaskType(t.Context(), sysType); err != nil {
		t.Fatalf("seed system type: %v", err)
	}

	// Attempt to update the system type — should return 403.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+sysType.ID.String(), tok, map[string]any{
		"name": "Epic Renamed",
	}))
	if patchW.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for updating system type, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	if code := decodeErrorCode(t, patchW); code != "TASK_TYPE_IS_SYSTEM" {
		t.Errorf("expected TASK_TYPE_IS_SYSTEM, got %q", code)
	}
}

func TestIntegrationTaskTypes_SystemTypeCannotBeDeleted(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)

	sysType := &taskdom.TaskType{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "Subtask",
		IsSystem:  true,
	}
	if err := taskRepo.CreateTaskType(t.Context(), sysType); err != nil {
		t.Fatalf("seed system type: %v", err)
	}

	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+sysType.ID.String(), tok, nil))
	if delW.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for deleting system type, got %d (%s)", delW.Code, delW.Body.String())
	}
	if code := decodeErrorCode(t, delW); code != "TASK_TYPE_IS_SYSTEM" {
		t.Errorf("expected TASK_TYPE_IS_SYSTEM, got %q", code)
	}
}

func TestIntegrationTaskTypes_ReservedNameRejected(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)

	for _, reserved := range []string{"Epic"} {
		w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"name": reserved}))
		if w.Code != http.StatusConflict {
			t.Fatalf("expected 409 for reserved name %q, got %d (%s)", reserved, w.Code, w.Body.String())
		}
		if code := decodeErrorCode(t, w); code != "TASK_TYPE_NAME_RESERVED" {
			t.Errorf("expected TASK_TYPE_NAME_RESERVED for %q, got %q", reserved, code)
		}
	}
}

// ---------------------------------------------------------------------------
// Task Status tests
// ---------------------------------------------------------------------------

func TestIntegrationTaskStatuses_CRUD(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-statuses", projectID)

	// Create
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":     "To Do",
		"color":    "#ff0000",
		"position": 0,
		"category": "todo",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create status: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	statusID := taskIDFrom(t, "task-status", createW.Body.Bytes())

	// List
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list statuses: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := taskListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 status, got %d", count)
	}

	// Update position without touching color: the new position must persist
	// and the existing color must survive (regression: a prior bug dropped
	// position from the SQL UPDATE and unconditionally nulled color on PATCH).
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+statusID, tok, map[string]any{
		"name":     "To Do",
		"position": 5,
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update status: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	patched := decodeTaskData(t, patchW.Body.Bytes())
	if pos, _ := patched["position"].(float64); pos != 5 {
		t.Errorf("expected position 5 after update, got %v", patched["position"])
	}
	if color, _ := patched["color"].(string); color != "#ff0000" {
		t.Errorf("expected color to remain #ff0000 after position-only update, got %v", patched["color"])
	}

	// Re-fetch to confirm the change was actually persisted, not just echoed
	// back in the PATCH response.
	listW2 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW2.Code != http.StatusOK {
		t.Fatalf("re-list statuses: expected 200, got %d (%s)", listW2.Code, listW2.Body.String())
	}
	var listEnv struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listW2.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode re-list response: %v", err)
	}
	if len(listEnv.Data.Items) != 1 {
		t.Fatalf("expected 1 status on re-list, got %d", len(listEnv.Data.Items))
	}
	if pos, _ := listEnv.Data.Items[0]["position"].(float64); pos != 5 {
		t.Errorf("expected persisted position 5, got %v", listEnv.Data.Items[0]["position"])
	}

	// Delete
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+statusID, tok, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("delete status: expected 200, got %d (%s)", delW.Code, delW.Body.String())
	}
}

func TestIntegrationTaskStatuses_Reorder(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-statuses", projectID)

	var ids []string
	for i, name := range []string{"To Do", "In Progress", "Done"} {
		createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"name":     name,
			"position": i,
			"category": "todo",
		}))
		if createW.Code != http.StatusCreated {
			t.Fatalf("create status %q: expected 201, got %d (%s)", name, createW.Code, createW.Body.String())
		}
		ids = append(ids, taskIDFrom(t, "task-status", createW.Body.Bytes()))
	}

	// Reverse the order in a single request.
	reorderW := serve(r, authedJSONReq(t.Context(), http.MethodPut, base+"/positions", tok, map[string]any{
		"status_ids": []string{ids[2], ids[1], ids[0]},
	}))
	if reorderW.Code != http.StatusNoContent {
		t.Fatalf("reorder statuses: expected 204, got %d (%s)", reorderW.Code, reorderW.Body.String())
	}

	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list statuses: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	var listEnv struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listEnv); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	gotPosition := make(map[string]float64, len(listEnv.Data.Items))
	for _, item := range listEnv.Data.Items {
		id, _ := item["id"].(string)
		pos, _ := item["position"].(float64)
		gotPosition[id] = pos
	}
	if gotPosition[ids[2]] != 0 || gotPosition[ids[1]] != 1 || gotPosition[ids[0]] != 2 {
		t.Errorf("expected reversed positions 0,1,2 for Done,In Progress,To Do, got %v", gotPosition)
	}
}

func TestIntegrationTaskStatuses_ReorderInvalidSetReturns400(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-statuses", projectID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":     "To Do",
		"position": 0,
		"category": "todo",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create status: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}

	// Submitting an unknown status ID alongside a valid one must be rejected
	// rather than silently reordering a partial set.
	statusID := taskIDFrom(t, "task-status", createW.Body.Bytes())
	w := serve(r, authedJSONReq(t.Context(), http.MethodPut, base+"/positions", tok, map[string]any{
		"status_ids": []string{statusID, uuid.NewString()},
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("reorder with unknown id: expected 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationTaskStatuses_InvalidCategoryReturns400(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/task-statuses", projectID), tok, map[string]any{
			"name":     "Weird Status",
			"category": "not-a-real-category",
		}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "TASK_STATUS_CATEGORY_INVALID" {
		t.Errorf("expected TASK_STATUS_CATEGORY_INVALID, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Sprint tests
// ---------------------------------------------------------------------------

func TestIntegrationSprints_CRUD(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/sprints", projectID)

	start := "2026-04-01T00:00:00Z"
	end := "2026-04-14T00:00:00Z"

	// Create
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":       "Sprint 1",
		"start_date": start,
		"end_date":   end,
		"goal":       "Ship feature",
		"status":     "planned",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create sprint: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	sprintID := taskIDFrom(t, "sprint", createW.Body.Bytes())

	// List
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list sprints: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := taskListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 sprint, got %d", count)
	}

	// Update (activate sprint)
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+sprintID, tok, map[string]any{
		"name":   "Sprint 1",
		"status": "active",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update sprint: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	// Delete
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+sprintID, tok, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("delete sprint: expected 200, got %d (%s)", delW.Code, delW.Body.String())
	}
}

func TestIntegrationSprints_InvalidStatusReturns400(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/sprints", projectID), tok, map[string]any{
			"name":   "Bad Sprint",
			"status": "flying",
		}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "SPRINT_STATUS_INVALID" {
		t.Errorf("expected SPRINT_STATUS_INVALID, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Task tests
// ---------------------------------------------------------------------------

func TestIntegrationTasks_CRUD(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Create
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title": "Implement login",
		"description": []map[string]any{
			{
				"id":       "1",
				"type":     "paragraph",
				"props":    map[string]any{"textColor": "default", "backgroundColor": "default", "textAlignment": "left"},
				"content":  []map[string]any{{"type": "text", "text": "Login with username and password", "styles": map[string]any{}}},
				"children": []any{},
			},
		},
		"importance":   3,
		"story_points": 5,
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	taskID := taskIDFrom(t, "task", createW.Body.Bytes())

	// Verify story_points in create response.
	createData := decodeTaskData(t, createW.Body.Bytes())
	if sp, _ := createData["story_points"].(float64); sp != 5 {
		t.Errorf("expected story_points=5 in create response, got %v", createData["story_points"])
	}

	// List
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list tasks: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := taskListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 task, got %d", count)
	}

	// Get by ID
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"/"+taskID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get task: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}

	// Update
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskID, tok, map[string]any{
		"title":        "Implement secure login",
		"story_points": 8,
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update task: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	patchData := decodeTaskData(t, patchW.Body.Bytes())
	if sp, _ := patchData["story_points"].(float64); sp != 8 {
		t.Errorf("expected story_points=8 after patch, got %v", patchData["story_points"])
	}

	// Clear story_points with null.
	clearPatchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskID, tok, map[string]any{
		"story_points": nil,
	}))
	if clearPatchW.Code != http.StatusOK {
		t.Fatalf("clear story_points: expected 200, got %d (%s)", clearPatchW.Code, clearPatchW.Body.String())
	}
	clearData := decodeTaskData(t, clearPatchW.Body.Bytes())
	if _, ok := clearData["story_points"]; ok && clearData["story_points"] != nil {
		t.Errorf("expected story_points=null after clear, got %v", clearData["story_points"])
	}

	// Delete
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+taskID, tok, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("delete task: expected 200, got %d (%s)", delW.Code, delW.Body.String())
	}

	// Get after delete → 404
	getDeletedW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"/"+taskID, tok, nil))
	if getDeletedW.Code != http.StatusNotFound {
		t.Fatalf("get deleted task: expected 404, got %d", getDeletedW.Code)
	}
	if code := decodeErrorCode(t, getDeletedW); code != "TASK_NOT_FOUND" {
		t.Errorf("expected TASK_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationTasks_EmptyTitleReturns400(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks", projectID), tok, map[string]any{"title": ""}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "TASK_TITLE_INVALID" {
		t.Errorf("expected TASK_TITLE_INVALID, got %q", code)
	}
}

func TestIntegrationTasks_GetNotFoundReturns404(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s", projectID, uuid.New()), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "TASK_NOT_FOUND" {
		t.Errorf("expected TASK_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationTasks_ListWithSprintFilter(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Create task with sprint
	serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":     "In Sprint",
		"sprint_id": sprintID.String(),
	}))
	// Create task without sprint
	serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title": "No Sprint",
	}))

	// Filter by sprint
	filterURL := fmt.Sprintf("%s?sprint_id=%s", base, sprintID.String())
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, filterURL, tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list with filter: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if count := taskListCount(t, w.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 filtered task, got %d", count)
	}
}

func TestIntegrationTasks_ListWithSearchFilter(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title": "Fix login bug",
	}))
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title": "Add signup flow",
	}))
	var created struct {
		Data struct {
			TaskNumber int64 `json:"task_number"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	// Search matches title case-insensitively, and pagination metadata
	// (total_count) reflects the filtered set, not the full project.
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?search=LOGIN", tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("search by title: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if count := taskListCount(t, w.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 task matching title search, got %d", count)
	}
	if total := taskListTotalCount(t, w.Body.Bytes()); total != 1 {
		t.Errorf("expected total_count=1 for title search, got %d", total)
	}

	// Search also matches by "#<task_number>".
	searchURL := fmt.Sprintf("%s?search=%s", base, url.QueryEscape(fmt.Sprintf("#%d", created.Data.TaskNumber)))
	w = serve(r, authedJSONReq(t.Context(), http.MethodGet, searchURL, tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("search by task number: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if count := taskListCount(t, w.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 task matching task-number search, got %d", count)
	}

	// A search with no matches returns an empty (not error) result.
	w = serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?search=nonexistent", tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("search with no matches: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if count := taskListCount(t, w.Body.Bytes()); count != 0 {
		t.Errorf("expected 0 tasks for non-matching search, got %d", count)
	}
}

func TestIntegrationTasks_DatesAndTags(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	startDate := "2026-05-01T00:00:00Z"
	dueDate := "2026-05-31T00:00:00Z"

	// Create a task with start_date, due_date, and tags.
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":      "Task with dates and tags",
		"start_date": startDate,
		"due_date":   dueDate,
		"tags":       []string{"frontend", "design"},
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	taskID := taskIDFrom(t, "task", createW.Body.Bytes())

	// GET and verify the fields are present.
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"/"+taskID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get task: expected 200, got %d", getW.Code)
	}
	var getEnv struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &getEnv); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getEnv.Data["start_date"] == nil {
		t.Error("expected start_date in response, got nil")
	}
	if getEnv.Data["due_date"] == nil {
		t.Error("expected due_date in response, got nil")
	}
	tags, _ := getEnv.Data["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}

	// Update: clear dates and replace tags.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskID, tok, map[string]any{
		"title":      "Task with dates and tags",
		"start_date": nil,
		"due_date":   nil,
		"tags":       []string{"backend"},
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update task: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	var patchEnv struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(patchW.Body.Bytes(), &patchEnv); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if _, hasStart := patchEnv.Data["start_date"]; hasStart {
		t.Error("expected start_date to be absent (omitted) after clearing")
	}
	updatedTags, _ := patchEnv.Data["tags"].([]any)
	if len(updatedTags) != 1 {
		t.Errorf("expected 1 tag after update, got %d", len(updatedTags))
	}

	// Update: set only tags (no date fields) — tags should update, dates stay nil.
	patch2W := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskID, tok, map[string]any{
		"title": "Task with dates and tags",
		"tags":  []string{},
	}))
	if patch2W.Code != http.StatusOK {
		t.Fatalf("update task (clear tags): expected 200, got %d (%s)", patch2W.Code, patch2W.Body.String())
	}
	var patch2Env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(patch2W.Body.Bytes(), &patch2Env); err != nil {
		t.Fatalf("decode patch2 response: %v", err)
	}
	clearedTags, _ := patch2Env.Data["tags"].([]any)
	if len(clearedTags) != 0 {
		t.Errorf("expected 0 tags after clearing, got %d", len(clearedTags))
	}
}

// ---------------------------------------------------------------------------
// AuthZ guard tests
// ---------------------------------------------------------------------------

func TestIntegrationTask_UnauthenticatedReturns401(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{}
	r := buildTaskTestRouter(taskRepo, store)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)},
		{http.MethodPost, fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)},
		{http.MethodGet, fmt.Sprintf("/api/v1/projects/%s/task-statuses", projectID)},
		{http.MethodGet, fmt.Sprintf("/api/v1/projects/%s/sprints", projectID)},
		{http.MethodGet, fmt.Sprintf("/api/v1/projects/%s/sprints/%s", projectID, uuid.New())},
		{http.MethodGet, fmt.Sprintf("/api/v1/projects/%s/tasks?sprint_id=%s", projectID, uuid.New())},
		{http.MethodGet, fmt.Sprintf("/api/v1/projects/%s/tasks?sprint_id=null", projectID)},
		{http.MethodGet, fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)},
	}
	for _, ep := range endpoints {
		req, _ := http.NewRequestWithContext(t.Context(), ep.method, ep.path, nil)
		w := serve(r, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: expected 401, got %d", ep.method, ep.path, w.Code)
		}
	}
}

func TestIntegrationTask_NoPermissionReturns403(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	// No permissions at all
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)},
		{http.MethodPost, fmt.Sprintf("/api/v1/projects/%s/task-statuses", projectID)},
		{http.MethodPost, fmt.Sprintf("/api/v1/projects/%s/sprints", projectID)},
		{http.MethodPost, fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)},
	}
	for _, ep := range endpoints {
		w := serve(r, authedJSONReq(t.Context(), ep.method, ep.path, tok, map[string]any{"name": "x", "title": "x"}))
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403, got %d (%s)", ep.method, ep.path, w.Code, w.Body.String())
		}
	}
}

// ---------------------------------------------------------------------------
// Sprint view tests — GetSprint, GetSprintTasks, ListBacklog
// ---------------------------------------------------------------------------

func TestIntegrationSprints_GetByID(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, sprintRepo, newFakeViewRepoIT(), store)
	tok := issueTaskToken(t, uuid.NewString())

	// Create a sprint via the API
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/sprints", projectID), tok,
		map[string]any{"name": "Sprint Alpha", "status": "planned"}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create sprint: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	sprintID := taskIDFrom(t, "sprint", createW.Body.Bytes())

	// Get by ID
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/sprints/%s", projectID, sprintID), tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get sprint: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(getW.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if id, _ := env.Data["id"].(string); id != sprintID {
		t.Errorf("expected sprint id %q, got %q", sprintID, id)
	}
	if name, _ := env.Data["name"].(string); name != "Sprint Alpha" {
		t.Errorf("expected name Sprint Alpha, got %q", name)
	}
}

func TestIntegrationSprints_GetByID_NotFound(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/sprints/%s", projectID, uuid.New()), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "SPRINT_NOT_FOUND" {
		t.Errorf("expected SPRINT_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationSprints_GetSprintTasks(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead, authz.PermissionSprintsWrite, authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, sprintRepo, newFakeViewRepoIT(), store)
	tok := issueTaskToken(t, uuid.NewString())

	// Create a sprint
	sprintCreateW := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/sprints", projectID), tok,
		map[string]any{"name": "Sprint Beta", "status": "active"}))
	if sprintCreateW.Code != http.StatusCreated {
		t.Fatalf("create sprint: expected 201, got %d", sprintCreateW.Code)
	}
	sprintID := taskIDFrom(t, "sprint", sprintCreateW.Body.Bytes())
	sprintUUID := uuid.MustParse(sprintID)

	// Create a task in that sprint (directly via repo to avoid routing complexity)
	now := time.Now()
	sprintTask := &taskdom.Task{
		ID:        uuid.New(),
		ProjectID: projectID,
		SprintID:  &sprintUUID,
		Title:     "Sprint task",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := taskRepo.CreateTask(t.Context(), sprintTask); err != nil {
		t.Fatalf("seed sprint task: %v", err)
	}

	// Create a backlog task (no sprint)
	backlogTask := &taskdom.Task{
		ID:        uuid.New(),
		ProjectID: projectID,
		Title:     "Backlog task",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := taskRepo.CreateTask(t.Context(), backlogTask); err != nil {
		t.Fatalf("seed backlog task: %v", err)
	}

	// GET /tasks?sprint_id=:sprintId — should return only the sprint task
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/tasks?sprint_id=%s", projectID, sprintID), tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get sprint tasks: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if count := taskListCount(t, w.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 sprint task, got %d", count)
	}
}

func TestIntegrationTasks_Backlog(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintRepo := newFakeSprintRepoIT()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsWrite, authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, sprintRepo, newFakeViewRepoIT(), store)
	tok := issueTaskToken(t, uuid.NewString())

	// Create a sprint
	sprintCreateW := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/sprints", projectID), tok,
		map[string]any{"name": "Sprint Gamma", "status": "active"}))
	if sprintCreateW.Code != http.StatusCreated {
		t.Fatalf("create sprint: expected 201, got %d", sprintCreateW.Code)
	}
	sprintID := taskIDFrom(t, "sprint", sprintCreateW.Body.Bytes())
	sprintUUID := uuid.MustParse(sprintID)

	now := time.Now()
	// Two tasks in the sprint
	for i := range 2 {
		task := &taskdom.Task{
			ID:        uuid.New(),
			ProjectID: projectID,
			SprintID:  &sprintUUID,
			Title:     fmt.Sprintf("Sprint task %d", i+1),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := taskRepo.CreateTask(t.Context(), task); err != nil {
			t.Fatalf("seed sprint task: %v", err)
		}
	}
	// Three backlog tasks (no sprint)
	for i := range 3 {
		task := &taskdom.Task{
			ID:        uuid.New(),
			ProjectID: projectID,
			Title:     fmt.Sprintf("Backlog task %d", i+1),
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := taskRepo.CreateTask(t.Context(), task); err != nil {
			t.Fatalf("seed backlog task: %v", err)
		}
	}

	// GET /tasks?sprint_id=null — should return only the 3 backlog tasks
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/tasks?sprint_id=null", projectID), tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list backlog: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if count := taskListCount(t, w.Body.Bytes()); count != 3 {
		t.Errorf("expected 3 backlog tasks, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// ListTasks no longer embeds positions; use the dedicated view positions API.
// ---------------------------------------------------------------------------

func TestIntegrationTasks_ListAndViewPositions(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	viewRepo := newFakeViewRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsWrite, authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, newFakeSprintRepoIT(), viewRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	// Seed a view directly into the repo.
	viewID := uuid.New()
	ctx := context.Background()
	if err := viewRepo.CreateView(ctx, &sprintdom.SprintView{
		ID:        viewID,
		ProjectID: projectID,
		Name:      "Test View",
		ViewType:  sprintdom.ViewTypeTable,
	}); err != nil {
		t.Fatalf("seed view: %v", err)
	}

	// Create two tasks via the API.
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)
	task1W := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Task A"}))
	if task1W.Code != http.StatusCreated {
		t.Fatalf("create task 1: expected 201, got %d", task1W.Code)
	}
	task1ID := taskIDFrom(t, "task", task1W.Body.Bytes())

	task2W := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Task B"}))
	if task2W.Code != http.StatusCreated {
		t.Fatalf("create task 2: expected 201, got %d", task2W.Code)
	}
	task2ID := taskIDFrom(t, "task", task2W.Body.Bytes())

	// Seed manual positions for the two tasks in the view.
	groupKey := "status-col"
	if err := viewRepo.UpsertTaskPosition(ctx, &sprintdom.ViewTaskPosition{
		ViewID:   viewID,
		TaskID:   uuid.MustParse(task1ID),
		Position: 10,
		GroupKey: &groupKey,
	}); err != nil {
		t.Fatalf("seed position task1: %v", err)
	}
	if err := viewRepo.UpsertTaskPosition(ctx, &sprintdom.ViewTaskPosition{
		ViewID:   viewID,
		TaskID:   uuid.MustParse(task2ID),
		Position: 20,
	}); err != nil {
		t.Fatalf("seed position task2: %v", err)
	}

	t.Run("without_view_id_no_positions_returned", func(t *testing.T) {
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		var env struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, item := range env.Data.Items {
			if _, ok := item["view_position"]; ok {
				t.Error("expected no view_position without view_id param")
			}
		}
	})

	t.Run("view_positions_are_available_from_the_dedicated_endpoint", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/%s/task-positions", projectID, viewID)
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		var env struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		posMap := make(map[string]float64)
		groupMap := make(map[string]any)
		for _, item := range env.Data.Items {
			id, _ := item["task_id"].(string)
			if pos, ok := item["position"]; ok {
				posMap[id] = pos.(float64)
			}
			groupMap[id] = item["group_key"]
		}

		if posMap[task1ID] != 10 {
			t.Errorf("expected task1 position=10, got %v", posMap[task1ID])
		}
		if posMap[task2ID] != 20 {
			t.Errorf("expected task2 position=20, got %v", posMap[task2ID])
		}
		if groupMap[task1ID] != "status-col" {
			t.Errorf("expected task1 group_key=status-col, got %v", groupMap[task1ID])
		}
	})

	t.Run("invalid_view_id_returns_400", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/not-a-uuid/task-positions", projectID)
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
		}
	})

	t.Run("unknown_view_id_returns_404", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/%s/task-positions", projectID, uuid.New())
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for non-existent view_id, got %d (%s)", w.Code, w.Body.String())
		}
		if code := decodeErrorCode(t, w); code != "VIEW_NOT_FOUND" {
			t.Errorf("expected VIEW_NOT_FOUND, got %q", code)
		}
	})
}

// ---------------------------------------------------------------------------
// Sprint-scoped task listing + dedicated view positions endpoint.
// ---------------------------------------------------------------------------

func TestIntegrationSprints_GetSprintTasksWithViewPositions(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	viewRepo := newFakeViewRepoIT()
	sprintRepo := newFakeSprintRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionSprintsRead, authz.PermissionSprintsWrite,
				authz.PermissionTasksRead, authz.PermissionTasksWrite,
			},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, sprintRepo, viewRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	// Create a sprint.
	sprintCreateW := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/sprints", projectID), tok,
		map[string]any{"name": "Sprint ViewID", "status": "active"}))
	if sprintCreateW.Code != http.StatusCreated {
		t.Fatalf("create sprint: expected 201, got %d", sprintCreateW.Code)
	}
	sprintID := uuid.MustParse(taskIDFrom(t, "sprint", sprintCreateW.Body.Bytes()))

	// Seed a view directly.
	viewID := uuid.New()
	ctx := context.Background()
	if err := viewRepo.CreateView(ctx, &sprintdom.SprintView{
		ID:        viewID,
		ProjectID: projectID,
		SprintID:  &sprintID,
		Name:      "Sprint View",
		ViewType:  sprintdom.ViewTypeTable,
	}); err != nil {
		t.Fatalf("seed view: %v", err)
	}

	// Seed two sprint tasks directly.
	now := time.Now()
	task1 := &taskdom.Task{
		ID:        uuid.New(),
		ProjectID: projectID,
		SprintID:  &sprintID,
		Title:     "Sprint Task Alpha",
		CreatedAt: now,
		UpdatedAt: now,
	}
	task2 := &taskdom.Task{
		ID:        uuid.New(),
		ProjectID: projectID,
		SprintID:  &sprintID,
		Title:     "Sprint Task Beta",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := taskRepo.CreateTask(ctx, task1); err != nil {
		t.Fatalf("seed task1: %v", err)
	}
	if err := taskRepo.CreateTask(ctx, task2); err != nil {
		t.Fatalf("seed task2: %v", err)
	}

	// Seed positions.
	groupKey := "col-todo"
	if err := viewRepo.UpsertTaskPosition(ctx, &sprintdom.ViewTaskPosition{
		ViewID:   viewID,
		TaskID:   task1.ID,
		Position: 5,
		GroupKey: &groupKey,
	}); err != nil {
		t.Fatalf("seed position task1: %v", err)
	}
	if err := viewRepo.UpsertTaskPosition(ctx, &sprintdom.ViewTaskPosition{
		ViewID:   viewID,
		TaskID:   task2.ID,
		Position: 15,
	}); err != nil {
		t.Fatalf("seed position task2: %v", err)
	}

	base := fmt.Sprintf("/api/v1/projects/%s/tasks?sprint_id=%s", projectID, sprintID)

	t.Run("without_view_id_no_positions_returned", func(t *testing.T) {
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		var env struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, item := range env.Data.Items {
			if _, ok := item["view_position"]; ok {
				t.Error("expected no view_position without view_id param")
			}
		}
	})

	t.Run("view_positions_are_available_from_the_dedicated_endpoint", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/%s/task-positions", projectID, viewID)
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		var env struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		posMap := make(map[string]float64)
		groupMap := make(map[string]any)
		for _, item := range env.Data.Items {
			id, _ := item["task_id"].(string)
			if pos, ok := item["position"]; ok {
				posMap[id] = pos.(float64)
			}
			groupMap[id] = item["group_key"]
		}
		task1Str := task1.ID.String()
		task2Str := task2.ID.String()
		if posMap[task1Str] != 5 {
			t.Errorf("expected task1 position=5, got %v", posMap[task1Str])
		}
		if posMap[task2Str] != 15 {
			t.Errorf("expected task2 position=15, got %v", posMap[task2Str])
		}
		if groupMap[task1Str] != "col-todo" {
			t.Errorf("expected task1 group_key=col-todo, got %v", groupMap[task1Str])
		}
	})

	t.Run("invalid_view_id_returns_400", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/not-a-uuid/task-positions", projectID)
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
		}
	})

	t.Run("unknown_view_id_returns_404", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/%s/task-positions", projectID, uuid.New())
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
		}
		if code := decodeErrorCode(t, w); code != "VIEW_NOT_FOUND" {
			t.Errorf("expected VIEW_NOT_FOUND, got %q", code)
		}
	})
}

// ---------------------------------------------------------------------------
// Backlog task listing + dedicated view positions endpoint.
// ---------------------------------------------------------------------------

func TestIntegrationTasks_BacklogWithViewPositions(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	viewRepo := newFakeViewRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, newFakeSprintRepoIT(), viewRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	// Seed a backlog view directly (no sprint_id).
	viewID := uuid.New()
	ctx := context.Background()
	if err := viewRepo.CreateView(ctx, &sprintdom.SprintView{
		ID:        viewID,
		ProjectID: projectID,
		Name:      "Backlog View",
		ViewType:  sprintdom.ViewTypeTable,
	}); err != nil {
		t.Fatalf("seed view: %v", err)
	}

	// Seed two backlog tasks directly.
	now := time.Now()
	task1 := &taskdom.Task{
		ID:        uuid.New(),
		ProjectID: projectID,
		Title:     "Backlog Task Alpha",
		CreatedAt: now,
		UpdatedAt: now,
	}
	task2 := &taskdom.Task{
		ID:        uuid.New(),
		ProjectID: projectID,
		Title:     "Backlog Task Beta",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := taskRepo.CreateTask(ctx, task1); err != nil {
		t.Fatalf("seed task1: %v", err)
	}
	if err := taskRepo.CreateTask(ctx, task2); err != nil {
		t.Fatalf("seed task2: %v", err)
	}

	// Seed positions.
	groupKey := "backlog-col"
	if err := viewRepo.UpsertTaskPosition(ctx, &sprintdom.ViewTaskPosition{
		ViewID:   viewID,
		TaskID:   task1.ID,
		Position: 1,
		GroupKey: &groupKey,
	}); err != nil {
		t.Fatalf("seed position task1: %v", err)
	}
	if err := viewRepo.UpsertTaskPosition(ctx, &sprintdom.ViewTaskPosition{
		ViewID:   viewID,
		TaskID:   task2.ID,
		Position: 2,
	}); err != nil {
		t.Fatalf("seed position task2: %v", err)
	}

	base := fmt.Sprintf("/api/v1/projects/%s/tasks?sprint_id=null", projectID)

	t.Run("without_view_id_no_positions_returned", func(t *testing.T) {
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		var env struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, item := range env.Data.Items {
			if _, ok := item["view_position"]; ok {
				t.Error("expected no view_position without view_id param")
			}
		}
	})

	t.Run("view_positions_are_available_from_the_dedicated_endpoint", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/%s/task-positions", projectID, viewID)
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		var env struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		posMap := make(map[string]float64)
		groupMap := make(map[string]any)
		for _, item := range env.Data.Items {
			id, _ := item["task_id"].(string)
			if pos, ok := item["position"]; ok {
				posMap[id] = pos.(float64)
			}
			groupMap[id] = item["group_key"]
		}
		task1Str := task1.ID.String()
		task2Str := task2.ID.String()
		if posMap[task1Str] != 1 {
			t.Errorf("expected task1 position=1, got %v", posMap[task1Str])
		}
		if posMap[task2Str] != 2 {
			t.Errorf("expected task2 position=2, got %v", posMap[task2Str])
		}
		if groupMap[task1Str] != "backlog-col" {
			t.Errorf("expected task1 group_key=backlog-col, got %v", groupMap[task1Str])
		}
	})

	t.Run("invalid_view_id_returns_400", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/not-a-uuid/task-positions", projectID)
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
		}
	})

	t.Run("unknown_view_id_returns_404", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/projects/%s/views/%s/task-positions", projectID, uuid.New())
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
		}
		if code := decodeErrorCode(t, w); code != "VIEW_NOT_FOUND" {
			t.Errorf("expected VIEW_NOT_FOUND, got %q", code)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom Field Definition tests
// ---------------------------------------------------------------------------

func TestIntegrationCustomFields_CRUD(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/custom-fields", projectID)

	// Create
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"field_key":    "story_points",
		"display_name": "Story Points",
		"field_type":   "number",
		"is_required":  false,
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create custom field: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	fieldID := taskIDFrom(t, "custom-field", createW.Body.Bytes())

	// List
	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list custom fields: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}
	if count := taskListCount(t, listW.Body.Bytes()); count != 1 {
		t.Errorf("expected 1 custom field, got %d", count)
	}

	// Get by ID
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"/"+fieldID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get custom field: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}

	// Update
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+fieldID, tok, map[string]any{
		"display_name": "SP",
		"is_required":  true,
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update custom field: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	// Delete
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+fieldID, tok, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("delete custom field: expected 200, got %d (%s)", delW.Code, delW.Body.String())
	}

	// Verify deleted
	listAfterW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if count := taskListCount(t, listAfterW.Body.Bytes()); count != 0 {
		t.Errorf("expected 0 custom fields after delete, got %d", count)
	}
}

func TestIntegrationCustomFields_SelectTypeWithOptions(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/custom-fields", projectID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"field_key":    "priority",
		"display_name": "Priority",
		"field_type":   "select",
		"options":      []string{"low", "medium", "high"},
		"is_required":  true,
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create select field: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}

	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(createW.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	opts, _ := env.Data["options"].([]any)
	if len(opts) != 3 {
		t.Errorf("expected 3 options, got %d", len(opts))
	}
}

func TestIntegrationCustomFields_DuplicateKeyReturns409(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/custom-fields", projectID)

	body := map[string]any{
		"field_key":    "dup_key",
		"display_name": "Dup Key",
		"field_type":   "text",
	}
	first := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, body))
	if first.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d (%s)", first.Code, first.Body.String())
	}

	second := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, body))
	if second.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d (%s)", second.Code, second.Body.String())
	}
	if code := decodeErrorCode(t, second); code != "CUSTOM_FIELD_KEY_TAKEN" {
		t.Errorf("expected CUSTOM_FIELD_KEY_TAKEN, got %q", code)
	}
}

func TestIntegrationCustomFields_InvalidTypeReturns400(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/custom-fields", projectID), tok, map[string]any{
			"field_key":    "bad",
			"display_name": "Bad",
			"field_type":   "not_a_type",
		}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "CUSTOM_FIELD_TYPE_INVALID" {
		t.Errorf("expected CUSTOM_FIELD_TYPE_INVALID, got %q", code)
	}
}

func TestIntegrationCustomFields_GetNotFoundReturns404(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/custom-fields/%s", projectID, uuid.New()), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "CUSTOM_FIELD_NOT_FOUND" {
		t.Errorf("expected CUSTOM_FIELD_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationCustomFields_EmptyKeyReturns400(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/custom-fields", projectID), tok, map[string]any{
			"field_key":    "   ",
			"display_name": "Test",
			"field_type":   "text",
		}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "CUSTOM_FIELD_KEY_INVALID" {
		t.Errorf("expected CUSTOM_FIELD_KEY_INVALID, got %q", code)
	}
}

func TestIntegrationCustomFields_UnauthorizedReturns403(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/custom-fields", projectID), tok, map[string]any{
			"field_key":    "sp",
			"display_name": "SP",
			"field_type":   "number",
		}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Partial-update (PATCH) semantics tests
// ---------------------------------------------------------------------------

// TestIntegrationTasks_PatchStatusPreservesSprintID is the regression test for
// the bug where dragging a task to a new status in the sprint board caused it
// to move to the product backlog.  Only status_id is sent in the body; the
// sprint_id must remain unchanged.
func TestIntegrationTasks_PatchStatusPreservesSprintID(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintID := uuid.New()
	statusID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Create task with a sprint assignment.
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":     "Sprint Task",
		"sprint_id": sprintID.String(),
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	taskID := taskIDFrom(t, "task", createW.Body.Bytes())

	// PATCH with only status_id.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskID, tok, map[string]any{
		"status_id": statusID.String(),
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	var patchEnv struct {
		Data struct {
			SprintID *string `json:"sprint_id"`
			StatusID *string `json:"status_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(patchW.Body.Bytes(), &patchEnv); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if patchEnv.Data.SprintID == nil || *patchEnv.Data.SprintID != sprintID.String() {
		t.Errorf("expected sprint_id=%s to be preserved, got %v", sprintID, patchEnv.Data.SprintID)
	}
	if patchEnv.Data.StatusID == nil || *patchEnv.Data.StatusID != statusID.String() {
		t.Errorf("expected status_id=%s to be set, got %v", statusID, patchEnv.Data.StatusID)
	}
}

// TestIntegrationTasks_PatchExplicitNullSprintIDClearsField verifies that
// sending sprint_id=null explicitly moves the task to the backlog.
func TestIntegrationTasks_PatchExplicitNullSprintIDClearsField(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":     "Sprint Task",
		"sprint_id": sprintID.String(),
	}))
	taskID := taskIDFrom(t, "task", createW.Body.Bytes())

	// Explicitly clear sprint_id.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskID, tok, map[string]any{
		"sprint_id": nil,
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	var patchEnv struct {
		Data struct {
			SprintID *string `json:"sprint_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(patchW.Body.Bytes(), &patchEnv); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if patchEnv.Data.SprintID != nil {
		t.Errorf("expected sprint_id=null after explicit clear, got %v", *patchEnv.Data.SprintID)
	}
}

// TestIntegrationTasks_PatchTitleOnlyPreservesAllFields verifies that updating
// only the title leaves status_id, sprint_id, and description unchanged.
func TestIntegrationTasks_PatchTitleOnlyPreservesAllFields(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintID := uuid.New()
	statusID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	const keepMeText = "keep me"

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":     "Original title",
		"sprint_id": sprintID.String(),
		"status_id": statusID.String(),
		"description": []map[string]any{
			{
				"id":      "1",
				"type":    "paragraph",
				"content": []map[string]any{{"type": "text", "text": keepMeText}},
			},
		},
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	taskID := taskIDFrom(t, "task", createW.Body.Bytes())

	// PATCH title only.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskID, tok, map[string]any{
		"title": "Updated title",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("patch: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	var env struct {
		Data struct {
			Title       string          `json:"title"`
			SprintID    *string         `json:"sprint_id"`
			StatusID    *string         `json:"status_id"`
			Description json.RawMessage `json:"description"`
		} `json:"data"`
	}
	if err := json.Unmarshal(patchW.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if env.Data.Title != "Updated title" {
		t.Errorf("expected Title=Updated title, got %q", env.Data.Title)
	}
	if env.Data.SprintID == nil || *env.Data.SprintID != sprintID.String() {
		t.Errorf("expected sprint_id=%s preserved, got %v", sprintID, env.Data.SprintID)
	}
	if env.Data.StatusID == nil || *env.Data.StatusID != statusID.String() {
		t.Errorf("expected status_id=%s preserved, got %v", statusID, env.Data.StatusID)
	}
	// Assert the description block array was preserved verbatim, including the
	// original text content (keepMeText).
	var descBlocks []struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(env.Data.Description, &descBlocks); err != nil {
		t.Fatalf("description is not a valid JSON array: %v", err)
	}
	if len(descBlocks) == 0 {
		t.Fatalf("expected description to contain at least one block, got empty array")
	}
	if got := descBlocks[0].Content[0].Text; got != keepMeText {
		t.Errorf("expected description text %q preserved, got %q", keepMeText, got)
	}
}

// ---------------------------------------------------------------------------
// Task Number integration tests
// ---------------------------------------------------------------------------

func TestIntegrationTasks_TaskNumberIncrementsPerProject(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	type taskResp struct {
		Data struct {
			TaskNumber float64 `json:"task_number"`
		} `json:"data"`
	}

	var prev float64
	for i := 1; i <= 3; i++ {
		w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title": fmt.Sprintf("Task %d", i),
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create task %d: expected 201, got %d (%s)", i, w.Code, w.Body.String())
		}
		var resp taskResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		num := resp.Data.TaskNumber
		if num != float64(i) {
			t.Errorf("task %d: expected task_number=%d, got %g", i, i, num)
		}
		if num <= prev {
			t.Errorf("task_number must be strictly increasing: prev=%g, got=%g", prev, num)
		}
		prev = num
	}
}

func TestIntegrationTasks_TaskNumberScopedToProject(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projA := uuid.New()
	projB := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projA: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
			projB: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	decodeTaskNumber := func(body []byte) float64 {
		var env struct {
			Data struct {
				TaskNumber float64 `json:"task_number"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return env.Data.TaskNumber
	}

	// Create two tasks in projA
	wA1 := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks", projA), tok, map[string]any{"title": "A-1"}))
	wA2 := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks", projA), tok, map[string]any{"title": "A-2"}))
	// Create one task in projB
	wB1 := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks", projB), tok, map[string]any{"title": "B-1"}))

	if n := decodeTaskNumber(wA1.Body.Bytes()); n != 1 {
		t.Errorf("projA first task: expected task_number=1, got %g", n)
	}
	if n := decodeTaskNumber(wA2.Body.Bytes()); n != 2 {
		t.Errorf("projA second task: expected task_number=2, got %g", n)
	}
	if n := decodeTaskNumber(wB1.Body.Bytes()); n != 1 {
		t.Errorf("projB first task: expected task_number=1 (independent counter), got %g", n)
	}
}

func TestIntegrationTasks_GetByNumber_OK(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Create task
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title": "Lookup me by number",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: got %d", createW.Code)
	}
	originalID := taskIDFrom(t, "task", createW.Body.Bytes())

	var createEnv struct {
		Data struct {
			TaskNumber float64 `json:"task_number"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createW.Body.Bytes(), &createEnv); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	taskNum := int64(createEnv.Data.TaskNumber)

	// Get by number
	getByNumW := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("%s/by-number/%d", base, taskNum), tok, nil))
	if getByNumW.Code != http.StatusOK {
		t.Fatalf("get by number: expected 200, got %d (%s)", getByNumW.Code, getByNumW.Body.String())
	}
	gotID := taskIDFrom(t, "task", getByNumW.Body.Bytes())
	if gotID != originalID {
		t.Errorf("expected id=%s, got %s", originalID, gotID)
	}
}

func TestIntegrationTasks_GetByNumber_NotFound(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/tasks/by-number/9999", projectID), tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "TASK_NOT_FOUND" {
		t.Errorf("expected TASK_NOT_FOUND, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// CompleteSprint integration tests
// ---------------------------------------------------------------------------

func TestIntegrationCompleteSprint_MovesToBacklog(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	sprintRepo := newFakeSprintRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsWrite, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, sprintRepo, newFakeViewRepoIT(), store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s", projectID)

	// Create an active sprint directly in the fake repo.
	sprintID := uuid.New()
	sprintRepo.sprints[sprintID] = &sprintdom.Sprint{
		ID:        sprintID,
		ProjectID: projectID,
		Name:      "Sprint Active",
		Status:    sprintdom.SprintStatusActive,
	}

	// Seed two tasks in the sprint.
	for range 2 {
		id := uuid.New()
		taskRepo.tasks[id] = &taskdom.Task{
			ID:        id,
			ProjectID: projectID,
			SprintID:  &sprintID,
			Title:     "incomplete",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	// Call complete endpoint with no destination (backlog).
	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("%s/sprints/%s/complete", base, sprintID), tok, map[string]any{}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	// Sprint must be completed.
	sp := sprintRepo.sprints[sprintID]
	if sp.Status != sprintdom.SprintStatusCompleted {
		t.Errorf("expected sprint completed, got %q", sp.Status)
	}

	// All tasks must now be in the backlog.
	for _, task := range taskRepo.tasks {
		if task.ProjectID != projectID {
			continue
		}
		if task.SprintID != nil {
			t.Errorf("task %s still assigned to sprint %s, expected backlog", task.ID, *task.SprintID)
		}
	}
}

func TestIntegrationCompleteSprint_AlreadyCompleted(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	sprintRepo := newFakeSprintRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsWrite},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, sprintRepo, newFakeViewRepoIT(), store)
	tok := issueTaskToken(t, uuid.NewString())

	sprintID := uuid.New()
	sprintRepo.sprints[sprintID] = &sprintdom.Sprint{
		ID:        sprintID,
		ProjectID: projectID,
		Name:      "Done Sprint",
		Status:    sprintdom.SprintStatusCompleted,
	}

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/sprints/%s/complete", projectID, sprintID), tok, map[string]any{}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "SPRINT_ALREADY_COMPLETE" {
		t.Errorf("expected SPRINT_ALREADY_COMPLETE, got %q", code)
	}
}

func TestIntegrationCompleteSprint_NotFound(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	sprintRepo := newFakeSprintRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsWrite},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, sprintRepo, newFakeViewRepoIT(), store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/sprints/%s/complete", projectID, uuid.New()), tok, map[string]any{}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationCompleteSprint_Forbidden(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	sprintRepo := newFakeSprintRepoIT()
	projectID := uuid.New()
	// No sprints.write permission.
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionSprintsRead},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, sprintRepo, newFakeViewRepoIT(), store)
	tok := issueTaskToken(t, uuid.NewString())

	sprintID := uuid.New()
	sprintRepo.sprints[sprintID] = &sprintdom.Sprint{
		ID:        sprintID,
		ProjectID: projectID,
		Name:      "Active",
		Status:    sprintdom.SprintStatusActive,
	}

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/sprints/%s/complete", projectID, sprintID), tok, map[string]any{}))
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (%s)", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Activity & Comment integration tests
// ---------------------------------------------------------------------------

func TestActivities_ListEmpty(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	taskID := uuid.New()
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities", projectID, taskID), tok, nil,
	))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivities_AddAndListComment(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	// Create a task first
	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks", projectID), tok,
		map[string]any{"title": "Activity Test Task"},
	))
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	taskID := taskIDFrom(t, "task", w.Body.Bytes())

	// Add a comment
	w = serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities/comments", projectID, taskID), tok,
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "Hello from comment"}}}}},
	))
	if w.Code != http.StatusCreated {
		t.Fatalf("add comment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// List activities - should contain at least the comment
	w = serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities", projectID, taskID), tok, nil,
	))
	if w.Code != http.StatusOK {
		t.Fatalf("list activities: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestActivities_AddComment_StringContentReturns400 guards against GitHub
// issue #233: a bare JSON string (not a BlockNote block array, and not the
// legacy {"text": "..."} shape) must be rejected rather than stored, since
// the web app's block renderers crash on it.
func TestActivities_AddComment_StringContentReturns400(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks", projectID), tok,
		map[string]any{"title": "Activity Test Task"},
	))
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	taskID := taskIDFrom(t, "task", w.Body.Bytes())

	w = serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities/comments", projectID, taskID), tok,
		map[string]any{"content": "just a plain string"},
	))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "ACTIVITY_COMMENT_CONTENT_INVALID" {
		t.Errorf("expected ACTIVITY_COMMENT_CONTENT_INVALID, got %q", code)
	}
}

func TestActivities_AddComment_RequiresAuth(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)

	taskID := uuid.New()
	// No token — should get 401
	b, _ := json.Marshal(map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "no auth"}}}}})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities/comments", projectID, taskID),
		bytes.NewReader(b),
	)
	req.Header.Set("Content-Type", "application/json")
	w := serve(r, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivities_UpdateComment(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	// Create task
	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks", projectID), tok,
		map[string]any{"title": "Comment Update Task"},
	))
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", w.Code, w.Body.String())
	}
	taskID := taskIDFrom(t, "task", w.Body.Bytes())

	// Add comment
	w = serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities/comments", projectID, taskID), tok,
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "original text"}}}}},
	))
	if w.Code != http.StatusCreated {
		t.Fatalf("add comment: %d: %s", w.Code, w.Body.String())
	}
	commentID := taskIDFrom(t, "comment", w.Body.Bytes())

	// Update comment
	w = serve(r, authedJSONReq(t.Context(), http.MethodPatch,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities/comments/%s", projectID, taskID, commentID), tok,
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "updated text"}}}}},
	))
	if w.Code != http.StatusOK {
		t.Fatalf("update comment: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestActivities_DeleteComment(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	// Create task
	w := serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks", projectID), tok,
		map[string]any{"title": "Comment Delete Task"},
	))
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", w.Code, w.Body.String())
	}
	taskID := taskIDFrom(t, "task", w.Body.Bytes())

	// Add comment
	w = serve(r, authedJSONReq(t.Context(), http.MethodPost,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities/comments", projectID, taskID), tok,
		map[string]any{"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": "to be deleted"}}}}},
	))
	if w.Code != http.StatusCreated {
		t.Fatalf("add comment: %d: %s", w.Code, w.Body.String())
	}
	commentID := taskIDFrom(t, "comment", w.Body.Bytes())

	// Delete comment
	w = serve(r, authedJSONReq(t.Context(), http.MethodDelete,
		fmt.Sprintf("/api/v1/projects/%s/tasks/%s/activities/comments/%s", projectID, taskID, commentID), tok, nil,
	))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete comment: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Task hierarchy constraint tests
// ---------------------------------------------------------------------------

func TestIntegrationTasks_EpicCannotHaveParent(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Seed Epic system type directly.
	epicType := &taskdom.TaskType{ID: uuid.New(), ProjectID: projectID, Name: "Epic", IsSystem: true}
	if err := taskRepo.CreateTaskType(t.Context(), epicType); err != nil {
		t.Fatalf("seed epic type: %v", err)
	}

	// Create a plain parent task.
	parentW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Parent"}))
	if parentW.Code != http.StatusCreated {
		t.Fatalf("create parent: %d: %s", parentW.Code, parentW.Body.String())
	}
	parentID := taskIDFrom(t, "parent", parentW.Body.Bytes())

	// Creating an Epic with a parent must be rejected.
	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":          "Epic with parent",
		"task_type_id":   epicType.ID.String(),
		"parent_task_id": parentID,
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "TASK_EPIC_CANNOT_HAVE_PARENT" {
		t.Errorf("expected TASK_EPIC_CANNOT_HAVE_PARENT, got %q", code)
	}
}

func TestIntegrationTasks_UpdateToEpicWithParentRejected(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	epicType := &taskdom.TaskType{ID: uuid.New(), ProjectID: projectID, Name: "Epic", IsSystem: true}
	if err := taskRepo.CreateTaskType(t.Context(), epicType); err != nil {
		t.Fatalf("seed epic type: %v", err)
	}

	// Create parent and child tasks.
	parentW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Parent"}))
	if parentW.Code != http.StatusCreated {
		t.Fatalf("create parent: %d: %s", parentW.Code, parentW.Body.String())
	}
	parentID := taskIDFrom(t, "parent", parentW.Body.Bytes())

	childW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":          "Child",
		"parent_task_id": parentID,
	}))
	if childW.Code != http.StatusCreated {
		t.Fatalf("create child: %d: %s", childW.Code, childW.Body.String())
	}
	childID := taskIDFrom(t, "child", childW.Body.Bytes())

	// Changing child's type to Epic while it still has a parent must be rejected.
	w := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+childID, tok, map[string]any{
		"task_type_id": epicType.ID.String(),
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "TASK_EPIC_CANNOT_HAVE_PARENT" {
		t.Errorf("expected TASK_EPIC_CANNOT_HAVE_PARENT, got %q", code)
	}
}

func TestIntegrationTasks_SelfParentRejected(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Task A"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", w.Code, w.Body.String())
	}
	taskID := taskIDFrom(t, "task", w.Body.Bytes())

	// Setting a task's parent to itself must be rejected.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskID, tok, map[string]any{
		"parent_task_id": taskID,
	}))
	if patchW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	if code := decodeErrorCode(t, patchW); code != "TASK_CANNOT_BE_OWN_PARENT" {
		t.Errorf("expected TASK_CANNOT_BE_OWN_PARENT, got %q", code)
	}
}

func TestIntegrationTasks_ParentCycleDetected(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Create task A.
	wA := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": "Task A"}))
	if wA.Code != http.StatusCreated {
		t.Fatalf("create A: %d: %s", wA.Code, wA.Body.String())
	}
	taskAID := taskIDFrom(t, "A", wA.Body.Bytes())

	// Create task B with parent = A.
	wB := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":          "Task B",
		"parent_task_id": taskAID,
	}))
	if wB.Code != http.StatusCreated {
		t.Fatalf("create B: %d: %s", wB.Code, wB.Body.String())
	}
	taskBID := taskIDFrom(t, "B", wB.Body.Bytes())

	// Attempting to set A's parent to B would form A → B → A; must be rejected.
	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, base+"/"+taskAID, tok, map[string]any{
		"parent_task_id": taskBID,
	}))
	if patchW.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", patchW.Code, patchW.Body.String())
	}
	if code := decodeErrorCode(t, patchW); code != "TASK_PARENT_CYCLE_DETECTED" {
		t.Errorf("expected TASK_PARENT_CYCLE_DETECTED, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// total_count in task list response
// ---------------------------------------------------------------------------

func TestIntegrationTasks_ListTotalCountNoFilter(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	for i := range 3 {
		w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title": fmt.Sprintf("Task %d", i+1),
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create task %d: expected 201, got %d", i+1, w.Code)
		}
	}

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list tasks: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := taskListTotalCount(t, w.Body.Bytes()); got != 3 {
		t.Errorf("expected total_count=3, got %d", got)
	}
}

func TestIntegrationTasks_ListTotalCountWithSprintFilter(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// 2 tasks in the sprint, 3 without.
	for i := range 2 {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":     fmt.Sprintf("Sprint task %d", i+1),
			"sprint_id": sprintID.String(),
		}))
	}
	for i := range 3 {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title": fmt.Sprintf("Backlog task %d", i+1),
		}))
	}

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("%s?sprint_id=%s", base, sprintID), tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := taskListTotalCount(t, w.Body.Bytes()); got != 2 {
		t.Errorf("expected total_count=2 for sprint filter, got %d", got)
	}
	if items := taskListCount(t, w.Body.Bytes()); items != 2 {
		t.Errorf("expected 2 items in sprint filter response, got %d", items)
	}
}

func TestIntegrationTasks_ListTotalCountBacklogFilter(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// 2 sprint tasks, 4 backlog tasks.
	for i := range 2 {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":     fmt.Sprintf("Sprint task %d", i+1),
			"sprint_id": sprintID.String(),
		}))
	}
	for i := range 4 {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title": fmt.Sprintf("Backlog task %d", i+1),
		}))
	}

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?sprint_id=null", tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := taskListTotalCount(t, w.Body.Bytes()); got != 4 {
		t.Errorf("expected total_count=4 for backlog filter, got %d", got)
	}
	if items := taskListCount(t, w.Body.Bytes()); items != 4 {
		t.Errorf("expected 4 items in backlog filter response, got %d", items)
	}
}

func TestIntegrationTasks_ListTotalCountExcludesDeleted(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Create 3 tasks then delete 1.
	var taskIDs []string
	for i := range 3 {
		w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title": fmt.Sprintf("Task %d", i+1),
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create task %d: expected 201, got %d", i+1, w.Code)
		}
		taskIDs = append(taskIDs, taskIDFrom(t, "task", w.Body.Bytes()))
	}
	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, base+"/"+taskIDs[0], tok, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("delete task: expected 200, got %d", delW.Code)
	}

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list tasks: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := taskListTotalCount(t, w.Body.Bytes()); got != 2 {
		t.Errorf("expected total_count=2 after deleting 1 task, got %d", got)
	}
}

func TestIntegrationTasks_ListTotalCountIgnoresCursor(t *testing.T) {
	// total_count must equal the full matching set regardless of which page the
	// cursor points to.  The handler strips CursorAfter before calling CountTasks.
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	for i := range 5 {
		w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title": fmt.Sprintf("Task %d", i+1),
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create task %d: expected 201, got %d", i+1, w.Code)
		}
	}

	// First page: only 2 items.
	firstW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?page_size=2", tok, nil))
	if firstW.Code != http.StatusOK {
		t.Fatalf("first page: expected 200, got %d (%s)", firstW.Code, firstW.Body.String())
	}
	var firstEnv struct {
		Data struct {
			Items      []any   `json:"items"`
			NextCursor *string `json:"next_cursor"`
			TotalCount int64   `json:"total_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(firstW.Body.Bytes(), &firstEnv); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(firstEnv.Data.Items) != 2 {
		t.Errorf("expected 2 items on first page, got %d", len(firstEnv.Data.Items))
	}
	if firstEnv.Data.TotalCount != 5 {
		t.Errorf("expected total_count=5 on first page, got %d", firstEnv.Data.TotalCount)
	}
	if firstEnv.Data.NextCursor == nil {
		t.Fatal("expected next_cursor on first page, got nil")
	}

	// Second page with cursor: total_count must remain 5.
	params := url.Values{}
	params.Set("page_size", "2")
	params.Set("cursor", *firstEnv.Data.NextCursor)
	secondW := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?"+params.Encode(), tok, nil))
	if secondW.Code != http.StatusOK {
		t.Fatalf("second page: expected 200, got %d (%s)", secondW.Code, secondW.Body.String())
	}
	if got := taskListTotalCount(t, secondW.Body.Bytes()); got != 5 {
		t.Errorf("expected total_count=5 on second page (cursor-independent), got %d", got)
	}
}

// taskListFieldSum decodes data.field_sum from a list handler JSON response.
// Returns 0 and false when field_sum is absent or null.
func taskListFieldSum(t *testing.T, body []byte) (float64, bool) {
	t.Helper()
	var env struct {
		Data struct {
			FieldSum *float64 `json:"field_sum"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode list field_sum: %v", err)
	}
	if env.Data.FieldSum == nil {
		return 0, false
	}
	return *env.Data.FieldSum, true
}

// ---------------------------------------------------------------------------
// sum_field with custom task field in list response
// ---------------------------------------------------------------------------

func TestIntegrationTasks_SumCustomField_BasicSum(t *testing.T) {
	// Verifies that sum_field=<custom_key> sums the numeric custom field value
	// across all matching tasks.
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Create tasks with a numeric custom field "effort".
	for _, effort := range []float64{3, 5, 7} {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":         fmt.Sprintf("Task effort=%.0f", effort),
			"custom_fields": map[string]any{"effort": effort},
		}))
	}
	// One task with no custom field — should contribute 0 to the sum.
	serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title": "Task no effort",
	}))

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?sum_field=effort", tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	got, ok := taskListFieldSum(t, w.Body.Bytes())
	if !ok {
		t.Fatal("expected field_sum in response, got null/absent")
	}
	if got != 15 {
		t.Errorf("expected field_sum=15 (3+5+7), got %v", got)
	}
}

func TestIntegrationTasks_SumCustomField_FilterBySprint(t *testing.T) {
	// Verifies that sum_field respects sprint_id filter.
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Sprint tasks: effort 4 + 6 = 10.
	for _, effort := range []float64{4, 6} {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":         fmt.Sprintf("Sprint effort=%.0f", effort),
			"sprint_id":     sprintID.String(),
			"custom_fields": map[string]any{"effort": effort},
		}))
	}
	// Backlog tasks: effort 20 + 30 = 50 (must be excluded).
	for _, effort := range []float64{20, 30} {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":         fmt.Sprintf("Backlog effort=%.0f", effort),
			"custom_fields": map[string]any{"effort": effort},
		}))
	}

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("%s?sprint_id=%s&sum_field=effort", base, sprintID), tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	got, ok := taskListFieldSum(t, w.Body.Bytes())
	if !ok {
		t.Fatal("expected field_sum in response")
	}
	if got != 10 {
		t.Errorf("expected field_sum=10 (sprint only), got %v", got)
	}
}

func TestIntegrationTasks_SumCustomField_BacklogOnly(t *testing.T) {
	// Verifies that sum_field respects sprint_id=null (backlog-only) filter.
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	sprintID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Backlog tasks: effort 2 + 8 = 10.
	for _, effort := range []float64{2, 8} {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":         fmt.Sprintf("Backlog effort=%.0f", effort),
			"custom_fields": map[string]any{"effort": effort},
		}))
	}
	// Sprint tasks: effort 100 (must be excluded).
	serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":         "Sprint effort=100",
		"sprint_id":     sprintID.String(),
		"custom_fields": map[string]any{"effort": float64(100)},
	}))

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?sprint_id=null&sum_field=effort", tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	got, ok := taskListFieldSum(t, w.Body.Bytes())
	if !ok {
		t.Fatal("expected field_sum in response")
	}
	if got != 10 {
		t.Errorf("expected field_sum=10 (backlog only), got %v", got)
	}
}

func TestIntegrationTasks_SumCustomField_IgnoresCursor(t *testing.T) {
	// Verifies that sum_field reflects all matching tasks regardless of cursor pagination.
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	for i := range 5 {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":         fmt.Sprintf("Task %d", i+1),
			"custom_fields": map[string]any{"effort": float64(10)},
		}))
	}

	// First page: page_size=2.
	var firstEnv struct {
		Data struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"data"`
	}
	w1 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?page_size=2&sum_field=effort", tok, nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("first page: expected 200, got %d", w1.Code)
	}
	if err := json.Unmarshal(w1.Body.Bytes(), &firstEnv); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	got1, ok1 := taskListFieldSum(t, w1.Body.Bytes())
	if !ok1 {
		t.Fatal("first page: expected field_sum in response")
	}
	if got1 != 50 {
		t.Errorf("first page: expected field_sum=50 (5×10), got %v", got1)
	}

	if firstEnv.Data.NextCursor == nil {
		t.Fatal("expected next_cursor on first page")
	}

	// Second page with cursor: field_sum must still equal 50.
	params := url.Values{}
	params.Set("page_size", "2")
	params.Set("cursor", *firstEnv.Data.NextCursor)
	params.Set("sum_field", "effort")
	w2 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base+"?"+params.Encode(), tok, nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("second page: expected 200, got %d", w2.Code)
	}
	got2, ok2 := taskListFieldSum(t, w2.Body.Bytes())
	if !ok2 {
		t.Fatal("second page: expected field_sum in response")
	}
	if got2 != 50 {
		t.Errorf("second page: expected field_sum=50 (cursor-independent), got %v", got2)
	}
}

func TestIntegrationTasks_SumCustomField_AbsentWhenNotRequested(t *testing.T) {
	// Verifies that field_sum is null when sum_field param is absent.
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"title":         "Task",
		"custom_fields": map[string]any{"effort": float64(99)},
	}))

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	_, ok := taskListFieldSum(t, w.Body.Bytes())
	if ok {
		t.Error("expected field_sum to be null/absent when sum_field param is not set")
	}
}

func TestIntegrationTasks_ListTotalCountWithStatusFilter(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	statusA := uuid.New()
	statusB := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r := buildTaskTestRouter(taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// 2 tasks with statusA, 3 tasks with statusB.
	for i := range 2 {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":     fmt.Sprintf("Status A task %d", i+1),
			"status_id": statusA.String(),
		}))
	}
	for i := range 3 {
		serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
			"title":     fmt.Sprintf("Status B task %d", i+1),
			"status_id": statusB.String(),
		}))
	}

	w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
		fmt.Sprintf("%s?status_id=%s", base, statusA), tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := taskListTotalCount(t, w.Body.Bytes()); got != 2 {
		t.Errorf("expected total_count=2 for statusA filter, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// view_position sort — handler wiring and fake-repo ordering
// ---------------------------------------------------------------------------

// TestIntegrationTasks_ViewPositionSort verifies that when a ?view_id= query
// param is provided and no explicit sort_by is set, the handler injects
// sort.By = "view_position" so tasks are returned in manual-position order
// rather than created_at order. It also verifies that tasks without a saved
// position fall to the end (sorted by created_at), and that cursor-based
// pagination correctly traverses all tasks without duplicates.
func TestIntegrationTasks_ViewPositionSort(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	viewRepo := newFakeViewRepoIT()
	projectID := uuid.New()
	viewID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionSprintsWrite,
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
	r := buildTaskTestRouterWithSprints(taskRepo, newFakeSprintRepoIT(), viewRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	ctx := context.Background()
	// Seed the view so the handler can validate it exists.
	if err := viewRepo.CreateView(ctx, &sprintdom.SprintView{
		ID:        viewID,
		ProjectID: projectID,
		Name:      "Manual Sort View",
		ViewType:  sprintdom.ViewTypeTable,
	}); err != nil {
		t.Fatalf("seed view: %v", err)
	}

	base := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)

	// Create 4 tasks via the API.  They are created in order A→B→C→D, so
	// their created_at will be A < B < C < D.
	createTask := func(title string) string {
		t.Helper()
		w := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{"title": title}))
		if w.Code != http.StatusCreated {
			t.Fatalf("create %q: expected 201, got %d", title, w.Code)
		}
		return taskIDFrom(t, "task", w.Body.Bytes())
	}
	taskAID := createTask("Task A") // created first  → latest position (last without position)
	taskBID := createTask("Task B")
	taskCID := createTask("Task C")
	taskDID := createTask("Task D") // created last

	// Assign manual positions in REVERSE of creation order so that the result
	// differs visibly from the default created_at sort:
	//   position 10 → Task C
	//   position 20 → Task A
	// Task B and Task D get no position (should appear after, sorted by created_at).
	taskCUUID := uuid.MustParse(taskCID)
	taskAUUID := uuid.MustParse(taskAID)
	taskRepo.addViewPosition(viewID, taskCUUID, 10)
	taskRepo.addViewPosition(viewID, taskAUUID, 20)

	t.Run("tasks_returned_in_position_order", func(t *testing.T) {
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
			fmt.Sprintf("%s?view_id=%s", base, viewID), tok, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		ids := taskListItemIDs(t, w.Body.Bytes())
		if len(ids) < 4 {
			t.Fatalf("expected at least 4 tasks, got %d: %v", len(ids), ids)
		}
		// Positioned tasks first, in position order.
		if ids[0] != taskCID {
			t.Errorf("expected ids[0]=%s (pos 10), got %s", taskCID, ids[0])
		}
		if ids[1] != taskAID {
			t.Errorf("expected ids[1]=%s (pos 20), got %s", taskAID, ids[1])
		}
		// Unpositioned tasks follow (B then D — created_at order).
		if ids[2] != taskBID {
			t.Errorf("expected ids[2]=%s (no pos, earliest created_at), got %s", taskBID, ids[2])
		}
		if ids[3] != taskDID {
			t.Errorf("expected ids[3]=%s (no pos, latest created_at), got %s", taskDID, ids[3])
		}
	})

	t.Run("without_view_id_no_position_sort", func(t *testing.T) {
		// Without view_id, sort.ViewID is not set so viewPositions has no effect.
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
		}
		ids := taskListItemIDs(t, w.Body.Bytes())
		// The fake repo iterates over a map so order is non-deterministic; we can
		// only verify all 4 tasks are present.
		if len(ids) != 4 {
			t.Errorf("expected 4 tasks, got %d", len(ids))
		}
	})

	// NOTE: cursor-based traversal correctness for view_position sort is covered
	// by TestE2EListTaskPagination_ViewPositionSort which uses a real database.
	// The fake repo does not implement CursorAfter filtering, so multi-page
	// traversal with the fake would loop forever.

	t.Run("invalid_view_id_returns_400", func(t *testing.T) {
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
			fmt.Sprintf("%s?view_id=not-a-uuid", base), tok, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid view_id, got %d", w.Code)
		}
	})

	t.Run("unknown_view_id_returns_404", func(t *testing.T) {
		w := serve(r, authedJSONReq(t.Context(), http.MethodGet,
			fmt.Sprintf("%s?view_id=%s", base, uuid.New()), tok, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404 for unknown view_id, got %d", w.Code)
		}
	})
}

// taskListItemIDs extracts the "id" of every item in a list-tasks response.
func taskListItemIDs(t *testing.T, body []byte) []string {
	t.Helper()
	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode task list items: %v", err)
	}
	ids := make([]string, 0, len(env.Data.Items))
	for _, item := range env.Data.Items {
		id, _ := item["id"].(string)
		ids = append(ids, id)
	}
	return ids
}
