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

	"github.com/google/uuid"

	projectdom "github.com/Paca-AI/api/internal/domain/project"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	activitysvc "github.com/Paca-AI/api/internal/service/activity"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
)

type fakeProjectRepo struct {
	mu sync.RWMutex

	projects map[uuid.UUID]*projectdom.Project
	members  map[string]*projectdom.ProjectMember
}

func newFakeProjectRepo() *fakeProjectRepo {
	return &fakeProjectRepo{
		projects: make(map[uuid.UUID]*projectdom.Project),
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

func (r *fakeProjectRepo) Create(_ context.Context, p *projectdom.Project, setup projectdom.ProjectSetup) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, existing := range r.projects {
		if existing.Name == p.Name {
			return projectdom.ErrNameTaken
		}
	}
	r.projects[p.ID] = cloneProject(p)
	if setup.Creator != nil {
		roles := make([]roledom.Summary, 0, 1)
		roles = append(roles, roledom.Summary{ID: uuid.New(), Name: setup.CreatorRole})
		r.members[memberKey(p.ID, *setup.Creator)] = &projectdom.ProjectMember{
			ID: uuid.New(), ProjectID: p.ID, UserID: *setup.Creator, Roles: roles,
		}
	}
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

func (r *fakeProjectRepo) UpdateJevConfig(_ context.Context, projectID uuid.UUID, apiKeySecret, baseURL, model string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.projects[projectID]
	if !ok {
		return projectdom.ErrNotFound
	}
	p.JevAPIKeySecret = apiKeySecret
	p.JevBaseURL = baseURL
	p.JevModel = model
	return nil
}

func (r *fakeProjectRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.projects[id]; !ok {
		return projectdom.ErrNotFound
	}
	delete(r.projects, id)
	for key, m := range r.members {
		if m.ProjectID == id {
			delete(r.members, key)
		}
	}
	return nil
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

func (r *fakeProjectRepo) CountDistinctAgentsByProjects(_ context.Context, projectIDs []uuid.UUID) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	wanted := make(map[uuid.UUID]struct{}, len(projectIDs))
	for _, id := range projectIDs {
		wanted[id] = struct{}{}
	}
	agents := make(map[uuid.UUID]struct{})
	for _, m := range r.members {
		if _, ok := wanted[m.ProjectID]; !ok {
			continue
		}
		if m.AgentID != nil {
			agents[*m.AgentID] = struct{}{}
		}
	}
	return int64(len(agents)), nil
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

func (r *fakeProjectRepo) AddMember(_ context.Context, m *projectdom.ProjectMember, roleIDs []uuid.UUID, _ *uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	k := memberKey(m.ProjectID, m.UserID)
	if _, ok := r.members[k]; ok {
		return projectdom.ErrMemberAlreadyAdded
	}
	stored := cloneMember(m)
	stored.Roles = nil
	for _, id := range roleIDs {
		stored.Roles = append(stored.Roles, roledom.Summary{ID: id, Name: "role"})
	}
	r.members[k] = stored
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

func (r *fakeProjectRepo) AddAgentMember(_ context.Context, _, _, _ uuid.UUID, _ []uuid.UUID, _ *uuid.UUID) error {
	return nil
}
func (r *fakeProjectRepo) RemoveAgentMember(_ context.Context, _, _ uuid.UUID) error { return nil }
func (r *fakeProjectRepo) UpdateMemberDescription(_ context.Context, memberID uuid.UUID, description string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.members {
		if m.ID == memberID {
			m.Description = description
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
	globalPerms      []iam.Action
	projectPerms     map[uuid.UUID][]iam.Action
	userPerms        map[uuid.UUID]map[uuid.UUID][]iam.Action // user_id -> project_id -> permissions
	agentPerms       map[uuid.UUID]map[uuid.UUID][]iam.Action // project_id -> agent_id -> permissions
	agentGlobalPerms map[uuid.UUID][]iam.Action               // agent_id -> permissions (via its own global role)
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
	projectService := projectsvc.New(repo, taskRepo, nil)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	return router.New(router.Deps{
		TokenManager:         tm,
		IAM:                  newIAM(store),
		RolePolicies:         emptyRolePolicies{},
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, testCookieCfg),
		User:                 handler.NewUserHandler(userService),
		Project:              handler.NewProjectHandler(projectService, newIAM(store)),
		Task:                 handler.NewTaskHandler(tasksvc.New(taskRepo), sprintsvc.NewViewService(newFakeViewRepoIT(), newFakeSprintRepoIT(), taskRepo, nil), tasksvc.NewActivityService(activitysvc.New(newFakeTaskActivityRepo(), &fakeActivityMemberRepo{}, nil), taskRepo, &fakeActivityMemberRepo{})),
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
		globalPerms: []iam.Action{
			iam.ActionProjectsRead,
			iam.ActionProjectsWrite,
			iam.ActionProjectsCreate,
			iam.ActionProjectsDelete,
		},
		projectPerms: map[uuid.UUID][]iam.Action{},
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

	// projectsvc.Service.Create (exercised above via the real router) already
	// added this user as the new project's "Admin" member in fakeProjectRepo
	// — production authorizes the PATCH/DELETE below off that project-scoped
	// grant. projectPermStore is a hand-fed stub with no wiring back to
	// fakeProjectRepo's membership table, so it has to be told the same thing
	// explicitly. Before the GHSA-hjcj-373w-vq8m fix to hasPermissionsForActor,
	// this test passed those calls via the *global* projects.write/delete set
	// above leaking into the project-scoped check instead — i.e. it was
	// inadvertently asserting the vulnerability, not real project membership.
	store.projectPerms[uuid.MustParse(projectID)] = []iam.Action{actionAll}

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
	store := &projectPermStore{globalPerms: []iam.Action{iam.ActionProjectsRead}}
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

func TestIntegrationProjectMembers_Flow(t *testing.T) {
	repo := newFakeProjectRepo()
	projectID := uuid.New()
	repo.projects[projectID] = &projectdom.Project{ID: projectID, Name: "Proj", CreatedAt: time.Now()}

	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]iam.Action{
			projectID: {
				iam.ActionProjectMembersRead,
				iam.ActionProjectMembersWrite,
				iam.ActionRolesAssign, // role_ids on add-member are gated on roles:assign
			},
		},
	}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, uuid.NewString())

	// The roles API itself is covered by role_api_iam_test.go; this flow only
	// needs a role id to hand to the member.
	roleID := uuid.NewString()

	memberUserID := uuid.New()
	membersURL := fmt.Sprintf("/api/v1/projects/%s/members", projectID)
	addMemberW := serve(r, authedJSONReq(t.Context(), http.MethodPost, membersURL, tok, map[string]any{
		"user_id":  memberUserID,
		"role_ids": []string{roleID},
	}))
	if addMemberW.Code != http.StatusCreated {
		t.Fatalf("add member: expected 201, got %d (%s)", addMemberW.Code, addMemberW.Body.String())
	}
	memberID := memberIDFromCreate(t, addMemberW)

	dupMemberW := serve(r, authedJSONReq(t.Context(), http.MethodPost, membersURL, tok, map[string]any{
		"user_id":  memberUserID,
		"role_ids": []string{roleID},
	}))
	if dupMemberW.Code != http.StatusConflict {
		t.Fatalf("duplicate member: expected 409, got %d (%s)", dupMemberW.Code, dupMemberW.Body.String())
	}
	if code := decodeErrorCode(t, dupMemberW); code != "PROJECT_MEMBER_ALREADY_ADDED" {
		t.Fatalf("expected PROJECT_MEMBER_ALREADY_ADDED, got %q", code)
	}

	// Editing a member changes its description only; roles are replaced through
	// PUT .../members/{id}/roles.
	updateMemberURL := fmt.Sprintf("/api/v1/projects/%s/members/%s", projectID, memberID)
	updateMemberW := serve(r, authedJSONReq(t.Context(), http.MethodPatch, updateMemberURL, tok, map[string]any{
		"description": "backend",
	}))
	if updateMemberW.Code != http.StatusOK {
		t.Fatalf("update member: expected 200, got %d (%s)", updateMemberW.Code, updateMemberW.Body.String())
	}
	var updateMemberEnv struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(updateMemberW.Body).Decode(&updateMemberEnv); err != nil {
		t.Fatalf("decode update member response: %v", err)
	}
	if got, _ := updateMemberEnv.Data["description"].(string); got != "backend" {
		t.Fatalf("expected description %q, got %q", "backend", got)
	}
	roles, _ := updateMemberEnv.Data["roles"].([]any)
	if len(roles) != 1 {
		t.Fatalf("expected the member's roles list, got %v", updateMemberEnv.Data["roles"])
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
}

func TestIntegrationProjectCreation_DefaultTaskRecords(t *testing.T) {
	repo := newFakeProjectRepo()
	taskRepo := newFakeTaskRepoIT()
	store := &projectPermStore{
		globalPerms: []iam.Action{
			iam.ActionProjectsRead,
			iam.ActionProjectsWrite,
			iam.ActionProjectsCreate,
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
	repo.members[memberKey(projectID, userID)] = &projectdom.ProjectMember{
		ID:        uuid.New(),
		ProjectID: projectID,
		UserID:    userID,
		Roles:     []roledom.Summary{{ID: roleID, Name: "editor"}},
	}

	// The caller's role in the project, as migration 000064 attaches it.
	store := &projectPermStore{userPerms: map[uuid.UUID]map[uuid.UUID][]iam.Action{
		userID: {projectID: {iam.ActionTasksRead, iam.ActionTasksWrite, iam.ActionSprintsRead}},
	}}
	r := buildProjectTestRouter(repo, store)
	tok := issueProjectToken(t, userID.String())

	url := fmt.Sprintf("/api/v1/projects/%s/members/me/permissions", projectID)
	w := serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}

	var env struct {
		Data struct {
			Actions []string `json:"actions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got := map[string]bool{}
	for _, a := range env.Data.Actions {
		got[a] = true
	}
	for _, want := range []iam.Action{iam.ActionTasksRead, iam.ActionTasksWrite, iam.ActionSprintsRead} {
		if !got[string(want)] {
			t.Errorf("expected %q in actions, got %v", want, env.Data.Actions)
		}
	}
	if got[string(iam.ActionDocsRead)] || got["tasks.read"] {
		t.Errorf("unexpected entries in %v", env.Data.Actions)
	}
}

// A caller with no grant in the project (not a member, or no such project)
// simply has no actions there: 200 with an empty list, revealing nothing.
func assertNoProjectActions(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	var env struct {
		Data struct {
			Actions []string `json:"actions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.Actions == nil || len(env.Data.Actions) != 0 {
		t.Fatalf("expected an empty actions list, got %v", env.Data.Actions)
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
	assertNoProjectActions(t, serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil)))
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
	assertNoProjectActions(t, serve(r, authedJSONReq(t.Context(), http.MethodGet, url, tok, nil)))
}

// ---------------------------------------------------------------------------
// Public project integration tests
// ---------------------------------------------------------------------------

func TestIntegrationProject_IsPublicField(t *testing.T) {
	repo := newFakeProjectRepo()
	store := &projectPermStore{
		globalPerms: []iam.Action{
			iam.ActionProjectsRead,
			iam.ActionProjectsCreate,
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

// emptyRolePolicies is the role lookup behind the set-default guard: every role
// it is asked about exists and grants nothing, so any caller may hand it out.
type emptyRolePolicies struct{}

func (emptyRolePolicies) RolePolicies(_ context.Context, ids []uuid.UUID) (map[uuid.UUID][]byte, error) {
	out := make(map[uuid.UUID][]byte, len(ids))
	for _, id := range ids {
		out[id] = []byte(`{"version":"2026-10-01","statements":[]}`)
	}
	return out, nil
}
