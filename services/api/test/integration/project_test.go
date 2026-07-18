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

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/google/uuid"
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

func (r *fakeProjectRepo) ListAccessible(_ context.Context, userID uuid.UUID, offset, limit int) ([]*projectdom.Project, int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]*projectdom.Project, 0)
	for _, p := range r.projects {
		for _, m := range r.members {
			if m.ProjectID == p.ID && m.UserID == userID {
				all = append(all, cloneProject(p))
				break
			}
		}
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

func (r *fakeProjectRepo) FindMemberByAgent(_ context.Context, projectID, agentID uuid.UUID) (*projectdom.ProjectMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, m := range r.members {
		if m.ProjectID == projectID && m.AgentID != nil && *m.AgentID == agentID {
			return cloneMember(m), nil
		}
	}
	return nil, projectdom.ErrMemberNotFound
}

func (r *fakeProjectRepo) FindMemberByActor(_ context.Context, projectID, actorID uuid.UUID, agentID *uuid.UUID) (*projectdom.ProjectMember, error) {
	if agentID != nil {
		return r.FindMemberByAgent(context.Background(), projectID, *agentID)
	}
	return r.FindMemberByUserProject(context.Background(), actorID, projectID)
}

func (r *fakeProjectRepo) FindMemberByUserProject(_ context.Context, userID, projectID uuid.UUID) (*projectdom.ProjectMember, error) {
	return r.FindMember(context.Background(), projectID, userID)
}
func (r *fakeProjectRepo) FindMemberByID(_ context.Context, id uuid.UUID) (*projectdom.ProjectMember, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.members {
		if m.ID == id {
			return cloneMember(m), nil
		}
	}
	return nil, projectdom.ErrMemberNotFound
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

func (r *fakeProjectRepo) AddAgentMember(_ context.Context, _, _, _, _ uuid.UUID) error { return nil }
func (r *fakeProjectRepo) RemoveAgentMember(_ context.Context, _, _ uuid.UUID) error    { return nil }
func (r *fakeProjectRepo) UpdateMemberRoleByMemberID(_ context.Context, memberID, roleID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.members {
		if m.ID == memberID {
			m.ProjectRoleID = roleID
			return nil
		}
	}
	return projectdom.ErrMemberNotFound
}
func (r *fakeProjectRepo) RemoveMemberByMemberID(_ context.Context, memberID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, m := range r.members {
		if m.ID == memberID {
			delete(r.members, key)
			return nil
		}
	}
	return projectdom.ErrMemberNotFound
}

type projectPermStore struct {
	globalPerms  []authz.Permission
	projectPerms map[uuid.UUID][]authz.Permission
	userPerms    map[uuid.UUID]map[uuid.UUID][]authz.Permission // user_id -> project_id -> permissions
	agentPerms   map[uuid.UUID]map[uuid.UUID][]authz.Permission // project_id -> agent_id -> permissions
	agentRoles   map[uuid.UUID]map[uuid.UUID]string             // project_id -> agent_id -> role_name
}

func (s *projectPermStore) ListGlobalPermissions(context.Context, uuid.UUID) ([]authz.Permission, error) {
	return append([]authz.Permission(nil), s.globalPerms...), nil
}

func (s *projectPermStore) ListProjectPermissions(_ context.Context, userID uuid.UUID, projectID uuid.UUID) ([]authz.Permission, error) {
	if userMap, ok := s.userPerms[userID]; ok {
		if perms, ok := userMap[projectID]; ok {
			return append([]authz.Permission(nil), perms...), nil
		}
	}
	if s.projectPerms != nil {
		perms := s.projectPerms[projectID]
		return append([]authz.Permission(nil), perms...), nil
	}
	return nil, nil
}

func (s *projectPermStore) GetAgentProjectRoleName(_ context.Context, agentID, projectID uuid.UUID) (string, error) {
	if projMap, ok := s.agentRoles[projectID]; ok {
		if role, ok := projMap[agentID]; ok {
			return role, nil
		}
	}
	return "", fmt.Errorf("agent not found in project")
}

func (s *projectPermStore) ListAgentProjectPermissions(_ context.Context, agentID, projectID uuid.UUID) ([]authz.Permission, error) {
	if projMap, ok := s.agentPerms[projectID]; ok {
		return append([]authz.Permission(nil), projMap[agentID]...), nil
	}
	return nil, fmt.Errorf("agent permissions not found")
}

func buildProjectTestRouter(repo *fakeProjectRepo, store *projectPermStore) http.Handler {
	r, _ := buildProjectTestRouterWithTaskRepo(repo, store, newFakeTaskRepoIT())
	return r
}

func buildProjectTestRouterWithTaskRepo(repo *fakeProjectRepo, store *projectPermStore, taskRepo *fakeTaskRepo) (http.Handler, *fakeTaskRepo) {
	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	projectService := projectsvc.New(repo, taskRepo)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager:         tm,
		Authorizer:           authz.NewAuthorizer(store),
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, testCookieCfg),
		User:                 handler.NewUserHandler(userService),
		GlobalRole:           handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}, authz.NewAuthorizer(store)),
		Project:              handler.NewProjectHandler(projectService, authz.NewAuthorizer(store)),
		Task:                 handler.NewTaskHandler(tasksvc.New(taskRepo), sprintsvc.NewViewService(newFakeViewRepoIT()), tasksvc.NewActivityService(newFakeTaskActivityRepo(), &fakeActivityMemberRepo{}, nil)),
		Log:                  log,
	}), taskRepo
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

func serve(r http.Handler, req *http.Request) *httptest.ResponseRecorder {
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

func memberIDFromCreate(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode create member response: %v", err)
	}
	id, _ := env.Data["id"].(string)
	if id == "" {
		t.Fatal("missing member id")
	}
	return id
}

func TestIntegrationProjectManagement_AdminCRUD(t *testing.T) {
	repo := newFakeProjectRepo()
	store := &projectPermStore{
		globalPerms: []authz.Permission{
			authz.PermissionProjectsRead,
			authz.PermissionProjectsWrite,
			authz.PermissionProjectsCreate,
			authz.PermissionProjectsDelete,
		},
	}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	createReq := authedJSONReq(t.Context(), http.MethodPost, "/api/v1/projects", tok, map[string]any{
		"name":        "Project Alpha",
		"description": "first",
	})
	createW := serve(r, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	projectID := projectIDFromCreate(t, createW)

	listW := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/projects", tok, nil))
	if listW.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (%s)", listW.Code, listW.Body.String())
	}

	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/projects/"+projectID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}

	patchW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, "/api/v1/projects/"+projectID, tok, map[string]any{
		"name":        "Project Alpha Updated",
		"description": "updated",
	}))
	if patchW.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (%s)", patchW.Code, patchW.Body.String())
	}

	delW := serve(r, authedJSONReq(t.Context(), http.MethodDelete, "/api/v1/projects/"+projectID, tok, nil))
	if delW.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (%s)", delW.Code, delW.Body.String())
	}

	getDeletedW := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/projects/"+projectID, tok, nil))
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

	writeW := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/projects", tok, map[string]any{"name": "No Write"}))
	if writeW.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without projects.create, got %d (%s)", writeW.Code, writeW.Body.String())
	}
	if code := decodeErrorCode(t, writeW); code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN, got %q", code)
	}

	unauthW := serve(r, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/projects", nil))
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
				// The caller assigns a "developer" role granting tasks.read; the
				// PACA-4 grant ceiling requires the caller to hold what it hands
				// out, so it must itself have the task permissions.
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
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
	memberID := memberIDFromCreate(t, addMemberW)

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

	updateMemberURL := fmt.Sprintf("/api/v1/projects/%s/members/%s", projectID, memberID)
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

	removeMemberURL := fmt.Sprintf("/api/v1/projects/%s/members/%s", projectID, memberID)
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

func TestIntegrationProjectCreation_DefaultTaskRecords(t *testing.T) {
	repo := newFakeProjectRepo()
	taskRepo := newFakeTaskRepoIT()
	store := &projectPermStore{
		globalPerms: []authz.Permission{
			authz.PermissionProjectsRead,
			authz.PermissionProjectsWrite,
			authz.PermissionProjectsCreate,
		},
	}
	r, _ := buildProjectTestRouterWithTaskRepo(repo, store, taskRepo)
	tok := issueProjectToken(t, uuid.NewString())

	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/projects", tok, map[string]any{
		"name":        "Default Records Project",
		"description": "test",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	projectID := projectIDFromCreate(t, createW)

	// --- task types ---
	typesURL := fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)
	typesW := serve(r, authedJSONReq(t.Context(), http.MethodGet, typesURL, tok, nil))
	if typesW.Code != http.StatusOK {
		t.Fatalf("list task types: expected 200, got %d (%s)", typesW.Code, typesW.Body.String())
	}
	var typesEnv struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(typesW.Body).Decode(&typesEnv); err != nil {
		t.Fatalf("decode task types: %v", err)
	}
	const wantTypes = 4
	if got := len(typesEnv.Data.Items); got != wantTypes {
		t.Errorf("expected %d default task types, got %d", wantTypes, got)
	}
	gotTypeNames := map[string]bool{}
	for _, item := range typesEnv.Data.Items {
		name, _ := item["name"].(string)
		gotTypeNames[name] = true
	}
	for _, name := range []string{"Task", "Bug", "Story", "Epic"} {
		if !gotTypeNames[name] {
			t.Errorf("missing default task type %q", name)
		}
	}

	// "Task" should be the only type with is_default: true.
	for _, item := range typesEnv.Data.Items {
		name, _ := item["name"].(string)
		isDefault, _ := item["is_default"].(bool)
		if name == "Task" && !isDefault {
			t.Errorf("expected task type %q to have is_default=true", name)
		}
		if name != "Task" && isDefault {
			t.Errorf("expected task type %q to have is_default=false", name)
		}
	}

	// "Epic" should have is_system: true; others false.
	for _, item := range typesEnv.Data.Items {
		name, _ := item["name"].(string)
		isSystem, _ := item["is_system"].(bool)
		if name == "Epic" {
			if !isSystem {
				t.Errorf("expected task type %q to have is_system=true", name)
			}
		} else {
			if isSystem {
				t.Errorf("expected task type %q to have is_system=false", name)
			}
		}
	}

	// --- task statuses ---
	statusesURL := fmt.Sprintf("/api/v1/projects/%s/task-statuses", projectID)
	statusesW := serve(r, authedJSONReq(t.Context(), http.MethodGet, statusesURL, tok, nil))
	if statusesW.Code != http.StatusOK {
		t.Fatalf("list task statuses: expected 200, got %d (%s)", statusesW.Code, statusesW.Body.String())
	}
	var statusesEnv struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(statusesW.Body).Decode(&statusesEnv); err != nil {
		t.Fatalf("decode task statuses: %v", err)
	}
	const wantStatuses = 4
	if got := len(statusesEnv.Data.Items); got != wantStatuses {
		t.Errorf("expected %d default task statuses, got %d", wantStatuses, got)
	}
	gotStatusNames := map[string]bool{}
	for _, item := range statusesEnv.Data.Items {
		name, _ := item["name"].(string)
		gotStatusNames[name] = true
	}
	for _, name := range []string{"Backlog", "Todo", "In Progress", "Done"} {
		if !gotStatusNames[name] {
			t.Errorf("missing default task status %q", name)
		}
	}
}

// ---------------------------------------------------------------------------
// GetMyProjectPermissions integration tests
// ---------------------------------------------------------------------------

func TestIntegrationGetMyProjectPermissions_Success(t *testing.T) {
	repo := newFakeProjectRepo()
	projectID := uuid.New()
	roleID := uuid.New()
	userID := uuid.New()

	repo.projects[projectID] = &projectdom.Project{ID: projectID, Name: "Perms Project"}
	repo.roles[roleID] = &projectdom.ProjectRole{
		ID:          roleID,
		ProjectID:   &projectID,
		RoleName:    "editor",
		Permissions: map[string]any{"tasks.read": true, "tasks.write": true, "sprints.read": true},
	}
	repo.members[memberKey(projectID, userID)] = &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projectID,
		UserID:        userID,
		ProjectRoleID: roleID,
	}

	store := &projectPermStore{}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, userID.String())

	url := fmt.Sprintf("/api/v1/projects/%s/members/me/permissions", projectID)
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	perms, ok := env.Data["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected permissions map, got %T: %v", env.Data["permissions"], env.Data["permissions"])
	}
	for _, key := range []string{"tasks.read", "tasks.write", "sprints.read"} {
		if v, _ := perms[key].(bool); !v {
			t.Errorf("expected %q=true, got %v", key, perms[key])
		}
	}
}

func TestIntegrationGetMyProjectPermissions_NotMember(t *testing.T) {
	repo := newFakeProjectRepo()
	projectID := uuid.New()
	repo.projects[projectID] = &projectdom.Project{ID: projectID, Name: "Perms Project"}

	store := &projectPermStore{}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	url := fmt.Sprintf("/api/v1/projects/%s/members/me/permissions", projectID)
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "PROJECT_MEMBER_NOT_FOUND" {
		t.Fatalf("expected PROJECT_MEMBER_NOT_FOUND, got %q", code)
	}
}

func TestIntegrationGetMyProjectPermissions_Unauthenticated(t *testing.T) {
	repo := newFakeProjectRepo()
	projectID := uuid.New()
	repo.projects[projectID] = &projectdom.Project{ID: projectID, Name: "Perms Project"}

	store := &projectPermStore{}
	r := buildProjectTestRouter(repo, store)

	url := fmt.Sprintf("/api/v1/projects/%s/members/me/permissions", projectID)
	w := serve(r, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationGetMyProjectPermissions_BadProjectID(t *testing.T) {
	repo := newFakeProjectRepo()
	store := &projectPermStore{}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	url := "/api/v1/projects/not-a-uuid/members/me/permissions"
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationGetMyProjectPermissions_ProjectNotFound(t *testing.T) {
	repo := newFakeProjectRepo()
	store := &projectPermStore{}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	url := fmt.Sprintf("/api/v1/projects/%s/members/me/permissions", uuid.NewString())
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
	// User is not a member of a non-existent project → ErrMemberNotFound → 404.
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (%s)", w.Code, w.Body.String())
	}
	if code := decodeErrorCode(t, w); code != "PROJECT_MEMBER_NOT_FOUND" {
		t.Fatalf("expected PROJECT_MEMBER_NOT_FOUND, got %q", code)
	}
}

// ---------------------------------------------------------------------------
// Public project integration tests
// ---------------------------------------------------------------------------

func TestIntegrationProject_IsPublicField(t *testing.T) {
	repo := newFakeProjectRepo()
	store := &projectPermStore{
		globalPerms: []authz.Permission{
			authz.PermissionProjectsRead,
			authz.PermissionProjectsCreate,
		},
	}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	// Create a project with is_public: true.
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, "/api/v1/projects", tok, map[string]any{
		"name":      "Public Project",
		"is_public": true,
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}
	var createEnv struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(createW.Body).Decode(&createEnv); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if isPublic, _ := createEnv.Data["is_public"].(bool); !isPublic {
		t.Fatalf("expected is_public=true in create response, got %v", createEnv.Data["is_public"])
	}
	projectID, _ := createEnv.Data["id"].(string)

	// GET the project and verify is_public persists.
	getW := serve(r, authedJSONReq(t.Context(), http.MethodGet, "/api/v1/projects/"+projectID, tok, nil))
	if getW.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (%s)", getW.Code, getW.Body.String())
	}
	var getEnv struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(getW.Body).Decode(&getEnv); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if isPublic, _ := getEnv.Data["is_public"].(bool); !isPublic {
		t.Fatalf("expected is_public=true in get response, got %v", getEnv.Data["is_public"])
	}
}

func TestIntegrationProject_AnonymousAccess_PublicProject(t *testing.T) {
	repo := newFakeProjectRepo()
	projectID := uuid.New()
	repo.projects[projectID] = &projectdom.Project{
		ID:       projectID,
		Name:     "Public Project",
		IsPublic: true,
	}
	store := &projectPermStore{}
	r := buildProjectTestRouter(repo, store)

	// Anonymous GET (no Authorization header) on a public project should succeed.
	url := "/api/v1/projects/" + projectID.String()
	w := serve(r, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestIntegrationProject_AnonymousAccess_PrivateProject_Returns401(t *testing.T) {
	repo := newFakeProjectRepo()
	projectID := uuid.New()
	repo.projects[projectID] = &projectdom.Project{
		ID:       projectID,
		Name:     "Private Project",
		IsPublic: false,
	}
	store := &projectPermStore{}
	r := buildProjectTestRouter(repo, store)

	// Anonymous GET on a private project should return 401.
	url := "/api/v1/projects/" + projectID.String()
	w := serve(r, httptest.NewRequestWithContext(t.Context(), http.MethodGet, url, nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d (%s)", w.Code, w.Body.String())
	}
}
