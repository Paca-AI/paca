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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
	"github.com/paca/api/internal/transport/http/handler"
)

// ---------------------------------------------------------------------------
// mock
// ---------------------------------------------------------------------------

type mockProjectSvc struct {
	list         func(ctx context.Context, page, pageSize int) ([]*projectdom.Project, int64, error)
	getByID      func(ctx context.Context, id uuid.UUID) (*projectdom.Project, error)
	create       func(ctx context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error)
	update       func(ctx context.Context, id uuid.UUID, in projectdom.UpdateProjectInput) (*projectdom.Project, error)
	delete       func(ctx context.Context, id uuid.UUID) error
	listMembers  func(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error)
	addMember    func(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error)
	removeMember func(ctx context.Context, projectID, userID uuid.UUID) error
	listRoles    func(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error)
	createRole   func(ctx context.Context, projectID uuid.UUID, in projectdom.CreateRoleInput) (*projectdom.ProjectRole, error)
	updateRole   func(ctx context.Context, projectID, roleID uuid.UUID, in projectdom.UpdateRoleInput) (*projectdom.ProjectRole, error)
	deleteRole   func(ctx context.Context, projectID, roleID uuid.UUID) error
}

func (m *mockProjectSvc) List(ctx context.Context, page, pageSize int) ([]*projectdom.Project, int64, error) {
	if m.list != nil {
		return m.list(ctx, page, pageSize)
	}
	return []*projectdom.Project{}, 0, nil
}

func (m *mockProjectSvc) GetByID(ctx context.Context, id uuid.UUID) (*projectdom.Project, error) {
	if m.getByID != nil {
		return m.getByID(ctx, id)
	}
	return nil, projectdom.ErrNotFound
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

func (m *mockProjectSvc) AddMember(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error) {
	if m.addMember != nil {
		return m.addMember(ctx, projectID, in)
	}
	return nil, errors.New("mock: addMember not configured")
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

// compile-time interface check
var _ projectdom.Service = (*mockProjectSvc)(nil)

// ---------------------------------------------------------------------------
// router helper
// ---------------------------------------------------------------------------

func newProjectRouter(svc projectdom.Service) *gin.Engine {
	r := gin.New()
	h := handler.NewProjectHandler(svc)
	// Admin project CRUD
	r.GET("/admin/projects", h.ListProjects)
	r.POST("/admin/projects", h.CreateProject)
	r.GET("/admin/projects/:projectId", h.GetProject)
	r.PATCH("/admin/projects/:projectId", h.UpdateProject)
	r.DELETE("/admin/projects/:projectId", h.DeleteProject)
	// Project member routes
	r.GET("/projects/:projectId/members", h.ListMembers)
	r.POST("/projects/:projectId/members", h.AddMember)
	r.DELETE("/projects/:projectId/members/:userId", h.RemoveMember)
	// Project role routes
	r.GET("/projects/:projectId/roles", h.ListRoles)
	r.POST("/projects/:projectId/roles", h.CreateRole)
	r.PATCH("/projects/:projectId/roles/:roleId", h.UpdateRole)
	r.DELETE("/projects/:projectId/roles/:roleId", h.DeleteRole)
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
		create: func(_ context.Context, in projectdom.CreateProjectInput) (*projectdom.Project, error) {
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

func TestRemoveMember_Success(t *testing.T) {
	projID := uuid.New()
	userID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		removeMember: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/%s/members/%s", projID, userID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestRemoveMember_BadProjectID(t *testing.T) {
	userID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/not-a-uuid/members/%s", userID), nil)
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
	userID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		removeMember: func(_ context.Context, _, _ uuid.UUID) error {
			return projectdom.ErrMemberNotFound
		},
	})

	w := do(t, r, http.MethodDelete, fmt.Sprintf("/projects/%s/members/%s", projID, userID), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_MEMBER_NOT_FOUND" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}
