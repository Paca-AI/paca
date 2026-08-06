package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/Paca-AI/api/internal/apierr"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// taskServiceForStats is the minimal task-service surface used to compute
// workspace-level open-task counts in GetWorkspaceStats.
type taskServiceForStats interface {
	CountOpenTasksByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error)
}

// userServiceForStats is the minimal user-service surface used to compute
// the workspace-level team-member count in GetWorkspaceStats. It's a plain
// users-table count, not scoped to accessible projects — see
// userdom.Service.CountUsers.
type userServiceForStats interface {
	CountUsers(ctx context.Context) (int64, error)
}

// ProjectHandler handles project management endpoints.
type ProjectHandler struct {
	svc         projectdom.Service
	authorizer  *authz.Authorizer
	viewSvc     sprintdom.ViewService
	taskTypeSvc taskTypeLister
	taskSvc     taskServiceForStats
	userSvc     userServiceForStats
}

// ProjectHandlerOption customizes optional project-handler dependencies.
type ProjectHandlerOption func(*ProjectHandler)

// WithProjectDefaultViews enables API-side seeding of default backlog and timeline views.
func WithProjectDefaultViews(viewSvc sprintdom.ViewService, taskTypeSvc taskTypeLister) ProjectHandlerOption {
	return func(h *ProjectHandler) {
		h.viewSvc = viewSvc
		h.taskTypeSvc = taskTypeSvc
	}
}

// WithProjectStatsServices wires the task and user services used by the
// workspace stats endpoint's open-task and team-member counters. Passing nil
// for either is allowed and causes that counter to return zero. The AI-agent
// count doesn't need a separate service — it's computed via h.svc
// (projectdom.Service), which GetWorkspaceStats always has.
func WithProjectStatsServices(taskSvc taskServiceForStats, userSvc userServiceForStats) ProjectHandlerOption {
	return func(h *ProjectHandler) {
		h.taskSvc = taskSvc
		h.userSvc = userSvc
	}
}

// NewProjectHandler returns a ProjectHandler wired to the service and authorizer.
func NewProjectHandler(svc projectdom.Service, authorizer *authz.Authorizer, opts ...ProjectHandlerOption) *ProjectHandler {
	h := &ProjectHandler{svc: svc, authorizer: authorizer}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

// ListProjects handles GET /projects.
// Users with the global projects.read permission receive all projects.
// All other authenticated users receive only the projects they are a member of.
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	page, pageSize, err := pagingParams(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var (
		projects []*projectdom.Project
		total    int64
	)

	userID, parseErr := uuid.Parse(claims.Subject)
	if parseErr != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	hasGlobalRead, authzErr := h.authorizer.HasPermissions(
		r.Context(), userID, nil, claims.Role, authz.PermissionProjectsRead,
	)
	if authzErr != nil {
		presenter.Error(w, r, authzErr)
		return
	}

	if hasGlobalRead {
		projects, total, err = h.svc.List(r.Context(), page, pageSize)
	} else {
		projects, total, err = h.svc.ListAccessible(r.Context(), userID, page, pageSize)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	resp := make([]dto.ProjectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, dto.ProjectFromEntity(p))
	}
	presenter.OK(w, r, map[string]any{"items": resp, "total": total, "page": page, "page_size": pageSize})
}

// GetWorkspaceStats handles GET /projects/workspace-stats.
// It returns workspace-wide aggregate counts for the authenticated user:
// open tasks, unique team members, and AI agents across all accessible projects.
func (h *ProjectHandler) GetWorkspaceStats(w http.ResponseWriter, r *http.Request) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	userID, parseErr := uuid.Parse(claims.Subject)
	if parseErr != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return
	}

	hasGlobalRead, authzErr := h.authorizer.HasPermissions(
		r.Context(), userID, nil, claims.Role, authz.PermissionProjectsRead,
	)
	if authzErr != nil {
		presenter.Error(w, r, authzErr)
		return
	}

	var (
		projects []*projectdom.Project
		err      error
	)
	if hasGlobalRead {
		projects, _, err = h.svc.List(r.Context(), 1, 10000)
	} else {
		projects, _, err = h.svc.ListAccessible(r.Context(), userID, 1, 10000)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var stats dto.WorkspaceStatsResponse

	projectIDs := make([]uuid.UUID, len(projects))
	for i, p := range projects {
		projectIDs[i] = p.ID
	}

	// Each counter is a single aggregate query (not a per-project fan-out):
	// CountOpenTasksByProjects and CountDistinctAgentsByProjects each
	// dedupe/aggregate in SQL, so a task or invited global agent belonging
	// to several projects is counted once, not once per project. Both
	// tolerate an empty projectIDs slice, returning zero rather than
	// erroring. TeamMemberCount is a plain users-table count via
	// h.userSvc.CountUsers — team membership isn't scoped to accessible
	// projects, so it doesn't need projectIDs at all. All three run
	// concurrently since they're independent round trips; each writes only
	// its own disjoint stats field(s), so no mutex is needed — errgroup.Wait
	// establishes the happens-before edge before stats is read below.
	g, gctx := errgroup.WithContext(r.Context())

	if h.taskSvc != nil {
		g.Go(func() error {
			count, err := h.taskSvc.CountOpenTasksByProjects(gctx, projectIDs)
			if err != nil {
				return err
			}
			stats.OpenTaskCount = count
			return nil
		})
	}

	if h.userSvc != nil {
		g.Go(func() error {
			count, err := h.userSvc.CountUsers(gctx)
			if err != nil {
				return err
			}
			stats.TeamMemberCount = count
			return nil
		})
	}

	g.Go(func() error {
		count, err := h.svc.CountDistinctAgentsByProjects(gctx, projectIDs)
		if err != nil {
			return err
		}
		stats.AIAgentCount = count
		return nil
	})

	if err := g.Wait(); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, stats)
}

// GetProject handles GET /projects/:projectId.
func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	p, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProjectFromEntity(p))
}

// CreateProject handles POST /projects.
func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateProjectRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}

	var createdBy *uuid.UUID
	if actorUserID, ok := middleware.AgentActorUserIDFromRequest(r); ok {
		// A global agent's MCP tool call acting on behalf of a specific human
		// (e.g. "create a project for me" from the home-page chat) — attribute
		// the project to that human, not the shared agent-bot identity that
		// claims.Subject would otherwise resolve to. See parseActorUserIDHeader.
		createdBy = &actorUserID
	} else if claims := middleware.ClaimsFrom(r); claims != nil {
		if uid, err := uuid.Parse(claims.Subject); err == nil {
			createdBy = &uid
		}
	}

	p, err := h.svc.Create(r.Context(), projectdom.CreateProjectInput{
		Name:         req.Name,
		Description:  req.Description,
		TaskIDPrefix: req.TaskIDPrefix,
		IsPublic:     req.IsPublic,
		Settings:     req.Settings,
		CreatedBy:    createdBy,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	if h.viewSvc != nil {
		taskTypes, _ := loadTaskTypes(r.Context(), h.taskTypeSvc, p.ID)
		for _, input := range defaultProjectViewInputs(p.ID, taskTypes) {
			_, _ = h.viewSvc.CreateView(r.Context(), input)
		}
	}

	presenter.Created(w, r, dto.ProjectFromEntity(p))
}

// UpdateProject handles PATCH /projects/:projectId.
func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateProjectRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	p, err := h.svc.Update(r.Context(), id, projectdom.UpdateProjectInput{
		Name:         req.Name,
		Description:  req.Description,
		TaskIDPrefix: req.TaskIDPrefix,
		IsPublic:     req.IsPublic,
		Settings:     req.Settings,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProjectFromEntity(p))
}

// DeleteProject handles DELETE /projects/:projectId.
func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"message": "project deleted"})
}

// --- helpers ----------------------------------------------------------------

func parseProjectID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid project id")
	}
	return id, nil
}

func pagingParams(r *http.Request) (page, pageSize int, err error) {
	page, err = parsePage(r)
	if err != nil {
		return 0, 0, err
	}
	pageSize, err = parsePageSize(r, 20, 100)
	return page, pageSize, err
}
