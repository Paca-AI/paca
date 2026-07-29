package e2e_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

// listAssignedToMeViaAPI issues GET /users/me/tasks with the given query
// params and returns the decoded data map.
func listAssignedToMeViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token string, q url.Values) map[string]any {
	t.Helper()
	reqURL := env.base + "/api/v1/users/me/tasks"
	if len(q) > 0 {
		reqURL += "?" + q.Encode()
	}
	req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	return assertDataMap(t, env2)
}

// TestE2EAssignedToMeTasks_CrossProjectExcludesDoneAndUnassigned covers the
// three defining behaviors of GET /users/me/tasks: it aggregates tasks
// assigned to the caller across every project they belong to (resolving the
// caller's per-project member ID under the hood), excludes tasks whose
// status has category "done", and excludes tasks assigned to someone else.
func TestE2EAssignedToMeTasks_CrossProjectExcludesDoneAndUnassigned(t *testing.T) {
	env := newE2EEnv(t)
	ownerUsername := "assigned-to-me-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "assignedtomeowner1")
	client, token := taskMemberLogin(t, env, ownerUsername, "assignedtomeowner1")

	user, err := env.userRepo.FindByUsername(env.ctx, ownerUsername)
	if err != nil {
		t.Fatalf("find user %q: %v", ownerUsername, err)
	}

	projA := createProjectForTasksViaAPI(t, env, client, token)
	projB := createProjectForTasksViaAPI(t, env, client, token)

	membersA := listProjectMembersViaAPI(t, env, client, token, projA)
	memberInA := memberIDForUser(membersA, user.ID.String())
	if memberInA == "" {
		t.Fatalf("expected owner to resolve to a project member in projA, got %+v", membersA)
	}
	membersB := listProjectMembersViaAPI(t, env, client, token, projB)
	memberInB := memberIDForUser(membersB, user.ID.String())
	if memberInB == "" {
		t.Fatalf("expected owner to resolve to a project member in projB, got %+v", membersB)
	}

	otherMemberInA := addProjectMemberWithAutomationPerms(t, env, client, token, projA,
		"assigned-to-me-other-"+uuid.NewString(), "assignedtomeother1")

	statusesA := listTaskStatusesViaAPI(t, env, client, token, projA)
	todoIDA := statusIDByName(statusesA, "Todo")
	doneIDA := statusIDByName(statusesA, "Done")
	if todoIDA == "" || doneIDA == "" {
		t.Fatalf("expected default Todo/Done statuses in projA, got %+v", statusesA)
	}
	statusesB := listTaskStatusesViaAPI(t, env, client, token, projB)
	todoIDB := statusIDByName(statusesB, "Todo")
	if todoIDB == "" {
		t.Fatalf("expected default Todo status in projB, got %+v", statusesB)
	}

	mineOpenA := createTaskViaAPIWithBody(t, env, client, token, projA, map[string]any{
		"title": "Mine, open, in A", "status_id": todoIDA, "assignee_ids": []string{memberInA},
	})
	createTaskViaAPIWithBody(t, env, client, token, projA, map[string]any{
		"title": "Mine, but done, in A", "status_id": doneIDA, "assignee_ids": []string{memberInA},
	})
	createTaskViaAPIWithBody(t, env, client, token, projA, map[string]any{
		"title": "Not mine, in A", "status_id": todoIDA, "assignee_ids": []string{otherMemberInA},
	})
	mineOpenB := createTaskViaAPIWithBody(t, env, client, token, projB, map[string]any{
		"title": "Mine, open, in B", "status_id": todoIDB, "assignee_ids": []string{memberInB},
	})

	data := listAssignedToMeViaAPI(t, env, client, token, nil)
	got := itemIDs(data)
	want := map[string]bool{idOf(mineOpenA): true, idOf(mineOpenB): true}
	if len(got) != len(want) {
		t.Fatalf("expected %d assigned tasks, got %d: %v", len(want), len(got), got)
	}
	for _, id := range got {
		if !want[id] {
			t.Errorf("unexpected task %s in assigned-to-me response", id)
		}
	}
}

// TestE2EAssignedToMeTasks_CursorPagination verifies that GET /users/me/tasks
// paginates via next_cursor without skipping or repeating tasks, across
// several pages of a single page_size.
func TestE2EAssignedToMeTasks_CursorPagination(t *testing.T) {
	env := newE2EEnv(t)
	ownerUsername := "assigned-to-me-page-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "assignedtomepageowner1")
	client, token := taskMemberLogin(t, env, ownerUsername, "assignedtomepageowner1")

	user, err := env.userRepo.FindByUsername(env.ctx, ownerUsername)
	if err != nil {
		t.Fatalf("find user %q: %v", ownerUsername, err)
	}

	projID := createProjectForTasksViaAPI(t, env, client, token)
	members := listProjectMembersViaAPI(t, env, client, token, projID)
	memberID := memberIDForUser(members, user.ID.String())
	if memberID == "" {
		t.Fatalf("expected owner to resolve to a project member, got %+v", members)
	}

	statuses := listTaskStatusesViaAPI(t, env, client, token, projID)
	todoID := statusIDByName(statuses, "Todo")
	if todoID == "" {
		t.Fatalf("expected default Todo status, got %+v", statuses)
	}

	const total = 5
	wantIDs := make(map[string]bool, total)
	for i := 0; i < total; i++ {
		task := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
			"title": fmt.Sprintf("Assigned task %d", i), "status_id": todoID,
			"assignee_ids": []string{memberID},
		})
		wantIDs[idOf(task)] = true
	}

	seen := map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		if page > total {
			t.Fatalf("paginated more than %d times without exhausting %d tasks; seen=%v", total, total, seen)
		}
		q := url.Values{"page_size": {"2"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		data := listAssignedToMeViaAPI(t, env, client, token, q)
		for _, id := range itemIDs(data) {
			if seen[id] {
				t.Fatalf("task %s returned on more than one page", id)
			}
			seen[id] = true
		}
		cursor = nextCursorStr(data)
		if cursor == "" {
			break
		}
	}

	if len(seen) != len(wantIDs) {
		t.Fatalf("expected to see %d distinct tasks across pages, got %d: %v", len(wantIDs), len(seen), seen)
	}
	for id := range wantIDs {
		if !seen[id] {
			t.Errorf("expected task %s to appear across paginated results, never seen", id)
		}
	}
}
