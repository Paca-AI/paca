package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
)

// seedProjectAdminUser creates a user and assigns them a global role that grants
// all project, project-role and project-member permissions.
func seedProjectAdminUser(t *testing.T, env *e2eEnv, username, password string) {
	t.Helper()
	seedUser(t, env, username, password, "Project Admin")
	roleName := "PROJECT_ADMIN_" + uuid.NewString()
	if err := env.roleRepo.Create(env.ctx, &globalroledom.GlobalRole{
		ID:   uuid.New(),
		Name: roleName,
		Permissions: map[string]any{
			"projects.create":       true,
			"projects.read":         true,
			"projects.write":        true,
			"projects.delete":       true,
			"project.roles.read":    true,
			"project.roles.write":   true,
			"project.members.read":  true,
			"project.members.write": true,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create project-admin role: %v", err)
	}
	assignGlobalRolesByName(t, env, username, roleName)
}

// projectAdminLogin creates a fresh HTTP client with a cookie jar, logs in as
// the given user, and returns the client together with the access token value.
func projectAdminLogin(t *testing.T, env *e2eEnv, username, password string) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp := login(env.ctx, t, client, env.base, username, password)
	defer func() { _ = resp.Body.Close() }()
	token := cookieValue(resp, "access_token")
	return client, token
}

// createProjectViaAPI creates a project via the admin API and returns its ID.
func createProjectViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, name, description string) string {
	t.Helper()
	body := jsonBody(t, map[string]any{"name": name, "description": description})
	req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/projects", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	id, _ := data["id"].(string)
	return id
}

// createProjectRoleViaAPI creates a project-scoped role and returns its ID.
func createProjectRoleViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, roleName string) string {
	t.Helper()
	body := jsonBody(t, map[string]any{
		"role_name":   roleName,
		"permissions": map[string]any{"read": true},
	})
	url := fmt.Sprintf("%s/api/v1/projects/%s/roles", env.base, projectID)
	req := mustRequest(env.ctx, t, http.MethodPost, url, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	id, _ := data["id"].(string)
	return id
}

// ---------------------------------------------------------------------------
// Admin project CRUD
// ---------------------------------------------------------------------------

func TestE2EProjectManagement_AdminProjectCRUD(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "crud-admin", "crudpass1")
	client, token := projectAdminLogin(t, env, "crud-admin", "crudpass1")

	var projID string

	t.Run("create_project", func(t *testing.T) {
		projID = createProjectViaAPI(t, env, client, token, "My E2E Project", "A test project")
		if projID == "" {
			t.Fatal("expected non-empty project id")
		}
	})

	t.Run("get_project", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			env.base+"/api/v1/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if id, _ := data["id"].(string); id != projID {
			t.Errorf("expected id %q, got %q", projID, id)
		}
	})

	t.Run("list_projects", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/projects", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		total, _ := data["total"].(float64)
		if total < 1 {
			t.Errorf("expected total >= 1, got %v", total)
		}
	})

	t.Run("update_project", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"name":        "My E2E Project Updated",
			"description": "updated description",
		})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			env.base+"/api/v1/projects/"+projID, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if name, _ := data["name"].(string); name != "My E2E Project Updated" {
			t.Errorf("expected updated name, got %q", name)
		}
	})

	t.Run("delete_project", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete,
			env.base+"/api/v1/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("get_deleted_project_returns_not_found", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			env.base+"/api/v1/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "PROJECT_NOT_FOUND")
	})
}

// ---------------------------------------------------------------------------
// Unauthenticated access
// ---------------------------------------------------------------------------

func TestE2EProject_Unauthenticated(t *testing.T) {
	env := newE2EEnv(t)
	req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/projects", nil)
	resp := mustDo(t, env.client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

// ---------------------------------------------------------------------------
// Insufficient permissions
// ---------------------------------------------------------------------------

func TestE2EProject_InsufficientPermission(t *testing.T) {
	env := newE2EEnv(t)
	// A plain USER has no projects.create permission. They can list projects
	// (receiving an empty list) but cannot create one.
	seedUser(t, env, "plain-user", "plainpass1", "Plain User")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	loginResp := login(env.ctx, t, client, env.base, "plain-user", "plainpass1")
	token := cookieValue(loginResp, "access_token")
	_ = loginResp.Body.Close()

	t.Run("list_projects_returns_empty", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/projects", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		total, _ := data["total"].(float64)
		if total != 0 {
			t.Errorf("expected 0 projects for plain user, got %v", total)
		}
	})

	t.Run("create_project_forbidden", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": "should-fail", "description": ""})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/projects", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})
}

// ---------------------------------------------------------------------------
// Project role management
// ---------------------------------------------------------------------------

func TestE2EProjectRoles_FullLifecycle(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "roles-admin", "rolespass1")
	client, token := projectAdminLogin(t, env, "roles-admin", "rolespass1")
	projID := createProjectViaAPI(t, env, client, token, "roles-project-"+uuid.NewString(), "")

	var roleID string

	t.Run("create_role", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"role_name":   "viewer",
			"permissions": map[string]any{"read": true},
		})
		url := fmt.Sprintf("%s/api/v1/projects/%s/roles", env.base, projID)
		req := mustRequest(env.ctx, t, http.MethodPost, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if rn, _ := data["role_name"].(string); rn != "viewer" {
			t.Errorf("expected role_name 'viewer', got %q", rn)
		}
		roleID, _ = data["id"].(string)
	})

	t.Run("list_roles", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/roles", env.base, projID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		roles, ok := env2.Data.([]any)
		if !ok {
			t.Fatalf("expected roles array, got %T", env2.Data)
		}
		if len(roles) < 1 {
			t.Error("expected at least one role")
		}
	})

	t.Run("create_duplicate_role_conflict", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"role_name": "viewer"})
		url := fmt.Sprintf("%s/api/v1/projects/%s/roles", env.base, projID)
		req := mustRequest(env.ctx, t, http.MethodPost, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusConflict)
		assertErrorCode(t, resp, "PROJECT_ROLE_NAME_TAKEN")
	})

	t.Run("update_role", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"role_name":   "contributor",
			"permissions": map[string]any{"read": true, "write": true},
		})
		url := fmt.Sprintf("%s/api/v1/projects/%s/roles/%s", env.base, projID, roleID)
		req := mustRequest(env.ctx, t, http.MethodPatch, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if rn, _ := data["role_name"].(string); rn != "contributor" {
			t.Errorf("expected updated role_name 'contributor', got %q", rn)
		}
	})

	t.Run("delete_role", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/roles/%s", env.base, projID, roleID)
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})
}

// ---------------------------------------------------------------------------
// Delete role blocked when members still assigned
// ---------------------------------------------------------------------------

func TestE2EProjectRoles_DeleteRoleWithMembersConflict(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "roles-conflict-admin", "rcpass123")
	client, token := projectAdminLogin(t, env, "roles-conflict-admin", "rcpass123")
	projID := createProjectViaAPI(t, env, client, token, "roles-conflict-"+uuid.NewString(), "")
	roleID := createProjectRoleViaAPI(t, env, client, token, projID, "locked-role")

	// Seed a real user so the FK constraint on project_members is satisfied.
	seedUser(t, env, "locked-role-member", "memberpass1", "Locked Member")
	memberUser, err := env.userRepo.FindByUsername(env.ctx, "locked-role-member")
	if err != nil {
		t.Fatalf("find locked-role-member: %v", err)
	}

	// Seed a member directly via repo so the role cannot be deleted.
	projUUID, _ := uuid.Parse(projID)
	roleUUID, _ := uuid.Parse(roleID)
	if err := env.projectRepo.AddMember(context.Background(), &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projUUID,
		UserID:        memberUser.ID,
		ProjectRoleID: roleUUID,
	}); err != nil {
		t.Fatalf("seed project member: %v", err)
	}

	url := fmt.Sprintf("%s/api/v1/projects/%s/roles/%s", env.base, projID, roleID)
	req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusConflict)
	assertErrorCode(t, resp, "PROJECT_ROLE_HAS_MEMBERS")
}

// ---------------------------------------------------------------------------
// Project member management
// ---------------------------------------------------------------------------

func TestE2EProjectMembers_FullLifecycle(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "members-admin", "mbrpass1")
	client, token := projectAdminLogin(t, env, "members-admin", "mbrpass1")
	projID := createProjectViaAPI(t, env, client, token, "members-project-"+uuid.NewString(), "")
	roleID := createProjectRoleViaAPI(t, env, client, token, projID, "member-role")
	updatedRoleID := createProjectRoleViaAPI(t, env, client, token, projID, "member-role-updated")

	// Seed a separate user to add as a project member.
	memberUsername := "member-user-" + uuid.NewString()
	seedUser(t, env, memberUsername, "mbrpass1", "Member User")
	memberUser, err := env.userRepo.FindByUsername(env.ctx, memberUsername)
	if err != nil {
		t.Fatalf("find member user: %v", err)
	}
	memberUserID := memberUser.ID.String()

	membersURL := fmt.Sprintf("%s/api/v1/projects/%s/members", env.base, projID)

	var memberID string // project_member record UUID returned by the add_member endpoint

	t.Run("add_member", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodPost, membersURL,
			jsonBody(t, map[string]any{"user_id": memberUserID, "project_role_id": roleID}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if uid, _ := data["user_id"].(string); uid != memberUserID {
			t.Errorf("expected user_id %q, got %q", memberUserID, uid)
		}
		memberID, _ = data["id"].(string)
		if memberID == "" {
			t.Fatal("expected non-empty member id in response")
		}
	})

	t.Run("list_members", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, membersURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		members, ok := env2.Data.([]any)
		if !ok {
			t.Fatalf("expected members array, got %T", env2.Data)
		}
		if len(members) < 1 {
			t.Error("expected at least one member")
		}
	})

	t.Run("add_duplicate_member_conflict", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodPost, membersURL,
			jsonBody(t, map[string]any{"user_id": memberUserID, "project_role_id": roleID}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusConflict)
		assertErrorCode(t, resp, "PROJECT_MEMBER_ALREADY_ADDED")
	})

	t.Run("update_member_role", func(t *testing.T) {
		url := membersURL + "/" + memberID
		req := mustRequest(env.ctx, t, http.MethodPatch, url,
			jsonBody(t, map[string]any{"project_role_id": updatedRoleID}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if rid, _ := data["project_role_id"].(string); rid != updatedRoleID {
			t.Errorf("expected project_role_id %q, got %q", updatedRoleID, rid)
		}
	})

	t.Run("update_member_role_missing_member", func(t *testing.T) {
		url := membersURL + "/" + uuid.NewString()
		req := mustRequest(env.ctx, t, http.MethodPatch, url,
			jsonBody(t, map[string]any{"project_role_id": updatedRoleID}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "PROJECT_MEMBER_NOT_FOUND")
	})

	t.Run("remove_member", func(t *testing.T) {
		url := membersURL + "/" + memberID
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("remove_nonexistent_member_not_found", func(t *testing.T) {
		url := membersURL + "/" + memberID
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "PROJECT_MEMBER_NOT_FOUND")
	})
}

// ---------------------------------------------------------------------------
// Cross-resource: delete project cascades roles and members
// ---------------------------------------------------------------------------

func TestE2EProject_DeleteCascadesRolesAndMembers(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "cascade-admin", "cascadepass1")
	client, token := projectAdminLogin(t, env, "cascade-admin", "cascadepass1")
	projID := createProjectViaAPI(t, env, client, token, "cascade-project-"+uuid.NewString(), "")
	roleID := createProjectRoleViaAPI(t, env, client, token, projID, "cascade-role")

	// Seed a real user so the FK constraint on project_members is satisfied.
	seedUser(t, env, "cascade-member", "memberpass1", "Cascade Member")
	cascadeMember, err := env.userRepo.FindByUsername(env.ctx, "cascade-member")
	if err != nil {
		t.Fatalf("find cascade-member: %v", err)
	}

	projUUID, _ := uuid.Parse(projID)
	roleUUID, _ := uuid.Parse(roleID)
	if err := env.projectRepo.AddMember(context.Background(), &projectdom.ProjectMember{
		ID:            uuid.New(),
		ProjectID:     projUUID,
		UserID:        cascadeMember.ID,
		ProjectRoleID: roleUUID,
	}); err != nil {
		t.Fatalf("seed project member: %v", err)
	}

	// Delete the project — DB cascade constraints remove roles and members.
	req := mustRequest(env.ctx, t, http.MethodDelete,
		env.base+"/api/v1/projects/"+projID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	// Confirm the project is gone.
	req = mustRequest(env.ctx, t, http.MethodGet,
		env.base+"/api/v1/projects/"+projID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// Role-based access control workflow tests
// ---------------------------------------------------------------------------

// createProjectRoleWithPermsViaAPI creates a project-scoped role with arbitrary
// permissions and returns its ID.
func createProjectRoleWithPermsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, roleName string, permissions map[string]any) string {
	t.Helper()
	body := jsonBody(t, map[string]any{
		"role_name":   roleName,
		"permissions": permissions,
	})
	url := fmt.Sprintf("%s/api/v1/projects/%s/roles", env.base, projectID)
	req := mustRequest(env.ctx, t, http.MethodPost, url, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	id, _ := data["id"].(string)
	return id
}

// loginUser creates a fresh HTTP client, logs in, and returns the client plus
// access token extracted from the response cookie.
func loginUser(t *testing.T, env *e2eEnv, username, password string) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp := login(env.ctx, t, client, env.base, username, password)
	defer func() { _ = resp.Body.Close() }()
	return client, cookieValue(resp, "access_token")
}

// addMemberViaAPI adds a user as a project member using the given auth token.
func addMemberViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, userID, roleID string) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/members", env.base, projectID)
	req := mustRequest(env.ctx, t, http.MethodPost, url,
		jsonBody(t, map[string]any{"user_id": userID, "project_role_id": roleID}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
}

// TestE2EProject_ProjectViewerAccess verifies that a project member with a
// viewer-only role (projects.read) can GET the project but cannot update or
// delete it, and that the project appears in their accessible list.
func TestE2EProject_ProjectViewerAccess(t *testing.T) {
	env := newE2EEnv(t)

	seedProjectAdminUser(t, env, "viewer-access-admin", "adminpass1")
	adminClient, adminToken := projectAdminLogin(t, env, "viewer-access-admin", "adminpass1")
	projID := createProjectViaAPI(t, env, adminClient, adminToken, "viewer-access-project-"+uuid.NewString(), "")

	// Create a project role that only grants projects.read.
	viewerRoleID := createProjectRoleWithPermsViaAPI(t, env, adminClient, adminToken, projID, "read-only",
		map[string]any{"projects.read": true})

	// Seed a plain user and add them as a project member with the viewer role.
	seedUser(t, env, "viewer-access-user", "viewerpass1", "Viewer Access User")
	viewerUser, err := env.userRepo.FindByUsername(env.ctx, "viewer-access-user")
	if err != nil {
		t.Fatalf("find viewer user: %v", err)
	}
	addMemberViaAPI(t, env, adminClient, adminToken, projID, viewerUser.ID.String(), viewerRoleID)

	viewerClient, viewerToken := loginUser(t, env, "viewer-access-user", "viewerpass1")

	t.Run("viewer_can_get_project", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			env.base+"/api/v1/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp := mustDo(t, viewerClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if id, _ := data["id"].(string); id != projID {
			t.Errorf("expected project id %q, got %q", projID, id)
		}
	})

	t.Run("viewer_cannot_update_project", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": "hacked", "description": ""})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			env.base+"/api/v1/projects/"+projID, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp := mustDo(t, viewerClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("viewer_cannot_delete_project", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete,
			env.base+"/api/v1/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp := mustDo(t, viewerClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("project_appears_in_viewer_accessible_list", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			env.base+"/api/v1/projects", nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp := mustDo(t, viewerClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		total, _ := data["total"].(float64)
		if total < 1 {
			t.Errorf("expected viewer to see at least 1 accessible project, got %v", total)
		}
	})
}

// TestE2EProject_NonMemberForbidden verifies that an authenticated user who is
// not a project member and has no global projects.read cannot access any
// project-specific endpoint.
func TestE2EProject_NonMemberForbidden(t *testing.T) {
	env := newE2EEnv(t)

	seedProjectAdminUser(t, env, "non-member-admin", "adminpass2")
	adminClient, adminToken := projectAdminLogin(t, env, "non-member-admin", "adminpass2")
	projID := createProjectViaAPI(t, env, adminClient, adminToken, "non-member-project-"+uuid.NewString(), "")

	// Seed a plain user with no project membership.
	seedUser(t, env, "non-member-user", "nonmbrpass1", "Non Member User")
	nmClient, nmToken := loginUser(t, env, "non-member-user", "nonmbrpass1")

	t.Run("get_project_forbidden", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			env.base+"/api/v1/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+nmToken)
		resp := mustDo(t, nmClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("update_project_forbidden", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": "hacked"})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			env.base+"/api/v1/projects/"+projID, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+nmToken)
		resp := mustDo(t, nmClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("delete_project_forbidden", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete,
			env.base+"/api/v1/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+nmToken)
		resp := mustDo(t, nmClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("non_member_project_not_in_accessible_list", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			env.base+"/api/v1/projects", nil)
		req.Header.Set("Authorization", "Bearer "+nmToken)
		resp := mustDo(t, nmClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		total, _ := data["total"].(float64)
		if total != 0 {
			t.Errorf("expected non-member to see 0 projects, got %v", total)
		}
	})
}

// TestE2EProject_MemberRolePermissionsEnforced verifies that project members
// can only perform the operations their project role grants. A "manager" role
// (with members.write, roles.write, projects.write) is compared against a
// "viewer" role (projects.read only).
func TestE2EProject_MemberRolePermissionsEnforced(t *testing.T) {
	env := newE2EEnv(t)

	seedProjectAdminUser(t, env, "perm-test-admin", "adminpass3")
	adminClient, adminToken := projectAdminLogin(t, env, "perm-test-admin", "adminpass3")
	projID := createProjectViaAPI(t, env, adminClient, adminToken, "perm-test-project-"+uuid.NewString(), "")

	managerRoleID := createProjectRoleWithPermsViaAPI(t, env, adminClient, adminToken, projID, "proj-manager",
		map[string]any{
			"projects.read":         true,
			"projects.write":        true,
			"project.members.read":  true,
			"project.members.write": true,
			"project.roles.read":    true,
			"project.roles.write":   true,
		})
	viewerRoleID := createProjectRoleWithPermsViaAPI(t, env, adminClient, adminToken, projID, "proj-viewer",
		map[string]any{"projects.read": true})

	// Seed and add manager user.
	seedUser(t, env, "perm-mgr-user", "mgrpass1", "Perm Manager User")
	mgrUser, err := env.userRepo.FindByUsername(env.ctx, "perm-mgr-user")
	if err != nil {
		t.Fatalf("find manager user: %v", err)
	}
	addMemberViaAPI(t, env, adminClient, adminToken, projID, mgrUser.ID.String(), managerRoleID)

	// Seed and add viewer user.
	seedUser(t, env, "perm-viewer-user", "viewerpass2", "Perm Viewer User")
	viewerUser, err := env.userRepo.FindByUsername(env.ctx, "perm-viewer-user")
	if err != nil {
		t.Fatalf("find viewer user: %v", err)
	}
	addMemberViaAPI(t, env, adminClient, adminToken, projID, viewerUser.ID.String(), viewerRoleID)

	// Seed an extra user to use as the add/remove target.
	seedUser(t, env, "perm-extra-user", "extrapass1", "Extra User")
	extraUser, err := env.userRepo.FindByUsername(env.ctx, "perm-extra-user")
	if err != nil {
		t.Fatalf("find extra user: %v", err)
	}

	mgrClient, mgrToken := loginUser(t, env, "perm-mgr-user", "mgrpass1")
	viewerClient, viewerToken := loginUser(t, env, "perm-viewer-user", "viewerpass2")

	membersURL := fmt.Sprintf("%s/api/v1/projects/%s/members", env.base, projID)
	rolesURL := fmt.Sprintf("%s/api/v1/projects/%s/roles", env.base, projID)

	var extraMemberID string // project_member record UUID for extraUser

	t.Run("manager_can_update_project", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": "updated-by-manager", "description": "mgr update"})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			env.base+"/api/v1/projects/"+projID, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		resp := mustDo(t, mgrClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("viewer_cannot_update_project", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": "hacked-by-viewer", "description": ""})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			env.base+"/api/v1/projects/"+projID, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp := mustDo(t, viewerClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("manager_can_add_member", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodPost, membersURL,
			jsonBody(t, map[string]any{"user_id": extraUser.ID.String(), "project_role_id": viewerRoleID}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		resp := mustDo(t, mgrClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		extraMemberID, _ = data["id"].(string)
		if extraMemberID == "" {
			t.Fatal("expected non-empty member id in response")
		}
	})

	t.Run("viewer_cannot_add_member", func(t *testing.T) {
		// extraUser is already a member, but 403 is checked before the conflict.
		req := mustRequest(env.ctx, t, http.MethodPost, membersURL,
			jsonBody(t, map[string]any{"user_id": extraUser.ID.String(), "project_role_id": viewerRoleID}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp := mustDo(t, viewerClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("manager_can_remove_member", func(t *testing.T) {
		url := membersURL + "/" + extraMemberID
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		resp := mustDo(t, mgrClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("viewer_cannot_remove_member", func(t *testing.T) {
		// Target a random member UUID — authz check happens before existence check.
		url := membersURL + "/" + uuid.NewString()
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp := mustDo(t, viewerClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("manager_can_create_project_role", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"role_name":   "mgr-created-role",
			"permissions": map[string]any{"projects.read": true},
		})
		req := mustRequest(env.ctx, t, http.MethodPost, rolesURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		resp := mustDo(t, mgrClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
	})

	t.Run("viewer_cannot_create_project_role", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"role_name":   "viewer-attempted-role",
			"permissions": map[string]any{"projects.read": true},
		})
		req := mustRequest(env.ctx, t, http.MethodPost, rolesURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		resp := mustDo(t, viewerClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})
}

// TestE2EProject_GlobalReadSeesAllProjects verifies that a user with a global
// projects.read permission can see ALL projects (not just their own) in the
// list, as well as access individual project details.
func TestE2EProject_GlobalReadSeesAllProjects(t *testing.T) {
	env := newE2EEnv(t)

	// Admin A creates a project.
	seedProjectAdminUser(t, env, "global-read-admin-a", "adminpassA")
	clientA, tokenA := projectAdminLogin(t, env, "global-read-admin-a", "adminpassA")
	projID := createProjectViaAPI(t, env, clientA, tokenA, "global-read-project-"+uuid.NewString(), "")

	// Seed user B who has global projects.read but is NOT a member of A's project.
	seedUser(t, env, "global-read-user-b", "adminpassB", "Global Read User B")
	roleName := "GLOBAL_READ_" + uuid.NewString()
	if err := env.roleRepo.Create(env.ctx, &globalroledom.GlobalRole{
		ID:   uuid.New(),
		Name: roleName,
		Permissions: map[string]any{
			"projects.read": true,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create global-read role: %v", err)
	}
	assignGlobalRolesByName(t, env, "global-read-user-b", roleName)

	clientB, tokenB := loginUser(t, env, "global-read-user-b", "adminpassB")

	t.Run("global_read_user_can_list_all_projects", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/projects", nil)
		req.Header.Set("Authorization", "Bearer "+tokenB)
		resp := mustDo(t, clientB, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		found := false
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				if id, _ := m["id"].(string); id == projID {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("expected global-read user to see project %q in list", projID)
		}
	})

	t.Run("global_read_user_can_get_project_directly", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			env.base+"/api/v1/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+tokenB)
		resp := mustDo(t, clientB, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if id, _ := data["id"].(string); id != projID {
			t.Errorf("expected project id %q, got %q", projID, id)
		}
	})
}

// ---------------------------------------------------------------------------
// Default task records seeded on project creation
// ---------------------------------------------------------------------------

func TestE2EProjectCreation_DefaultTaskRecords(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "defaults-admin", "defaultspass1")
	client, token := projectAdminLogin(t, env, "defaults-admin", "defaultspass1")
	projID := createProjectViaAPI(t, env, client, token, "defaults-project-"+uuid.NewString(), "")

	t.Run("default_task_types", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/task-types", env.base, projID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 4 {
			t.Errorf("expected 4 default task types, got %d", len(items))
		}
		gotNames := map[string]bool{}
		for _, item := range items {
			m, _ := item.(map[string]any)
			name, _ := m["name"].(string)
			gotNames[name] = true
		}
		for _, name := range []string{"Task", "Bug", "Story", "Epic"} {
			if !gotNames[name] {
				t.Errorf("missing default task type %q", name)
			}
		}

		// "Task" should be the only type with is_default: true.
		for _, item := range items {
			m, _ := item.(map[string]any)
			name, _ := m["name"].(string)
			isDefault, _ := m["is_default"].(bool)
			if name == "Task" && !isDefault {
				t.Errorf("expected task type %q to have is_default=true", name)
			}
			if name != "Task" && isDefault {
				t.Errorf("expected task type %q to have is_default=false", name)
			}
		}

		// "Epic" should have is_system: true; others false.
		for _, item := range items {
			m, _ := item.(map[string]any)
			name, _ := m["name"].(string)
			isSystem, _ := m["is_system"].(bool)
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
	})

	t.Run("default_task_statuses", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/task-statuses", env.base, projID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 4 {
			t.Errorf("expected 4 default task statuses, got %d", len(items))
		}
		gotNames := map[string]bool{}
		for _, item := range items {
			m, _ := item.(map[string]any)
			name, _ := m["name"].(string)
			gotNames[name] = true
		}
		for _, name := range []string{"Backlog", "Todo", "In Progress", "Done"} {
			if !gotNames[name] {
				t.Errorf("missing default task status %q", name)
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Set-default task type
// ---------------------------------------------------------------------------

func TestE2ETaskTypes_SetDefault(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "setdefault-admin", "setdefaultpass1")
	client, token := projectAdminLogin(t, env, "setdefault-admin", "setdefaultpass1")
	projID := createProjectViaAPI(t, env, client, token, "setdefault-project-"+uuid.NewString(), "")

	// Fetch the default task types to find "Bug" and "Task" IDs.
	typesURL := fmt.Sprintf("%s/api/v1/projects/%s/task-types", env.base, projID)
	req := mustRequest(env.ctx, t, http.MethodGet, typesURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	var typesEnv envelope
	decodeJSON(t, resp, &typesEnv)
	typesData := assertDataMap(t, typesEnv)
	items, _ := typesData["items"].([]any)

	typeIDs := map[string]string{}
	for _, item := range items {
		m, _ := item.(map[string]any)
		name, _ := m["name"].(string)
		id, _ := m["id"].(string)
		typeIDs[name] = id
	}

	bugID, ok := typeIDs["Bug"]
	if !ok {
		t.Fatal("could not find Bug task type in default types")
	}

	t.Run("set_bug_as_default", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/task-types/%s/set-default", env.base, projID, bugID)
		req := mustRequest(env.ctx, t, http.MethodPut, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if isDefault, _ := data["is_default"].(bool); !isDefault {
			t.Errorf("expected is_default=true in response, got %v", data["is_default"])
		}
	})

	t.Run("only_one_default_after_switch", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, typesURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items2, _ := data["items"].([]any)

		defaultCount := 0
		for _, item := range items2 {
			m, _ := item.(map[string]any)
			if isDefault, _ := m["is_default"].(bool); isDefault {
				defaultCount++
				if id, _ := m["id"].(string); id != bugID {
					t.Errorf("expected default to be Bug (%s), got %s", bugID, id)
				}
			}
		}
		if defaultCount != 1 {
			t.Errorf("expected exactly 1 default type, got %d", defaultCount)
		}
	})

	t.Run("set_default_not_found_returns_404", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/task-types/%s/set-default", env.base, projID, uuid.NewString())
		req := mustRequest(env.ctx, t, http.MethodPut, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "TASK_TYPE_NOT_FOUND")
	})
}

// ---------------------------------------------------------------------------
// GetMyProjectPermissions e2e tests
// ---------------------------------------------------------------------------

// getMyProjectPermissionsViaAPI calls GET /api/v1/projects/:id/members/me/permissions
// and returns the decoded permissions map on success.
func getMyProjectPermissionsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string) map[string]any {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/members/me/permissions", env.base, projectID)
	req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	rawPerms := data["permissions"]
	perms, ok := rawPerms.(map[string]any)
	if !ok {
		t.Fatalf("expected data.permissions to be an object, got %T: %#v", rawPerms, rawPerms)
	}
	return perms
}

// TestE2EGetMyProjectPermissions_Success verifies that an authenticated project
// member receives their role's permissions from the endpoint.
func TestE2EGetMyProjectPermissions_Success(t *testing.T) {
	env := newE2EEnv(t)

	seedProjectAdminUser(t, env, "perms-admin", "permspass1")
	adminClient, adminToken := projectAdminLogin(t, env, "perms-admin", "permspass1")
	projID := createProjectViaAPI(t, env, adminClient, adminToken, "perms-project-"+uuid.NewString(), "")

	editorPerms := map[string]any{
		"tasks.read":   true,
		"tasks.write":  true,
		"sprints.read": true,
	}
	editorRoleID := createProjectRoleWithPermsViaAPI(t, env, adminClient, adminToken, projID, "editor", editorPerms)

	memberUsername := "perms-member-" + uuid.NewString()
	seedUser(t, env, memberUsername, "permspass1", "Perms Member")
	memberUser, err := env.userRepo.FindByUsername(env.ctx, memberUsername)
	if err != nil {
		t.Fatalf("find member user: %v", err)
	}
	addMemberViaAPI(t, env, adminClient, adminToken, projID, memberUser.ID.String(), editorRoleID)

	memberClient, memberToken := loginUser(t, env, memberUsername, "permspass1")
	perms := getMyProjectPermissionsViaAPI(t, env, memberClient, memberToken, projID)

	for _, key := range []string{"tasks.read", "tasks.write", "sprints.read"} {
		if v, _ := perms[key].(bool); !v {
			t.Errorf("expected permission %q=true, got %v", key, perms[key])
		}
	}
}

// TestE2EGetMyProjectPermissions_NotMember verifies that an authenticated user
// who is not a project member receives a 404 PROJECT_MEMBER_NOT_FOUND.
func TestE2EGetMyProjectPermissions_NotMember(t *testing.T) {
	env := newE2EEnv(t)

	seedProjectAdminUser(t, env, "perms-nm-admin", "permspass2")
	adminClient, adminToken := projectAdminLogin(t, env, "perms-nm-admin", "permspass2")
	projID := createProjectViaAPI(t, env, adminClient, adminToken, "perms-nm-project-"+uuid.NewString(), "")

	// Seed a plain user but do NOT add them as a member.
	seedUser(t, env, "perms-nm-user", "permspass2", "Perms Non-Member")
	nmClient, nmToken := loginUser(t, env, "perms-nm-user", "permspass2")

	url := fmt.Sprintf("%s/api/v1/projects/%s/members/me/permissions", env.base, projID)
	req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+nmToken)
	resp := mustDo(t, nmClient, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, resp, "PROJECT_MEMBER_NOT_FOUND")
}

// TestE2EGetMyProjectPermissions_Unauthenticated verifies that requests without
// a valid token receive a 401 Unauthorized.
func TestE2EGetMyProjectPermissions_Unauthenticated(t *testing.T) {
	env := newE2EEnv(t)

	seedProjectAdminUser(t, env, "perms-unauth-admin", "permspass3")
	adminClient, adminToken := projectAdminLogin(t, env, "perms-unauth-admin", "permspass3")
	projID := createProjectViaAPI(t, env, adminClient, adminToken, "perms-unauth-project-"+uuid.NewString(), "")

	url := fmt.Sprintf("%s/api/v1/projects/%s/members/me/permissions", env.base, projID)
	req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
	// No Authorization header.
	resp := mustDo(t, env.client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

// ---------------------------------------------------------------------------
// Public project e2e tests
// ---------------------------------------------------------------------------

func TestE2EProject_PublicProjectAnonymousAccess(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "pub-anon-admin", "pubpass1")
	adminClient, adminToken := projectAdminLogin(t, env, "pub-anon-admin", "pubpass1")

	// Create a public project.
	body := jsonBody(t, map[string]any{
		"name":      "public-project-" + uuid.NewString(),
		"is_public": true,
	})
	req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/projects", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp := mustDo(t, adminClient, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	projID, _ := data["id"].(string)
	if isPublic, _ := data["is_public"].(bool); !isPublic {
		t.Fatalf("expected is_public=true in create response")
	}

	// Anonymous GET (no auth) should succeed.
	getURL := fmt.Sprintf("%s/api/v1/projects/%s", env.base, projID)
	anonReq := mustRequest(env.ctx, t, http.MethodGet, getURL, nil)
	anonResp := mustDo(t, env.client, anonReq)
	defer func() { _ = anonResp.Body.Close() }()
	assertStatus(t, anonResp, http.StatusOK)
	var getEnv envelope
	decodeJSON(t, anonResp, &getEnv)
	getData := assertDataMap(t, getEnv)
	if isPublic, _ := getData["is_public"].(bool); !isPublic {
		t.Fatalf("expected is_public=true in anonymous get response")
	}
}

func TestE2EProject_PrivateProjectAnonymousAccessDenied(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "priv-anon-admin", "privpass1")
	adminClient, adminToken := projectAdminLogin(t, env, "priv-anon-admin", "privpass1")

	// Create a private project (default is_public: false).
	projID := createProjectViaAPI(t, env, adminClient, adminToken, "private-project-"+uuid.NewString(), "")

	// Anonymous GET should return 401.
	getURL := fmt.Sprintf("%s/api/v1/projects/%s", env.base, projID)
	anonReq := mustRequest(env.ctx, t, http.MethodGet, getURL, nil)
	anonResp := mustDo(t, env.client, anonReq)
	defer func() { _ = anonResp.Body.Close() }()
	assertStatus(t, anonResp, http.StatusUnauthorized)
}

func TestE2EProject_UpdateProjectVisibility(t *testing.T) {
	env := newE2EEnv(t)
	seedProjectAdminUser(t, env, "vis-admin", "vispass1")
	adminClient, adminToken := projectAdminLogin(t, env, "vis-admin", "vispass1")

	// Create a private project.
	projID := createProjectViaAPI(t, env, adminClient, adminToken, "vis-project-"+uuid.NewString(), "")

	// Anonymous access should fail.
	getURL := fmt.Sprintf("%s/api/v1/projects/%s", env.base, projID)
	anonReq := mustRequest(env.ctx, t, http.MethodGet, getURL, nil)
	anonResp := mustDo(t, env.client, anonReq)
	defer func() { _ = anonResp.Body.Close() }()
	assertStatus(t, anonResp, http.StatusUnauthorized)

	// PATCH to make it public.
	patchBody := jsonBody(t, map[string]any{"is_public": true})
	patchReq := mustRequest(env.ctx, t, http.MethodPatch, getURL, patchBody)
	patchReq.Header.Set("Content-Type", "application/json")
	patchReq.Header.Set("Authorization", "Bearer "+adminToken)
	patchResp := mustDo(t, adminClient, patchReq)
	defer func() { _ = patchResp.Body.Close() }()
	assertStatus(t, patchResp, http.StatusOK)
	var patchEnv envelope
	decodeJSON(t, patchResp, &patchEnv)
	patchData := assertDataMap(t, patchEnv)
	if isPublic, _ := patchData["is_public"].(bool); !isPublic {
		t.Fatalf("expected is_public=true after PATCH")
	}

	// Anonymous GET should now succeed.
	anonReq2 := mustRequest(env.ctx, t, http.MethodGet, getURL, nil)
	anonResp2 := mustDo(t, env.client, anonReq2)
	defer func() { _ = anonResp2.Body.Close() }()
	assertStatus(t, anonResp2, http.StatusOK)
}
