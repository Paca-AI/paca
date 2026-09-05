package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// TaskHandler handles task management endpoints.
type TaskHandler struct {
	svc             taskdom.Service
	viewSvc         sprintdom.ViewService
	activitySvc     taskdom.ActivityService
	publisher       *messaging.Publisher
	projectSvc      projectServiceForAssigned
	avatarSvc       attachmentdom.AvatarService
	notificationSvc notificationdom.Service
}

// NewTaskHandler returns a TaskHandler wired to the task service, view service,
// and activity service.
func NewTaskHandler(svc taskdom.Service, viewSvc sprintdom.ViewService, activitySvc taskdom.ActivityService, opts ...TaskHandlerOption) *TaskHandler {
	h := &TaskHandler{svc: svc, viewSvc: viewSvc, activitySvc: activitySvc}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// TaskHandlerOption is a functional option for TaskHandler.
type TaskHandlerOption func(*TaskHandler)

// WithTaskPublisher attaches a Valkey publisher used to enqueue assignment
// events for the NotificationConsumer worker.
func WithTaskPublisher(p *messaging.Publisher) TaskHandlerOption {
	return func(h *TaskHandler) {
		h.publisher = p
	}
}

// WithTaskAvatarService configures avatar URL resolution for activity
// responses (comments/system events show the actor's avatar).
func WithTaskAvatarService(svc attachmentdom.AvatarService) TaskHandlerOption {
	return func(h *TaskHandler) {
		h.avatarSvc = svc
	}
}

// WithTaskNotificationService configures dispatching a plugin event (no
// in-app row) when a task's description is updated to newly @mention a
// user (see notificationdom.Service.NotifyTaskDescriptionMentioned).
func WithTaskNotificationService(svc notificationdom.Service) TaskHandlerOption {
	return func(h *TaskHandler) {
		h.notificationSvc = svc
	}
}

// toActivityResponse maps a to an ActivityResponse and, if an AvatarService
// is configured, resolves the actor's avatar keys into presigned display URLs.
func (h *TaskHandler) toActivityResponse(ctx context.Context, a *taskdom.Activity) dto.ActivityResponse {
	resp := dto.ActivityFromEntity(a)
	if h.avatarSvc != nil {
		resp.ActorAvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, a.ActorAvatarKey)
		resp.ActorAvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, a.ActorAvatarThumbKey)
	}
	return resp
}

// assignedToMeMemberLookupConcurrency caps how many per-project ListMembers
// lookups ListAssignedToMe issues concurrently while resolving the caller's
// member IDs.
const assignedToMeMemberLookupConcurrency = 20

// projectServiceForAssigned is the minimal project-service surface used by
// ListAssignedToMe to resolve which projects the caller belongs to and their
// per-project member ID — task_assignees references project_members.id, not
// the global user ID, so a membership lookup is required before the tasks
// can be queried.
type projectServiceForAssigned interface {
	ListAccessible(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*projectdom.Project, int64, error)
	ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error)
}

// WithTaskAssignedProjectService wires the project service used by
// ListAssignedToMe. Omitting this option makes ListAssignedToMe respond with
// an empty list.
func WithTaskAssignedProjectService(svc projectServiceForAssigned) TaskHandlerOption {
	return func(h *TaskHandler) {
		h.projectSvc = svc
	}
}

// --- Task Types -------------------------------------------------------------

// ListTaskTypes handles GET /projects/:projectId/task-types.
func (h *TaskHandler) ListTaskTypes(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	types, err := h.svc.ListTaskTypes(r.Context(), projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.TaskTypeResponse, 0, len(types))
	for _, t := range types {
		resp = append(resp, dto.TaskTypeFromEntity(t))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// CreateTaskType handles POST /projects/:projectId/task-types.
func (h *TaskHandler) CreateTaskType(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateTaskTypeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}

	t, err := h.svc.CreateTaskType(r.Context(), taskdom.CreateTaskTypeInput{
		ProjectID:   projectID,
		Name:        req.Name,
		Icon:        req.Icon,
		Color:       req.Color,
		Description: req.Description,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.TaskTypeFromEntity(t))
}

// UpdateTaskType handles PATCH /projects/:projectId/task-types/:typeId.
func (h *TaskHandler) UpdateTaskType(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	typeID, err := parseTaskTypeID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateTaskTypeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	t, err := h.svc.UpdateTaskType(r.Context(), projectID, typeID, taskdom.UpdateTaskTypeInput{
		Name:        req.Name,
		Icon:        req.Icon.Ptr(),
		Color:       req.Color.Ptr(),
		Description: req.Description.Ptr(),
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.TaskTypeFromEntity(t))
}

// DeleteTaskType handles DELETE /projects/:projectId/task-types/:typeId.
func (h *TaskHandler) DeleteTaskType(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	typeID, err := parseTaskTypeID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteTaskType(r.Context(), projectID, typeID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "task type deleted"})
}

// SetDefaultTaskType handles PUT /projects/:projectId/task-types/:typeId/set-default.
func (h *TaskHandler) SetDefaultTaskType(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	typeID, err := parseTaskTypeID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	t, err := h.svc.SetDefaultTaskType(r.Context(), projectID, typeID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.TaskTypeFromEntity(t))
}

// --- Task Statuses ----------------------------------------------------------

// ListTaskStatuses handles GET /projects/:projectId/task-statuses.
func (h *TaskHandler) ListTaskStatuses(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	statuses, err := h.svc.ListTaskStatuses(r.Context(), projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.TaskStatusResponse, 0, len(statuses))
	for _, s := range statuses {
		resp = append(resp, dto.TaskStatusFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// CreateTaskStatus handles POST /projects/:projectId/task-statuses.
func (h *TaskHandler) CreateTaskStatus(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateTaskStatusRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}
	if req.Category == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "category is required"))
		return
	}

	s, err := h.svc.CreateTaskStatus(r.Context(), taskdom.CreateTaskStatusInput{
		ProjectID: projectID,
		Name:      req.Name,
		Color:     req.Color,
		Position:  req.Position,
		Category:  req.Category,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.TaskStatusFromEntity(s))
}

// UpdateTaskStatus handles PATCH /projects/:projectId/task-statuses/:statusId.
func (h *TaskHandler) UpdateTaskStatus(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	statusID, err := parseTaskStatusID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateTaskStatusRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	s, err := h.svc.UpdateTaskStatus(r.Context(), projectID, statusID, taskdom.UpdateTaskStatusInput{
		Name:     req.Name,
		Color:    req.Color,
		Position: req.Position,
		Category: req.Category,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.TaskStatusFromEntity(s))
}

// DeleteTaskStatus handles DELETE /projects/:projectId/task-statuses/:statusId.
func (h *TaskHandler) DeleteTaskStatus(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	statusID, err := parseTaskStatusID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteTaskStatus(r.Context(), projectID, statusID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "task status deleted"})
}

// SetDefaultTaskStatus handles PUT /projects/:projectId/task-statuses/:statusId/set-default.
func (h *TaskHandler) SetDefaultTaskStatus(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	statusID, err := parseTaskStatusID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	s, err := h.svc.SetDefaultTaskStatus(r.Context(), projectID, statusID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.TaskStatusFromEntity(s))
}

// ReorderTaskStatuses handles PUT /projects/:projectId/task-statuses/positions.
func (h *TaskHandler) ReorderTaskStatuses(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.ReorderTaskStatusesRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.StatusIDs) == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "status_ids must not be empty"))
		return
	}

	if err := h.svc.ReorderTaskStatuses(r.Context(), projectID, req.StatusIDs); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// --- Tasks ------------------------------------------------------------------

// parseTaskSort resolves the sort_by query parameter into a TaskSort.
// For custom field sort keys the field definition is looked up via the service
// (ListCustomFieldDefinitions is cached, so this is cheap).
// Note: the public "created" key maps to the DB column created_at in applyTaskSort.
func parseTaskSort(ctx context.Context, svc taskdom.Service, projectID uuid.UUID, sortByRaw string) taskdom.TaskSort {
	sortBy := strings.TrimSpace(sortByRaw)
	switch sortBy {
	case "importance", "title", "story_points", "start_date", "due_date", "created":
		return taskdom.TaskSort{By: sortBy}
	case "", "manual":
		return taskdom.TaskSort{}
	default:
		cfs, err := svc.ListCustomFieldDefinitions(ctx, projectID)
		if err != nil {
			return taskdom.TaskSort{}
		}
		for _, cf := range cfs {
			if cf.FieldKey == sortBy {
				optValues := make([]string, len(cf.Options))
				for i, o := range cf.Options {
					optValues[i] = o.Value
				}
				return taskdom.TaskSort{
					By:     sortBy,
					CFType: string(cf.FieldType),
					CFOpts: optValues,
				}
			}
		}
		return taskdom.TaskSort{}
	}
}

// customFieldFilterParam is the wire shape of one entry in the
// custom_field_filters query parameter (a JSON object keyed by field key).
// Which members are meaningful is determined server-side from the field's
// CustomFieldDefinition — see parseCustomFieldFilters.
type customFieldFilterParam struct {
	Values   []string `json:"values,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	After    *string  `json:"after,omitempty"`
	Before   *string  `json:"before,omitempty"`
	Contains *string  `json:"contains,omitempty"`
}

// parseCustomFieldFilters decodes the custom_field_filters query parameter
// (a JSON object keyed by custom field key) and resolves each entry's
// FieldType against the project's custom field definitions — the field type
// is never trusted from the client, since it determines how the value is
// cast in SQL. Unknown field keys are silently ignored (e.g. a stale saved
// view referencing a since-deleted custom field).
func parseCustomFieldFilters(ctx context.Context, svc taskdom.Service, projectID uuid.UUID, raw string) (map[string]taskdom.CustomFieldFilterQuery, error) {
	var parsed map[string]customFieldFilterParam
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "invalid custom_field_filters")
	}
	if len(parsed) == 0 {
		return nil, nil
	}
	cfs, err := svc.ListCustomFieldDefinitions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	cfByKey := make(map[string]*taskdom.CustomFieldDefinition, len(cfs))
	for _, cf := range cfs {
		cfByKey[cf.FieldKey] = cf
	}
	resolved := make(map[string]taskdom.CustomFieldFilterQuery, len(parsed))
	for key, p := range parsed {
		cf, ok := cfByKey[key]
		if !ok {
			continue
		}
		resolved[key] = taskdom.CustomFieldFilterQuery{
			FieldType: string(cf.FieldType),
			Values:    p.Values,
			Min:       p.Min,
			Max:       p.Max,
			After:     p.After,
			Before:    p.Before,
			Contains:  p.Contains,
		}
	}
	return resolved, nil
}

// isValidDateString reports whether s is a valid "YYYY-MM-DD" date, matching
// the format the start_date/due_date columns and their filter query params use.
func isValidDateString(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// importanceRangeParam is the wire shape of one entry in the
// importance_ranges query parameter (a JSON array of inclusive [min,max]
// ranges, OR'd together — see taskdom.TaskFilter.ImportanceRanges).
type importanceRangeParam struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// parseImportanceRanges decodes the importance_ranges query parameter.
func parseImportanceRanges(raw string) ([]taskdom.IntRange, error) {
	var parsed []importanceRangeParam
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "invalid importance_ranges")
	}
	ranges := make([]taskdom.IntRange, 0, len(parsed))
	for _, p := range parsed {
		if p.Min > p.Max {
			return nil, apierr.New(apierr.CodeBadRequest, "invalid importance_ranges: min > max")
		}
		ranges = append(ranges, taskdom.IntRange{Min: p.Min, Max: p.Max})
	}
	return ranges, nil
}

// ListTasks handles GET /projects/:projectId/tasks.
// Supported filter query params:
//   - sprint_id=<uuid>|null or sprint_ids=<uuid,uuid>
//   - status_id=<uuid> or status_ids=<uuid,uuid>
//   - assignee_id=<uuid> or assignee_ids=<uuid,uuid>
//   - task_type_ids=<uuid,uuid>
//   - parent_task_id=<uuid>
//   - search=<text> (matches title or "#<task_number>", case-insensitive)
//   - custom_field_filters=<json> (object keyed by custom field key; see
//     customFieldFilterParam for the shape of each entry)
//   - start_date_after=<YYYY-MM-DD>, start_date_before=<YYYY-MM-DD>
//   - due_date_after=<YYYY-MM-DD>, due_date_before=<YYYY-MM-DD>
//   - story_points_min=<int>, story_points_max=<int>
//   - importance_ranges=<json> (array of {"min":int,"max":int}, OR'd together)
//   - tags=<tag,tag,...> (matches tasks with ANY of these tags)
func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	pageSize, err := parsePageSize(r, 20, 200)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	filter := taskdom.TaskFilter{}

	if raw := r.URL.Query().Get("sprint_id"); raw != "" {
		if strings.EqualFold(strings.TrimSpace(raw), "null") {
			filter.BacklogOnly = true
		} else {
			id, err := uuid.Parse(raw)
			if err != nil {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid sprint_id"))
				return
			}
			filter.SprintID = &id
		}
	}
	if ids, err := parseQueryUUIDs(r.URL.Query().Get("sprint_ids")); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid sprint_ids"))
		return
	} else if len(ids) > 0 {
		filter.SprintIDs = ids
		filter.BacklogOnly = false
		filter.SprintID = nil
	}
	if raw := r.URL.Query().Get("status_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid status_id"))
			return
		}
		filter.StatusID = &id
	}
	if ids, err := parseQueryUUIDs(r.URL.Query().Get("status_ids")); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid status_ids"))
		return
	} else if len(ids) > 0 {
		filter.StatusIDs = ids
	}
	if raw := r.URL.Query().Get("assignee_id"); raw != "" {
		if strings.EqualFold(strings.TrimSpace(raw), "null") {
			filter.AssigneeNull = true
		} else {
			id, err := uuid.Parse(raw)
			if err != nil {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid assignee_id"))
				return
			}
			filter.AssigneeID = &id
		}
	}
	if ids, err := parseQueryUUIDs(r.URL.Query().Get("assignee_ids")); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid assignee_ids"))
		return
	} else if len(ids) > 0 {
		filter.AssigneeIDs = ids
	}
	if ids, err := parseQueryUUIDs(r.URL.Query().Get("task_type_ids")); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid task_type_ids"))
		return
	} else if len(ids) > 0 {
		filter.TaskTypeIDs = ids
	}
	if raw := r.URL.Query().Get("task_type_id"); raw != "" {
		if strings.EqualFold(strings.TrimSpace(raw), "null") {
			filter.TaskTypeNull = true
		} else {
			id, err := uuid.Parse(raw)
			if err != nil {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid task_type_id"))
				return
			}
			filter.TaskTypeIDs = []uuid.UUID{id}
		}
	}
	if raw := r.URL.Query().Get("parent_task_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid parent_task_id"))
			return
		}
		filter.ParentTaskID = &id
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("search")); raw != "" {
		filter.Search = &raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("custom_field_filters")); raw != "" {
		cfFilters, err := parseCustomFieldFilters(r.Context(), h.svc, projectID, raw)
		if err != nil {
			presenter.Error(w, r, err)
			return
		}
		filter.CustomFieldFilters = cfFilters
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("start_date_after")); raw != "" {
		if !isValidDateString(raw) {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid start_date_after"))
			return
		}
		filter.StartDateAfter = &raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("start_date_before")); raw != "" {
		if !isValidDateString(raw) {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid start_date_before"))
			return
		}
		filter.StartDateBefore = &raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("due_date_after")); raw != "" {
		if !isValidDateString(raw) {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid due_date_after"))
			return
		}
		filter.DueDateAfter = &raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("due_date_before")); raw != "" {
		if !isValidDateString(raw) {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid due_date_before"))
			return
		}
		filter.DueDateBefore = &raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("story_points_min")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid story_points_min"))
			return
		}
		filter.StoryPointsMin = &v
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("story_points_max")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid story_points_max"))
			return
		}
		filter.StoryPointsMax = &v
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("importance_ranges")); raw != "" {
		ranges, err := parseImportanceRanges(raw)
		if err != nil {
			presenter.Error(w, r, err)
			return
		}
		filter.ImportanceRanges = ranges
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("tags")); raw != "" {
		var tags []string
		for _, tag := range strings.Split(raw, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
		filter.Tags = tags
	}
	if cursorRaw := r.URL.Query().Get("cursor"); cursorRaw != "" {
		filter.CursorAfter = &cursorRaw
	}
	sort := parseTaskSort(r.Context(), h.svc, projectID, r.URL.Query().Get("sort_by"))

	var posMap map[uuid.UUID]*sprintdom.ViewTaskPosition
	if raw := r.URL.Query().Get("view_id"); raw != "" {
		viewID, err := uuid.Parse(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid view_id"))
			return
		}
		positions, err := h.viewSvc.ListTaskPositions(r.Context(), projectID, viewID)
		if err != nil {
			presenter.Error(w, r, err)
			return
		}
		posMap = make(map[uuid.UUID]*sprintdom.ViewTaskPosition, len(positions))
		for _, p := range positions {
			cp := p
			posMap[p.TaskID] = cp
		}
		// When no explicit sort_by is requested (manual sort), order by the saved
		// view positions so the first page reflects the user's manual order.
		if sort.By == "" {
			sort.By = "view_position"
			sort.ViewID = &viewID
		}
	}

	// Count without cursor so the total reflects all matching tasks, not just the current page.
	aggFilter := filter
	aggFilter.CursorAfter = nil
	// Optionally sum a numeric field across all matching tasks.
	// Returns null in the response when sum_field is absent or "count".
	sumField := strings.TrimSpace(r.URL.Query().Get("sum_field"))

	var (
		tasks      []*taskdom.Task
		hasMore    bool
		totalCount int64
		fieldSumV  float64
	)
	g, gctx := errgroup.WithContext(r.Context())
	g.Go(func() error {
		var err error
		tasks, hasMore, err = h.svc.ListTasks(gctx, projectID, filter, pageSize, sort)
		return err
	})
	g.Go(func() error {
		var err error
		totalCount, err = h.svc.CountTasks(gctx, projectID, aggFilter)
		return err
	})
	if sumField != "" && sumField != "count" {
		g.Go(func() error {
			var err error
			fieldSumV, err = h.svc.SumTaskField(gctx, projectID, aggFilter, sumField)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		presenter.Error(w, r, err)
		return
	}

	var fieldSum *float64
	if sumField != "" && sumField != "count" {
		fieldSum = &fieldSumV
	}

	resp := make([]dto.TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		r := dto.TaskFromEntity(t)
		if pos, ok := posMap[t.ID]; ok {
			r.ViewPosition = &pos.Position
			r.ViewGroupKey = pos.GroupKey
		}
		resp = append(resp, r)
	}

	var nextCursor *string
	if hasMore && len(tasks) > 0 {
		last := tasks[len(tasks)-1]
		s := taskdom.EncodeTaskCursor(last, sort)
		nextCursor = &s
	}
	presenter.OK(w, r, map[string]any{
		"items":       resp,
		"page_size":   pageSize,
		"next_cursor": nextCursor,
		"total_count": totalCount,
		"field_sum":   fieldSum,
	})
}

// ListAssignedToMe handles GET /users/me/tasks. It returns open (non-done)
// tasks assigned to the authenticated user across every project they belong
// to, ordered by importance (most important first) then created_at (oldest
// first), keyset-paginated via the same opaque "cursor" query param used by
// ListTasks. Powers the cross-project "assigned to me" widget on the home page.
func (h *TaskHandler) ListAssignedToMe(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	pageSize, err := parsePageSize(r, 10, 100)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var cursor *string
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor = &raw
	}

	empty := func() {
		presenter.OK(w, r, map[string]any{
			"items":       []dto.TaskResponse{},
			"page_size":   pageSize,
			"next_cursor": nil,
		})
	}

	if h.projectSvc == nil {
		empty()
		return
	}

	projects, _, err := h.projectSvc.ListAccessible(r.Context(), userID, 1, 10000)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if len(projects) == 0 {
		empty()
		return
	}

	// Resolve the caller's project_members.id in each project — task
	// assignment is stored by member ID, not user ID. Capped so a caller
	// belonging to many projects can't fan out unbounded concurrent lookups.
	g, gctx := errgroup.WithContext(r.Context())
	g.SetLimit(assignedToMeMemberLookupConcurrency)
	var mu sync.Mutex
	var memberIDs []uuid.UUID
	for _, p := range projects {
		projectID := p.ID
		g.Go(func() error {
			members, listErr := h.projectSvc.ListMembers(gctx, projectID)
			if listErr != nil {
				return listErr
			}
			for _, m := range members {
				if m.UserID == userID {
					mu.Lock()
					memberIDs = append(memberIDs, m.ID)
					mu.Unlock()
					break
				}
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if len(memberIDs) == 0 {
		empty()
		return
	}

	tasks, hasMore, err := h.svc.ListAssignedTasks(r.Context(), memberIDs, pageSize, cursor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	resp := make([]dto.TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		resp = append(resp, dto.TaskFromEntity(t))
	}

	var nextCursor *string
	if hasMore && len(tasks) > 0 {
		s := taskdom.EncodeTaskCursor(tasks[len(tasks)-1], taskdom.TaskSort{By: "importance"})
		nextCursor = &s
	}
	presenter.OK(w, r, map[string]any{
		"items":       resp,
		"page_size":   pageSize,
		"next_cursor": nextCursor,
	})
}

// GetTask handles GET /projects/:projectId/tasks/:taskId.
func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	t, err := h.svc.GetTask(r.Context(), projectID, taskID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.TaskFromEntity(t))
}

// GetTaskByNumber handles GET /projects/:projectId/tasks/by-number/:taskNumber.
// It looks up a task by its project-scoped sequential number.
func (h *TaskHandler) GetTaskByNumber(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var taskNumber int64
	if _, err := fmt.Sscanf(chi.URLParam(r, "taskNumber"), "%d", &taskNumber); err != nil || taskNumber < 1 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid task number"))
		return
	}
	t, err := h.svc.GetTaskByNumber(r.Context(), projectID, taskNumber)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.TaskFromEntity(t))
}

// CreateTask handles POST /projects/:projectId/tasks.
func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateTaskRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Description != nil {
		if err := dto.ValidateBlockNoteContent(*req.Description); err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "description: "+err.Error()))
			return
		}
	}

	t, err := h.svc.CreateTask(r.Context(), taskdom.CreateTaskInput{
		ProjectID:    projectID,
		TaskTypeID:   req.TaskTypeID,
		StatusID:     req.StatusID,
		SprintID:     req.SprintID,
		ParentTaskID: req.ParentTaskID,
		Title:        req.Title,
		Description:  req.NormalizedDescription(),
		Importance:   req.Importance,
		StoryPoints:  req.StoryPoints,
		AssigneeIDs:  req.AssigneeIDs,
		ReporterID:   req.ReporterID,
		CustomFields: req.CustomFields,
		StartDate:    req.StartDate,
		DueDate:      req.DueDate,
		Tags:         req.Tags,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record creation activity (best-effort).
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		agentID, _ := middleware.AgentIDFromContext(r.Context())
		var agentIDPtr *uuid.UUID
		if agentID != uuid.Nil {
			agentIDPtr = &agentID
		}
		content, _ := json.Marshal(map[string]any{"title": t.Title})
		_ = h.activitySvc.RecordActivity(r.Context(), taskdom.RecordActivityInput{
			TaskID:       t.ID,
			ProjectID:    projectID,
			ActorID:      &actorID,
			ActorAgentID: agentIDPtr,
			ActivityType: taskdom.ActivityTypeTaskCreated,
			Content:      content,
		})

		// Enqueue one assignment event per assignee so the NotificationConsumer
		// can create the in-app notification asynchronously (best-effort).
		for _, memberID := range req.AssigneeIDs {
			_ = events.PublishAssignmentChanged(r.Context(), h.publisher, t.ID, projectID, memberID, nil, actorID, agentIDPtr, nil)
		}
	}

	presenter.Created(w, r, dto.TaskFromEntity(t))
}

// UpdateTask handles PATCH /projects/:projectId/tasks/:taskId.
func (h *TaskHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateTaskRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Description.Set && req.Description.Value != nil {
		if err := dto.ValidateBlockNoteContent(req.Description.Value); err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "description: "+err.Error()))
			return
		}
	}

	// Fetch old state before mutating so we can record before/after values.
	oldTask, _ := h.svc.GetTask(r.Context(), projectID, taskID)

	t, err := h.svc.UpdateTask(r.Context(), projectID, taskID, taskdom.UpdateTaskInput{
		TaskTypeID:   req.TaskTypeID.Ptr(),
		StatusID:     req.StatusID.Ptr(),
		SprintID:     req.SprintID.Ptr(),
		ParentTaskID: req.ParentTaskID.Ptr(),
		Title:        req.Title,
		Description:  req.Description.Ptr(),
		Importance:   req.Importance,
		StoryPoints:  req.StoryPoints.Ptr(),
		AssigneeIDs:  req.AssigneeIDs.Ptr(),
		ReporterID:   req.ReporterID.Ptr(),
		CustomFields: req.CustomFields,
		StartDate:    req.StartDate.Ptr(),
		DueDate:      req.DueDate.Ptr(),
		Tags:         req.Tags,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record update activity (best-effort).
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok && oldTask != nil {
		agentID, _ := middleware.AgentIDFromContext(r.Context())
		var agentIDPtr *uuid.UUID
		if agentID != uuid.Nil {
			agentIDPtr = &agentID
		}
		changes := h.taskChangedFields(r.Context(), oldTask, req)
		if len(changes) > 0 {
			content, _ := json.Marshal(map[string]any{"changes": changes})
			_ = h.activitySvc.RecordActivity(r.Context(), taskdom.RecordActivityInput{
				TaskID:       taskID,
				ProjectID:    projectID,
				ActorID:      &actorID,
				ActorAgentID: agentIDPtr,
				ActivityType: taskdom.ActivityTypeTaskUpdated,
				Content:      content,
			})
		}

		// Enqueue an assignment event for each newly-added assignee so the
		// NotificationConsumer can create the in-app notification
		// asynchronously. Members already assigned before this update don't
		// get a duplicate event — only genuinely new assignments notify.
		if req.AssigneeIDs.Set {
			oldSet := make(map[uuid.UUID]struct{}, len(oldTask.AssigneeIDs))
			for _, id := range oldTask.AssigneeIDs {
				oldSet[id] = struct{}{}
			}
			for _, memberID := range req.AssigneeIDs.Value {
				if _, alreadyAssigned := oldSet[memberID]; alreadyAssigned {
					continue
				}
				_ = events.PublishAssignmentChanged(r.Context(), h.publisher, taskID, projectID, memberID, nil, actorID, agentIDPtr, nil)
			}
		}

		// Publish a plugin event for each user newly @mentioned in the
		// description (no in-app row — see
		// notificationSvc.NotifyTaskDescriptionMentioned's doc comment).
		if h.notificationSvc != nil && req.Description.Set && req.Description.Value != nil {
			for _, mentionedID := range newlyMentionedUserIDs(oldTask.Description, req.Description.Value) {
				h.notificationSvc.NotifyTaskDescriptionMentioned(r.Context(), mentionedID, actorID, projectID, taskID)
			}
		}
	}

	presenter.OK(w, r, dto.TaskFromEntity(t))
}

// taskChangedFields compares the old task snapshot against the patch request
// and returns a FieldChange for each field that actually changed.  Old and New
// values are populated so activity consumers can render before/after messages.
// Status and type names are resolved via the service; for ID-only references
// (assignee, reporter, sprint, parent) the UUID string is stored.
func (h *TaskHandler) taskChangedFields(ctx context.Context, old *taskdom.Task, req dto.UpdateTaskRequest) []taskdom.FieldChange {
	var changes []taskdom.FieldChange

	if req.Title != "" && req.Title != old.Title {
		changes = append(changes, taskdom.FieldChange{Field: "title", Old: old.Title, New: req.Title})
	}

	if req.StatusID.Set {
		oldName := h.resolveStatusName(ctx, old.StatusID)
		newName := h.resolveStatusName(ctx, req.StatusID.Value)
		if fmt.Sprint(oldName) != fmt.Sprint(newName) {
			changes = append(changes, taskdom.FieldChange{Field: "status", Old: oldName, New: newName})
		}
	}

	if req.TaskTypeID.Set {
		oldName := h.resolveTaskTypeName(ctx, old.TaskTypeID)
		newName := h.resolveTaskTypeName(ctx, req.TaskTypeID.Value)
		if fmt.Sprint(oldName) != fmt.Sprint(newName) {
			changes = append(changes, taskdom.FieldChange{Field: "task_type", Old: oldName, New: newName})
		}
	}

	if req.Importance != nil && *req.Importance != old.Importance {
		changes = append(changes, taskdom.FieldChange{Field: "importance", Old: old.Importance, New: *req.Importance})
	}

	if req.StoryPoints.Set {
		oldVal := intPtrToStr(old.StoryPoints)
		newVal := intPtrToStr(req.StoryPoints.Value)
		if oldVal != newVal {
			changes = append(changes, taskdom.FieldChange{Field: "story_points", Old: old.StoryPoints, New: req.StoryPoints.Value})
		}
	}

	if req.AssigneeIDs.Set {
		oldVal := uuidSliceToStrs(old.AssigneeIDs)
		newVal := uuidSliceToStrs(req.AssigneeIDs.Value)
		if !equalStrSets(oldVal, newVal) {
			changes = append(changes, taskdom.FieldChange{Field: "assignee", Old: oldVal, New: newVal})
		}
	}

	if req.ReporterID.Set {
		oldVal := uuidPtrToStr(old.ReporterID)
		newVal := uuidPtrToStr(req.ReporterID.Value)
		if oldVal != newVal {
			changes = append(changes, taskdom.FieldChange{Field: "reporter", Old: oldVal, New: newVal})
		}
	}

	if req.SprintID.Set {
		oldVal := uuidPtrToStr(old.SprintID)
		newVal := uuidPtrToStr(req.SprintID.Value)
		if oldVal != newVal {
			changes = append(changes, taskdom.FieldChange{Field: "sprint", Old: oldVal, New: newVal})
		}
	}

	if req.ParentTaskID.Set {
		oldVal := uuidPtrToStr(old.ParentTaskID)
		newVal := uuidPtrToStr(req.ParentTaskID.Value)
		if oldVal != newVal {
			changes = append(changes, taskdom.FieldChange{Field: "parent_task", Old: oldVal, New: newVal})
		}
	}

	if req.StartDate.Set {
		oldVal := timePtrToStr(old.StartDate)
		newVal := timePtrToStr(req.StartDate.Value)
		if oldVal != newVal {
			changes = append(changes, taskdom.FieldChange{Field: "start_date", Old: oldVal, New: newVal})
		}
	}

	if req.DueDate.Set {
		oldVal := timePtrToStr(old.DueDate)
		newVal := timePtrToStr(req.DueDate.Value)
		if oldVal != newVal {
			changes = append(changes, taskdom.FieldChange{Field: "due_date", Old: oldVal, New: newVal})
		}
	}

	if req.Description.Set && string(req.Description.Value) != string(old.Description) {
		changes = append(changes, taskdom.FieldChange{Field: "description", Old: old.Description, New: req.Description.Value})
	}

	if req.Tags != nil {
		changes = append(changes, taskdom.FieldChange{Field: "tags", Old: old.Tags, New: req.Tags})
	}

	if req.CustomFields != nil {
		oldJSON, oldErr := json.Marshal(old.CustomFields)
		newJSON, newErr := json.Marshal(*req.CustomFields)
		if oldErr != nil || newErr != nil || string(oldJSON) != string(newJSON) {
			changes = append(changes, taskdom.FieldChange{Field: "custom_fields"})
		}
	}

	return changes
}

// resolveStatusName looks up a status by ID and returns its name, falling back
// to the UUID string if the lookup fails.  Returns nil for a nil ID.
func (h *TaskHandler) resolveStatusName(ctx context.Context, id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	if s, err := h.svc.GetTaskStatus(ctx, *id); err == nil {
		return s.Name
	}
	return id.String()
}

// resolveTaskTypeName looks up a task type by ID and returns its name, falling
// back to the UUID string if the lookup fails.  Returns nil for a nil ID.
func (h *TaskHandler) resolveTaskTypeName(ctx context.Context, id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	if t, err := h.svc.GetTaskType(ctx, *id); err == nil {
		return t.Name
	}
	return id.String()
}

// uuidPtrToStr converts a *uuid.UUID to a string (empty string for nil).
func uuidPtrToStr(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

// uuidSliceToStrs converts a UUID slice to a sorted string slice, for
// order-insensitive set comparison and stable activity-log rendering.
func uuidSliceToStrs(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	sort.Strings(out)
	return out
}

// equalStrSets reports whether a and b (both already sorted) contain the same elements.
func equalStrSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// timePtrToStr formats a *time.Time as a date string (empty string for nil).
func timePtrToStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// intPtrToStr converts a *int to a string (empty string for nil).
func intPtrToStr(n *int) string {
	if n == nil {
		return ""
	}
	return fmt.Sprintf("%d", *n)
}

// DeleteTask handles DELETE /projects/:projectId/tasks/:taskId.
func (h *TaskHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteTask(r.Context(), projectID, taskID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record deletion activity (best-effort).
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		agentID, _ := middleware.AgentIDFromContext(r.Context())
		var agentIDPtr *uuid.UUID
		if agentID != uuid.Nil {
			agentIDPtr = &agentID
		}
		_ = h.activitySvc.RecordActivity(r.Context(), taskdom.RecordActivityInput{
			TaskID:       taskID,
			ProjectID:    projectID,
			ActorID:      &actorID,
			ActorAgentID: agentIDPtr,
			ActivityType: taskdom.ActivityTypeTaskDeleted,
			Content:      json.RawMessage(`{}`),
		})
	}

	presenter.OK(w, r, map[string]any{"message": "task deleted"})
}

// --- helpers ----------------------------------------------------------------

func parseTaskTypeID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "typeId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid task type id")
	}
	return id, nil
}

func parseTaskStatusID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "statusId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid task status id")
	}
	return id, nil
}

func parseTaskID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "taskId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid task id")
	}
	return id, nil
}

func parseQueryUUIDs(raw string) ([]uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]uuid.UUID, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := uuid.Parse(part)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// --- Custom Field Definitions -----------------------------------------------

// ListCustomFieldDefinitions handles GET /projects/:projectId/custom-fields.
func (h *TaskHandler) ListCustomFieldDefinitions(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	fields, err := h.svc.ListCustomFieldDefinitions(r.Context(), projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.CustomFieldDefinitionResponse, 0, len(fields))
	for _, f := range fields {
		resp = append(resp, dto.CustomFieldDefinitionFromEntity(f))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// GetCustomFieldDefinition handles GET /projects/:projectId/custom-fields/:fieldId.
func (h *TaskHandler) GetCustomFieldDefinition(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	fieldID, err := parseCustomFieldID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	f, err := h.svc.GetCustomFieldDefinition(r.Context(), projectID, fieldID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.CustomFieldDefinitionFromEntity(f))
}

// CreateCustomFieldDefinition handles POST /projects/:projectId/custom-fields.
func (h *TaskHandler) CreateCustomFieldDefinition(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateCustomFieldDefinitionRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.FieldKey == "" || req.DisplayName == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "field_key and display_name are required"))
		return
	}
	if req.FieldType == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "field_type is required"))
		return
	}

	f, err := h.svc.CreateCustomFieldDefinition(r.Context(), taskdom.CreateCustomFieldDefinitionInput{
		ProjectID:   projectID,
		FieldKey:    req.FieldKey,
		DisplayName: req.DisplayName,
		FieldType:   req.FieldType,
		Options:     req.CustomFieldOptionsToDomain(),
		IsRequired:  req.IsRequired,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.CustomFieldDefinitionFromEntity(f))
}

// UpdateCustomFieldDefinition handles PATCH /projects/:projectId/custom-fields/:fieldId.
func (h *TaskHandler) UpdateCustomFieldDefinition(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	fieldID, err := parseCustomFieldID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateCustomFieldDefinitionRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	f, err := h.svc.UpdateCustomFieldDefinition(r.Context(), projectID, fieldID, taskdom.UpdateCustomFieldDefinitionInput{
		DisplayName: req.DisplayName,
		FieldType:   req.FieldType,
		Options:     req.CustomFieldOptionsToDomain(),
		IsRequired:  req.IsRequired,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.CustomFieldDefinitionFromEntity(f))
}

// DeleteCustomFieldDefinition handles DELETE /projects/:projectId/custom-fields/:fieldId.
func (h *TaskHandler) DeleteCustomFieldDefinition(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	fieldID, err := parseCustomFieldID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteCustomFieldDefinition(r.Context(), projectID, fieldID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "custom field deleted"})
}

func parseCustomFieldID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "fieldId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid custom field id")
	}
	return id, nil
}

// --- Activities / Comments --------------------------------------------------

// parseCommentID parses the :commentId path parameter.
func parseCommentID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "commentId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid comment id")
	}
	return id, nil
}

// ListTaskActivities handles GET /projects/:projectId/tasks/:taskId/activities.
func (h *TaskHandler) ListTaskActivities(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	activities, err := h.activitySvc.ListActivities(r.Context(), projectID, taskID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.ActivityResponse, 0, len(activities))
	for _, a := range activities {
		resp = append(resp, h.toActivityResponse(r.Context(), a))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// AddComment handles POST /projects/:projectId/tasks/:taskId/activities/comments.
func (h *TaskHandler) AddComment(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	actorID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	var req dto.AddCommentRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.Content) == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content is required"))
		return
	}

	agentID, _ := middleware.AgentIDFromContext(r.Context())
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}

	a, err := h.activitySvc.AddComment(r.Context(), taskdom.AddCommentInput{
		TaskID:    taskID,
		ProjectID: projectID,
		ActorID:   actorID,
		AgentID:   agentIDPtr,
		Content:   req.Content,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, h.toActivityResponse(r.Context(), a))
}

// UpdateComment handles PATCH /projects/:projectId/tasks/:taskId/activities/comments/:commentId.
func (h *TaskHandler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	commentID, err := parseCommentID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	actorID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	var req dto.UpdateCommentRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.Content) == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content is required"))
		return
	}

	agentID, _ := middleware.AgentIDFromContext(r.Context())
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}

	a, err := h.activitySvc.UpdateComment(r.Context(), commentID, projectID, actorID, agentIDPtr, req.Content)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toActivityResponse(r.Context(), a))
}

// DeleteComment handles DELETE /projects/:projectId/tasks/:taskId/activities/comments/:commentId.
func (h *TaskHandler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	commentID, err := parseCommentID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	actorID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	agentID, _ := middleware.AgentIDFromContext(r.Context())
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}

	if err := h.activitySvc.DeleteComment(r.Context(), commentID, projectID, actorID, agentIDPtr); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// --- Task Links -------------------------------------------------------------

// ListTaskLinks handles GET /projects/:projectId/tasks/:taskId/links.
func (h *TaskHandler) ListTaskLinks(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	links, err := h.svc.ListTaskLinks(r.Context(), projectID, taskID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	resp := make([]dto.TaskLinkResponse, 0, len(links))
	for _, l := range links {
		resp = append(resp, dto.TaskLinkFromEntity(l))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// CreateTaskLink handles POST /projects/:projectId/tasks/:taskId/links.
func (h *TaskHandler) CreateTaskLink(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateTaskLinkRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.TargetTaskID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "target_task_id is required"))
		return
	}
	if !taskdom.ValidLinkTypes[req.LinkType] {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid link_type"))
		return
	}

	actorID, _ := middleware.ActorIDFromContext(r.Context())
	var createdBy *uuid.UUID
	if actorID != uuid.Nil {
		createdBy = &actorID
	}

	link, err := h.svc.CreateTaskLink(r.Context(), taskdom.CreateTaskLinkInput{
		ProjectID:    projectID,
		SourceTaskID: taskID,
		TargetTaskID: req.TargetTaskID,
		LinkType:     req.LinkType,
		CreatedBy:    createdBy,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record link creation activity (best-effort).
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		agentID, _ := middleware.AgentIDFromContext(r.Context())
		var agentIDPtr *uuid.UUID
		if agentID != uuid.Nil {
			agentIDPtr = &agentID
		}
		content, _ := json.Marshal(map[string]any{
			"target_task_id": req.TargetTaskID,
			"link_type":      req.LinkType,
		})
		_ = h.activitySvc.RecordActivity(r.Context(), taskdom.RecordActivityInput{
			TaskID:       taskID,
			ProjectID:    projectID,
			ActorID:      &actorID,
			ActorAgentID: agentIDPtr,
			ActivityType: taskdom.ActivityTypeTaskLinkAdded,
			Content:      content,
		})
	}

	presenter.Created(w, r, dto.TaskLinkFromEntity(link))
}

// DeleteTaskLink handles DELETE /projects/:projectId/tasks/:taskId/links/:linkId.
func (h *TaskHandler) DeleteTaskLink(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	linkID, err := parseLinkID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	if err := h.svc.DeleteTaskLink(r.Context(), projectID, taskID, linkID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record link deletion activity (best-effort).
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		agentID, _ := middleware.AgentIDFromContext(r.Context())
		var agentIDPtr *uuid.UUID
		if agentID != uuid.Nil {
			agentIDPtr = &agentID
		}
		content, _ := json.Marshal(map[string]any{"link_id": linkID})
		_ = h.activitySvc.RecordActivity(r.Context(), taskdom.RecordActivityInput{
			TaskID:       taskID,
			ProjectID:    projectID,
			ActorID:      &actorID,
			ActorAgentID: agentIDPtr,
			ActivityType: taskdom.ActivityTypeTaskLinkRemoved,
			Content:      content,
		})
	}

	presenter.NoContent(w)
}

// parseLinkID parses the :linkId path parameter.
func parseLinkID(r *http.Request) (uuid.UUID, error) {
	raw := chi.URLParam(r, "linkId")
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid link id")
	}
	return id, nil
}
