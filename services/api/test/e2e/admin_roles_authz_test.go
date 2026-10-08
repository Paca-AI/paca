package e2e_test

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/google/uuid"
)

// readOnlyPolicy is a minimal valid policy to send as a role body.
func readOnlyPolicy() map[string]any {
	return map[string]any{
		"version": "2026-10-01",
		"statements": []any{map[string]any{
			"effect": "Allow", "actions": []string{"roles:read"}, "resources": []string{"role/*"},
		}},
	}
}

func TestAdminRolesAuthorization(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const username = "rolecheck"
	const password = "supersecret"

	seedUser(t, env, username, password, "Role Check")

	readOnlyName := "READ_ONLY_" + uuid.NewString()
	createPlatformRole(t, env, readOnlyName, "roles:read")
	writeOnlyName := "WRITE_ONLY_" + uuid.NewString()
	createPlatformRole(t, env, writeOnlyName, "roles:write")
	assignOnlyName := "ASSIGN_ONLY_" + uuid.NewString()
	createPlatformRole(t, env, assignOnlyName, "roles:assign", "users:read")

	loginAs := func(t *testing.T, roleNames ...string) *http.Client {
		t.Helper()
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
		assignPlatformRole(t, env, username, roleNames...)
		resp := login(env.ctx, t, client, env.base, username, password)
		_ = resp.Body.Close()
		return client
	}

	t.Run("without_role_permissions_read_is_forbidden", func(t *testing.T) {
		client := loginAs(t)

		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/roles", nil)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()

		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("read_permission_allows_list_but_not_write", func(t *testing.T) {
		client := loginAs(t, readOnlyName)

		listReq := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/roles", nil)
		listResp := mustDo(t, client, listReq)
		defer func() { _ = listResp.Body.Close() }()
		assertStatus(t, listResp, http.StatusOK)

		createBody := jsonBody(t, map[string]any{
			"name":   "READ_ONLY_CANNOT_CREATE_" + uuid.NewString(),
			"policy": readOnlyPolicy(),
		})
		createReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/admin/roles", createBody)
		createReq.Header.Set("Content-Type", "application/json")
		createResp := mustDo(t, client, createReq)
		defer func() { _ = createResp.Body.Close() }()

		assertStatus(t, createResp, http.StatusForbidden)
		assertErrorCode(t, createResp, "FORBIDDEN")
	})

	t.Run("write_permission_cannot_assign_roles", func(t *testing.T) {
		client := loginAs(t, writeOnlyName)

		target, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find target user: %v", err)
		}

		assignReq := mustRequest(
			env.ctx,
			t,
			http.MethodPut,
			env.base+"/api/v1/admin/users/"+target.ID.String()+"/roles",
			jsonBody(t, map[string]any{"role_ids": []string{}}),
		)
		assignReq.Header.Set("Content-Type", "application/json")

		assignResp := mustDo(t, client, assignReq)
		defer func() { _ = assignResp.Body.Close() }()

		assertStatus(t, assignResp, http.StatusForbidden)
		assertErrorCode(t, assignResp, "FORBIDDEN")
	})

	t.Run("assign_permission_allows_role_assignment", func(t *testing.T) {
		client := loginAs(t, assignOnlyName)

		target, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find target user: %v", err)
		}
		userRoleID := platformRoleID(t, env, "USER")

		assignReq := mustRequest(
			env.ctx,
			t,
			http.MethodPut,
			env.base+"/api/v1/admin/users/"+target.ID.String()+"/roles",
			jsonBody(t, map[string]any{"role_ids": []string{userRoleID.String()}}),
		)
		assignReq.Header.Set("Content-Type", "application/json")

		assignResp := mustDo(t, client, assignReq)
		defer func() { _ = assignResp.Body.Close() }()

		assertStatus(t, assignResp, http.StatusOK)
	})
}
