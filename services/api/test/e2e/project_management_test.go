package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	projectdom "github.com/paca/api/internal/domain/project"
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
		url := membersURL + "/" + memberUserID
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
		url := membersURL + "/" + memberUserID
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("remove_nonexistent_member_not_found", func(t *testing.T) {
		url := membersURL + "/" + memberUserID
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
		url := membersURL + "/" + extraUser.ID.String()
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+mgrToken)
		resp := mustDo(t, mgrClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("viewer_cannot_remove_member", func(t *testing.T) {
		// Target the manager user — authz check happens before existence check.
		url := membersURL + "/" + mgrUser.ID.String()
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
		if len(items) != 3 {
			t.Errorf("expected 3 default task types, got %d", len(items))
		}
		gotNames := map[string]bool{}
		for _, item := range items {
			m, _ := item.(map[string]any)
			name, _ := m["name"].(string)
			gotNames[name] = true
		}
		for _, name := range []string{"Task", "Bug", "Story"} {
			if !gotNames[name] {
				t.Errorf("missing default task type %q", name)
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
