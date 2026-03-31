package e2e_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// Admin helpers
// ---------------------------------------------------------------------------

// adminLogin logs in with the seeded admin credentials and returns the client
// whose cookie jar holds valid access/refresh tokens.
func adminLogin(t *testing.T, env *e2eEnv) *http.Client {
	t.Helper()
	seedUser(t, env, "e2e-admin", "adminpass1", "E2E Admin")
	assignGlobalRolesByName(t, env, "e2e-admin", "ADMIN")

	_ = login(env.ctx, t, env.client, env.base, "e2e-admin", "adminpass1")
	return env.client
}

// adminBearerToken logs in with admin credentials and returns the raw
// access-token string for use in Authorization headers.
func adminBearerToken(t *testing.T, env *e2eEnv, username, password string) string {
	t.Helper()
	resp := login(env.ctx, t, env.client, env.base, username, password)
	defer func() { _ = resp.Body.Close() }()
	for _, c := range resp.Cookies() {
		if c.Name == "access_token" {
			return c.Value
		}
	}
	t.Fatal("access_token cookie not found after login")
	return ""
}

// createUserViaAPI calls POST /api/v1/admin/users and returns the created user's ID.
func createUserViaAPI(t *testing.T, env *e2eEnv, adminToken, username, password, fullName string) string {
	t.Helper()
	body := jsonBody(t, map[string]any{
		"username":  username,
		"password":  password,
		"full_name": fullName,
	})
	req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/admin/users", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp := mustDo(t, env.client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)

	var env2 envelope
	if err := json.NewDecoder(resp.Body).Decode(&env2); err != nil {
		t.Fatalf("decode create-user response: %v", err)
	}
	data, ok := env2.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", env2.Data)
	}
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty id in create-user response")
	}
	return id
}

// ---------------------------------------------------------------------------
// Admin user management
// ---------------------------------------------------------------------------

func TestUserManagement_AdminCRUD(t *testing.T) {
	env := newE2EEnv(t)
	seedUser(t, env, "crud-admin", "adminpass1", "CRUD Admin")
	assignGlobalRolesByName(t, env, "crud-admin", "ADMIN")
	adminToken := adminBearerToken(t, env, "crud-admin", "adminpass1")

	t.Run("list_users_empty", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/users", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var envResp envelope
		decodeJSON(t, resp, &envResp)
		data := assertDataMap(t, envResp)
		total, _ := data["total"].(float64)
		if total < 1 { // at least the admin user itself
			t.Errorf("expected total >= 1, got %v", total)
		}
	})

	t.Run("create_user", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"username":  "new-u1",
			"password":  "newpass123",
			"full_name": "New User One",
		})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/admin/users", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)

		var envResp envelope
		decodeJSON(t, resp, &envResp)
		data := assertDataMap(t, envResp)
		if data["username"] != "new-u1" {
			t.Errorf("expected username=new-u1, got %v", data["username"])
		}
		// Admin-created users must have must_change_password=true.
		if data["must_change_password"] != true {
			t.Errorf("expected must_change_password=true, got %v", data["must_change_password"])
		}
	})

	t.Run("create_user_duplicate_username", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"username":  "new-u1",
			"password":  "newpass123",
			"full_name": "Duplicate",
		})
		req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/admin/users", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusConflict)
		assertErrorCode(t, resp, "USER_USERNAME_TAKEN")
	})

	t.Run("get_user_by_id", func(t *testing.T) {
		id := createUserViaAPI(t, env, adminToken, "get-target", "pass1234", "Get Target")

		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/users/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var envResp envelope
		decodeJSON(t, resp, &envResp)
		data := assertDataMap(t, envResp)
		if data["id"] != id {
			t.Errorf("expected id=%s, got %v", id, data["id"])
		}
		if data["username"] != "get-target" {
			t.Errorf("expected username=get-target, got %v", data["username"])
		}
	})

	t.Run("get_user_by_id_not_found", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/users/00000000-0000-0000-0000-000000000099", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "USER_NOT_FOUND")
	})

	t.Run("update_user_full_name", func(t *testing.T) {
		id := createUserViaAPI(t, env, adminToken, "update-target", "pass1234", "Original Name")

		body := jsonBody(t, map[string]string{"full_name": "Updated Name"})
		req := mustRequest(env.ctx, t, http.MethodPatch, env.base+"/api/v1/admin/users/"+id, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var envResp envelope
		decodeJSON(t, resp, &envResp)
		data := assertDataMap(t, envResp)
		if data["full_name"] != "Updated Name" {
			t.Errorf("expected full_name='Updated Name', got %v", data["full_name"])
		}
	})

	t.Run("delete_user", func(t *testing.T) {
		id := createUserViaAPI(t, env, adminToken, "delete-target", "pass1234", "Delete Target")

		req := mustRequest(env.ctx, t, http.MethodDelete, env.base+"/api/v1/admin/users/"+id, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)

		// Verify it's gone.
		getReq := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/users/"+id, nil)
		getReq.Header.Set("Authorization", "Bearer "+adminToken)
		getResp := mustDo(t, env.client, getReq)
		defer func() { _ = getResp.Body.Close() }()
		assertStatus(t, getResp, http.StatusNotFound)
	})

	t.Run("admin_route_requires_auth", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/users", nil)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
		assertErrorCode(t, resp, "AUTH_MISSING_TOKEN")
	})
}

// ---------------------------------------------------------------------------
// Self-service: GetMe / UpdateMe
// ---------------------------------------------------------------------------

func TestUserManagement_SelfService(t *testing.T) {
	env := newE2EEnv(t)
	seedUser(t, env, "self-user", "selfpass1", "Self User")

	t.Run("get_me", func(t *testing.T) {
		resp := login(env.ctx, t, env.client, env.base, "self-user", "selfpass1")
		_ = resp.Body.Close()

		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me", nil)
		resp2 := mustDo(t, env.client, req)
		defer func() { _ = resp2.Body.Close() }()
		assertStatus(t, resp2, http.StatusOK)

		var envResp envelope
		decodeJSON(t, resp2, &envResp)
		data := assertDataMap(t, envResp)
		if data["username"] != "self-user" {
			t.Errorf("expected username=self-user, got %v", data["username"])
		}
	})

	t.Run("get_me_unauthenticated", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me", nil)
		resp := mustDo(t, &http.Client{}, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnauthorized)
	})

	t.Run("update_me", func(t *testing.T) {
		resp := login(env.ctx, t, env.client, env.base, "self-user", "selfpass1")
		_ = resp.Body.Close()

		body := jsonBody(t, map[string]string{"full_name": "Updated Self"})
		req := mustRequest(env.ctx, t, http.MethodPatch, env.base+"/api/v1/users/me", body)
		req.Header.Set("Content-Type", "application/json")
		resp2 := mustDo(t, env.client, req)
		defer func() { _ = resp2.Body.Close() }()
		assertStatus(t, resp2, http.StatusOK)

		var envResp envelope
		decodeJSON(t, resp2, &envResp)
		data := assertDataMap(t, envResp)
		if data["full_name"] != "Updated Self" {
			t.Errorf("expected full_name='Updated Self', got %v", data["full_name"])
		}
	})
}

// ---------------------------------------------------------------------------
// ChangeMyPassword
// ---------------------------------------------------------------------------

func TestUserManagement_ChangeMyPassword(t *testing.T) {
	env := newE2EEnv(t)
	seedUser(t, env, "pwchange-user", "oldpass123", "PW Change User")

	t.Run("change_password_success", func(t *testing.T) {
		loginResp := login(env.ctx, t, env.client, env.base, "pwchange-user", "oldpass123")
		_ = loginResp.Body.Close()

		body := jsonBody(t, map[string]string{
			"current_password": "oldpass123",
			"new_password":     "newpass456",
		})
		req := mustRequest(env.ctx, t, http.MethodPatch, env.base+"/api/v1/users/me/password", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)

		// Can log in with the new password.
		loginResp2 := login(env.ctx, t, env.client, env.base, "pwchange-user", "newpass456")
		_ = loginResp2.Body.Close()
	})

	t.Run("change_password_wrong_current", func(t *testing.T) {
		loginResp := login(env.ctx, t, env.client, env.base, "pwchange-user", "newpass456")
		_ = loginResp.Body.Close()

		body := jsonBody(t, map[string]string{
			"current_password": "notthepassword",
			"new_password":     "doesntmatter123",
		})
		req := mustRequest(env.ctx, t, http.MethodPatch, env.base+"/api/v1/users/me/password", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusUnprocessableEntity)
		assertErrorCode(t, resp, "USER_INVALID_CURRENT_PASSWORD")
	})

	t.Run("change_password_new_too_short", func(t *testing.T) {
		loginResp := login(env.ctx, t, env.client, env.base, "pwchange-user", "newpass456")
		_ = loginResp.Body.Close()

		body := jsonBody(t, map[string]string{
			"current_password": "newpass456",
			"new_password":     "short",
		})
		req := mustRequest(env.ctx, t, http.MethodPatch, env.base+"/api/v1/users/me/password", body)
		req.Header.Set("Content-Type", "application/json")
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "BAD_REQUEST")
	})
}

// ---------------------------------------------------------------------------
// Admin reset password + MustChangePassword flow
// ---------------------------------------------------------------------------

func TestUserManagement_AdminResetPassword(t *testing.T) {
	env := newE2EEnv(t)
	seedUser(t, env, "reset-admin", "adminpass1", "Reset Admin")
	assignGlobalRolesByName(t, env, "reset-admin", "ADMIN")
	adminToken := adminBearerToken(t, env, "reset-admin", "adminpass1")

	t.Run("reset_password_sets_must_change", func(t *testing.T) {
		seedUser(t, env, "pw-reset-target", "original123", "Reset Target")
		id := createUserViaAPI(t, env, adminToken, "pw-to-reset", "pass1234", "PW To Reset")

		body := jsonBody(t, map[string]string{"new_password": "brandnew123"})
		req := mustRequest(env.ctx, t, http.MethodPatch, env.base+"/api/v1/admin/users/"+id+"/password", body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		resp := mustDo(t, env.client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)

		// The user must be able to log in with the new password.
		userLoginResp := login(env.ctx, t, env.client, env.base, "pw-to-reset", "brandnew123")
		_ = userLoginResp.Body.Close()
	})
}

func TestUserManagement_MustChangePasswordFlow(t *testing.T) {
	env := newE2EEnv(t)
	seedUser(t, env, "flow-admin", "adminpass1", "Flow Admin")
	assignGlobalRolesByName(t, env, "flow-admin", "ADMIN")
	adminToken := adminBearerToken(t, env, "flow-admin", "adminpass1")

	t.Run("new_user_is_blocked_until_password_changed", func(t *testing.T) {
		id := createUserViaAPI(t, env, adminToken, "forced-flow-user", "firstpass1", "Forced Flow User")
		_ = id

		// Log in as the forced-change user.
		loginResp := login(env.ctx, t, env.client, env.base, "forced-flow-user", "firstpass1")
		_ = loginResp.Body.Close()

		// GET /users/me should be blocked.
		meReq := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me", nil)
		meResp := mustDo(t, env.client, meReq)
		defer func() { _ = meResp.Body.Close() }()
		assertStatus(t, meResp, http.StatusForbidden)
		assertErrorCode(t, meResp, "AUTH_PASSWORD_CHANGE_REQUIRED")

		// PATCH /users/me/password should be accessible.
		changeBody := jsonBody(t, map[string]string{
			"current_password": "firstpass1",
			"new_password":     "changed456",
		})
		changeReq := mustRequest(env.ctx, t, http.MethodPatch, env.base+"/api/v1/users/me/password", changeBody)
		changeReq.Header.Set("Content-Type", "application/json")
		changeResp := mustDo(t, env.client, changeReq)
		defer func() { _ = changeResp.Body.Close() }()
		assertStatus(t, changeResp, http.StatusNoContent)

		// After changing, re-login and GET /users/me should now work.
		loginResp2 := login(env.ctx, t, env.client, env.base, "forced-flow-user", "changed456")
		_ = loginResp2.Body.Close()

		meReq2 := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/users/me", nil)
		meResp2 := mustDo(t, env.client, meReq2)
		defer func() { _ = meResp2.Body.Close() }()
		assertStatus(t, meResp2, http.StatusOK)
	})
}
