package e2e_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// listProjectMembersViaAPI returns all members of a project as decoded maps.
func listProjectMembersViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string) []map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/members", env.base, projectID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	raw, ok := env2.Data.([]any)
	if !ok {
		t.Fatalf("expected members array, got %T", env2.Data)
	}
	members := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			members = append(members, m)
		}
	}
	return members
}

// memberIDForUser returns the project_members.id whose user_id is userID, or
// "" if not found.
func memberIDForUser(members []map[string]any, userID string) string {
	for _, m := range members {
		if uid, _ := m["user_id"].(string); uid == userID {
			id, _ := m["id"].(string)
			return id
		}
	}
	return ""
}

// listTaskStatusesViaAPI returns all task statuses for a project as decoded maps.
func listTaskStatusesViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string) []map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/task-statuses", env.base, projectID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	items, _ := data["items"].([]any)
	statuses := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			statuses = append(statuses, m)
		}
	}
	return statuses
}

// statusIDByName returns the id of the task status with the given name, or
// "" if not found.
func statusIDByName(statuses []map[string]any, name string) string {
	for _, s := range statuses {
		if n, _ := s["name"].(string); n == name {
			id, _ := s["id"].(string)
			return id
		}
	}
	return ""
}

// addProjectMemberWithAutomationPerms seeds a user, grants them a role with
// tasks + automation (workflows.*) read/write permissions, adds them to
// projectID, and returns their project_members.id.
func addProjectMemberWithAutomationPerms(t *testing.T, env *e2eEnv, ownerClient *http.Client, ownerToken, projectID, username, password string) string {
	t.Helper()
	seedUser(t, env, username, password, username)
	user, err := env.userRepo.FindByUsername(env.ctx, username)
	if err != nil {
		t.Fatalf("find user %q: %v", username, err)
	}
	roleID := createProjectRoleWithPermsViaAPI(t, env, ownerClient, ownerToken, projectID, "editor-"+uuid.NewString(),
		map[string]any{"projects.read": true, "tasks.read": true, "tasks.write": true, "workflows.read": true, "workflows.write": true})
	addMemberViaAPI(t, env, ownerClient, ownerToken, projectID, user.ID.String(), roleID)

	members := listProjectMembersViaAPI(t, env, ownerClient, ownerToken, projectID)
	memberID := memberIDForUser(members, user.ID.String())
	if memberID == "" {
		t.Fatalf("expected user %q to resolve to a project member", username)
	}
	return memberID
}
