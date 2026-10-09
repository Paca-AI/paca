package e2e_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// defaultRoleNames lists the names of the platform roles that carry is_default,
// as GET /admin/roles reports them.
func defaultRoleNames(t *testing.T, env *e2eEnv, client *http.Client) []string {
	t.Helper()
	status, out := doJSON(t, env, client, http.MethodGet, "/api/v1/admin/roles", nil)
	if status != http.StatusOK {
		t.Fatalf("list roles: want 200, got %d", status)
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

// createPlatformRoleViaAPI creates a platform role through the API and returns
// its id.
func createPlatformRoleViaAPI(t *testing.T, env *e2eEnv, client *http.Client, name string) string {
	t.Helper()
	status, out := doJSON(t, env, client, http.MethodPost, "/api/v1/admin/roles", map[string]any{
		"name": name,
		"policy": map[string]any{
			"version": "2026-10-01",
			"statements": []any{map[string]any{
				"effect": "Allow", "actions": []string{"users:read"}, "resources": []string{"user/*"},
			}},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("create role %q: want 201, got %d (%s)", name, status, out.ErrorCode)
	}
	id, _ := assertDataMap(t, out)["id"].(string)
	return id
}

// createUserAsAdmin creates a user through POST /admin/users. It returns the
// HTTP status, the names of the roles the account started with (on success)
// and the API error code (on failure).
func createUserAsAdmin(t *testing.T, env *e2eEnv, client *http.Client, username string) (int, string, string) {
	t.Helper()
	status, out := doJSON(t, env, client, http.MethodPost, "/api/v1/admin/users", map[string]any{
		"username": username, "password": "supersecret", "full_name": username,
	})
	if status != http.StatusCreated {
		return status, "", out.ErrorCode
	}
	return status, strings.Join(roleNames(assertDataMap(t, out)), ","), ""
}

// holderRoles returns the names of the platform roles the user named username
// holds.
func holderRoles(t *testing.T, env *e2eEnv, username string) string {
	t.Helper()
	return roleNameOf(t, env, username)
}

// TestDefaultRole covers the default role end to end on a real database: a
// fresh install starts with USER as the default, the default can be moved, new
// users follow it, and the default cannot be deleted.
func TestDefaultRole(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const (
		rootName = "root"
		password = "supersecret"
	)
	seedUser(t, env, rootName, password, "Root")
	assignPlatformRole(t, env, rootName, "SUPER_ADMIN")
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

	memberID := createPlatformRoleViaAPI(t, env, client, "MEMBER")

	t.Run("a_new_role_is_not_the_default", func(t *testing.T) {
		if names := defaultRoleNames(t, env, client); len(names) != 1 || names[0] != "USER" {
			t.Fatalf("creating a role changed the default: %v", names)
		}
	})

	t.Run("setting_the_default_moves_it_and_new_users_follow", func(t *testing.T) {
		status, out := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/roles/"+memberID+"/default", nil)
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
		if status, _ := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/roles/"+memberID+"/default", nil); status != http.StatusOK {
			t.Fatalf("want 200, got %d", status)
		}
		if names := defaultRoleNames(t, env, client); len(names) != 1 || names[0] != "MEMBER" {
			t.Fatalf("default roles = %v, want exactly [MEMBER]", names)
		}
	})

	t.Run("the_default_role_cannot_be_deleted", func(t *testing.T) {
		status, out := doJSON(t, env, client, http.MethodDelete, "/api/v1/admin/roles/"+memberID, nil)
		if status != http.StatusConflict || out.ErrorCode != "ROLE_IS_DEFAULT" {
			t.Fatalf("delete default: want 409 ROLE_IS_DEFAULT, got %d %q", status, out.ErrorCode)
		}
		if !platformRoleExists(t, env, "MEMBER") {
			t.Fatal("the default role must still exist after a refused delete")
		}
	})

	t.Run("an_unknown_role_cannot_be_made_the_default", func(t *testing.T) {
		status, out := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/roles/"+uuid.NewString()+"/default", nil)
		if status != http.StatusNotFound || out.ErrorCode != "ROLE_NOT_FOUND" {
			t.Fatalf("want 404 ROLE_NOT_FOUND, got %d %q", status, out.ErrorCode)
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
		if got := roleNames(assertDataMap(t, out)); len(got) != 1 || got[0] != "MEMBER" {
			t.Errorf("new agent's roles = %v, want the default [MEMBER]", got)
		}
	})

	t.Run("the_agent_roles_can_be_changed_afterwards_on_their_own_route", func(t *testing.T) {
		userRoleID := platformRoleID(t, env, "USER")
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

		status, out := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/agents/"+agentID+"/roles",
			map[string]any{"role_ids": []string{userRoleID.String()}})
		if status != http.StatusOK {
			t.Fatalf("assign role: want 200, got %d (%s)", status, out.ErrorCode)
		}
		roles, _ := out.Data.([]any)
		if len(roles) != 1 {
			t.Fatalf("agent roles = %v, want exactly [USER]", out.Data)
		}
		if m, _ := roles[0].(map[string]any); m["id"] != userRoleID.String() {
			t.Errorf("agent role = %v, want %s", m["id"], userRoleID)
		}
	})

	t.Run("system_roles_cannot_be_deleted_even_when_no_longer_the_default", func(t *testing.T) {
		userRoleID := platformRoleID(t, env, "USER")
		status, out := doJSON(t, env, client, http.MethodDelete, "/api/v1/admin/roles/"+userRoleID.String(), nil)
		if status != http.StatusConflict || out.ErrorCode != "ROLE_IS_SYSTEM" {
			t.Fatalf("want 409 ROLE_IS_SYSTEM, got %d %q", status, out.ErrorCode)
		}
	})
}

// TestDeletingTheOldDefaultRoleDoesNotBreakCreatingUsers is the regression test
// for the bug that started this: new accounts were given the role *named*
// USER, so once USER had been deleted creating a user failed. The role is now
// whichever one is the default, and a deleted former default takes its holders'
// attachments with it.
func TestDeletingTheOldDefaultRoleDoesNotBreakCreatingUsers(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const (
		rootName = "root"
		password = "supersecret"
	)
	seedUser(t, env, rootName, password, "Root")
	assignPlatformRole(t, env, rootName, "SUPER_ADMIN")
	client := newLoggedInClient(t, env, rootName, password)

	firstID := createPlatformRoleViaAPI(t, env, client, "FIRST")
	secondID := createPlatformRoleViaAPI(t, env, client, "SECOND")
	if status, _ := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/roles/"+firstID+"/default", nil); status != http.StatusOK {
		t.Fatalf("set default: want 200, got %d", status)
	}

	// While FIRST is the default it cannot go, even though nobody holds it.
	status, out := doJSON(t, env, client, http.MethodDelete, "/api/v1/admin/roles/"+firstID, nil)
	if status != http.StatusConflict || out.ErrorCode != "ROLE_IS_DEFAULT" {
		t.Fatalf("delete the default: want 409 ROLE_IS_DEFAULT, got %d %q", status, out.ErrorCode)
	}

	// An account created now holds FIRST.
	if status, role, code := createUserAsAdmin(t, env, client, "holds-first"); status != http.StatusCreated || role != "FIRST" {
		t.Fatalf("create user: want 201 with role FIRST, got %d %q (%s)", status, role, code)
	}

	// Make another role the default, then FIRST can be deleted, whoever holds it.
	if status, _ := doJSON(t, env, client, http.MethodPut, "/api/v1/admin/roles/"+secondID+"/default", nil); status != http.StatusOK {
		t.Fatalf("set default: want 200, got %d", status)
	}
	if status, out := doJSON(t, env, client, http.MethodDelete, "/api/v1/admin/roles/"+firstID, nil); status != http.StatusNoContent {
		t.Fatalf("delete the former default: want 204, got %d (%s)", status, out.ErrorCode)
	}
	if got := holderRoles(t, env, "holds-first"); got != "" {
		t.Errorf("the holder of the deleted role still has %q", got)
	}

	// The scenario that used to fail: the role named USER is no longer what
	// new accounts get.
	status2, role, code := createUserAsAdmin(t, env, client, "after-first-deleted")
	if status2 != http.StatusCreated {
		t.Fatalf("create user after the delete: want 201, got %d (%s)", status2, code)
	}
	if role != "SECOND" {
		t.Errorf("new user's role = %q, want the default SECOND", role)
	}
}

// Setting the default changes what every new account and agent starts with, so
// it is role-definition work (roles:write) — assigning roles to accounts
// (roles:assign) is not enough.
func TestSettingTheDefaultRoleNeedsRoleWrite(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	const password = "supersecret"
	createPlatformRole(t, env, "ASSIGNER", "roles:assign", "roles:read")
	// Making a role the default hands it to every new account, so the writer
	// must hold what the target grants.
	createPlatformRole(t, env, "ROLE_WRITER", "roles:write", "roles:read", "users:read")
	seedUser(t, env, "assigner", password, "Assigner")
	assignPlatformRole(t, env, "assigner", "ASSIGNER")
	seedUser(t, env, "role-writer", password, "Role Writer")
	assignPlatformRole(t, env, "role-writer", "ROLE_WRITER")
	targetID := createPlatformRole(t, env, "TARGET", "users:read")
	path := "/api/v1/admin/roles/" + targetID.String() + "/default"
	defaultName := func() string {
		var name string
		if err := env.db.Get(&name, `SELECT name FROM roles WHERE is_default`); err != nil {
			t.Fatalf("read the default role: %v", err)
		}
		return name
	}

	assignerClient := newLoggedInClient(t, env, "assigner", password)
	if status, _ := doJSON(t, env, assignerClient, http.MethodPut, path, nil); status != http.StatusForbidden {
		t.Errorf("roles:assign alone: want 403, got %d", status)
	}
	if got := defaultName(); got != "USER" {
		t.Fatalf("a refused request must not change the default; got %q", got)
	}

	writerClient := newLoggedInClient(t, env, "role-writer", password)
	if status, out := doJSON(t, env, writerClient, http.MethodPut, path, nil); status != http.StatusOK {
		t.Fatalf("roles:write: want 200, got %d (%s)", status, out.ErrorCode)
	}
	if got := defaultName(); got != "TARGET" {
		t.Fatalf("expected TARGET to be the default, got %q", got)
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
	assignPlatformRole(t, env, rootName, "SUPER_ADMIN")
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
