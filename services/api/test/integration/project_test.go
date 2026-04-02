package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	projectdom "github.com/paca/api/internal/domain/project"
	"github.com/paca/api/internal/platform/authz"
	jwttoken "github.com/paca/api/internal/platform/token"
	authsvc "github.com/paca/api/internal/service/auth"
	projectsvc "github.com/paca/api/internal/service/project"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
)

type fakeProjectRepo struct {
	mu sync.RWMutex

	projects map[uuid.UUID]*projectdom.Project
	roles    map[uuid.UUID]*projectdom.ProjectRole
	members  map[string]*projectdom.ProjectMember
}

func newFakeProjectRepo() *fakeProjectRepo {
	return &fakeProjectRepo{
		projects: make(map[uuid.UUID]*projectdom.Project),
		roles:    make(map[uuid.UUID]*projectdom.ProjectRole),
		members:  make(map[string]*projectdom.ProjectMember),
	}
}

func memberKey(projectID, userID uuid.UUID) string {
	return projectID.String() + ":" + userID.String()
}

func cloneProject(in *projectdom.Project) *projectdom.Project {
	if in == nil {
		return nil
	}
	out := *in
	if in.Settings != nil {
		out.Settings = make(map[string]any, len(in.Settings))
		for k, v := range in.Settings {
			out.Settings[k] = v
		}
	}
	return &out
}

func cloneRole(in *projectdom.ProjectRole) *projectdom.ProjectRole {
	if in == nil {
		return nil
	}
	out := *in
	if in.ProjectID != nil {
		pid := *in.ProjectID
		out.ProjectID = &pid
	}
	if in.Permissions != nil {
		out.Permissions = make(map[string]any, len(in.Permissions))
		for k, v := range in.Permissions {
			out.Permissions[k] = v
		}
	}
	return &out
}

func cloneMember(in *projectdom.ProjectMember) *projectdom.ProjectMember {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func (r *fakeProjectRepo) List(_ context.Context, offset, limit int) ([]*projectdom.Project, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]*projectdom.Project, 0, len(r.projects))
	for _, p := range r.projects {
		all = append(all, cloneProject(p))
	}
	total := int64(len(all))
	if offset >= len(all) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func (r *fakeProjectRepo) FindByID(_ context.Context, id uuid.UUID) (*projectdom.Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.projects[id]
	if !ok {
		return nil, projectdom.ErrNotFound
	}
	return cloneProject(p), nil
}

func (r *fakeProjectRepo) Create(_ context.Context, p *projectdom.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.projects {
		if existing.Name == p.Name {
			return projectdom.ErrNameTaken
		}
	}
	r.projects[p.ID] = cloneProject(p)
	return nil
}

func (r *fakeProjectRepo) Update(_ context.Context, p *projectdom.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.projects[p.ID]; !ok {
		return projectdom.ErrNotFound
	}
	for _, existing := range r.projects {
		if existing.ID != p.ID && existing.Name == p.Name {
			return projectdom.ErrNameTaken
		}
	}
	r.projects[p.ID] = cloneProject(p)
	return nil
}

func (r *fakeProjectRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.projects[id]; !ok {
		return projectdom.ErrNotFound
	}
	delete(r.projects, id)
	for roleID, role := range r.roles {
		if role.ProjectID != nil && *role.ProjectID == id {
			delete(r.roles, roleID)
		}
	}
	for key, m := range r.members {
		if m.ProjectID == id {
			delete(r.members, key)
		}
	}
	return nil
}

func (r *fakeProjectRepo) ListRoles(_ context.Context, projectID uuid.UUID) ([]*projectdom.ProjectRole, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*projectdom.ProjectRole, 0)
	for _, role := range r.roles {
		if role.ProjectID != nil && *role.ProjectID == projectID {
			out = append(out, cloneRole(role))
		}
	}
	return out, nil
}

func (r *fakeProjectRepo) FindRoleByID(_ context.Context, id uuid.UUID) (*projectdom.ProjectRole, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	role, ok := r.roles[id]
	if !ok {
		return nil, projectdom.ErrRoleNotFound
	}
	return cloneRole(role), nil
}

func (r *fakeProjectRepo) FindRoleByName(_ context.Context, projectID uuid.UUID, name string) (*projectdom.ProjectRole, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, role := range r.roles {
		if role.ProjectID != nil && *role.ProjectID == projectID && role.RoleName == name {
			return cloneRole(role), nil
		}
	}
	return nil, projectdom.ErrRoleNotFound
}

func (r *fakeProjectRepo) CreateRole(_ context.Context, role *projectdom.ProjectRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.roles {
		if existing.ProjectID != nil && role.ProjectID != nil && *existing.ProjectID == *role.ProjectID && existing.RoleName == role.RoleName {
			return projectdom.ErrRoleNameTaken
		}
	}
	r.roles[role.ID] = cloneRole(role)
	return nil
}

func (r *fakeProjectRepo) UpdateRole(_ context.Context, role *projectdom.ProjectRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.roles[role.ID]; !ok {
		return projectdom.ErrRoleNotFound
	}
	r.roles[role.ID] = cloneRole(role)
	return nil
}

func (r *fakeProjectRepo) DeleteRole(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.roles[id]; !ok {
		return projectdom.ErrRoleNotFound
	}
	delete(r.roles, id)
	return nil
}

func (r *fakeProjectRepo) CountMembersWithRole(_ context.Context, roleID uuid.UUID) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var count int64
	for _, m := range r.members {
		if m.ProjectRoleID == roleID {
			count++
		}
	}
	return count, nil
}

func (r *fakeProjectRepo) ListMembers(_ context.Context, projectID uuid.UUID) ([]*projectdom.ProjectMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*projectdom.ProjectMember, 0)
	for _, m := range r.members {
		if m.ProjectID == projectID {
			out = append(out, cloneMember(m))
		}
	}
	return out, nil
}

func (r *fakeProjectRepo) FindMember(_ context.Context, projectID, userID uuid.UUID) (*projectdom.ProjectMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	m, ok := r.members[memberKey(projectID, userID)]
	if !ok {
		return nil, projectdom.ErrMemberNotFound
	}
	return cloneMember(m), nil
}

func (r *fakeProjectRepo) AddMember(_ context.Context, m *projectdom.ProjectMember) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := memberKey(m.ProjectID, m.UserID)
	if _, ok := r.members[k]; ok {
		return projectdom.ErrMemberAlreadyAdded
	}
	r.members[k] = cloneMember(m)
	return nil
}

func (r *fakeProjectRepo) RemoveMember(_ context.Context, projectID, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := memberKey(projectID, userID)
	if _, ok := r.members[k]; !ok {
		return projectdom.ErrMemberNotFound
	}
	delete(r.members, k)
	return nil
}

func (r *fakeProjectRepo) UpdateMemberRole(_ context.Context, projectID, userID, roleID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := memberKey(projectID, userID)
	m, ok := r.members[k]
	if !ok {
		return projectdom.ErrMemberNotFound
	}
	m.ProjectRoleID = roleID
	r.members[k] = cloneMember(m)
	return nil
}

type projectPermStore struct {
	globalPerms  []authz.Permission
	projectPerms map[uuid.UUID][]authz.Permission
}

func (s *projectPermStore) ListGlobalPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return append([]authz.Permission(nil), s.globalPerms...), nil
}

func (s *projectPermStore) ListProjectPermissions(_ context.Context, _ uuid.UUID, projectID uuid.UUID) ([]authz.Permission, error) {
	perms := s.projectPerms[projectID]
	return append([]authz.Permission(nil), perms...), nil
}

func buildProjectTestRouter(repo *fakeProjectRepo, store *projectPermStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	projectService := projectsvc.New(repo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(store),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, testCookieCfg),
		User:         handler.NewUserHandler(userService),
		GlobalRole:   handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}),
		Project:      handler.NewProjectHandler(projectService),
		Log:          log,
	})
}

func issueProjectToken(t *testing.T, subject string) string {
	t.Helper()
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	tok, err := tm.IssueAccess(subject, "project-user", "USER", "fam-project", false)
	if err != nil {
		t.Fatalf("issue project token: %v", err)
	}
	return tok
}

func serve(r *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func authedJSONReq(ctx context.Context, method, url, token string, body any) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req := httptest.NewRequestWithContext(ctx, method, url, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func projectIDFromCreate(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode create project response: %v", err)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatal("missing project id")
	}
	return id
}

func roleIDFromCreate(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode create role response: %v", err)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatal("missing role id")
	}
	return id
}

func TestIntegrationProjectManagement_AdminCRUD(t *testing.T) {
	repo := newFakeProjectRepo()
	store := &projectPermStore{
		globalPerms: []authz.Permission{authz.PermissionProjectsRead, authz.PermissionProjectsWrite},
	}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	createReq := authedJSONReq(t.Context(), http.MethodPost, "/api/v1/admin/projects", tok, map[string]any{
		"name":        "Project Alpha",
		"description": "first",
	})
	createW := serve(r, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	projectID := projectIDFromCreate(t, createW)

	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/admin/projects", tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}

	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/admin/projects/"+projectID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}

	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, "/api/v1/admin/projects/"+projectID, tok, map[string]any{
		"name":        "Project Alpha Updated",
		"description": "updated",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, "/api/v1/admin/projects/"+projectID, tok, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (%s)", delW.Code, delW.Body.String())
	}

	getDeletedW := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/admin/projects/"+projectID, tok, nil))
	if getDeletedW.Code != http.StatusNotFound {
		t.Fatalf("get deleted: expected 404, got %d (%s)", getDeletedW.Code, getDeletedW.Body.String())
	}
	if code := decodeErrorCode(t, getDeletedW); code != "PROJECT_NOT_FOUND" {
		t.Fatalf("expected PROJECT_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationProjectManagement_AuthzGuards(t *testing.T) {
	repo := newFakeProjectRepo()
	store := &projectPermStore{globalPerms: []authz.Permission{authz.PermissionProjectsRead}}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	writeW := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/admin/projects", tok, map[string]any{"name": "No Write"}))
	if writeW.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without projects.write, got %d (%s)", writeW.Code, writeW.Body.String())
	}
	if code := decodeErrorCode(t, writeW); code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %q", code)
	}

	unauthW := serve(r, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/admin/projects", nil))
	if unauthW.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d (%s)", unauthW.Code, unauthW.Body.String())
	}
}

func TestIntegrationProjectRolesAndMembers_Flow(t *testing.T) {
	repo := newFakeProjectRepo()
	projectID := uuid.New()
	repo.projects[projectID] = &projectdom.Project{ID: projectID, Name: "Proj", CreatedAt: time.Now()}

	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionProjectRolesRead,
				authz.PermissionProjectRolesWrite,
				authz.PermissionProjectMembersRead,
				authz.PermissionProjectMembersWrite,
			},
		},
	}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	createRoleURL := fmt.Sprintf("/api/v1/projects/%s/roles", projectID)
	createRoleW := serve(r, authedJSONReq(t.Context(), http.MethodPost, createRoleURL, tok, map[string]any{
		"role_name":   "developer",
		"permissions": map[string]any{"tasks.read": true},
	}))
	if createRoleW.Code != http.StatusCreated {
		t.Fatalf("create role: expected 201, got %d (%s)", createRoleW.Code, createRoleW.Body.String())
	}
	roleID := roleIDFromCreate(t, createRoleW)

	dupRoleW := serve(r, authedJSONReq(t.Context(), http.MethodPost, createRoleURL, tok, map[string]any{
		"role_name": "developer",
	}))
	if dupRoleW.Code != http.StatusConflict {
		t.Fatalf("duplicate role: expected 409, got %d (%s)", dupRoleW.Code, dupRoleW.Body.String())
	}
	if code := decodeErrorCode(t, dupRoleW); code != "PROJECT_ROLE_NAME_TAKEN" {
		t.Fatalf("expected PROJECT_ROLE_NAME_TAKEN, got %q", code)
	}

	memberUserID := uuid.New()
	membersURL := fmt.Sprintf("/api/v1/projects/%s/members", projectID)
	addMemberW := serve(r, authedJSONReq(t.Context(), http.MethodPost, membersURL, tok, map[string]any{
		"user_id":         memberUserID,
		"project_role_id": roleID,
	}))
	if addMemberW.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d (%s)", addMemberW.Code, addMemberW.Body.String())
	}

	dupMemberW := serve(r, authedJSONReq(t.Context(), http.MethodPost, membersURL, tok, map[string]any{
		"user_id":         memberUserID,
		"project_role_id": roleID,
	}))
	if dupMemberW.Code != http.StatusConflict {
		t.Fatalf("duplicate member: expected 409, got %d (%s)", dupMemberW.Code, dupMemberW.Body.String())
	}
	if code := decodeErrorCode(t, dupMemberW); code != "PROJECT_MEMBER_ALREADY_ADDED" {
		t.Fatalf("expected PROJECT_MEMBER_ALREADY_ADDED, got %q", code)
	}

	updatedRoleW := serve(r, authedJSONReq(t.Context(), http.MethodPost, createRoleURL, tok, map[string]any{
		"role_name": "qa",
	}))
	if updatedRoleW.Code != http.StatusCreated {
		t.Fatalf("create second role: expected 201, got %d (%s)", updatedRoleW.Code, updatedRoleW.Body.String())
	}
	updatedRoleID := roleIDFromCreate(t, updatedRoleW)

	updateMemberURL := fmt.Sprintf("/api/v1/projects/%s/members/%s", projectID, memberUserID)
	updateMemberW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, updateMemberURL, tok, map[string]any{
		"project_role_id": updatedRoleID,
	}))
	if updateMemberW.Code != http.StatusOK {
		t.Fatalf("update member role: expected 200, got %d (%s)", updateMemberW.Code, updateMemberW.Body.String())
	}

	var updateMemberEnv struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(updateMemberW.Body).Decode(&updateMemberEnv); err != nil {
		t.Fatalf("decode update member response: %v", err)
	}
	if got, _ := updateMemberEnv.Data["project_role_id"].(string); got != updatedRoleID {
		t.Fatalf("expected updated role id %q, got %q", updatedRoleID, got)
	}

	missingMemberW := serve(r, authedJSONReq(t.Context(), http.MethodPatch,
		fmt.Sprintf("/api/v1/projects/%s/members/%s", projectID, uuid.New()), tok, map[string]any{
			"project_role_id": updatedRoleID,
		}))
	if missingMemberW.Code != http.StatusNotFound {
		t.Fatalf("update missing member: expected 404, got %d (%s)", missingMemberW.Code, missingMemberW.Body.String())
	}
	if code := decodeErrorCode(t, missingMemberW); code != "PROJECT_MEMBER_NOT_FOUND" {
		t.Fatalf("expected PROJECT_MEMBER_NOT_FOUND, got %q", code)
	}

	deleteRoleURL := fmt.Sprintf("/api/v1/projects/%s/roles/%s", projectID, updatedRoleID)
	deleteRoleWhileAssignedW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, deleteRoleURL, tok, nil))
	if deleteRoleWhileAssignedW.Code != http.StatusConflict {
		t.Fatalf("delete role in use: expected 409, got %d (%s)", deleteRoleWhileAssignedW.Code, deleteRoleWhileAssignedW.Body.String())
	}
	if code := decodeErrorCode(t, deleteRoleWhileAssignedW); code != "PROJECT_ROLE_HAS_MEMBERS" {
		t.Fatalf("expected PROJECT_ROLE_HAS_MEMBERS, got %q", code)
	}

	removeMemberURL := fmt.Sprintf("/api/v1/projects/%s/members/%s", projectID, memberUserID)
	removeW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, removeMemberURL, tok, nil))
	if removeW.Code != http.StatusOK {
		t.Fatalf("remove member: expected 200, got %d (%s)", removeW.Code, removeW.Body.String())
	}

	removeMissingW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, removeMemberURL, tok, nil))
	if removeMissingW.Code != http.StatusNotFound {
		t.Fatalf("remove missing member: expected 404, got %d (%s)", removeMissingW.Code, removeMissingW.Body.String())
	}
	if code := decodeErrorCode(t, removeMissingW); code != "PROJECT_MEMBER_NOT_FOUND" {
		t.Fatalf("expected PROJECT_MEMBER_NOT_FOUND, got %q", code)
	}

	deleteRoleW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, deleteRoleURL, tok, nil))
	if deleteRoleW.Code != http.StatusOK {
		t.Fatalf("delete role: expected 200, got %d (%s)", deleteRoleW.Code, deleteRoleW.Body.String())
	}
}
