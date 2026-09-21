package e2e_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// defaultRoleNames lists the names of the global roles that carry is_default,
// as GET /admin/global-roles reports them.
func defaultRoleNames(t *testing.T, env *e2eEnv, client *http.Client) []string {
	t.Helper()
	status, out := doJSON(t, env, client, http.MethodGet, "/api/v1/admin/global-roles", nil)
	if status != http.StatusOK {
		t.Fatalf("list global roles: want 200, got %d", status)
	}
	items, _ := out.Data.([]any)
	var names []string
	for _, item := range items {
		role, _ := item.(map[string]any)
		if isDefault, _ := role["is_default"].(bool); isDefault {
			name, _ := role["name"].(string)
			names = append(names, name)
		}
	}
	return names
}

// createGlobalRoleViaAPI creates a role through the API and returns its id.
func createGlobalRoleViaAPI(t *testing.T, env *e2eEnv, client *http.Client, name string) string {
	t.Helper()
	status, out := doJSON(t, env, client, http.MethodPost, "/api/v1/admin/global-roles", map[string]any{
		"name": name, "permissions": map[string]any{"users.read": true},
	})
	if status != http.StatusCreated {
		t.Fatalf("create role %q: want 201, got %d (%s)", name, status, out.ErrorCode)
	}
	id, _ := assertDataMap(t, out)["id"].(string)
	return id
}

// createUserAsAdmin creates a user through POST /admin/users. It returns the
// HTTP status, the role the account started with (on success) and the API
// error code (on failure).
func createUserAsAdmin(t *testing.T, env *e2eEnv, client *http.Client, username string) (int, string, string) {
	t.Helper()
	status, out := doJSON(t, env, client, http.MethodPost, "/api/v1/admin/users", map[string]any{
		"username": username, "password": "supersecret", "full_name": username,
	})
	if status != http.StatusCreated {
		return status, "", out.ErrorCode
	}
	role, _ := assertDataMap(t, out)["role"].(string)
	return status, role, ""
}

// TestDefaultGlobalRole covers the default global role end to end on a real
// database: a fresh install starts with USER as the default, the default can
// be moved, new users follow it, and the default cannot be deleted.
func TestDefaultGlobalRole(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const (
		rootName = "root"
		password = "supersecret"
	)
	seedUser(t, env, rootName, password, "Root")
	assignGlobalRolesByName(t, env, rootName, "SUPER_ADMIN")
	client := newLoggedInClient(t, env, rootName, password)

	t.Run("a_fresh_install_has_USER_as_its_only_default", func(t *testing.T) {
		names := defaultRoleNames(t, env, client)
		if len(names) != 1 || names[0] != "USER" {
			t.Fatalf("default roles = %v, want exactly [USER]", names)
		}
	})

	t.Run("new_users_start_with_the_default_role", func(t *testing.T) {
		status, role, code := createUserAsAdmin(t, env, client, "newbie-one")
		if status != http.StatusCreated {
			t.Fatalf("create user: want 201, got %d (%s)", status, code)
		}
		if role != "USER" {
			t.Errorf("new user's role = %q, want the default USER", role)
		}
	})

	memberID := createGlobalRoleViaAPI(t, env, client, "MEMBER")

	t.Run("a_new_role_is_not_the_default", func(t *testing.T) {
		if names := defaultRoleNames(t, env, client); len(names) != 1 || names[0] != "USER" {
			t.Fatalf("creating a role changed the default: %v", names)
		}
	})

	t.Run("setting_the_default_moves_it_and_new_users_follow", func(t *testing.T) {
		status, out := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/global-roles/"+memberID+"/set-default", nil)
		if status != http.StatusOK {
			t.Fatalf("set default: want 200, got %d (%s)", status, out.ErrorCode)
		}
		data := assertDataMap(t, out)
		if isDefault, _ := data["is_default"].(bool); !isDefault || data["name"] != "MEMBER" {
			t.Fatalf("response should be the MEMBER role as the default, got %v", data)
		}

		// Exactly one default afterwards, and it is MEMBER: the flag moved,
		// it was not added.
		if names := defaultRoleNames(t, env, client); len(names) != 1 || names[0] != "MEMBER" {
			t.Fatalf("default roles = %v, want exactly [MEMBER]", names)
		}

		status, role, code := createUserAsAdmin(t, env, client, "newbie-two")
		if status != http.StatusCreated {
			t.Fatalf("create user: want 201, got %d (%s)", status, code)
		}
		if role != "MEMBER" {
			t.Errorf("new user's role = %q, want the new default MEMBER", role)
		}
	})

	t.Run("setting_the_same_default_again_is_harmless", func(t *testing.T) {
		if status, _ := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/global-roles/"+memberID+"/set-default", nil); status != http.StatusOK {
			t.Fatalf("want 200, got %d", status)
		}
		if names := defaultRoleNames(t, env, client); len(names) != 1 || names[0] != "MEMBER" {
			t.Fatalf("default roles = %v, want exactly [MEMBER]", names)
		}
	})

	t.Run("the_default_role_cannot_be_deleted", func(t *testing.T) {
		// newbie-two holds MEMBER too, but the more specific reason wins: the
		// person has to make another role the default first either way.
		status, out := doJSON(t, env, client, http.MethodDelete, "/api/v1/admin/global-roles/"+memberID, nil)
		if status != http.StatusConflict || out.ErrorCode != "GLOBAL_ROLE_IS_DEFAULT" {
			t.Fatalf("delete default: want 409 GLOBAL_ROLE_IS_DEFAULT, got %d %q", status, out.ErrorCode)
		}
		if _, err := env.roleRepo.FindByID(env.ctx, uuid.MustParse(memberID)); err != nil {
			t.Fatalf("the default role must still exist after a refused delete: %v", err)
		}
	})

	t.Run("an_unknown_role_cannot_be_made_the_default", func(t *testing.T) {
		status, out := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/global-roles/"+uuid.NewString()+"/set-default", nil)
		if status != http.StatusNotFound || out.ErrorCode != "GLOBAL_ROLE_NOT_FOUND" {
			t.Fatalf("want 404 GLOBAL_ROLE_NOT_FOUND, got %d %q", status, out.ErrorCode)
		}
		if names := defaultRoleNames(t, env, client); len(names) != 1 || names[0] != "MEMBER" {
			t.Fatalf("a failed set-default changed the default: %v", names)
		}
	})

	t.Run("new_global_agents_start_with_the_default_role", func(t *testing.T) {
		status, out := doJSON(t, env, client, http.MethodPost, "/api/v1/admin/agents", map[string]any{
			"name": "Default Role Bot", "handle": "default-role-bot", "agent_type": "acp", "acp_provider": "claude-code",
		})
		if status != http.StatusCreated {
			t.Fatalf("create global agent: want 201, got %d (%s)", status, out.ErrorCode)
		}
		if got, _ := assertDataMap(t, out)["global_role_id"].(string); got != memberID {
			t.Errorf("new agent's global_role_id = %q, want the default MEMBER (%s)", got, memberID)
		}
	})

	t.Run("the_agent_role_can_be_changed_afterwards_on_its_own_route", func(t *testing.T) {
		userRole, err := env.roleRepo.FindByName(env.ctx, "USER")
		if err != nil {
			t.Fatalf("find USER: %v", err)
		}
		_, out := doJSON(t, env, client, http.MethodGet, "/api/v1/admin/agents", nil)
		items, _ := assertDataMap(t, out)["items"].([]any)
		var agentID string
		for _, item := range items {
			if a, _ := item.(map[string]any); a["handle"] == "default-role-bot" {
				agentID, _ = a["id"].(string)
			}
		}
		if agentID == "" {
			t.Fatal("could not find the agent that was just created")
		}

		status, out := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/agents/"+agentID+"/global-role",
			map[string]any{"global_role_id": userRole.ID.String()})
		if status != http.StatusOK {
			t.Fatalf("assign role: want 200, got %d (%s)", status, out.ErrorCode)
		}
		if got, _ := assertDataMap(t, out)["global_role_id"].(string); got != userRole.ID.String() {
			t.Errorf("global_role_id = %q, want %s", got, userRole.ID)
		}
	})

	t.Run("the_former_default_is_only_protected_while_someone_holds_it", func(t *testing.T) {
		// USER stopped being the default when MEMBER became it, so the default
		// rule no longer applies. newbie-one still holds it, so the delete is
		// refused for that reason instead.
		userRole, err := env.roleRepo.FindByName(env.ctx, "USER")
		if err != nil {
			t.Fatalf("find USER: %v", err)
		}
		status, out := doJSON(t, env, client, http.MethodDelete, "/api/v1/admin/global-roles/"+userRole.ID.String(), nil)
		if status != http.StatusConflict || out.ErrorCode != "GLOBAL_ROLE_HAS_ASSIGNED_USERS" {
			t.Fatalf("want 409 GLOBAL_ROLE_HAS_ASSIGNED_USERS, got %d %q", status, out.ErrorCode)
		}
	})
}

// TestDeletingTheOldDefaultRoleDoesNotBreakCreatingUsers is the regression test
// for the bug that started this: new accounts were given the role *named*
// USER, so once USER had been deleted (allowed when nobody held it) creating a
// user failed. The role is now whichever one is the default.
func TestDeletingTheOldDefaultRoleDoesNotBreakCreatingUsers(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const (
		rootName = "root"
		password = "supersecret"
	)
	seedUser(t, env, rootName, password, "Root")
	assignGlobalRolesByName(t, env, rootName, "SUPER_ADMIN")
	client := newLoggedInClient(t, env, rootName, password)

	userRole, err := env.roleRepo.FindByName(env.ctx, "USER")
	if err != nil {
		t.Fatalf("find USER: %v", err)
	}

	// While USER is the default it cannot go, even though nobody holds it.
	status, out := doJSON(t, env, client, http.MethodDelete, "/api/v1/admin/global-roles/"+userRole.ID.String(), nil)
	if status != http.StatusConflict || out.ErrorCode != "GLOBAL_ROLE_IS_DEFAULT" {
		t.Fatalf("delete the default: want 409 GLOBAL_ROLE_IS_DEFAULT, got %d %q", status, out.ErrorCode)
	}

	// Make another role the default, then USER can be deleted.
	memberID := createGlobalRoleViaAPI(t, env, client, "MEMBER")
	if status, _ := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/global-roles/"+memberID+"/set-default", nil); status != http.StatusOK {
		t.Fatalf("set default: want 200, got %d", status)
	}
	if status, out := doJSON(t, env, client, http.MethodDelete, "/api/v1/admin/global-roles/"+userRole.ID.String(), nil); status != http.StatusOK {
		t.Fatalf("delete the former default: want 200, got %d (%s)", status, out.ErrorCode)
	}

	// The scenario that used to fail: no USER role exists any more.
	status, role, code := createUserAsAdmin(t, env, client, "after-user-deleted")
	if status != http.StatusCreated {
		t.Fatalf("create user without a USER role: want 201, got %d (%s)", status, code)
	}
	if role != "MEMBER" {
		t.Errorf("new user's role = %q, want the default MEMBER", role)
	}
}

// Setting the default changes what every new account and agent starts with, so
// it is role-definition work (global_roles.write) — assigning roles to
// accounts (global_roles.assign) is not enough.
func TestSettingTheDefaultRoleNeedsRoleWrite(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const password = "supersecret"
	assigner := createRoleWithPermissions(t, env, "ASSIGNER", map[string]any{"global_roles.assign": true, "global_roles.read": true})
	writer := createRoleWithPermissions(t, env, "ROLE_WRITER", map[string]any{"global_roles.write": true, "global_roles.read": true})
	seedUser(t, env, "assigner", password, "Assigner")
	assignGlobalRolesByName(t, env, "assigner", assigner.Name)
	seedUser(t, env, "role-writer", password, "Role Writer")
	assignGlobalRolesByName(t, env, "role-writer", writer.Name)
	target := createRoleWithPermissions(t, env, "TARGET", map[string]any{"users.read": true})
	path := "/api/v1/admin/global-roles/" + target.ID.String() + "/set-default"

	assignerClient := newLoggedInClient(t, env, "assigner", password)
	if status, _ := doJSON(t, env, assignerClient, http.MethodPut, path, nil); status != http.StatusForbidden {
		t.Errorf("global_roles.assign alone: want 403, got %d", status)
	}
	if def, err := env.roleRepo.FindDefault(env.ctx); err != nil || def.Name != "USER" {
		t.Fatalf("a refused request must not change the default; got %+v (%v)", def, err)
	}

	writerClient := newLoggedInClient(t, env, "role-writer", password)
	if status, out := doJSON(t, env, writerClient, http.MethodPut, path, nil); status != http.StatusOK {
		t.Fatalf("global_roles.write: want 200, got %d (%s)", status, out.ErrorCode)
	}
	if def, err := env.roleRepo.FindDefault(env.ctx); err != nil || def.ID != target.ID {
		t.Fatalf("expected TARGET to be the default, got %+v (%v)", def, err)
	}
}

// New tasks without a status (or type) start in the project's default one, so
// the default cannot be deleted; making another the default frees it.
func TestDefaultTaskStatusAndTypeCannotBeDeleted(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const (
		rootName = "root"
		password = "supersecret"
	)
	seedUser(t, env, rootName, password, "Root")
	assignGlobalRolesByName(t, env, rootName, "SUPER_ADMIN")
	client := newLoggedInClient(t, env, rootName, password)

	status, out := doJSON(t, env, client, http.MethodPost, "/api/v1/projects", map[string]any{
		"name": "default-guard-" + uuid.NewString(), "description": "",
	})
	if status != http.StatusCreated {
		t.Fatalf("create project: want 201, got %d (%s)", status, out.ErrorCode)
	}
	projectID, _ := assertDataMap(t, out)["id"].(string)

	for _, kind := range []struct {
		collection, idParam, errorCode string
		create                         map[string]any
	}{
		{"task-statuses", "statusId", "TASK_STATUS_IS_DEFAULT", map[string]any{"name": "QA", "category": "todo", "position": 50}},
		{"task-types", "typeId", "TASK_TYPE_IS_DEFAULT", map[string]any{"name": "Chore"}},
	} {
		t.Run(kind.collection, func(t *testing.T) {
			base := fmt.Sprintf("/api/v1/projects/%s/%s", projectID, kind.collection)

			// Two custom rows nothing else references, so a delete can only be
			// refused for being the default.
			ids := map[string]string{}
			for _, name := range []string{"first", "second"} {
				body := map[string]any{}
				for k, v := range kind.create {
					body[k] = v
				}
				body["name"] = fmt.Sprintf("%v-%s", kind.create["name"], name)
				status, out := doJSON(t, env, client, http.MethodPost, base, body)
				if status != http.StatusCreated {
					t.Fatalf("create %s %s: want 201, got %d (%s)", kind.collection, name, status, out.ErrorCode)
				}
				ids[name], _ = assertDataMap(t, out)["id"].(string)
			}

			if status, out := doJSON(t, env, client, http.MethodPut, base+"/"+ids["first"]+"/set-default", nil); status != http.StatusOK {
				t.Fatalf("set default: want 200, got %d (%s)", status, out.ErrorCode)
			}

			status, out := doJSON(t, env, client, http.MethodDelete, base+"/"+ids["first"], nil)
			if status != http.StatusConflict || out.ErrorCode != kind.errorCode {
				t.Fatalf("delete the default: want 409 %s, got %d %q", kind.errorCode, status, out.ErrorCode)
			}

			// It is still there, still the default.
			_, list := doJSON(t, env, client, http.MethodGet, base, nil)
			found := false
			for _, item := range assertDataMap(t, list)["items"].([]any) {
				row, _ := item.(map[string]any)
				if row["id"] == ids["first"] {
					found = true
					if isDefault, _ := row["is_default"].(bool); !isDefault {
						t.Errorf("the refused delete changed is_default on %s", ids["first"])
					}
				}
			}
			if !found {
				t.Fatal("the default row disappeared despite the refused delete")
			}

			// A row that is not the default can go, and so can the old default
			// once another one has taken over.
			if status, out := doJSON(t, env, client, http.MethodDelete, base+"/"+ids["second"], nil); status != http.StatusOK {
				t.Fatalf("delete a non-default row: want 200, got %d (%s)", status, out.ErrorCode)
			}
			_, out = doJSON(t, env, client, http.MethodPost, base, func() map[string]any {
				body := map[string]any{}
				for k, v := range kind.create {
					body[k] = v
				}
				body["name"] = fmt.Sprintf("%v-third", kind.create["name"])
				return body
			}())
			thirdID, _ := assertDataMap(t, out)["id"].(string)
			if status, _ := doJSON(t, env, client, http.MethodPut, base+"/"+thirdID+"/set-default", nil); status != http.StatusOK {
				t.Fatalf("switch default: want 200, got %d", status)
			}
			if status, out := doJSON(t, env, client, http.MethodDelete, base+"/"+ids["first"], nil); status != http.StatusOK {
				t.Fatalf("delete the former default: want 200, got %d (%s)", status, out.ErrorCode)
			}
			if status, out := doJSON(t, env, client, http.MethodDelete, base+"/"+thirdID, nil); status != http.StatusConflict || out.ErrorCode != kind.errorCode {
				t.Fatalf("the new default must be refused: want 409 %s, got %d %q", kind.errorCode, status, out.ErrorCode)
			}
		})
	}
}
