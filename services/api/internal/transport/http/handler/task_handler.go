package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	taskdom "github.com/paca/api/internal/domain/task"
	"github.com/paca/api/internal/transport/http/dto"
	"github.com/paca/api/internal/transport/http/middleware"
	"github.com/paca/api/internal/transport/http/presenter"
)

// TaskHandler handles task management endpoints.
type TaskHandler struct {
	svc taskdom.Service
}

// NewTaskHandler returns a TaskHandler wired to the task service.
func NewTaskHandler(svc taskdom.Service) *TaskHandler {
	return &TaskHandler{svc: svc}
}

// --- Task Types -------------------------------------------------------------

// ListTaskTypes handles GET /projects/:projectId/task-types.
func (h *TaskHandler) ListTaskTypes(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	types, err := h.svc.ListTaskTypes(c.Request.Context(), projectID)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	resp := make([]dto.TaskTypeResponse, 0, len(types))
	for _, t := range types {
		resp = append(resp, dto.TaskTypeFromEntity(t))
	}
	presenter.OK(c, gin.H{"items": resp})
}

// CreateTaskType handles POST /projects/:projectId/task-types.
func (h *TaskHandler) CreateTaskType(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.CreateTaskTypeRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	t, err := h.svc.CreateTaskType(c.Request.Context(), taskdom.CreateTaskTypeInput{
		ProjectID:   projectID,
		Name:        req.Name,
		Icon:        req.Icon,
		Color:       req.Color,
		Description: req.Description,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.Created(c, dto.TaskTypeFromEntity(t))
}

// UpdateTaskType handles PATCH /projects/:projectId/task-types/:typeId.
func (h *TaskHandler) UpdateTaskType(c *gin.Context) {
	typeID, err := parseTaskTypeID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.UpdateTaskTypeRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	t, err := h.svc.UpdateTaskType(c.Request.Context(), typeID, taskdom.UpdateTaskTypeInput{
		Name:        req.Name,
		Icon:        req.Icon,
		Color:       req.Color,
		Description: req.Description,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.TaskTypeFromEntity(t))
}

// DeleteTaskType handles DELETE /projects/:projectId/task-types/:typeId.
func (h *TaskHandler) DeleteTaskType(c *gin.Context) {
	typeID, err := parseTaskTypeID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	if err := h.svc.DeleteTaskType(c.Request.Context(), typeID); err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, gin.H{"message": "task type deleted"})
}

// --- Task Statuses ----------------------------------------------------------

// ListTaskStatuses handles GET /projects/:projectId/task-statuses.
func (h *TaskHandler) ListTaskStatuses(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	statuses, err := h.svc.ListTaskStatuses(c.Request.Context(), projectID)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	resp := make([]dto.TaskStatusResponse, 0, len(statuses))
	for _, s := range statuses {
		resp = append(resp, dto.TaskStatusFromEntity(s))
	}
	presenter.OK(c, gin.H{"items": resp})
}

// CreateTaskStatus handles POST /projects/:projectId/task-statuses.
func (h *TaskHandler) CreateTaskStatus(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.CreateTaskStatusRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	s, err := h.svc.CreateTaskStatus(c.Request.Context(), taskdom.CreateTaskStatusInput{
		ProjectID: projectID,
		Name:      req.Name,
		Color:     req.Color,
		Position:  req.Position,
		Category:  req.Category,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.Created(c, dto.TaskStatusFromEntity(s))
}

// UpdateTaskStatus handles PATCH /projects/:projectId/task-statuses/:statusId.
func (h *TaskHandler) UpdateTaskStatus(c *gin.Context) {
	statusID, err := parseTaskStatusID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.UpdateTaskStatusRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	s, err := h.svc.UpdateTaskStatus(c.Request.Context(), statusID, taskdom.UpdateTaskStatusInput{
		Name:     req.Name,
		Color:    req.Color,
		Position: req.Position,
		Category: req.Category,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.TaskStatusFromEntity(s))
}

// DeleteTaskStatus handles DELETE /projects/:projectId/task-statuses/:statusId.
func (h *TaskHandler) DeleteTaskStatus(c *gin.Context) {
	statusID, err := parseTaskStatusID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	if err := h.svc.DeleteTaskStatus(c.Request.Context(), statusID); err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, gin.H{"message": "task status deleted"})
}

// --- Tasks ------------------------------------------------------------------

// ListTasks handles GET /projects/:projectId/tasks.
func (h *TaskHandler) ListTasks(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	page, pageSize := pagingParams(c)

	filter := taskdom.TaskFilter{}
	if raw := c.Query("sprint_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.SprintID = &id
		}
	}
	if raw := c.Query("status_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.StatusID = &id
		}
	}
	if raw := c.Query("assignee_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.AssigneeID = &id
		}
	}

	tasks, total, err := h.svc.ListTasks(c.Request.Context(), projectID, filter, page, pageSize)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	resp := make([]dto.TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		resp = append(resp, dto.TaskFromEntity(t))
	}
	presenter.OK(c, gin.H{"items": resp, "total": total, "page": page, "page_size": pageSize})
}

// GetTask handles GET /projects/:projectId/tasks/:taskId.
func (h *TaskHandler) GetTask(c *gin.Context) {
	taskID, err := parseTaskID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	t, err := h.svc.GetTask(c.Request.Context(), taskID)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.TaskFromEntity(t))
}

// CreateTask handles POST /projects/:projectId/tasks.
func (h *TaskHandler) CreateTask(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.CreateTaskRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	t, err := h.svc.CreateTask(c.Request.Context(), taskdom.CreateTaskInput{
		ProjectID:     projectID,
		TaskTypeID:    req.TaskTypeID,
		StatusID:      req.StatusID,
		SprintID:      req.SprintID,
		ParentTaskID:  req.ParentTaskID,
		Title:         req.Title,
		Description:   req.Description,
		Importance:    req.Importance,
		BoardPosition: req.BoardPosition,
		AssigneeID:    req.AssigneeID,
		ReporterID:    req.ReporterID,
		CustomFields:  req.CustomFields,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.Created(c, dto.TaskFromEntity(t))
}

// UpdateTask handles PATCH /projects/:projectId/tasks/:taskId.
func (h *TaskHandler) UpdateTask(c *gin.Context) {
	taskID, err := parseTaskID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	var req dto.UpdateTaskRequest
	if !middleware.BindJSON(c, &req) {
		return
	}

	t, err := h.svc.UpdateTask(c.Request.Context(), taskID, taskdom.UpdateTaskInput{
		TaskTypeID:    req.TaskTypeID,
		StatusID:      req.StatusID,
		SprintID:      req.SprintID,
		ParentTaskID:  req.ParentTaskID,
		Title:         req.Title,
		Description:   req.Description,
		Importance:    req.Importance,
		BoardPosition: req.BoardPosition,
		AssigneeID:    req.AssigneeID,
		ReporterID:    req.ReporterID,
		CustomFields:  req.CustomFields,
	})
	if err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, dto.TaskFromEntity(t))
}

// DeleteTask handles DELETE /projects/:projectId/tasks/:taskId.
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	taskID, err := parseTaskID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	if err := h.svc.DeleteTask(c.Request.Context(), taskID); err != nil {
		presenter.Error(c, err)
		return
	}
	presenter.OK(c, gin.H{"message": "task deleted"})
}

// --- helpers ----------------------------------------------------------------

func parseTaskTypeID(c *gin.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("typeId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid task type id")
	}
	return id, nil
}

func parseTaskStatusID(c *gin.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("statusId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid task status id")
	}
	return id, nil
}

func parseTaskID(c *gin.Context) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param("taskId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid task id")
	}
	return id, nil
}

// GetSprintTasks handles GET /projects/:projectId/sprints/:sprintId/tasks.
// Returns a paginated list of tasks assigned to a specific sprint.
func (h *TaskHandler) GetSprintTasks(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}
	sprintID, err := uuid.Parse(c.Param("sprintId"))
	if err != nil {
		presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid sprint id"))
		return
	}

	page, pageSize := pagingParams(c)
	filter := taskdom.TaskFilter{SprintID: &sprintID}
	if raw := c.Query("status_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.StatusID = &id
		}
	}
	if raw := c.Query("assignee_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.AssigneeID = &id
		}
	}

	tasks, total, err := h.svc.ListTasks(c.Request.Context(), projectID, filter, page, pageSize)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	resp := make([]dto.TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		resp = append(resp, dto.TaskFromEntity(t))
	}
	presenter.OK(c, gin.H{"items": resp, "total": total, "page": page, "page_size": pageSize})
}

// ListBacklogTasks handles GET /projects/:projectId/product-backlog.
// Returns a paginated list of tasks not assigned to any sprint (sprint_id IS NULL).
// This represents the product backlog — work that has been identified but not yet
// committed to a sprint, distinct from any sprint's own task list.
func (h *TaskHandler) ListBacklogTasks(c *gin.Context) {
	projectID, err := parseProjectID(c)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	page, pageSize := pagingParams(c)
	filter := taskdom.TaskFilter{BacklogOnly: true}
	if raw := c.Query("status_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.StatusID = &id
		}
	}
	if raw := c.Query("assignee_id"); raw != "" {
		if id, err := uuid.Parse(raw); err == nil {
			filter.AssigneeID = &id
		}
	}

	tasks, total, err := h.svc.ListTasks(c.Request.Context(), projectID, filter, page, pageSize)
	if err != nil {
		presenter.Error(c, err)
		return
	}

	resp := make([]dto.TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		resp = append(resp, dto.TaskFromEntity(t))
	}
	presenter.OK(c, gin.H{"items": resp, "total": total, "page": page, "page_size": pageSize})
}
