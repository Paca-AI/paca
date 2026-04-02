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
			"projects.read":         true,
			"projects.write":        true,
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
	req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/admin/projects", body)
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
			env.base+"/api/v1/admin/projects/"+projID, nil)
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
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/projects", nil)
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
			env.base+"/api/v1/admin/projects/"+projID, body)
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
			env.base+"/api/v1/admin/projects/"+projID, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("get_deleted_project_returns_not_found", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			env.base+"/api/v1/admin/projects/"+projID, nil)
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
	req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/projects", nil)
	resp := mustDo(t, env.client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

// ---------------------------------------------------------------------------
// Insufficient permissions
// ---------------------------------------------------------------------------

func TestE2EProject_InsufficientPermission(t *testing.T) {
	env := newE2EEnv(t)
	// A plain USER should not have projects.read permission unless their global
	// role grants it. Seed a user with only the default USER role.
	seedUser(t, env, "plain-user", "plainpass1", "Plain User")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	loginResp := login(env.ctx, t, client, env.base, "plain-user", "plainpass1")
	_ = loginResp.Body.Close()

	req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/projects", nil)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusForbidden)
	assertErrorCode(t, resp, "FORBIDDEN")
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
		env.base+"/api/v1/admin/projects/"+projID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)

	// Confirm the project is gone.
	req = mustRequest(env.ctx, t, http.MethodGet,
		env.base+"/api/v1/admin/projects/"+projID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusNotFound)
}
