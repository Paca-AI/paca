package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/platform/jev"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
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
	countDistinctAgents     func(ctx context.Context, projectIDs []uuid.UUID) (int64, error)
	addMember               func(ctx context.Context, projectID uuid.UUID, in projectdom.AddMemberInput) (*projectdom.ProjectMember, error)
	removeMember            func(ctx context.Context, projectID, userID uuid.UUID) error
	updateMemberDescription func(ctx context.Context, projectID, memberID uuid.UUID, description string) (*projectdom.ProjectMember, error)
	removeMemberByMemberID  func(ctx context.Context, projectID, memberID uuid.UUID) error
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

func (m *mockProjectSvc) InitiateAvatarUpload(context.Context, uuid.UUID, string, string, int64, uuid.UUID) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{}, nil
}
func (m *mockProjectSvc) CompleteAvatarUpload(context.Context, uuid.UUID, uuid.UUID) (*projectdom.Project, error) {
	return nil, projectdom.ErrNotFound
}
func (m *mockProjectSvc) RemoveAvatar(context.Context, uuid.UUID) (*projectdom.Project, error) {
	return nil, projectdom.ErrNotFound
}

func (m *mockProjectSvc) ListMembers(ctx context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	if m.listMembers != nil {
		return m.listMembers(ctx, projectID)
	}
	return []*projectdom.ProjectMember{}, nil
}

func (m *mockProjectSvc) CountDistinctAgentsByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error) {
	if m.countDistinctAgents != nil {
		return m.countDistinctAgents(ctx, projectIDs)
	}
	return 0, nil
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

func (m *mockProjectSvc) AddAgentMember(_ context.Context, _, _, _ uuid.UUID, _ []uuid.UUID, _ *uuid.UUID) error {
	return nil
}
func (m *mockProjectSvc) RemoveAgentMember(_ context.Context, _, _ uuid.UUID) error { return nil }
func (m *mockProjectSvc) UpdateMemberDescription(_ context.Context, projectID, memberID uuid.UUID, description string) (*projectdom.ProjectMember, error) {
	if m.updateMemberDescription != nil {
		return m.updateMemberDescription(context.Background(), projectID, memberID, description)
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

// adminClaimsMiddleware injects synthetic claims into the request context so
// unit tests can exercise the handler without a real JWT stack. The role name
// is informational only — what the caller may do comes from the Authorizer's
// store (see adminAuthorizer).
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

// adminAuthorizer returns a real IAM Authorizer whose store answers as an
// admin's platform role would for these tests: projects.read on the
// platform roots (incl. the project collection "project"). That —
// not the "ADMIN" name on the synthetic claims above — is what makes
// ListProjects/GetWorkspaceStats return every project.
func adminAuthorizer() *iam.Authorizer {
	return iam.NewAuthorizer(&fakeGlobalPermStore{globalPerms: []iam.Action{iam.ActionProjectsRead}}, iam.NewRegistry(), iam.NewAttributeSchema())
}

func newProjectRouter(svc projectdom.Service) chi.Router {
	authorizer := adminAuthorizer()
	r := chi.NewRouter()
	r.Use(adminClaimsMiddleware())
	// h also gets a plain transport for its Jev client: the default is
	// SSRF-safe (netguard) and rejects the loopback httptest.Servers these
	// tests point jev_base_url at — see WithProjectJevHTTPClient.
	h := handler.NewProjectHandler(svc, authorizer,
		handler.WithProjectJevHTTPClient(&http.Client{Timeout: 5 * time.Second}))
	// Admin project CRUD
	r.Get("/admin/projects", h.ListProjects)
	r.Post("/admin/projects", h.CreateProject)
	r.Get("/admin/projects/{projectId}", h.GetProject)
	r.Patch("/admin/projects/{projectId}", h.UpdateProject)
	r.Delete("/admin/projects/{projectId}", h.DeleteProject)
	// Project member routes
	r.Get("/projects/{projectId}/members", h.ListMembers)
	r.Post("/projects/{projectId}/members", h.AddMember)
	r.Patch("/projects/{projectId}/members/{memberId}", h.UpdateMember)
	r.Delete("/projects/{projectId}/members/{memberId}", h.RemoveMember)
	// Jev config test route — encryptor is nil here (matches an instance
	// with no ENCRYPTION_KEY configured; jev.ClientForProject treats stored
	// keys as plaintext in that case, see its doc comment).
	r.Post("/projects/{projectId}/jev-config/test", h.TestJevConfig)
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

func TestListProjects_PageValid(t *testing.T) {
	cases := []struct {
		name     string
		query    string
		wantPage int
	}{
		{"absent_defaults", "", 1},
		{"valid_custom_page_kept", "page=3", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPage int
			r := newProjectRouter(&mockProjectSvc{
				list: func(_ context.Context, page, _ int) ([]*projectdom.Project, int64, error) {
					gotPage = page
					return []*projectdom.Project{}, 0, nil
				},
			})
			w := do(t, r, http.MethodGet, "/admin/projects?"+tc.query, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			if gotPage != tc.wantPage {
				t.Errorf("expected service called with page=%d, got %d", tc.wantPage, gotPage)
			}
		})
	}
}

// TestListProjects_PageInvalidRejected covers the same silent-substitution
// fix applied to page_size: an explicitly supplied invalid page value now
// fails the request instead of quietly running with page=1, which would
// otherwise mask a bug in the caller's own paging logic.
func TestListProjects_PageInvalidRejected(t *testing.T) {
	cases := []string{"page=0", "page=-1", "page=abc"}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			r := newProjectRouter(&mockProjectSvc{
				list: func(_ context.Context, page, _ int) ([]*projectdom.Project, int64, error) {
					t.Fatalf("service should not be called for invalid page, got page=%d", page)
					return nil, 0, nil
				},
			})
			w := do(t, r, http.MethodGet, "/admin/projects?"+query, nil)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
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

// ---------------------------------------------------------------------------
// TestJevConfig
// ---------------------------------------------------------------------------

func TestTestJevConfig_NotConfigured(t *testing.T) {
	id := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id}, nil // JevAPIKeySecret is empty
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/jev-config/test", id), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestJevConfig_ProjectNotFound(t *testing.T) {
	id := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*projectdom.Project, error) {
			return nil, projectdom.ErrNotFound
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/jev-config/test", id), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestTestJevConfig_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jev.Response{
			Answers: map[string]jev.Answer{"connectivity": {Type: jev.TypeNoul, Noul: 1}},
		})
	}))
	defer srv.Close()

	id := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id, JevAPIKeySecret: "test-key", JevBaseURL: srv.URL}, nil
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/jev-config/test", id), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data dto.TestJevConfigResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.Success {
		t.Fatalf("expected success=true, got %+v", envelope.Data)
	}
}

func TestTestJevConfig_CallFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	id := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		getByID: func(_ context.Context, _ uuid.UUID) (*projectdom.Project, error) {
			return &projectdom.Project{ID: id, JevAPIKeySecret: "test-key", JevBaseURL: srv.URL}, nil
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/jev-config/test", id), nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
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

	authorizer := adminAuthorizer()
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

// A member shows the roles it holds in the project as a list of {id, name};
// the single legacy project_role_id/role_name fields are gone.
func TestListMembers_ShowsRolesAsAList(t *testing.T) {
	projID, roleID := uuid.New(), uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		listMembers: func(context.Context, uuid.UUID) ([]*projectdom.ProjectMember, error) {
			return []*projectdom.ProjectMember{
				{ID: uuid.New(), ProjectID: projID, UserID: uuid.New(), Roles: []roledom.Summary{{ID: roleID, Name: "Editor"}}},
				{ID: uuid.New(), ProjectID: projID, UserID: uuid.New()},
			}, nil
		},
	})

	w := do(t, r, http.MethodGet, fmt.Sprintf("/projects/%s/members", projID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("members = %v", resp.Data)
	}
	for _, m := range resp.Data {
		for _, legacy := range []string{"project_role_id", "role_name"} {
			if _, ok := m[legacy]; ok {
				t.Errorf("legacy field %q still in the response", legacy)
			}
		}
	}
	first, _ := resp.Data[0]["roles"].([]any)
	if len(first) != 1 || first[0].(map[string]any)["name"] != "Editor" || first[0].(map[string]any)["id"] != roleID.String() {
		t.Errorf("roles = %v", resp.Data[0]["roles"])
	}
	if empty, ok := resp.Data[1]["roles"].([]any); !ok || len(empty) != 0 {
		t.Errorf("a member without roles must show an empty list, got %v", resp.Data[1]["roles"])
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
				ID:        memberID,
				ProjectID: pid,
				UserID:    in.UserID,
				Roles:     []roledom.Summary{{ID: in.RoleIDs[0], Name: "Viewer"}},
			}, nil
		},
	})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/members", projID),
		jsonBody(t, map[string]any{"user_id": userID, "role_ids": []uuid.UUID{roleID}}))
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
		jsonBody(t, map[string]any{"user_id": uuid.New(), "role_ids": []uuid.UUID{uuid.New()}}))
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
		jsonBody(t, map[string]any{"user_id": uuid.New(), "role_ids": []uuid.UUID{uuid.New()}}))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_MEMBER_ALREADY_ADDED" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

func TestUpdateMember_Description_Success(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{
		updateMemberDescription: func(_ context.Context, pid, mid uuid.UUID, description string) (*projectdom.ProjectMember, error) {
			if pid != projID || mid != memberID || description != "frontend lead" {
				t.Fatalf("unexpected call %s %s %q", pid, mid, description)
			}
			return &projectdom.ProjectMember{ID: mid, ProjectID: pid, Description: description}, nil
		},
	})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/%s", projID, memberID),
		jsonBody(t, map[string]any{"description": "frontend lead"}))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// The old project_role_id field is gone: a body carrying only it changes
// nothing and is rejected.
func TestUpdateMember_RoleFieldIsNotAccepted(t *testing.T) {
	projID := uuid.New()
	memberID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/%s", projID, memberID),
		jsonBody(t, map[string]any{"role_ids": []uuid.UUID{uuid.New()}}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateMember_BadProjectID(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})
	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/not-a-uuid/members/%s", uuid.New()),
		jsonBody(t, map[string]any{"description": "x"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateMember_BadMemberID(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{})
	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/not-a-uuid", uuid.New()),
		jsonBody(t, map[string]any{"description": "x"}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestUpdateMember_MemberNotFound(t *testing.T) {
	r := newProjectRouter(&mockProjectSvc{
		updateMemberDescription: func(context.Context, uuid.UUID, uuid.UUID, string) (*projectdom.ProjectMember, error) {
			return nil, projectdom.ErrMemberNotFound
		},
	})
	w := do(t, r, http.MethodPatch, fmt.Sprintf("/projects/%s/members/%s", uuid.New(), uuid.New()),
		jsonBody(t, map[string]any{"description": "x"}))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if code := errorCode(t, w); code != "PROJECT_MEMBER_NOT_FOUND" {
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
		jsonBody(t, map[string]any{"role_ids": []uuid.UUID{uuid.New()}}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing user_id, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddMember_MissingRoleIDs_Returns400(t *testing.T) {
	projID := uuid.New()
	r := newProjectRouter(&mockProjectSvc{})

	w := do(t, r, http.MethodPost, fmt.Sprintf("/projects/%s/members", projID),
		jsonBody(t, map[string]any{"user_id": uuid.New()}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing role_ids, got %d: %s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "ROLE_REQUIRED" {
		t.Fatalf("unexpected error_code: %s", code)
	}
}

// ---------------------------------------------------------------------------
// GetWorkspaceStats
// ---------------------------------------------------------------------------

// mockTaskStatsSvc implements the handler package's unexported
// taskServiceForStats interface structurally (Go interfaces are satisfied
// implicitly, so an external test package can implement one without naming it).
type mockTaskStatsSvc struct {
	countOpenTasksByProjects func(ctx context.Context, projectIDs []uuid.UUID) (int64, error)
}

func (m *mockTaskStatsSvc) CountOpenTasksByProjects(ctx context.Context, projectIDs []uuid.UUID) (int64, error) {
	return m.countOpenTasksByProjects(ctx, projectIDs)
}

// mockUserStatsSvc implements handler.userServiceForStats structurally.
type mockUserStatsSvc struct {
	countUsers func(ctx context.Context) (int64, error)
}

func (m *mockUserStatsSvc) CountUsers(ctx context.Context) (int64, error) {
	return m.countUsers(ctx)
}

// TestGetWorkspaceStats_SingleAggregateQueryPerCounter guards against a
// regression back to the old per-project fan-out (one ListMembers/ListAgents
// call per accessible project, deduped in Go) that both inflated the "AI
// agents" count for an agent invited into multiple projects and did an
// avoidable N-query loop. It asserts each counter is computed via exactly
// one call — the team-member count via a plain users-table count that
// ignores accessible-project scoping entirely, and the AI-agent count via
// one aggregate call covering every accessible project at once.
func TestGetWorkspaceStats_SingleAggregateQueryPerCounter(t *testing.T) {
	proj1, proj2 := uuid.New(), uuid.New()

	var memberCalls, taskCalls, userCalls int
	var gotMemberProjectIDs, gotTaskProjectIDs []uuid.UUID

	projectSvc := &mockProjectSvc{
		list: func(_ context.Context, _, _ int) ([]*projectdom.Project, int64, error) {
			return []*projectdom.Project{{ID: proj1}, {ID: proj2}}, 2, nil
		},
		countDistinctAgents: func(_ context.Context, projectIDs []uuid.UUID) (int64, error) {
			memberCalls++
			gotMemberProjectIDs = projectIDs
			return 2, nil
		},
	}
	taskSvc := &mockTaskStatsSvc{
		countOpenTasksByProjects: func(_ context.Context, projectIDs []uuid.UUID) (int64, error) {
			taskCalls++
			gotTaskProjectIDs = projectIDs
			return 7, nil
		},
	}
	userSvc := &mockUserStatsSvc{
		countUsers: func(_ context.Context) (int64, error) {
			userCalls++
			return 5, nil
		},
	}

	authorizer := adminAuthorizer()
	r := chi.NewRouter()
	r.Use(adminClaimsMiddleware())
	h := handler.NewProjectHandler(projectSvc, authorizer, handler.WithProjectStatsServices(taskSvc, userSvc))
	r.Get("/projects/workspace-stats", h.GetWorkspaceStats)

	w := do(t, r, http.MethodGet, "/projects/workspace-stats", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if memberCalls != 1 {
		t.Fatalf("expected CountDistinctAgentsByProjects called exactly once, got %d calls", memberCalls)
	}
	if taskCalls != 1 {
		t.Fatalf("expected CountOpenTasksByProjects called exactly once, got %d calls", taskCalls)
	}
	if userCalls != 1 {
		t.Fatalf("expected CountUsers called exactly once, got %d calls", userCalls)
	}
	assertSameProjectIDs(t, gotMemberProjectIDs, []uuid.UUID{proj1, proj2})
	assertSameProjectIDs(t, gotTaskProjectIDs, []uuid.UUID{proj1, proj2})

	var envelope struct {
		Data dto.WorkspaceStatsResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if envelope.Data.TeamMemberCount != 5 {
		t.Fatalf("expected TeamMemberCount=5, got %d", envelope.Data.TeamMemberCount)
	}
	if envelope.Data.AIAgentCount != 2 {
		t.Fatalf("expected AIAgentCount=2, got %d", envelope.Data.AIAgentCount)
	}
	if envelope.Data.OpenTaskCount != 7 {
		t.Fatalf("expected OpenTaskCount=7, got %d", envelope.Data.OpenTaskCount)
	}
}

func assertSameProjectIDs(t *testing.T, got []uuid.UUID, want []uuid.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %d project IDs, got %d (%v)", len(want), len(got), got)
	}
	wantSet := make(map[uuid.UUID]struct{}, len(want))
	for _, id := range want {
		wantSet[id] = struct{}{}
	}
	for _, id := range got {
		if _, ok := wantSet[id]; !ok {
			t.Fatalf("unexpected project ID %s in %v (want %v)", id, got, want)
		}
	}
}
