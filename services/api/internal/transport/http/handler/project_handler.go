package handler

import (
	"context"
	"net/http"
	"strconv"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// taskServiceForStats is the minimal task-service surface used to compute
// workspace-level open-task counts in GetWorkspaceStats.
type taskServiceForStats interface {
	ListTaskStatuses(ctx context.Context, projectID uuid.UUID) ([]*taskdom.TaskStatus, error)
	CountTasks(ctx context.Context, projectID uuid.UUID, filter taskdom.TaskFilter) (int64, error)
}

// agentServiceForStats is the minimal agent-service surface used to compute
// workspace-level AI-agent counts in GetWorkspaceStats.
type agentServiceForStats interface {
	ListAgents(ctx context.Context, projectID uuid.UUID) ([]*agentdom.Agent, error)
}

// ProjectHandler handles project management endpoints.
type ProjectHandler struct {
	svc         projectdom.Service
	authorizer  *authz.Authorizer
	viewSvc     sprintdom.ViewService
	taskTypeSvc taskTypeLister
	taskSvc     taskServiceForStats
	agentSvc    agentServiceForStats
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

// WithProjectStatsServices wires the task and agent services used by the
// workspace stats endpoint. Passing nil is allowed and causes the affected
// counters to return zero.
func WithProjectStatsServices(taskSvc taskServiceForStats, agentSvc agentServiceForStats) ProjectHandlerOption {
	return func(h *ProjectHandler) {
		h.taskSvc = taskSvc
		h.agentSvc = agentSvc
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
	if len(projects) == 0 {
		presenter.OK(w, r, stats)
		return
	}

	g, gctx := errgroup.WithContext(r.Context())
	var mu sync.Mutex
	seenMembers := make(map[uuid.UUID]struct{})

	for _, p := range projects {
		projectID := p.ID

		if h.taskSvc != nil {
			g.Go(func() error {
				statuses, listErr := h.taskSvc.ListTaskStatuses(gctx, projectID)
				if listErr != nil {
					return listErr
				}
				openIDs := make([]uuid.UUID, 0, len(statuses))
				for _, s := range statuses {
					if s.Category != taskdom.StatusCategoryDone {
						openIDs = append(openIDs, s.ID)
					}
				}
				if len(openIDs) == 0 {
					return nil
				}
				count, countErr := h.taskSvc.CountTasks(gctx, projectID, taskdom.TaskFilter{StatusIDs: openIDs})
				if countErr != nil {
					return countErr
				}
				mu.Lock()
				stats.OpenTaskCount += count
				mu.Unlock()
				return nil
			})
		}

		g.Go(func() error {
			members, listErr := h.svc.ListMembers(gctx, projectID)
			if listErr != nil {
				return listErr
			}
			mu.Lock()
			for _, m := range members {
				if m.UserID == uuid.Nil {
					continue
				}
				if _, ok := seenMembers[m.UserID]; !ok {
					seenMembers[m.UserID] = struct{}{}
					stats.TeamMemberCount++
				}
			}
			mu.Unlock()
			return nil
		})

		if h.agentSvc != nil {
			g.Go(func() error {
				agents, listErr := h.agentSvc.ListAgents(gctx, projectID)
				if listErr != nil {
					return listErr
				}
				mu.Lock()
				stats.AIAgentCount += int64(len(agents))
				mu.Unlock()
				return nil
			})
		}
	}

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

	claims := middleware.ClaimsFrom(r)
	var createdBy *uuid.UUID
	if claims != nil {
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
	page, _ = strconv.Atoi(defaultQuery(r, "page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, err = parsePageSize(r, 20, 100)
	return page, pageSize, err
}
