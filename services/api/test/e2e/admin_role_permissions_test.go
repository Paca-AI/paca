package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
)

// newLoggedInClient logs username in and returns a cookie-jar client carrying
// the resulting session, so a test can act as that user.
func newLoggedInClient(t *testing.T, env *e2eEnv, username, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp := login(env.ctx, t, client, env.base, username, password)
	_ = resp.Body.Close()
	return client
}

// doJSON sends method+path (with an optional JSON body) as client and returns
// the HTTP status code and the decoded response envelope.
func doJSON(t *testing.T, env *e2eEnv, client *http.Client, method, path string, body any) (int, envelope) {
	t.Helper()
	var payload *bytes.Buffer
	if body != nil {
		payload = jsonBody(t, body)
	}
	req := mustRequest(env.ctx, t, method, env.base+path, payload)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()

	// A 204 or an error page has no JSON envelope; the status alone is what
	// callers then look at.
	var out envelope
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func createRoleWithPermissions(t *testing.T, env *e2eEnv, name string, perms map[string]any) *globalroledom.GlobalRole {
	t.Helper()
	role := &globalroledom.GlobalRole{
		ID:          uuid.New(),
		Name:        name,
		Permissions: perms,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := env.roleRepo.Create(env.ctx, role); err != nil {
		t.Fatalf("create role %q: %v", name, err)
	}
	return role
}

func roleNameOf(t *testing.T, env *e2eEnv, username string) string {
	t.Helper()
	u, err := env.userRepo.FindByUsername(env.ctx, username)
	if err != nil {
		t.Fatalf("find user %q: %v", username, err)
	}
	return u.Role
}

// TestBuiltinAdminRoleStoredPermissionsAreEnforced is the regression test for
// the bug where a user holding the built-in ADMIN global role could still
// change users' roles and edit global roles after those permissions had been
// removed from the ADMIN role. Authorization used to union a hardcoded
// permission set keyed on the role *name* (carried in the JWT) with the
// permissions the role row actually stores, so removing a permission from the
// ADMIN role had no effect. Only the stored permissions may grant access.
func TestBuiltinAdminRoleStoredPermissionsAreEnforced(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const (
		adminName  = "ops-admin"
		victimName = "victim"
		password   = "supersecret"
	)
	seedUser(t, env, adminName, password, "Ops Admin")
	seedUser(t, env, victimName, password, "Victim")
	assignGlobalRolesByName(t, env, adminName, "ADMIN")

	// Strip the ADMIN role down to read-only user access: no users.write, no
	// global_roles.*, no agents.*. This is what an operator does in the Global
	// Roles UI when they want ADMIN to stop managing roles.
	adminRole, err := env.roleRepo.FindByName(env.ctx, "ADMIN")
	if err != nil {
		t.Fatalf("find ADMIN role: %v", err)
	}
	adminRole.Permissions = map[string]any{"users.read": true}
	if err := env.roleRepo.Update(env.ctx, adminRole); err != nil {
		t.Fatalf("strip ADMIN permissions: %v", err)
	}

	victim, err := env.userRepo.FindByUsername(env.ctx, victimName)
	if err != nil {
		t.Fatalf("find victim: %v", err)
	}
	victimPath := "/api/v1/admin/users/" + victim.ID.String()
	someAgent := uuid.NewString()

	client := newLoggedInClient(t, env, adminName, password)

	t.Run("still_allowed_what_the_role_stores", func(t *testing.T) {
		if got, _ := doJSON(t, env, client, http.MethodGet, "/api/v1/admin/users", nil); got != http.StatusOK {
			t.Errorf("GET /admin/users: want 200 (role stores users.read), got %d", got)
		}
	})

	forbidden := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"list_global_roles", http.MethodGet, "/api/v1/admin/global-roles", nil},
		{"create_global_role", http.MethodPost, "/api/v1/admin/global-roles", map[string]any{
			"name": "SNEAKY", "permissions": map[string]any{"*": true},
		}},
		{"update_global_role", http.MethodPatch, "/api/v1/admin/global-roles/" + adminRole.ID.String(), map[string]any{
			"name": "ADMIN", "permissions": map[string]any{"*": true},
		}},
		{"delete_global_role", http.MethodDelete, "/api/v1/admin/global-roles/" + uuid.NewString(), nil},
		{"assign_user_global_role", http.MethodPut, victimPath + "/global-roles", map[string]any{
			"role_ids": []string{adminRole.ID.String()},
		}},
		{"edit_user_profile", http.MethodPatch, victimPath, map[string]any{"full_name": "Renamed"}},
		{"create_user", http.MethodPost, "/api/v1/admin/users", map[string]any{
			"username": "newbie", "password": "supersecret", "full_name": "Newbie",
		}},
		{"reset_user_password", http.MethodPatch, victimPath + "/password", map[string]any{"new_password": "anothersecret"}},
		{"delete_user", http.MethodDelete, victimPath, nil},
		{"list_global_agents", http.MethodGet, "/api/v1/admin/agents", nil},
		{"bind_agent_global_role", http.MethodPut, "/api/v1/admin/agents/" + someAgent + "/global-role", map[string]any{
			"global_role_id": adminRole.ID.String(),
		}},
		{"unbind_agent_global_role", http.MethodDelete, "/api/v1/admin/agents/" + someAgent + "/global-role", nil},
	}
	for _, tc := range forbidden {
		t.Run("forbidden_"+tc.name, func(t *testing.T) {
			if got, _ := doJSON(t, env, client, tc.method, tc.path, tc.body); got != http.StatusForbidden {
				t.Errorf("%s %s: want 403 (ADMIN role no longer stores that permission), got %d", tc.method, tc.path, got)
			}
		})
	}

	t.Run("nothing_was_modified", func(t *testing.T) {
		if got := roleNameOf(t, env, victimName); got != "USER" {
			t.Errorf("victim role changed to %q despite every attempt being forbidden", got)
		}
		if _, err := env.roleRepo.FindByName(env.ctx, "SNEAKY"); err == nil {
			t.Error("SNEAKY role was created despite being forbidden")
		}
		stored, err := env.roleRepo.FindByName(env.ctx, "ADMIN")
		if err != nil {
			t.Fatalf("find ADMIN role: %v", err)
		}
		if _, escalated := stored.Permissions["*"]; escalated || len(stored.Permissions) != 1 {
			t.Errorf("ADMIN role permissions changed to %v; the update must have been refused", stored.Permissions)
		}
	})

	t.Run("permission_listing_matches_stored_role", func(t *testing.T) {
		status, out := doJSON(t, env, client, http.MethodGet, "/api/v1/users/me/global-permissions", nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d", status)
		}
		perms, _ := assertDataMap(t, out)["permissions"].([]any)
		if len(perms) != 1 || perms[0] != "users.read" {
			t.Errorf("global-permissions = %v, want exactly [users.read]", perms)
		}
	})
}

// TestUserRoleAssignmentIsItsOwnPrivilege covers the second path to the same
// outcome. POST /admin/users and PATCH /admin/users/{id} used to accept a
// "role" and were gated only by users.write, so a caller lacking
// global_roles.assign could still hand out (or take) any global role —
// SUPER_ADMIN included. Now they edit the profile only and refuse a role
// outright; the role changes solely through PUT /admin/users/{id}/global-roles,
// which requires global_roles.assign.
func TestUserRoleAssignmentIsItsOwnPrivilege(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const (
		editorName   = "user-editor"
		assignerName = "role-assigner"
		victimName   = "victim"
		password     = "supersecret"
	)
	createRoleWithPermissions(t, env, "USER_EDITOR", map[string]any{"users.read": true, "users.write": true})
	createRoleWithPermissions(t, env, "ROLE_ASSIGNER", map[string]any{"global_roles.assign": true})

	seedUser(t, env, editorName, password, "Editor")
	seedUser(t, env, assignerName, password, "Assigner")
	seedUser(t, env, victimName, password, "Victim")
	assignGlobalRolesByName(t, env, editorName, "USER_EDITOR")
	assignGlobalRolesByName(t, env, assignerName, "ROLE_ASSIGNER")

	victim, err := env.userRepo.FindByUsername(env.ctx, victimName)
	if err != nil {
		t.Fatalf("find victim: %v", err)
	}
	superAdmin, err := env.roleRepo.FindByName(env.ctx, "SUPER_ADMIN")
	if err != nil {
		t.Fatalf("find SUPER_ADMIN: %v", err)
	}
	editorRole, err := env.roleRepo.FindByName(env.ctx, "USER_EDITOR")
	if err != nil {
		t.Fatalf("find USER_EDITOR: %v", err)
	}
	victimPath := "/api/v1/admin/users/" + victim.ID.String()

	t.Run("users_write_edits_the_profile_only", func(t *testing.T) {
		client := newLoggedInClient(t, env, editorName, password)

		if got, _ := doJSON(t, env, client, http.MethodPatch, victimPath, map[string]any{"full_name": "Renamed"}); got != http.StatusOK {
			t.Errorf("rename: want 200, got %d", got)
		}
		// Any role in the body is refused — even the user's current one — so a
		// client can never believe a role change went through.
		for _, role := range []string{"SUPER_ADMIN", "USER"} {
			if got, _ := doJSON(t, env, client, http.MethodPatch, victimPath, map[string]any{"full_name": "Again", "role": role}); got != http.StatusBadRequest {
				t.Errorf("PATCH with role %q: want 400, got %d", role, got)
			}
		}
		if got := roleNameOf(t, env, victimName); got != "USER" {
			t.Errorf("victim role = %q, want USER", got)
		}

		if got, _ := doJSON(t, env, client, http.MethodPost, "/api/v1/admin/users", map[string]any{
			"username": "plain", "password": "supersecret", "full_name": "Plain",
		}); got != http.StatusCreated {
			t.Errorf("create user: want 201, got %d", got)
		}
		if got := roleNameOf(t, env, "plain"); got != "USER" {
			t.Errorf("new user role = %q, want the default USER", got)
		}
		if got, _ := doJSON(t, env, client, http.MethodPost, "/api/v1/admin/users", map[string]any{
			"username": "backdoor", "password": "supersecret", "full_name": "Backdoor", "role": "SUPER_ADMIN",
		}); got != http.StatusBadRequest {
			t.Errorf("create with role: want 400, got %d", got)
		}
		if _, err := env.userRepo.FindByUsername(env.ctx, "backdoor"); err == nil {
			t.Error("backdoor user was created despite the role being refused")
		}
	})

	t.Run("users_write_alone_cannot_assign_a_role", func(t *testing.T) {
		client := newLoggedInClient(t, env, editorName, password)
		if got, _ := doJSON(t, env, client, http.MethodPut, victimPath+"/global-roles", map[string]any{
			"role_ids": []string{superAdmin.ID.String()},
		}); got != http.StatusForbidden {
			t.Errorf("PUT global-roles without global_roles.assign: want 403, got %d", got)
		}
		if got := roleNameOf(t, env, victimName); got != "USER" {
			t.Errorf("victim role = %q, want USER", got)
		}
	})

	t.Run("global_roles_assign_changes_the_role", func(t *testing.T) {
		client := newLoggedInClient(t, env, assignerName, password)
		if got, _ := doJSON(t, env, client, http.MethodPut, victimPath+"/global-roles", map[string]any{
			"role_ids": []string{editorRole.ID.String()},
		}); got != http.StatusOK {
			t.Errorf("PUT global-roles with global_roles.assign: want 200, got %d", got)
		}
		if got := roleNameOf(t, env, victimName); got != "USER_EDITOR" {
			t.Errorf("victim role = %q, want USER_EDITOR", got)
		}
	})
}

// TestGlobalAgentRoleBindingIsItsOwnPrivilege is the agent counterpart: a global
// agent's role decides what the agent may do, so binding one needs
// global_roles.assign on top of agents.write, and create/update no longer
// carry it.
func TestGlobalAgentRoleBindingIsItsOwnPrivilege(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const password = "supersecret"
	createRoleWithPermissions(t, env, "AGENT_EDITOR", map[string]any{"agents.read": true, "agents.write": true})
	createRoleWithPermissions(t, env, "AGENT_ROLE_MANAGER", map[string]any{
		"agents.read": true, "agents.write": true, "global_roles.assign": true,
	})
	createRoleWithPermissions(t, env, "ROLE_ASSIGNER", map[string]any{"global_roles.assign": true})
	target := createRoleWithPermissions(t, env, "BOT_ROLE", map[string]any{"users.read": true})

	for username, role := range map[string]string{
		"agent-editor":  "AGENT_EDITOR",
		"role-manager":  "AGENT_ROLE_MANAGER",
		"assigner-only": "ROLE_ASSIGNER",
	} {
		seedUser(t, env, username, password, username)
		assignGlobalRolesByName(t, env, username, role)
	}
	editor := newLoggedInClient(t, env, "agent-editor", password)
	manager := newLoggedInClient(t, env, "role-manager", password)
	assignerOnly := newLoggedInClient(t, env, "assigner-only", password)

	agentBody := func(extra map[string]any) map[string]any {
		body := map[string]any{
			"name": "Bot", "handle": "bot-" + uuid.NewString()[:8],
			"agent_type": "acp", "acp_provider": "claude-code",
		}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}

	status, created := doJSON(t, env, editor, http.MethodPost, "/api/v1/admin/agents", agentBody(nil))
	if status != http.StatusCreated {
		t.Fatalf("agents.write can create a global agent: want 201, got %d (%s)", status, created.Error)
	}
	agentID, _ := assertDataMap(t, created)["id"].(string)
	agentPath := "/api/v1/admin/agents/" + agentID
	rolePath := agentPath + "/global-role"
	bindBody := map[string]any{"global_role_id": target.ID.String()}

	boundRole := func() any {
		status, got := doJSON(t, env, manager, http.MethodGet, agentPath, nil)
		if status != http.StatusOK {
			t.Fatalf("get agent: want 200, got %d", status)
		}
		return assertDataMap(t, got)["global_role_id"]
	}

	t.Run("create_and_update_refuse_a_role", func(t *testing.T) {
		if got, _ := doJSON(t, env, manager, http.MethodPost, "/api/v1/admin/agents",
			agentBody(map[string]any{"global_role_id": target.ID.String()})); got != http.StatusBadRequest {
			t.Errorf("create with global_role_id: want 400, got %d", got)
		}
		if got, _ := doJSON(t, env, manager, http.MethodPatch, agentPath, bindBody); got != http.StatusBadRequest {
			t.Errorf("update with global_role_id: want 400, got %d", got)
		}
		if got := boundRole(); got != nil {
			t.Errorf("agent has role %v, want none", got)
		}
	})

	t.Run("binding_needs_both_permissions", func(t *testing.T) {
		for name, client := range map[string]*http.Client{"agents.write only": editor, "global_roles.assign only": assignerOnly} {
			if got, _ := doJSON(t, env, client, http.MethodPut, rolePath, bindBody); got != http.StatusForbidden {
				t.Errorf("PUT as %s: want 403, got %d", name, got)
			}
		}
		if got := boundRole(); got != nil {
			t.Errorf("agent has role %v after refused attempts, want none", got)
		}
	})

	t.Run("holding_both_binds_and_unbinds", func(t *testing.T) {
		if got, _ := doJSON(t, env, manager, http.MethodPut, rolePath, bindBody); got != http.StatusOK {
			t.Fatalf("PUT with both permissions: want 200, got %d", got)
		}
		if got := boundRole(); got != target.ID.String() {
			t.Errorf("agent role = %v, want %s", got, target.ID)
		}

		if got, _ := doJSON(t, env, editor, http.MethodDelete, rolePath, nil); got != http.StatusForbidden {
			t.Errorf("unbinding without global_roles.assign: want 403, got %d", got)
		}
		if got, _ := doJSON(t, env, manager, http.MethodDelete, rolePath, nil); got != http.StatusOK {
			t.Errorf("unbinding with both permissions: want 200, got %d", got)
		}
		if got := boundRole(); got != nil {
			t.Errorf("agent role = %v after unbinding, want none", got)
		}
	})

	t.Run("unknown_role_is_rejected", func(t *testing.T) {
		if got, _ := doJSON(t, env, manager, http.MethodPut, rolePath, map[string]any{"global_role_id": uuid.NewString()}); got != http.StatusNotFound {
			t.Errorf("binding a nonexistent role: want 404, got %d", got)
		}
	})
}
