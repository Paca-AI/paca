package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// ViewHandler handles sprint-view and task-position endpoints.
type ViewHandler struct {
	svc sprintdom.ViewService
}

// NewViewHandler returns a ViewHandler wired to the view service.
func NewViewHandler(svc sprintdom.ViewService) *ViewHandler {
	return &ViewHandler{svc: svc}
}

// viewContextFromQuery reads the ?context query param (sprint | backlog | timeline).
// Returns an error for unknown values. Defaults to "sprint" when omitted.
func viewContextFromQuery(r *http.Request) (sprintdom.ViewContext, error) {
	raw := defaultQuery(r, "context", string(sprintdom.ViewContextSprint))
	vc := sprintdom.ViewContext(raw)
	switch vc {
	case sprintdom.ViewContextSprint, sprintdom.ViewContextBacklog, sprintdom.ViewContextTimeline:
		return vc, nil
	default:
		return "", apierr.New(apierr.CodeBadRequest, "invalid context: must be sprint, backlog, or timeline")
	}
}

// parseQueryUUID reads a named UUID from the request query string.
func parseQueryUUID(r *http.Request, param string) (uuid.UUID, error) {
	raw := r.URL.Query().Get(param)
	if raw == "" {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, param+" is required")
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid "+param)
	}
	return id, nil
}

// ListViews handles GET /projects/:projectId/views?context=sprint|backlog|timeline.
// Sprint context additionally requires ?sprint_id=<uuid>.
func (h *ViewHandler) ListViews(w http.ResponseWriter, r *http.Request) {
	viewCtx, err := viewContextFromQuery(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var views []*sprintdom.SprintView
	if viewCtx == sprintdom.ViewContextSprint {
		var sprintID uuid.UUID
		sprintID, err = parseQueryUUID(r, "sprint_id")
		if err != nil {
			presenter.Error(w, r, err)
			return
		}
		views, err = h.svc.ListViews(r.Context(), projectID, sprintID)
	} else {
		views, err = h.svc.ListProjectViews(r.Context(), projectID, viewCtx)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	// Overlay the current user's personal config so settings/filters stay
	// per-user. Runs after the (shared) cache read; unauthenticated callers
	// (public projects) fall through to the shared defaults.
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		if err := h.svc.OverlayUserConfigs(r.Context(), actorID, views); err != nil {
			presenter.Error(w, r, err)
			return
		}
	}
	resp := make([]dto.ViewResponse, 0, len(views))
	for _, v := range views {
		resp = append(resp, dto.ViewFromEntity(v))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// GetView handles GET /projects/:projectId/views/:viewId.
func (h *ViewHandler) GetView(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	viewID, err := parseViewID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	v, err := h.svc.GetView(r.Context(), projectID, viewID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		if err := h.svc.OverlayUserConfigs(r.Context(), actorID, []*sprintdom.SprintView{v}); err != nil {
			presenter.Error(w, r, err)
			return
		}
	}
	presenter.OK(w, r, dto.ViewFromEntity(v))
}

// CreateView handles POST /projects/:projectId/views?context=sprint|backlog|timeline.
// Sprint context additionally requires ?sprint_id=<uuid>.
func (h *ViewHandler) CreateView(w http.ResponseWriter, r *http.Request) {
	viewCtx, err := viewContextFromQuery(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateViewRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}

	var input sprintdom.CreateViewInput
	if viewCtx == sprintdom.ViewContextSprint {
		sprintID, err := parseQueryUUID(r, "sprint_id")
		if err != nil {
			presenter.Error(w, r, err)
			return
		}
		input = req.ToCreateInput(sprintID, projectID)
	} else {
		input = req.ToCreateProjectViewInput(projectID, viewCtx)
	}

	v, err := h.svc.CreateView(r.Context(), input)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.ViewFromEntity(v))
}

// UpdateView handles PATCH /projects/:projectId/views/:viewId.
func (h *ViewHandler) UpdateView(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	viewID, err := parseViewID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateViewRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	v, err := h.svc.UpdateView(r.Context(), projectID, viewID, req.ToUpdateInput())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ViewFromEntity(v))
}

// UpdateMyViewConfig handles PUT /projects/:projectId/views/:viewId/config.
// It stores the authenticated user's personal view config (settings and
// filters) without mutating the shared view, so the change never leaks to
// other project members.
func (h *ViewHandler) UpdateMyViewConfig(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	viewID, err := parseViewID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	actorID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok || actorID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "authentication required"))
		return
	}

	var req dto.UpdateUserViewConfigRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	// A PUT replaces the personal config, so require it explicitly: an omitted
	// or null `config` would otherwise silently wipe the user's saved settings.
	if req.Config == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "config is required"))
		return
	}

	v, err := h.svc.SetUserViewConfig(r.Context(), projectID, viewID, actorID, req.ToViewConfig())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ViewFromEntity(v))
}

// DeleteView handles DELETE /sprints/:sprintId/views/:viewId.
func (h *ViewHandler) DeleteView(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	viewID, err := parseViewID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteView(r.Context(), projectID, viewID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// ListTaskPositions handles GET /projects/:projectId/views/:viewId/task-positions.
func (h *ViewHandler) ListTaskPositions(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	viewID, err := parseViewID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	positions, err := h.svc.ListTaskPositions(r.Context(), projectID, viewID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.TaskPositionResponse, 0, len(positions))
	for _, p := range positions {
		resp = append(resp, dto.TaskPositionFromEntity(p))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// MoveTask handles PUT /views/:viewId/task-positions/:taskId.
func (h *ViewHandler) MoveTask(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	viewID, err := parseViewID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskIDParam(r, "taskId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.MoveTaskRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	if err := h.svc.MoveTask(r.Context(), projectID, viewID, sprintdom.MoveTaskInput{
		TaskID:   taskID,
		Position: req.Position,
		GroupKey: req.GroupKey,
	}); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// BulkMoveTasks handles PUT /views/:viewId/task-positions.
// Upserts the manual positions of multiple tasks in a view within a single
// database transaction.
func (h *ViewHandler) BulkMoveTasks(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	viewID, err := parseViewID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.BulkMoveTasksRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.Items) == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "items must not be empty"))
		return
	}

	items := make([]sprintdom.MoveTaskInput, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, sprintdom.MoveTaskInput{
			TaskID:   item.TaskID,
			Position: item.Position,
			GroupKey: item.GroupKey,
		})
	}

	if err := h.svc.BulkMoveTasks(r.Context(), projectID, viewID, items); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// parseViewID extracts and validates the :viewId path parameter.
func parseViewID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "viewId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid view id")
	}
	return id, nil
}

// parseTaskIDParam extracts and validates a task UUID from the named path parameter.
func parseTaskIDParam(r *http.Request, param string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid task id")
	}
	return id, nil
}

// ReorderViews handles PUT /projects/:projectId/views/positions?context=sprint|backlog|timeline.
// Sprint context additionally requires ?sprint_id=<uuid>.
func (h *ViewHandler) ReorderViews(w http.ResponseWriter, r *http.Request) {
	viewCtx, err := viewContextFromQuery(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.ReorderViewsRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.ViewIDs) == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "view_ids must not be empty"))
		return
	}

	if viewCtx == sprintdom.ViewContextSprint {
		var sprintID uuid.UUID
		sprintID, err = parseQueryUUID(r, "sprint_id")
		if err != nil {
			presenter.Error(w, r, err)
			return
		}
		err = h.svc.ReorderViews(r.Context(), projectID, sprintID, req.ViewIDs)
	} else {
		err = h.svc.ReorderProjectViews(r.Context(), projectID, viewCtx, req.ViewIDs)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}
