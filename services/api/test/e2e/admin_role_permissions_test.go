package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/google/uuid"
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

func roleNameOf(t *testing.T, env *e2eEnv, username string) string {
	t.Helper()
	u, err := env.userRepo.FindByUsername(env.ctx, username)
	if err != nil {
		t.Fatalf("find user %q: %v", username, err)
	}
	return u.RoleClaim()
}

// policyDocument returns the stored policy of the platform role named name.
func policyDocument(t *testing.T, env *e2eEnv, name string) string {
	t.Helper()
	var policy string
	if err := env.db.Get(&policy, `SELECT policy::text FROM roles WHERE name = $1 AND project_id IS NULL`, name); err != nil {
		t.Fatalf("read policy of %q: %v", name, err)
	}
	return policy
}

// TestBuiltinAdminRoleStoredPermissionsAreEnforced is the regression test for
// the bug where a user holding the built-in ADMIN role could still change
// users' roles and edit roles after those permissions had been removed from
// the ADMIN role. Authorization used to union a hardcoded permission set keyed
// on the role *name* (carried in the JWT) with the permissions the role row
// actually stores, so removing a permission from the ADMIN role had no effect.
// Only the stored policy may grant access.
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
	assignPlatformRole(t, env, adminName, "ADMIN")

	// Strip the ADMIN role down to read-only user access: no users:write, no
	// agents:*, nothing else. Whatever the stored policy no longer allows must
	// stop working at once, with the "ADMIN" name in the caller's token
	// granting nothing in its place.
	setPlatformRoleActions(t, env, "ADMIN", "users:read")
	strippedPolicy := policyDocument(t, env, "ADMIN")
	adminRoleID := platformRoleID(t, env, "ADMIN")

	victim, err := env.userRepo.FindByUsername(env.ctx, victimName)
	if err != nil {
		t.Fatalf("find victim: %v", err)
	}
	victimPath := "/api/v1/admin/users/" + victim.ID.String()
	someAgent := uuid.NewString()

	client := newLoggedInClient(t, env, adminName, password)

	t.Run("still_allowed_what_the_role_stores", func(t *testing.T) {
		if got, _ := doJSON(t, env, client, http.MethodGet, "/api/v1/admin/users", nil); got != http.StatusOK {
			t.Errorf("GET /admin/users: want 200 (role stores users:read), got %d", got)
		}
	})

	forbidden := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"list_roles", http.MethodGet, "/api/v1/admin/roles", nil},
		{"create_role", http.MethodPost, "/api/v1/admin/roles", map[string]any{
			"name": "SNEAKY", "policy": readOnlyPolicy(),
		}},
		{"update_role", http.MethodPut, "/api/v1/admin/roles/" + adminRoleID.String(), map[string]any{
			"name": "ADMIN", "policy": readOnlyPolicy(),
		}},
		{"delete_role", http.MethodDelete, "/api/v1/admin/roles/" + uuid.NewString(), nil},
		{"assign_user_roles", http.MethodPut, victimPath + "/roles", map[string]any{
			"role_ids": []string{adminRoleID.String()},
		}},
		{"edit_user_profile", http.MethodPatch, victimPath, map[string]any{"full_name": "Renamed"}},
		{"create_user", http.MethodPost, "/api/v1/admin/users", map[string]any{
			"username": "newbie", "password": "supersecret", "full_name": "Newbie",
		}},
		{"reset_user_password", http.MethodPatch, victimPath + "/password", map[string]any{"new_password": "anothersecret"}},
		{"delete_user", http.MethodDelete, victimPath, nil},
		{"list_global_agents", http.MethodGet, "/api/v1/admin/agents", nil},
		{"assign_agent_roles", http.MethodPut, "/api/v1/admin/agents/" + someAgent + "/roles", map[string]any{
			"role_ids": []string{adminRoleID.String()},
		}},
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
		if platformRoleExists(t, env, "SNEAKY") {
			t.Error("SNEAKY role was created despite being forbidden")
		}
		if got := policyDocument(t, env, "ADMIN"); got != strippedPolicy {
			t.Errorf("ADMIN policy changed to %s; the update must have been refused", got)
		}
	})

	t.Run("permission_listing_matches_stored_role", func(t *testing.T) {
		status, out := doJSON(t, env, client, http.MethodGet, "/api/v1/users/me/global-permissions", nil)
		if status != http.StatusOK {
			t.Fatalf("expected 200, got %d", status)
		}
		perms, _ := assertDataMap(t, out)["actions"].([]any)
		if len(perms) != 1 || perms[0] != "users:read" {
			t.Errorf("global-permissions actions = %v, want exactly [users:read]", perms)
		}
	})
}

// TestUserRoleAssignmentIsItsOwnPrivilege covers the second path to the same
// outcome. POST /admin/users and PATCH /admin/users/{id} edit the profile
// only: a "role" in the body is ignored, and a user's roles change solely
// through PUT /admin/users/{id}/roles, which requires roles:assign on the
// resource of every role added or removed (iam:PassRole semantics: what the
// caller holds themselves is not asked).
func TestUserRoleAssignmentIsItsOwnPrivilege(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const (
		editorName   = "user-editor"
		assignerName = "role-assigner"
		victimName   = "victim"
		password     = "supersecret"
	)
	editorRoleID := createPlatformRole(t, env, "USER_EDITOR", "users:read", "users:write")
	createPlatformRole(t, env, "ROLE_ASSIGNER", "roles:assign", "users:read")

	seedUser(t, env, editorName, password, "Editor")
	seedUser(t, env, assignerName, password, "Assigner")
	seedUser(t, env, victimName, password, "Victim")
	assignPlatformRole(t, env, editorName, "USER_EDITOR")
	assignPlatformRole(t, env, assignerName, "ROLE_ASSIGNER")

	victim, err := env.userRepo.FindByUsername(env.ctx, victimName)
	if err != nil {
		t.Fatalf("find victim: %v", err)
	}
	superAdminID := platformRoleID(t, env, "SUPER_ADMIN")
	victimPath := "/api/v1/admin/users/" + victim.ID.String()

	t.Run("users_write_edits_the_profile_only", func(t *testing.T) {
		client := newLoggedInClient(t, env, editorName, password)

		if got, _ := doJSON(t, env, client, http.MethodPatch, victimPath, map[string]any{"full_name": "Renamed"}); got != http.StatusOK {
			t.Errorf("rename: want 200, got %d", got)
		}
		// A role in the body is ignored, never applied.
		for _, role := range []string{"SUPER_ADMIN", "USER"} {
			doJSON(t, env, client, http.MethodPatch, victimPath, map[string]any{"full_name": "Again", "role": role})
			if got := roleNameOf(t, env, victimName); got != "USER" {
				t.Errorf("PATCH with role %q changed the victim's role to %q", role, got)
			}
		}

		if got, _ := doJSON(t, env, client, http.MethodPost, "/api/v1/admin/users", map[string]any{
			"username": "plain", "password": "supersecret", "full_name": "Plain",
		}); got != http.StatusCreated {
			t.Errorf("create user: want 201, got %d", got)
		}
		if got := roleNameOf(t, env, "plain"); got != "USER" {
			t.Errorf("new user role = %q, want the default USER", got)
		}
		doJSON(t, env, client, http.MethodPost, "/api/v1/admin/users", map[string]any{
			"username": "backdoor", "password": "supersecret", "full_name": "Backdoor", "role": "SUPER_ADMIN",
		})
		if got := roleNameOf(t, env, "backdoor"); got == "SUPER_ADMIN" {
			t.Error("a role in the create body was applied")
		}
	})

	t.Run("users_write_alone_cannot_assign_a_role", func(t *testing.T) {
		client := newLoggedInClient(t, env, editorName, password)
		if got, _ := doJSON(t, env, client, http.MethodPut, victimPath+"/roles", map[string]any{
			"role_ids": []string{superAdminID.String()},
		}); got != http.StatusForbidden {
			t.Errorf("PUT roles without roles:assign: want 403, got %d", got)
		}
		if got := roleNameOf(t, env, victimName); got != "USER" {
			t.Errorf("victim role = %q, want USER", got)
		}
	})

	t.Run("roles_assign_hands_out_roles_the_assigner_does_not_hold", func(t *testing.T) {
		// PassRole semantics: roles:assign on every role is root-equivalent, and
		// the assigner need not hold what the role grants.
		client := newLoggedInClient(t, env, assignerName, password)
		if got, _ := doJSON(t, env, client, http.MethodPut, victimPath+"/roles", map[string]any{
			"role_ids": []string{superAdminID.String()},
		}); got != http.StatusOK {
			t.Errorf("assigning SUPER_ADMIN with roles:assign on every role: want 200, got %d", got)
		}
		if got := roleNameOf(t, env, victimName); got != "SUPER_ADMIN" {
			t.Errorf("victim role = %q, want SUPER_ADMIN", got)
		}
	})

	t.Run("roles_assign_changes_the_role", func(t *testing.T) {
		client := newLoggedInClient(t, env, assignerName, password)
		if got, _ := doJSON(t, env, client, http.MethodPut, victimPath+"/roles", map[string]any{
			"role_ids": []string{editorRoleID.String()},
		}); got != http.StatusOK {
			t.Errorf("PUT roles with roles:assign: want 200, got %d", got)
		}
		if got := roleNameOf(t, env, victimName); got != "USER_EDITOR" {
			t.Errorf("victim role = %q, want USER_EDITOR", got)
		}
	})
}

// TestGlobalAgentRoleBindingIsItsOwnPrivilege is the agent counterpart: a global
// agent's roles decide what the agent may do, so replacing them needs
// roles:assign on top of agents:write, and create/update do not carry them.
func TestGlobalAgentRoleBindingIsItsOwnPrivilege(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const password = "supersecret"
	createPlatformRole(t, env, "AGENT_EDITOR", "agents:read", "agents:write")
	createPlatformRole(t, env, "AGENT_ROLE_MANAGER", "agents:read", "agents:write", "roles:read", "roles:assign", "users:read")
	createPlatformRole(t, env, "ROLE_ASSIGNER", "roles:assign", "users:read")
	targetID := createPlatformRole(t, env, "BOT_ROLE", "users:read")

	for username, role := range map[string]string{
		"agent-editor":  "AGENT_EDITOR",
		"role-manager":  "AGENT_ROLE_MANAGER",
		"assigner-only": "ROLE_ASSIGNER",
	} {
		seedUser(t, env, username, password, username)
		assignPlatformRole(t, env, username, role)
	}
	editor := newLoggedInClient(t, env, "agent-editor", password)
	manager := newLoggedInClient(t, env, "role-manager", password)
	assignerOnly := newLoggedInClient(t, env, "assigner-only", password)

	status, created := doJSON(t, env, editor, http.MethodPost, "/api/v1/admin/agents", map[string]any{
		"name": "Bot", "handle": "bot-" + uuid.NewString()[:8],
		"agent_type": "acp", "acp_provider": "claude-code",
	})
	if status != http.StatusCreated {
		t.Fatalf("agents:write can create a global agent: want 201, got %d (%s)", status, created.Error)
	}
	agentID, _ := assertDataMap(t, created)["id"].(string)
	agentPath := "/api/v1/admin/agents/" + agentID
	rolePath := agentPath + "/roles"
	bindBody := map[string]any{"role_ids": []string{targetID.String()}}

	boundRoles := func() []string {
		status, got := doJSON(t, env, manager, http.MethodGet, rolePath, nil)
		if status != http.StatusOK {
			t.Fatalf("get agent roles: want 200, got %d", status)
		}
		roles, _ := got.Data.([]any)
		var names []string
		for _, r := range roles {
			m, _ := r.(map[string]any)
			name, _ := m["name"].(string)
			names = append(names, name)
		}
		return names
	}

	// Creating an agent never carries a role, but it does start with the default
	// one (the same role a new user gets), so "unchanged" is that role, not none.
	startingRoles := boundRoles()
	if len(startingRoles) != 1 || startingRoles[0] != "USER" {
		t.Fatalf("new agent's roles = %v, want the default [USER]", startingRoles)
	}

	t.Run("binding_needs_both_permissions", func(t *testing.T) {
		for name, client := range map[string]*http.Client{"agents:write only": editor, "roles:assign only": assignerOnly} {
			if got, _ := doJSON(t, env, client, http.MethodPut, rolePath, bindBody); got != http.StatusForbidden {
				t.Errorf("PUT as %s: want 403, got %d", name, got)
			}
		}
		if got := boundRoles(); len(got) != 1 || got[0] != "USER" {
			t.Errorf("agent roles = %v after refused attempts, want them unchanged", got)
		}
	})

	t.Run("holding_both_replaces_the_roles", func(t *testing.T) {
		if got, _ := doJSON(t, env, manager, http.MethodPut, rolePath, bindBody); got != http.StatusOK {
			t.Fatalf("PUT with both permissions: want 200, got %d", got)
		}
		if got := boundRoles(); len(got) != 1 || got[0] != "BOT_ROLE" {
			t.Errorf("agent roles = %v, want [BOT_ROLE]", got)
		}

		if got, _ := doJSON(t, env, editor, http.MethodPut, rolePath, map[string]any{"role_ids": []string{}}); got != http.StatusForbidden {
			t.Errorf("clearing the roles without roles:assign: want 403, got %d", got)
		}
		if got, _ := doJSON(t, env, manager, http.MethodPut, rolePath, map[string]any{"role_ids": []string{}}); got != http.StatusOK {
			t.Errorf("clearing the roles with both permissions: want 200, got %d", got)
		}
		if got := boundRoles(); len(got) != 0 {
			t.Errorf("agent roles = %v after clearing, want none", got)
		}
	})

	t.Run("unknown_role_is_rejected", func(t *testing.T) {
		got, out := doJSON(t, env, manager, http.MethodPut, rolePath, map[string]any{"role_ids": []string{uuid.NewString()}})
		if got != http.StatusUnprocessableEntity || out.ErrorCode != "ROLE_NOT_ATTACHABLE" {
			t.Errorf("binding a nonexistent role: want 422 ROLE_NOT_ATTACHABLE, got %d %q", got, out.ErrorCode)
		}
	})
}
