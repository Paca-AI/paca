package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// mock
// ---------------------------------------------------------------------------

type mockProjectSvc struct {
	list                    func(ctx context.Context, page, pageSize int) ([]*projectdom.Project, int64, error)
	listAccessible          func(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*projectdom.Project, int64, error)
	getByID                 func(ctx context.Context, id uuid.UUID) (*projectdom.Project, error)
	isProjectPublic         func(ctx context.Context, id uuid.UUID) (bool, error)
	create                  func(ctx context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error)
	update                  func(ctx context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error)
	delete                  func(ctx context.Context, id uuid.UUID) error
	listMembers             func(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error)
	addMember               func(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error)
	updateMember            func(ctx context.Context, projectID, userID uuid.UUID, in projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error)
	removeMember            func(ctx context.Context, projectID, userID uuid.UUID) error
	updateMemberByMemberID  func(ctx context.Context, projectID, memberID uuid.UUID, in projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error)
	removeMemberByMemberID  func(ctx context.Context, projectID, memberID uuid.UUID) error
	listRoles               func(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error)
	createRole              func(ctx context.Context, projectID uuid.UUID, in projectdom.CreateRoleInput) (*projectdom.ProjectRole, error)
	updateRole              func(ctx context.Context, projectID, roleID uuid.UUID, in projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error)
	deleteRole              func(ctx context.Context, projectID, roleID uuid.UUID) error
	getMyProjectPermissions func(ctx context.Context, projectID, userID uuid.UUID, agentID *uuid.UUID) (map[string]any, error)
}

func (m *mockProjectSvc) List(ctx context.Context, page, pageSize int) ([]*projectdom.Project, int64, error) {
	if m.list != nil {
		return m.list(ctx, page, pageSize)
	}
	return []*projectdom.Project{}, 0, nil
}

func (m *mockProjectSvc) ListAccessible(ctx context.Context, userID uuid.UUID, page, pageSize int) ([]*projectdom.Project, int64, error) {
	if m.listAccessible != nil {
		return m.listAccessible(ctx, userID, page, pageSize)
	}
	return []*projectdom.Project{}, 0, nil
}

func (m *mockProjectSvc) GetByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error) {
	if m.getByID != nil {
		return m.getByID(ctx, id)
	}
	return nil, projectdom.ErrNotFound
}

func (m *mockProjectSvc) IsProjectPublic(ctx context.Context, id uuid.UUID) (bool, error) {
	if m.isProjectPublic != nil {
		return m.isProjectPublic(ctx, id)
	}
	return false, nil
}

func (m *mockProjectSvc) Create(ctx context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error) {
	if m.create != nil {
		return m.create(ctx, in)
	}
	return nil, errors.New("mock: create not configured")
}

func (m *mockProjectSvc) Update(ctx context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error) {
	if m.update != nil {
		return m.update(ctx, id, in)
	}
	return nil, projectdom.ErrNotFound
}

func (m *mockProjectSvc) Delete(ctx context.Context, id uuid.UUID) error {
	if m.delete != nil {
		return m.delete(ctx, id)
	}
	return nil
}

func (m *mockProjectSvc) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	if m.listMembers != nil {
		return m.listMembers(ctx, projectID)
	}
	return []*projectdom.ProjectMember{}, nil
}

func (m *mockProjectSvc) AddMember(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput, _ authz.PermissionSet) (*projectdom.ProjectMember, error) {
	if m.addMember != nil {
		return m.addMember(ctx, projectID, in)
	}
	return nil, errors.New("mock: addMember not configured")
}

func (m *mockProjectSvc) UpdateMemberRole(ctx context.Context, projectID, userID uuid.UUID, in projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
	if m.updateMember != nil {
		return m.updateMember(ctx, projectID, userID, in)
	}
	return nil, errors.New("mock: updateMember not configured")
}

func (m *mockProjectSvc) RemoveMember(ctx context.Context, projectID, userID uuid.UUID) error {
	if m.removeMember != nil {
		return m.removeMember(ctx, projectID, userID)
	}
	return nil
}

func (m *mockProjectSvc) ListRoles(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error) {
	if m.listRoles != nil {
		return m.listRoles(ctx, projectID)
	}
	return []*projectdom.ProjectRole{}, nil
}

func (m *mockProjectSvc) CreateRole(ctx context.Context, projectID uuid.UUID, in projectdom.CreateRoleInput) (*projectdom.ProjectRole, error) {
	if m.createRole != nil {
		return m.createRole(ctx, projectID, in)
	}
	return nil, errors.New("mock: createRole not configured")
}

func (m *mockProjectSvc) UpdateRole(ctx context.Context, projectID, roleID uuid.UUID, in projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error) {
	if m.updateRole != nil {
		return m.updateRole(ctx, projectID, roleID, in)
	}
	return nil, projectdom.ErrRoleNotFound
}

func (m *mockProjectSvc) DeleteRole(ctx context.Context, projectID, roleID uuid.UUID) error {
	if m.deleteRole != nil {
		return m.deleteRole(ctx, projectID, roleID)
	}
	return nil
}

func (m *mockProjectSvc) GetMyProjectPermissions(ctx context.Context, projectID, userID uuid.UUID, agentID *uuid.UUID) (map[string]any, error) {
	if m.getMyProjectPermissions != nil {
		return m.getMyProjectPermissions(ctx, projectID, userID, agentID)
	}
	return nil, nil
}

func (m *mockProjectSvc) AddAgentMember(_ context.Context, _, _, _, _ uuid.UUID) error { return nil }
func (m *mockProjectSvc) RemoveAgentMember(_ context.Context, _, _ uuid.UUID) error    { return nil }
func (m *mockProjectSvc) UpdateMemberRoleByMemberID(_ context.Context, projectID, memberID uuid.UUID, in projectdom.UpdateMemberRoleInput, _ authz.PermissionSet) (*projectdom.ProjectMember, error) {
	if m.updateMemberByMemberID != nil {
		return m.updateMemberByMemberID(context.Background(), projectID, memberID, in)
	}
	return nil, projectdom.ErrNotFound
}
func (m *mockProjectSvc) RemoveMemberByMemberID(_ context.Context, projectID, memberID uuid.UUID) error {
	if m.removeMemberByMemberID != nil {
		return m.removeMemberByMemberID(context.Background(), projectID, memberID)
	}
	return projectdom.ErrNotFound
}

// compile-time interface check
var _ projectdom.Service = (*mockProjectSvc)(nil)

// ---------------------------------------------------------------------------
// router helper
// ---------------------------------------------------------------------------

// adminClaimsMiddleware injects a synthetic ADMIN claims into the request context so
// unit tests can exercise the handler without a real JWT stack.
func adminClaimsMiddleware() func(http.Handler) http.Handler {
	claims := &domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: uuid.New().String(),
		},
		Role: "ADMIN",
		Kind: "access",
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), httpmw.ClaimsContextKey(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newProjectRouter(svc projectdom.Service) chi.Router {
	// Use a real Authorizer with nil store — legacy ADMIN role grants everything
	// without any database calls.
	authorizer := authz.NewAuthorizer(nil)
	r := chi.NewRouter()
	r.Use(adminClaimsMiddleware())
	h := handler.NewProjectHandler(svc, authorizer)
	// Admin project CRUD
	r.Get("/admin/projects", h.ListProjects)
	r.Post("/admin/projects", h.CreateProject)
	r.Get("/admin/projects/{projectId}", h.GetProject)
	r.Patch("/admin/projects/{projectId}", h.UpdateProject)
	r.Delete("/admin/projects/{projectId}", h.DeleteProject)
	// Project member routes
	r.Get("/projects/{projectId}/members", h.ListMembers)
	r.Post("/projects/{projectId}/members", h.AddMember)
	r.Patch("/projects/{projectId}/members/{memberId}", h.UpdateMemberRole)
	r.Delete("/projects/{projectId}/members/{memberId}", h.RemoveMember)
	// Project role routes
	r.Get("/projects/{projectId}/roles", h.ListRoles)
	r.Post("/projects/{projectId}/roles", h.CreateRole)
	r.Patch("/projects/{projectId}/roles/{roleId}", h.UpdateRole)
	r.Delete("/projects/{projectId}/roles/{roleId}", h.DeleteRole)
	return r
}

// ---------------------------------------------------------------------------
// Project CRUD
// ---------------------------------------------------------------------------

func TestListProjects_Success(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		list: func(_ context.Context, _, _ int) ([]*projectdom.Project, int64, error) {
			return []*projectdom.Project{
				{ID: projID, Name: "alpha", CreatedAt: time.Now()},
			}, 1, nil
		},
	})

	w := do(t, r, http.MethodGet, "/admin/projects", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListProjects_ServiceError(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{
		list: func(_ context.Context, _, _ int) ([]*projectdom.Project, int64, error) {
			return nil, 0, errors.New("db error")
		},
	})

	w := do(t, r, http.MethodGet, "/admin/projects", nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetProject_Success(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		getByID: func(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
			if id != projID {
				return nil, projectdom.ErrNotFound
			}
			return &projectdom.Project{ID: projID, Name: "alpha"}, nil
		},
	})

	w := do(t, r, http.MethodGet, fmt.Sprintf("/admin/projects/%s", projID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetProject_BadID(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodGet, "/admin/projects/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestGetProject_NotFound(t *testing.T) {
	id := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*projectdom.Project, error) {
			return nil, projectdom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodGet, fmt.Sprintf("/admin/projects/%s", id), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestCreateProject_Success(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		create: func(_ context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error) {
			return &projectdom.Project{ID: projID, Name: in.Name, Description: in.Description}, nil
		},
	})

	w := do(t, r, http.MethodPost, "/admin/projects",
		jsonBody(t, map[string]any{"name": "beta", "description": "a project"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProject_SeedsDefaultViews(t *testing.T) {
	projectID := uuid.New()
	viewSvc := &fakeViewSvcH{}
	taskTypeSvc := &fakeTaskTypeSvcH{taskTypes: []*taskdom.TaskType{
		{ID: uuid.New(), Name: "Task"},
		{ID: uuid.New(), Name: "Bug"},
		{ID: uuid.New(), Name: "Epic", IsSystem: true},
		{ID: uuid.New(), Name: "Subtask", IsSystem: true},
	}}
	projectSvc := &mockProjectSvc{
		create: func(_ context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error) {
			return &projectdom.Project{ID: projectID, Name: in.Name}, nil
		},
	}

	authorizer := authz.NewAuthorizer(nil)
	r := chi.NewRouter()
	r.Use(adminClaimsMiddleware())
	h := handler.NewProjectHandler(projectSvc, authorizer, handler.WithProjectDefaultViews(viewSvc, taskTypeSvc))
	r.Post("/admin/projects", h.CreateProject)

	w := do(t, r, http.MethodPost, "/admin/projects", jsonBody(t, map[string]any{"name": "alpha"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if len(viewSvc.created) != 2 {
		t.Fatalf("expected 2 seeded views, got %d", len(viewSvc.created))
	}

	var backlogView, timelineView *sprintdom.SprintView
	for _, view := range viewSvc.created {
		switch view.ViewContext {
		case sprintdom.ViewContextBacklog:
			backlogView = view
		case sprintdom.ViewContextTimeline:
			timelineView = view
		case sprintdom.ViewContextSprint:
			// sprint-context views are not expected here
		}
	}
	if backlogView == nil || timelineView == nil {
		t.Fatal("expected both backlog and timeline default views")
	}
	if backlogView.ViewType != sprintdom.ViewTypeTable || backlogView.Config.ColumnBy != "sprint" {
		t.Fatalf("unexpected backlog default view: %+v", backlogView)
	}
	if timelineView.ViewType != sprintdom.ViewTypeRoadmap {
		t.Fatalf("expected roadmap timeline view, got %+v", timelineView.ViewType)
	}
	// Backlog view must use the "normal" virtual group to include all non-system
	// types dynamically.
	if backlogView.Config.Filters == nil || backlogView.Config.Filters.TaskTypes == nil {
		t.Fatalf("expected backlog view to have a task type filter, got %+v", backlogView.Config.Filters)
	}
	normalEntry, hasNormal := backlogView.Config.Filters.TaskTypes.Items["normal"]
	if !hasNormal || !normalEntry.IsNested() || !normalEntry.Config().All {
		t.Fatalf("expected backlog view task types to use the all-normal group, got %+v", backlogView.Config.Filters.TaskTypes)
	}
	// Timeline view must have exactly one explicit task type ID (Epic) and no
	// virtual groups.
	if timelineView.Config.Filters == nil || timelineView.Config.Filters.TaskTypes == nil {
		t.Fatalf("expected timeline view to have a task type filter, got %+v", timelineView.Config.Filters)
	}
	if _, hasNormalTimeline := timelineView.Config.Filters.TaskTypes.Items["normal"]; hasNormalTimeline {
		t.Fatalf("expected no normal group in timeline view task types")
	}
	epicCount := 0
	for _, entry := range timelineView.Config.Filters.TaskTypes.Items {
		if !entry.IsNested() && entry.Flag() {
			epicCount++
		}
	}
	if epicCount != 1 {
		t.Fatalf("expected epic-only timeline default (1 explicit ID), got %+v", timelineView.Config.Filters.TaskTypes)
	}
}

func TestCreateProject_MissingName(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPost, "/admin/projects",
		jsonBody(t, map[string]any{"description": "no name"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProject_WhitespaceName(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{
		create: func(_ context.Context, _ projectdom.CreateProjectInput) (*projectdom.Project, error) {
			return nil, projectdom.ErrNameInvalid
		},
	})

	w := do(t, r, http.MethodPost, "/admin/projects",
		jsonBody(t, map[string]any{"name": "   "}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "PROJECT_NAME_INVALID" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestCreateProject_MalformedJSON(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPost, "/admin/projects", bytes.NewBufferString("{bad json"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProject_NameTaken(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{
		create: func(_ context.Context, _ projectdom.CreateProjectInput) (*projectdom.Project, error) {
			return nil, projectdom.ErrNameTaken
		},
	})

	w := do(t, r, http.MethodPost, "/admin/projects",
		jsonBody(t, map[string]any{"name": "alpha"}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_NAME_TAKEN" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestUpdateProject_Success(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		update: func(_ context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id, Name: in.Name}, nil
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/projects/%s", projID),
		jsonBody(t, map[string]any{"name": "updated"}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateProject_PreservesOmittedFields(t *testing.T) {
	projID := uuid.New()
	const existingDesc = "existing description"
	r := newProjectRouter(&mockProjectSvc{
		update: func(_ context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error) {
			// Simulate service PATCH semantics: only overwrite description when provided.
			desc := existingDesc
			if strings.TrimSpace(in.Description) != "" {
				desc = strings.TrimSpace(in.Description)
			}
			return &projectdom.Project{ID: id, Name: in.Name, Description: desc}, nil
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/projects/%s", projID),
		jsonBody(t, map[string]any{"name": "updated"}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			Description string `json:"description"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.Description != existingDesc {
		t.Fatalf("expected description %q to be preserved, got %q", existingDesc, env.Data.Description)
	}
}

func TestUpdateProject_BadID(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPatch, "/admin/projects/not-a-uuid",
		jsonBody(t, map[string]any{"name": "updated"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateProject_NotFound(t *testing.T) {
	id := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		update: func(_ context.Context, _ uuid.UUID, _ projectdom.UpdateProjectInput) (*projectdom.Project, error) {
			return nil, projectdom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/admin/projects/%s", id),
		jsonBody(t, map[string]any{"name": "updated"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteProject_Success(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		delete: func(_ context.Context, _ uuid.UUID) error { return nil },
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/admin/projects/%s", projID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestDeleteProject_BadID(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodDelete, "/admin/projects/not-a-uuid", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteProject_NotFound(t *testing.T) {
	id := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		delete: func(_ context.Context, _ uuid.UUID) error { return projectdom.ErrNotFound },
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/admin/projects/%s", id), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Project Role
// ---------------------------------------------------------------------------

func TestListRoles_Success(t *testing.T) {
	projID := uuid.New()
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		listRoles: func(_ context.Context, _ uuid.UUID) ([]*projectdom.ProjectRole, error) {
			return []*projectdom.ProjectRole{
				{ID: roleID, ProjectID: &projID, RoleName: "viewer"},
			}, nil
		},
	})

	w := do(t, r, http.MethodGet, fmt.Sprintf("/projects/%s/roles", projID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListRoles_BadProjectID(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodGet, "/projects/not-a-uuid/roles", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListRoles_ProjectNotFound(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		listRoles: func(_ context.Context, _ uuid.UUID) ([]*projectdom.ProjectRole, error) {
			return nil, projectdom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodGet, fmt.Sprintf("/projects/%s/roles", projID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateRole_Success(t *testing.T) {
	projID := uuid.New()
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		createRole: func(_ context.Context, pid uuid.UUID, in projectdom.CreateRoleInput) (*projectdom.ProjectRole, error) {
			return &projectdom.ProjectRole{ID: roleID, ProjectID: &pid, RoleName: in.RoleName}, nil
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/roles", projID),
		jsonBody(t, map[string]any{"role_name": "viewer", "permissions": map[string]any{}}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateRole_MissingRoleName(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/roles", projID),
		jsonBody(t, map[string]any{"permissions": map[string]any{}}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateRole_ProjectNotFound(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		createRole: func(_ context.Context, _ uuid.UUID, _ projectdom.CreateRoleInput) (*projectdom.ProjectRole, error) {
			return nil, projectdom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/roles", projID),
		jsonBody(t, map[string]any{"role_name": "viewer"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateRole_RoleNameTaken(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		createRole: func(_ context.Context, _ uuid.UUID, _ projectdom.CreateRoleInput) (*projectdom.ProjectRole, error) {
			return nil, projectdom.ErrRoleNameTaken
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/roles", projID),
		jsonBody(t, map[string]any{"role_name": "viewer"}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_ROLE_NAME_TAKEN" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestUpdateRole_Success(t *testing.T) {
	projID := uuid.New()
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		updateRole: func(_ context.Context, pid, rid uuid.UUID, in projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error) {
			return &projectdom.ProjectRole{ID: rid, ProjectID: &pid, RoleName: in.RoleName}, nil
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/roles/%s", projID, roleID),
		jsonBody(t, map[string]any{"role_name": "editor", "permissions": map[string]any{}}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateRole_BadProjectID(t *testing.T) {
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/not-a-uuid/roles/%s", roleID),
		jsonBody(t, map[string]any{"role_name": "editor"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateRole_BadRoleID(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/roles/not-a-uuid", projID),
		jsonBody(t, map[string]any{"role_name": "editor"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateRole_RoleNotFound(t *testing.T) {
	projID := uuid.New()
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		updateRole: func(_ context.Context, _, _ uuid.UUID, _ projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error) {
			return nil, projectdom.ErrRoleNotFound
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/roles/%s", projID, roleID),
		jsonBody(t, map[string]any{"role_name": "editor"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_ROLE_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestDeleteRole_Success(t *testing.T) {
	projID := uuid.New()
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		deleteRole: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/%s/roles/%s", projID, roleID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestDeleteRole_BadProjectID(t *testing.T) {
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/not-a-uuid/roles/%s", roleID), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteRole_BadRoleID(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/%s/roles/not-a-uuid", projID), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestDeleteRole_RoleHasMembers(t *testing.T) {
	projID := uuid.New()
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		deleteRole: func(_ context.Context, _, _ uuid.UUID) error {
			return projectdom.ErrRoleHasMembers
		},
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/%s/roles/%s", projID, roleID), nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_ROLE_HAS_MEMBERS" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

// ---------------------------------------------------------------------------
// Project Members
// ---------------------------------------------------------------------------

func TestListMembers_Success(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		listMembers: func(_ context.Context, _ uuid.UUID) ([]*projectdom.ProjectMember, error) {
			return []*projectdom.ProjectMember{
				{ID: memberID, ProjectID: projID, UserID: uuid.New()},
			}, nil
		},
	})

	w := do(t, r, http.MethodGet, fmt.Sprintf("/projects/%s/members", projID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListMembers_BadProjectID(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodGet, "/projects/not-a-uuid/members", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListMembers_ProjectNotFound(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		listMembers: func(_ context.Context, _ uuid.UUID) ([]*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodGet, fmt.Sprintf("/projects/%s/members", projID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAddMember_Success(t *testing.T) {
	projID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		addMember: func(_ context.Context, pid uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
			return &projectdom.ProjectMember{
				ID:            memberID,
				ProjectID:     pid,
				UserID:        in.UserID,
				ProjectRoleID: in.ProjectRoleID,
			}, nil
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/members", projID),
		jsonBody(t, map[string]any{"user_id": userID, "project_role_id": roleID}))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddMember_MalformedJSON(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/members", projID),
		bytes.NewBufferString("{bad"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestAddMember_ProjectNotFound(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		addMember: func(_ context.Context, _ uuid.UUID, _ projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/members", projID),
		jsonBody(t, map[string]any{"user_id": uuid.New(), "project_role_id": uuid.New()}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestAddMember_MemberAlreadyAdded(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		addMember: func(_ context.Context, _ uuid.UUID, _ projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrMemberAlreadyAdded
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/members", projID),
		jsonBody(t, map[string]any{"user_id": uuid.New(), "project_role_id": uuid.New()}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_MEMBER_ALREADY_ADDED" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestUpdateMemberRole_Success(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	roleID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		updateMemberByMemberID: func(_ context.Context, pid, mid uuid.UUID, in projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
			if pid != projID {
				t.Fatalf("expected project id %s, got %s", projID, pid)
			}
			if mid != memberID {
				t.Fatalf("expected member id %s, got %s", memberID, mid)
			}
			if in.ProjectRoleID != roleID {
				t.Fatalf("expected role id %s, got %s", roleID, in.ProjectRoleID)
			}
			return &projectdom.ProjectMember{
				ID:            mid,
				ProjectID:     pid,
				ProjectRoleID: in.ProjectRoleID,
			}, nil
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/%s", projID, memberID),
		jsonBody(t, map[string]any{"project_role_id": roleID}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMemberRole_BadProjectID(t *testing.T) {
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/not-a-uuid/members/%s", memberID),
		jsonBody(t, map[string]any{"project_role_id": uuid.New()}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateMemberRole_BadUserID(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/not-a-uuid", projID),
		jsonBody(t, map[string]any{"project_role_id": uuid.New()}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateMemberRole_MemberNotFound(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		updateMemberByMemberID: func(_ context.Context, _, _ uuid.UUID, _ projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrMemberNotFound
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/%s", projID, memberID),
		jsonBody(t, map[string]any{"project_role_id": uuid.New()}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_MEMBER_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestUpdateMemberRole_RoleNotFound(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		updateMemberByMemberID: func(_ context.Context, _, _ uuid.UUID, _ projectdom.UpdateMemberRoleInput) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrRoleNotFound
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/%s", projID, memberID),
		jsonBody(t, map[string]any{"project_role_id": uuid.New()}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_ROLE_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestRemoveMember_Success(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		removeMemberByMemberID: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/%s/members/%s", projID, memberID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRemoveMember_BadProjectID(t *testing.T) {
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/not-a-uuid/members/%s", memberID), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRemoveMember_BadUserID(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/%s/members/not-a-uuid", projID), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRemoveMember_MemberNotFound(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		removeMemberByMemberID: func(_ context.Context, _, _ uuid.UUID) error {
			return projectdom.ErrMemberNotFound
		},
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/%s/members/%s", projID, memberID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_MEMBER_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestAddMember_MissingUserID_Returns400(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/members", projID),
		jsonBody(t, map[string]any{"project_role_id": uuid.New()}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing user_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddMember_MissingProjectRoleID_Returns400(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/members", projID),
		jsonBody(t, map[string]any{"user_id": uuid.New()}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing project_role_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMemberRole_MissingProjectRoleID_Returns400(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/%s", projID, memberID),
		jsonBody(t, map[string]any{}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing project_role_id, got %d: %s", w.Code, w.Body.String())
	}
}
