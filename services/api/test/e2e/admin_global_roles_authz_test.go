package e2e_test

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
)

func TestAdminGlobalRolesAuthorization(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const username = "rolecheck"
	const password = "supersecret"

	seedUser(t, env, username, password, "Role Check")

	readOnlyName := "READ_ONLY_" + uuid.NewString()
	if err := env.roleRepo.Create(env.ctx, &globalroledom.GlobalRole{
		ID:          uuid.New(),
		Name:        readOnlyName,
		Permissions: map[string]any{"global_roles.read": true},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("create read-only role: %v", err)
	}

	writeOnlyName := "WRITE_ONLY_" + uuid.NewString()
	if err := env.roleRepo.Create(env.ctx, &globalroledom.GlobalRole{
		ID:          uuid.New(),
		Name:        writeOnlyName,
		Permissions: map[string]any{"global_roles.write": true},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("create write-only role: %v", err)
	}

	assignOnlyName := "ASSIGN_ONLY_" + uuid.NewString()
	if err := env.roleRepo.Create(env.ctx, &globalroledom.GlobalRole{
		ID:          uuid.New(),
		Name:        assignOnlyName,
		Permissions: map[string]any{"global_roles.assign": true},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}); err != nil {
		t.Fatalf("create assign-only role: %v", err)
	}

	t.Run("without_global_role_permissions_read_is_forbidden", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		assignGlobalRolesByName(t, env, username)
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		req := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/global-roles", nil)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()

		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})

	t.Run("read_permission_allows_list_but_not_write", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		assignGlobalRolesByName(t, env, username, readOnlyName)
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		listReq := mustRequest(env.ctx, t, http.MethodGet, env.base+"/api/v1/admin/global-roles", nil)
		listResp := mustDo(t, client, listReq)
		defer func() { _ = listResp.Body.Close() }()
		assertStatus(t, listResp, http.StatusOK)

		createBody := jsonBody(t, map[string]any{
			"name":        "READ_ONLY_CANNOT_CREATE_" + uuid.NewString(),
			"permissions": map[string]any{"global_roles.read": true},
		})
		createReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/admin/global-roles", createBody)
		createReq.Header.Set("Content-Type", "application/json")
		createResp := mustDo(t, client, createReq)
		defer func() { _ = createResp.Body.Close() }()

		assertStatus(t, createResp, http.StatusForbidden)
		assertErrorCode(t, createResp, "FORBIDDEN")
	})

	t.Run("write_permission_cannot_assign_roles", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		assignGlobalRolesByName(t, env, username, writeOnlyName)
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		target, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find target user: %v", err)
		}

		assignReq := mustRequest(
			env.ctx,
			t,
			http.MethodPut,
			env.base+"/api/v1/admin/users/"+target.ID.String()+"/global-roles",
			jsonBody(t, map[string]any{"role_ids": []string{}}),
		)
		assignReq.Header.Set("Content-Type", "application/json")

		assignResp := mustDo(t, client, assignReq)
		defer func() { _ = assignResp.Body.Close() }()

		assertStatus(t, assignResp, http.StatusForbidden)
		assertErrorCode(t, assignResp, "FORBIDDEN")
	})

	t.Run("assign_permission_allows_role_assignment", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		assignGlobalRolesByName(t, env, username, assignOnlyName)
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		target, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find target user: %v", err)
		}

		// Resolve the USER role id — schema requires exactly one role (NOT NULL).
		userRole, err := env.roleRepo.FindByName(env.ctx, "USER")
		if err != nil {
			t.Fatalf("find USER role: %v", err)
		}

		assignReq := mustRequest(
			env.ctx,
			t,
			http.MethodPut,
			env.base+"/api/v1/admin/users/"+target.ID.String()+"/global-roles",
			jsonBody(t, map[string]any{"role_ids": []string{userRole.ID.String()}}),
		)
		assignReq.Header.Set("Content-Type", "application/json")

		assignResp := mustDo(t, client, assignReq)
		defer func() { _ = assignResp.Body.Close() }()

		assertStatus(t, assignResp, http.StatusOK)
	})
}

// TestAdminGlobalRolesCannotEscalateToSuperAdmin proves the
// privilege-escalation guard end to end against the real database: an ADMIN
// (global_roles.* but no universal PermissionAll) must not be able to grant
// the universal permission to a role or assign the SUPER_ADMIN role to a
// user — while a SUPER_ADMIN can do both.
func TestAdminGlobalRolesCannotEscalateToSuperAdmin(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const username = "escalation"
	const password = "supersecret"

	seedUser(t, env, username, password, "Escalation Check")

	t.Run("admin_cannot_assign_superadmin_role", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		// The real built-in ADMIN role: global_roles.* without "*".
		assignGlobalRolesByName(t, env, username, "ADMIN")
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		target, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find target user: %v", err)
		}
		superAdminRole, err := env.roleRepo.FindByName(env.ctx, "SUPER_ADMIN")
		if err != nil {
			t.Fatalf("find SUPER_ADMIN role: %v", err)
		}

		assignReq := mustRequest(
			env.ctx,
			t,
			http.MethodPut,
			env.base+"/api/v1/admin/users/"+target.ID.String()+"/global-roles",
			jsonBody(t, map[string]any{"role_ids": []string{superAdminRole.ID.String()}}),
		)
		assignReq.Header.Set("Content-Type", "application/json")

		assignResp := mustDo(t, client, assignReq)
		defer func() { _ = assignResp.Body.Close() }()

		assertStatus(t, assignResp, http.StatusForbidden)
	})

	t.Run("admin_cannot_mint_god_role", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		assignGlobalRolesByName(t, env, username, "ADMIN")
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		createReq := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/admin/global-roles",
			jsonBody(t, map[string]any{
				"name":        "GOD_" + uuid.NewString(),
				"permissions": map[string]any{"*": true},
			}))
		createReq.Header.Set("Content-Type", "application/json")

		createResp := mustDo(t, client, createReq)
		defer func() { _ = createResp.Body.Close() }()

		assertStatus(t, createResp, http.StatusForbidden)
	})

	t.Run("admin_can_still_assign_normal_role", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		assignGlobalRolesByName(t, env, username, "ADMIN")
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		target, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find target user: %v", err)
		}
		userRole, err := env.roleRepo.FindByName(env.ctx, "USER")
		if err != nil {
			t.Fatalf("find USER role: %v", err)
		}

		assignReq := mustRequest(
			env.ctx,
			t,
			http.MethodPut,
			env.base+"/api/v1/admin/users/"+target.ID.String()+"/global-roles",
			jsonBody(t, map[string]any{"role_ids": []string{userRole.ID.String()}}),
		)
		assignReq.Header.Set("Content-Type", "application/json")

		assignResp := mustDo(t, client, assignReq)
		defer func() { _ = assignResp.Body.Close() }()

		assertStatus(t, assignResp, http.StatusOK)
	})

	t.Run("superadmin_can_assign_superadmin_role", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		// Grant god mode directly (the owner's prerogative), then confirm the
		// API lets a SUPER_ADMIN do the same — the guard must not over-block.
		assignGlobalRolesByName(t, env, username, "SUPER_ADMIN")
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		target, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find target user: %v", err)
		}
		superAdminRole, err := env.roleRepo.FindByName(env.ctx, "SUPER_ADMIN")
		if err != nil {
			t.Fatalf("find SUPER_ADMIN role: %v", err)
		}

		assignReq := mustRequest(
			env.ctx,
			t,
			http.MethodPut,
			env.base+"/api/v1/admin/users/"+target.ID.String()+"/global-roles",
			jsonBody(t, map[string]any{"role_ids": []string{superAdminRole.ID.String()}}),
		)
		assignReq.Header.Set("Content-Type", "application/json")

		assignResp := mustDo(t, client, assignReq)
		defer func() { _ = assignResp.Body.Close() }()

		assertStatus(t, assignResp, http.StatusOK)
	})

	t.Run("admin_cannot_promote_via_user_update", func(t *testing.T) {
		jar, _ := cookiejar.New(nil)
		client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

		assignGlobalRolesByName(t, env, username, "ADMIN")
		loginResp := login(env.ctx, t, client, env.base, username, password)
		_ = loginResp.Body.Close()

		target, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find target user: %v", err)
		}

		// What the admin user form actually sends: PATCH /admin/users/:id with
		// a role *name*, which the service resolves into both users.role_id
		// and the legacy users.role claim the token issuer copies into tokens.
		promoteReq := mustRequest(
			env.ctx,
			t,
			http.MethodPatch,
			env.base+"/api/v1/admin/users/"+target.ID.String(),
			jsonBody(t, map[string]any{"role": "SUPER_ADMIN"}),
		)
		promoteReq.Header.Set("Content-Type", "application/json")

		promoteResp := mustDo(t, client, promoteReq)
		defer func() { _ = promoteResp.Body.Close() }()

		assertStatus(t, promoteResp, http.StatusForbidden)
	})
}
