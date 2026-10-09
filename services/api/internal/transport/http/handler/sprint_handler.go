package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// SprintHandler handles sprint management endpoints.
type SprintHandler struct {
	svc           sprintdom.SprintService
	viewSvc       sprintdom.ViewService
	taskTypeSvc   taskTypeLister
	taskStatusSvc taskStatusLister
	listScoper    ListScoper
}

// SprintHandlerOption customizes optional sprint-handler dependencies.
type SprintHandlerOption func(*SprintHandler)

// WithSprintListScoper limits the project's sprint list to the sprints the
// caller may read, inside the query (see scopedContext).
func WithSprintListScoper(s ListScoper) SprintHandlerOption {
	return func(h *SprintHandler) { h.listScoper = s }
}

// WithSprintDefaultTaskTypes enables sprint default-view seeding with explicit task-type filters.
func WithSprintDefaultTaskTypes(taskTypeSvc taskTypeLister) SprintHandlerOption {
	return func(h *SprintHandler) {
		h.taskTypeSvc = taskTypeSvc
	}
}

// WithSprintDefaultTaskStatuses enables sprint default-view seeding with backlog status collapsed columns.
func WithSprintDefaultTaskStatuses(taskStatusSvc taskStatusLister) SprintHandlerOption {
	return func(h *SprintHandler) {
		h.taskStatusSvc = taskStatusSvc
	}
}

// NewSprintHandler returns a SprintHandler wired to the sprint and view services.
func NewSprintHandler(svc sprintdom.SprintService, viewSvc sprintdom.ViewService, opts ...SprintHandlerOption) *SprintHandler {
	h := &SprintHandler{svc: svc, viewSvc: viewSvc}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

// ListSprints handles GET /projects/:projectId/sprints.
func (h *SprintHandler) ListSprints(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	ctx, err := scopedContext(r, h.listScoper, iam.ActionSprintsRead, projectID, "sprint")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sprints, err := h.svc.ListSprints(ctx, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.SprintResponse, 0, len(sprints))
	for _, s := range sprints {
		resp = append(resp, dto.SprintFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// CreateSprint handles POST /projects/:projectId/sprints.
func (h *SprintHandler) CreateSprint(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateSprintRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}

	s, err := h.svc.CreateSprint(r.Context(), sprintdom.CreateSprintInput{
		ProjectID: projectID,
		Name:      req.Name,
		StartDate: req.StartDate,
		EndDate:   req.EndDate,
		Goal:      req.Goal,
		Status:    req.Status,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Seed default views for every new sprint.
	ctx := r.Context()
	taskTypes, _ := loadTaskTypes(ctx, h.taskTypeSvc, s.ProjectID)
	statuses, _ := loadTaskStatuses(ctx, h.taskStatusSvc, s.ProjectID)
	for _, input := range defaultSprintViewInputs(s.ProjectID, s.ID, taskTypes, statuses) {
		_, _ = h.viewSvc.CreateView(ctx, input)
	}

	presenter.Created(w, r, dto.SprintFromEntity(s))
}

// UpdateSprint handles PATCH /projects/:projectId/sprints/:sprintId.
func (h *SprintHandler) UpdateSprint(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sprintID, err := parseSprintID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateSprintRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	s, err := h.svc.UpdateSprint(r.Context(), projectID, sprintID, sprintdom.UpdateSprintInput{
		Name:      req.Name,
		StartDate: req.StartDate.Ptr(),
		EndDate:   req.EndDate.Ptr(),
		Goal:      req.Goal.Ptr(),
		Status:    req.Status,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.SprintFromEntity(s))
}

// DeleteSprint handles DELETE /projects/:projectId/sprints/:sprintId.
func (h *SprintHandler) DeleteSprint(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sprintID, err := parseSprintID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteSprint(r.Context(), projectID, sprintID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "sprint deleted"})
}

// CompleteSprint handles POST /projects/:projectId/sprints/:sprintId/complete.
// It bulk-moves all non-done tasks to the requested destination sprint (or the
// backlog when move_to_sprint_id is absent/null) and marks the sprint completed.
func (h *SprintHandler) CompleteSprint(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sprintID, err := parseSprintID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CompleteSprintRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	s, err := h.svc.CompleteSprint(r.Context(), projectID, sprintID, sprintdom.CompleteSprintInput{
		MoveToSprintID: req.MoveToSprintID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.SprintFromEntity(s))
}

// parseSprintID extracts and validates the :sprintId path parameter.
func parseSprintID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "sprintId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid sprint id")
	}
	return id, nil
}

// GetSprint handles GET /projects/:projectId/sprints/:sprintId.
func (h *SprintHandler) GetSprint(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	sprintID, err := parseSprintID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	s, err := h.svc.GetSprint(r.Context(), projectID, sprintID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.SprintFromEntity(s))
}
