package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/platform/jev"
	"github.com/Paca-AI/api/internal/platform/netguard"
	"github.com/Paca-AI/api/internal/platform/secret"
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

// workspaceTrendSource rebuilds the workspace counters as daily series for
// GetWorkspaceTrend (see postgres.WorkspaceTrendRepository). Each method
// returns one value per day end from firstDayEnd to lastDayEnd inclusive.
type workspaceTrendSource interface {
	ProjectCountTrend(ctx context.Context, projectIDs []uuid.UUID, firstDayEnd, lastDayEnd time.Time) ([]int64, error)
	OpenTaskTrend(ctx context.Context, projectIDs []uuid.UUID, firstDayEnd, lastDayEnd time.Time) ([]int64, error)
	UserCountTrend(ctx context.Context, firstDayEnd, lastDayEnd time.Time) ([]int64, error)
}

// projectJevConfigService is the minimal surface used to update a
// project's Jev credentials — deliberately not part of projectdom.Service
// (see WithProjectJevConfigService's doc comment for why).
type projectJevConfigService interface {
	UpdateJevConfig(ctx context.Context, projectID uuid.UUID, apiKey, baseURL, model *string) (*projectdom.Project, error)
}

// ProjectsScoper is the part of *iam.Authorizer a list spanning projects uses
// to learn, per project, which resources of a kind the caller may act on (see
// ListScoper, which answers for one project).
type ProjectsScoper interface {
	ListScopes(ctx context.Context, p iam.Principal, action string, projectIDs []string, kind string) (map[string]*iam.Node, error)
}

// projectAuthorizer is what ProjectHandler asks the IAM engine: single
// decisions and a caller's effective actions. Satisfied by *iam.Authorizer.
type projectAuthorizer interface {
	iam.Checker
	EffectiveActions(ctx context.Context, p iam.Principal, projectID string) ([]string, error)
}

// ProjectHandler handles project management endpoints.
type ProjectHandler struct {
	svc          projectdom.Service
	authorizer   projectAuthorizer // nil = deny-all
	viewSvc      sprintdom.ViewService
	taskTypeSvc  taskTypeLister
	taskSvc      taskServiceForStats
	userSvc      userServiceForStats
	avatarSvc    attachmentdom.AvatarService
	jevConfigSvc projectJevConfigService
	// taskScoper limits the workspace open-task count to the tasks the caller
	// may read in each project. Nil leaves it unscoped.
	taskScoper ProjectsScoper
	// trend backs GetWorkspaceTrend. Nil makes it return empty series.
	trend workspaceTrendSource
	// now is the clock GetWorkspaceTrend's day boundaries are taken from;
	// time.Now unless a test replaces it.
	now       func() time.Time
	encryptor *secret.Encryptor
	// jevHTTPClient is the transport TestJevConfig's Jev client uses. Nil
	// means "use jev.New's own SSRF-safe default" — see
	// WithProjectJevHTTPClient.
	jevHTTPClient *http.Client
	// jevPrivateHosts is the operator's JEV_PRIVATE_HOSTS set — the hostnames
	// the Jev client may reach on a private network address. Nil keeps the
	// guard strict — see WithProjectJevPrivateHosts.
	jevPrivateHosts *netguard.PrivateHosts
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

// WithProjectTaskScoper limits the workspace stats' open-task count to the
// tasks the caller may read, per project, inside the count query (see
// iam.WithProjectScopes).
func WithProjectTaskScoper(s ProjectsScoper) ProjectHandlerOption {
	return func(h *ProjectHandler) { h.taskScoper = s }
}

// WithProjectWorkspaceTrend wires the source of the daily series returned by
// GET /projects/workspace-trend.
func WithProjectWorkspaceTrend(src workspaceTrendSource) ProjectHandlerOption {
	return func(h *ProjectHandler) { h.trend = src }
}

// WithProjectAvatarService configures avatar URL resolution for member
// responses (ListMembers, AddMember, UpdateMemberRole) and for the
// project's own avatar (ProjectResponse), and enables the project avatar
// upload endpoints.
func WithProjectAvatarService(svc attachmentdom.AvatarService) ProjectHandlerOption {
	return func(h *ProjectHandler) {
		h.avatarSvc = svc
	}
}

// WithProjectJevHTTPClient overrides the transport TestJevConfig's Jev
// client uses. Defaults to jev.New's own SSRF-safe client — override only for
// tests that need to reach a local httptest.Server, which the default would
// otherwise reject as a private address (see netguard). Mirrors
// AutomationConsumer.WithHTTPClient.
func WithProjectJevHTTPClient(client *http.Client) ProjectHandlerOption {
	return func(h *ProjectHandler) {
		h.jevHTTPClient = client
	}
}

// WithProjectJevPrivateHosts lists the hostnames the Jev client may reach on
// a private network address (JEV_PRIVATE_HOSTS) — for an instance that hosts
// its own Jev-compatible gateway on an internal address. Nil (the default)
// keeps the guard strict: every private address stays unreachable.
func WithProjectJevPrivateHosts(hosts *netguard.PrivateHosts) ProjectHandlerOption {
	return func(h *ProjectHandler) {
		h.jevPrivateHosts = hosts
	}
}

// WithProjectJevConfigService wires the update path for a project's Jev
// credentials (UpdateJevConfig, the encrypt-at-write handler). Passed
// separately from svc (projectdom.Service) — bootstrap wires this straight
// to the cached wrapper, which delegates to the concrete *projectsvc.Service
// while also dropping the project's cache entry (see its own UpdateJevConfig:
// without that, save-then-test would exercise the credentials just replaced)
// rather than rippling
// UpdateJevConfig through the projectdom.Service interface and every mock
// that implements it (see the many-file cost of doing that for
// UpdateMemberDescription, an earlier, narrower precedent for this same
// project handler). Reads still go through the normal svc.GetByID path —
// Project already carries JevConfigured/JevBaseURL/JevModel as plain
// fields, no new read capability needed.
//
// enc decrypts the stored API key secret for TestJevConfig (see
// jev.ClientForProject) — may be nil, matching every other at-rest secret
// in this codebase when ENCRYPTION_KEY is unset.
func WithProjectJevConfigService(svc projectJevConfigService, enc *secret.Encryptor) ProjectHandlerOption {
	return func(h *ProjectHandler) {
		h.jevConfigSvc = svc
		h.encryptor = enc
	}
}

// NewProjectHandler returns a ProjectHandler wired to the service and authorizer.
func NewProjectHandler(svc projectdom.Service, authorizer *iam.Authorizer, opts ...ProjectHandlerOption) *ProjectHandler {
	h := &ProjectHandler{svc: svc, now: time.Now}
	if authorizer != nil { // never store a typed nil in the interface
		h.authorizer = authorizer
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

// toProjectResponse maps p to a ProjectResponse and, if an AvatarService is
// configured, resolves its avatar keys into presigned display URLs.
func (h *ProjectHandler) toProjectResponse(ctx context.Context, p *projectdom.Project) dto.ProjectResponse {
	resp := dto.ProjectFromEntity(p)
	if h.avatarSvc != nil {
		resp.AvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, p.AvatarKey)
		resp.AvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, p.AvatarThumbKey)
	}
	return resp
}

// ListProjects handles GET /projects.
// Users with the global projects.read permission receive all projects.
// All other authenticated users receive only the projects they are a member of.
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := pagingParams(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var (
		projects []*projectdom.Project
		total    int64
	)

	accountID, ok := requireAccountID(w, r)
	if !ok {
		return
	}

	hasGlobalRead, authzErr := h.hasPlatformProjectsRead(r)
	if authzErr != nil {
		presenter.Error(w, r, authzErr)
		return
	}

	if hasGlobalRead {
		projects, total, err = h.svc.List(r.Context(), page, pageSize)
	} else {
		projects, total, err = h.svc.ListAccessible(r.Context(), accountID, page, pageSize)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	resp := make([]dto.ProjectResponse, 0, len(projects))
	for _, p := range projects {
		resp = append(resp, h.toProjectResponse(r.Context(), p))
	}
	presenter.OK(w, r, map[string]any{"items": resp, "total": total, "page": page, "page_size": pageSize})
}

// hasPlatformProjectsRead reports whether the caller may read every project:
// projects:read on the project collection "project" (the platform-wide
// projects.read). Judged as the token's subject, as before.
func (h *ProjectHandler) hasPlatformProjectsRead(r *http.Request) (bool, error) {
	if h.authorizer == nil {
		return false, nil
	}
	principal, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		return false, err
	}
	return iam.AllowedAll(r.Context(), h.authorizer, principal, "project", iam.ActionProjectsRead)
}

// workspaceProjectIDs returns the projects the workspace counters cover:
// every project for a caller with platform-wide projects:read, otherwise the
// ones they can access.
func (h *ProjectHandler) workspaceProjectIDs(r *http.Request, accountID uuid.UUID) ([]uuid.UUID, error) {
	hasGlobalRead, err := h.hasPlatformProjectsRead(r)
	if err != nil {
		return nil, err
	}
	var projects []*projectdom.Project
	if hasGlobalRead {
		projects, _, err = h.svc.List(r.Context(), 1, 10000)
	} else {
		projects, _, err = h.svc.ListAccessible(r.Context(), accountID, 1, 10000)
	}
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(projects))
	for i, p := range projects {
		ids[i] = p.ID
	}
	return ids, nil
}

// workspaceTaskScope attaches to ctx the scope of the tasks the caller may
// read in each of projectIDs, so a task query spanning them counts only those;
// a project they may read no task in contributes nothing. ctx is returned
// unchanged when no scoper is wired.
func (h *ProjectHandler) workspaceTaskScope(ctx context.Context, r *http.Request, projectIDs []uuid.UUID) (context.Context, error) {
	if h.taskScoper == nil {
		return ctx, nil
	}
	principal, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(projectIDs))
	for i, id := range projectIDs {
		ids[i] = id.String()
	}
	byProject, err := h.taskScoper.ListScopes(r.Context(), principal, string(iam.ActionTasksRead), ids, "task")
	if err != nil {
		return nil, err
	}
	return iam.WithProjectScopes(ctx, "task", byProject), nil
}

// GetWorkspaceStats handles GET /projects/workspace-stats.
// It returns workspace-wide aggregate counts for the authenticated user:
// open tasks and AI agents across all accessible projects, plus
// TeamMemberCount — unlike the other two counters, this one is a plain
// platform-wide user count (see userdom.Service.CountUsers), not scoped to
// the caller's accessible projects.
func (h *ProjectHandler) GetWorkspaceStats(w http.ResponseWriter, r *http.Request) {
	accountID, ok := requireAccountID(w, r)
	if !ok {
		return
	}

	projectIDs, err := h.workspaceProjectIDs(r, accountID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var stats dto.WorkspaceStatsResponse

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
		countCtx, serr := h.workspaceTaskScope(gctx, r, projectIDs)
		if serr != nil {
			presenter.Error(w, r, serr)
			return
		}
		g.Go(func() error {
			count, err := h.taskSvc.CountOpenTasksByProjects(countCtx, projectIDs)
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
	presenter.OK(w, r, h.toProjectResponse(r.Context(), p))
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

	// The caller is the creator, whatever kind of account it is: an agent that
	// creates a project does so with its own permissions and owns the result.
	var createdBy *uuid.UUID
	if p, ok := middleware.PrincipalFrom(r); ok {
		createdBy = &p.AccountID
	}

	p, err := h.svc.Create(r.Context(), projectdom.CreateProjectInput{
		Name:         req.Name,
		Description:  req.Description,
		TaskIDPrefix: req.TaskIDPrefix,
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

	presenter.Created(w, r, h.toProjectResponse(r.Context(), p))
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
		Settings:     req.Settings,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toProjectResponse(r.Context(), p))
}

// UpdateJevConfig handles PATCH /projects/:projectId/jev-config — sets this
// project's own Jev (AI decision API) credentials. There is no instance-
// wide Jev config; every project brings its own key and, optionally, its
// own Jev-compatible provider (base_url/model — see platform/jev.New's
// doc comment on third-party providers like OpenJev).
func (h *ProjectHandler) UpdateJevConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if h.jevConfigSvc == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "jev config is not available"))
		return
	}

	var req dto.UpdateProjectJevConfigRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	p, err := h.jevConfigSvc.UpdateJevConfig(r.Context(), id, req.APIKey, req.BaseURL, req.Model)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProjectJevConfigResponse{
		Configured: p.JevConfigured(),
		BaseURL:    p.JevBaseURL,
		Model:      p.JevModel,
	})
}

// TestJevConfig handles POST /projects/:projectId/jev-config/test — sends a
// minimal, throwaway question to this project's currently-stored Jev
// credentials (whatever was last saved via UpdateJevConfig, not whatever the
// caller may have typed but not yet saved) and reports whether Jev answered
// successfully, so a user can verify their key/host/model actually work
// before relying on them for autofill/auto-assign.
func (h *ProjectHandler) TestJevConfig(w http.ResponseWriter, r *http.Request) {
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

	client := jev.ClientForProject(p.JevAPIKeySecret, p.JevBaseURL, p.JevModel, h.encryptor, h.jevPrivateHosts)
	if h.jevHTTPClient != nil && client.Enabled() {
		client = client.WithHTTPClient(h.jevHTTPClient)
	}
	if !client.Enabled() {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "Jev is not configured for this project"))
		return
	}

	_, err = client.SystemOne(r.Context(), map[string]any{"ping": "test"}, map[string]jev.Question{
		"connectivity": {
			Type:         jev.TypeNoul,
			Instructions: "This is a connectivity test, not a real decision — answer true.",
		},
	})
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "Could not reach Jev with these credentials: "+err.Error()))
		return
	}

	presenter.OK(w, r, dto.TestJevConfigResponse{Success: true})
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

// InitiateAvatarUpload handles POST /projects/:projectId/avatar/initiate-upload.
func (h *ProjectHandler) InitiateAvatarUpload(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	uploaderID, ok := requireAccountID(w, r)
	if !ok {
		return
	}

	var req dto.InitiateUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	session, err := h.svc.InitiateAvatarUpload(r.Context(), id, req.FileName, req.ContentType, req.FileSize, uploaderID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.UploadSessionFromDomain(session))
}

// CompleteAvatarUpload handles POST /projects/:projectId/avatar/complete-upload.
func (h *ProjectHandler) CompleteAvatarUpload(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CompleteAvatarUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	p, err := h.svc.CompleteAvatarUpload(r.Context(), id, req.FileID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toProjectResponse(r.Context(), p))
}

// DeleteAvatar handles DELETE /projects/:projectId/avatar.
func (h *ProjectHandler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	p, err := h.svc.RemoveAvatar(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toProjectResponse(r.Context(), p))
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

const (
	workspaceTrendDefaultDays = 30
	workspaceTrendMinDays     = 7
	workspaceTrendMaxDays     = 90
)

// GetWorkspaceTrend handles GET /projects/workspace-trend?days=N. It returns
// the workspace counters of GetWorkspaceStats as daily series over the last
// N UTC days (default 30, 7 to 90), for the home page's stat-card charts.
// Projects and open tasks cover the same projects, and the same task scope,
// as GetWorkspaceStats, so each series ends at the live value; the open-task
// series is rebuilt from the activity log and is approximate further back
// (see postgres.WorkspaceTrendRepository.OpenTaskTrend). There is no
// AI-agent series: agent membership has no history to rebuild it from.
func (h *ProjectHandler) GetWorkspaceTrend(w http.ResponseWriter, r *http.Request) {
	accountID, ok := requireAccountID(w, r)
	if !ok {
		return
	}

	days := workspaceTrendDefaultDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < workspaceTrendMinDays || n > workspaceTrendMaxDays {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, fmt.Sprintf(
				"days must be an integer between %d and %d", workspaceTrendMinDays, workspaceTrendMaxDays)))
			return
		}
		days = n
	}

	// Day ends are UTC midnights; the last one is the coming midnight, so
	// today's point includes everything up to now.
	now := time.Now
	if h.now != nil {
		now = h.now
	}
	today := now().UTC().Truncate(24 * time.Hour)
	lastDayEnd := today.Add(24 * time.Hour)
	firstDayEnd := lastDayEnd.Add(-time.Duration(days-1) * 24 * time.Hour)

	resp := dto.WorkspaceTrendResponse{
		Days:        days,
		Dates:       make([]string, days),
		Projects:    []int64{},
		OpenTasks:   []int64{},
		TeamMembers: []int64{},
	}
	for i := range days {
		resp.Dates[i] = firstDayEnd.Add(time.Duration(i-1) * 24 * time.Hour).Format(time.DateOnly)
	}
	if h.trend == nil {
		presenter.OK(w, r, resp)
		return
	}

	projectIDs, err := h.workspaceProjectIDs(r, accountID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	g, gctx := errgroup.WithContext(r.Context())
	g.Go(func() error {
		series, err := h.trend.ProjectCountTrend(gctx, projectIDs, firstDayEnd, lastDayEnd)
		resp.Projects = series
		return err
	})
	g.Go(func() error {
		series, err := h.trend.UserCountTrend(gctx, firstDayEnd, lastDayEnd)
		resp.TeamMembers = series
		return err
	})
	taskCtx, err := h.workspaceTaskScope(gctx, r, projectIDs)
	if err != nil {
		_ = g.Wait()
		presenter.Error(w, r, err)
		return
	}
	g.Go(func() error {
		series, err := h.trend.OpenTaskTrend(taskCtx, projectIDs, firstDayEnd, lastDayEnd)
		resp.OpenTasks = series
		return err
	})
	if err := g.Wait(); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, resp)
}
