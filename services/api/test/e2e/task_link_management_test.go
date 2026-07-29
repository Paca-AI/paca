package e2e_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
)

// createTaskViaAPI is a helper to create a task via the API
func createTaskViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, title string) string {
	t.Helper()
	body := jsonBody(t, map[string]any{
		"title":        title,
		"importance":   1,
		"story_points": 1,
	})
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projectID), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	id, _ := data["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty task id")
	}
	return id
}

// ---------------------------------------------------------------------------
// Task Link Management E2E Tests
// ---------------------------------------------------------------------------

func TestE2ETaskLinkManagement_TaskLinkingLifecycle(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "tasklink-user-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "tasklinkpass1")
	client, token := taskMemberLogin(t, env, username, "tasklinkpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	// Create two tasks to link between
	taskID1 := createTaskViaAPI(t, env, client, token, projID, "Task 1 - Source Task")
	taskID2 := createTaskViaAPI(t, env, client, token, projID, "Task 2 - Target Task")
	taskID3 := createTaskViaAPI(t, env, client, token, projID, "Task 3 - Another Task")

	var linkID string

	t.Run("create_task_link_blocks", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": taskID2,
			"link_type":      "blocks",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID1), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		linkID, _ = data["id"].(string)
		if linkID == "" {
			t.Fatal("expected non-empty link id")
		}
		if sourceID, _ := data["source_task_id"].(string); sourceID != taskID1 {
			t.Errorf("expected source_task_id %q, got %q", taskID1, sourceID)
		}
		if targetID, _ := data["target_task_id"].(string); targetID != taskID2 {
			t.Errorf("expected target_task_id %q, got %q", taskID2, targetID)
		}
		if linkType, _ := data["link_type"].(string); linkType != "blocks" {
			t.Errorf("expected link_type \"blocks\", got %q", linkType)
		}
		// Verify linked_task is present
		linkedTask, ok := data["linked_task"].(map[string]any)
		if !ok {
			t.Fatal("expected linked_task to be an object")
		}
		if linkedTaskID, _ := linkedTask["id"].(string); linkedTaskID != taskID2 {
			t.Errorf("expected linked_task.id %q, got %q", taskID2, linkedTaskID)
		}
	})

	t.Run("create_task_link_relates_to", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": taskID3,
			"link_type":      "relates_to",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID1), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if linkType, _ := data["link_type"].(string); linkType != "relates_to" {
			t.Errorf("expected link_type \"relates_to\", got %q", linkType)
		}
	})

	t.Run("create_task_link_duplicates", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": taskID2,
			"link_type":      "duplicates",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID3), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
	})

	t.Run("list_task_links_for_task1", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID1), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, ok := data["items"].([]any)
		if !ok {
			t.Fatalf("expected items array, got %T", data["items"])
		}
		// Should have 2 links: blocks and relates_to
		if len(items) != 2 {
			t.Errorf("expected 2 links for task1, got %d", len(items))
		}
	})

	t.Run("list_task_links_for_task2", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID2), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, ok := data["items"].([]any)
		if !ok {
			t.Fatalf("expected items array, got %T", data["items"])
		}
		// Should have at least duplicates link created from task3
		if len(items) < 1 {
			t.Errorf("expected at least 1 link for task2, got %d", len(items))
		}
	})

	t.Run("delete_task_link", func(t *testing.T) {
		if linkID == "" {
			t.Skip("link_id not available (create_task_link may have failed)")
		}
		req := mustRequest(env.ctx, t, http.MethodDelete,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links/%s", env.base, projID, taskID1, linkID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("verify_link_deleted", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID1), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, ok := data["items"].([]any)
		if !ok {
			t.Fatalf("expected items array, got %T", data["items"])
		}
		// Should now have only 1 link (relates_to), as the blocks link was deleted
		if len(items) != 1 {
			t.Errorf("expected 1 link after deletion, got %d", len(items))
		}
	})
}

// TestE2ETaskLinkManagement_ErrorCases tests various error scenarios
func TestE2ETaskLinkManagement_ErrorCases(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "tasklink-error-user-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "tasklinkerror1")
	loginClient, loginToken := taskMemberLogin(t, env, username, "tasklinkerror1")
	projID := createProjectForTasksViaAPI(t, env, loginClient, loginToken)

	taskID := createTaskViaAPI(t, env, loginClient, loginToken, projID, "Task A")
	otherTaskID := createTaskViaAPI(t, env, loginClient, loginToken, projID, "Task B")

	t.Run("create_link_missing_target_task_id", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"link_type": "blocks",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("create_link_invalid_target_task_id", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": "not-a-uuid",
			"link_type":      "blocks",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("create_link_missing_link_type", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": otherTaskID,
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("create_link_invalid_link_type", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": otherTaskID,
			"link_type":      "invalid_type",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("create_link_to_nonexistent_task", func(t *testing.T) {
		nonexistentTaskID := uuid.NewString()
		body := jsonBody(t, map[string]any{
			"target_task_id": nonexistentTaskID,
			"link_type":      "blocks",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
	})

	t.Run("create_link_to_task_in_different_project", func(t *testing.T) {
		// Create a task in a different project
		otherProjID := createProjectForTasksViaAPI(t, env, loginClient, loginToken)
		taskInOtherProj := createTaskViaAPI(t, env, loginClient, loginToken, otherProjID, "Task in other project")

		body := jsonBody(t, map[string]any{
			"target_task_id": taskInOtherProj,
			"link_type":      "blocks",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		// Should fail - cannot link to tasks in different projects
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("create_duplicate_link_rejected", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": otherTaskID,
			"link_type":      "relates_to",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)

		// Creating the exact same link again should be rejected as a duplicate.
		dupBody := jsonBody(t, map[string]any{
			"target_task_id": otherTaskID,
			"link_type":      "relates_to",
		})
		req2 := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), dupBody)
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer "+loginToken)
		resp2 := mustDo(t, loginClient, req2)
		defer func() { _ = resp2.Body.Close() }()
		assertStatus(t, resp2, http.StatusConflict)

		// relates_to is symmetric: the reverse direction is the same relationship
		// and must also be rejected as a duplicate.
		reverseBody := jsonBody(t, map[string]any{
			"target_task_id": taskID,
			"link_type":      "relates_to",
		})
		req3 := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, otherTaskID), reverseBody)
		req3.Header.Set("Content-Type", "application/json")
		req3.Header.Set("Authorization", "Bearer "+loginToken)
		resp3 := mustDo(t, loginClient, req3)
		defer func() { _ = resp3.Body.Close() }()
		assertStatus(t, resp3, http.StatusConflict)
	})

	t.Run("list_links_for_task_in_different_project_not_found", func(t *testing.T) {
		// A task that exists, but in a different project than the one in the URL.
		otherProjID := createProjectForTasksViaAPI(t, env, loginClient, loginToken)
		taskInOtherProj := createTaskViaAPI(t, env, loginClient, loginToken, otherProjID, "Task in other project")

		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskInOtherProj), nil)
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		// The task does not belong to projID, so listing its links through that
		// project's URL must not leak data across the project boundary.
		assertStatus(t, resp, http.StatusNotFound)
	})

	t.Run("delete_nonexistent_link", func(t *testing.T) {
		nonexistentLinkID := uuid.NewString()
		req := mustRequest(env.ctx, t, http.MethodDelete,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links/%s", env.base, projID, taskID, nonexistentLinkID), nil)
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
	})

	t.Run("delete_link_invalid_link_id", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links/not-a-uuid", env.base, projID, taskID), nil)
		req.Header.Set("Authorization", "Bearer "+loginToken)
		resp := mustDo(t, loginClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})
}

// TestE2ETaskLinkManagement_Authorization tests permission requirements
func TestE2ETaskLinkManagement_Authorization(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)

	// Create two users: one with write access, one with read-only access
	writeUser := "tasklink-write-" + uuid.NewString()
	readUser := "tasklink-read-" + uuid.NewString()
	seedTaskMemberUser(t, env, writeUser, "tasklinkwrite1")

	// Create a user with only read permissions
	readonlyUsername := "readonly-user-" + uuid.NewString()
	seedUser(t, env, readonlyUsername, "readonlypass", "Read Only User")
	readonlyRoleName := "READONLY_" + uuid.NewString()
	if err := env.roleRepo.Create(env.ctx, &globalroledom.GlobalRole{
		ID:   uuid.New(),
		Name: readonlyRoleName,
		Permissions: map[string]any{
			"projects.read": true,
			"tasks.read":    true,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create readonly role: %v", err)
	}
	assignGlobalRolesByName(t, env, readonlyUsername, readonlyRoleName)

	writeClient, writeToken := taskMemberLogin(t, env, writeUser, "tasklinkwrite1")
	seedTaskMemberUser(t, env, readUser, "tasklinkread1")
	readClient, readToken := taskMemberLogin(t, env, readUser, "tasklinkread1")
	readonlyClient, readonlyToken := taskMemberLogin(t, env, readonlyUsername, "readonlypass")

	// Create tasks using write user
	projID := createProjectForTasksViaAPI(t, env, writeClient, writeToken)
	taskID1 := createTaskViaAPI(t, env, writeClient, writeToken, projID, "Task Write 1")
	taskID2 := createTaskViaAPI(t, env, writeClient, writeToken, projID, "Task Write 2")

	var linkID string

	t.Run("read_user_can_list_links", func(t *testing.T) {
		// First create a link with write user
		body := jsonBody(t, map[string]any{
			"target_task_id": taskID2,
			"link_type":      "blocks",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID1), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+writeToken)
		resp := mustDo(t, writeClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		linkID, _ = data["id"].(string)

		// Now read user should be able to list links
		req = mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID1), nil)
		req.Header.Set("Authorization", "Bearer "+readToken)
		resp = mustDo(t, readClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env3 envelope
		decodeJSON(t, resp, &env3)
		data2 := assertDataMap(t, env3)
		items, _ := data2["items"].([]any)
		if len(items) < 1 {
			t.Errorf("expected at least 1 link, got %d", len(items))
		}
	})

	t.Run("readonly_user_cannot_create_link", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": taskID1,
			"link_type":      "relates_to",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID2), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+readonlyToken)
		resp := mustDo(t, readonlyClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
	})

	t.Run("readonly_user_cannot_delete_link", func(t *testing.T) {
		if linkID == "" {
			t.Skip("link_id not available")
		}

		req := mustRequest(env.ctx, t, http.MethodDelete,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links/%s", env.base, projID, taskID1, linkID), nil)
		req.Header.Set("Authorization", "Bearer "+readonlyToken)
		resp := mustDo(t, readonlyClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
	})
}

// TestE2ETaskLinkManagement_AllLinkTypes tests that all valid link types work correctly
func TestE2ETaskLinkManagement_AllLinkTypes(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "tasklink-alltypes-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "tasklinkalltypes1")
	client, token := taskMemberLogin(t, env, username, "tasklinkalltypes1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	taskID := createTaskViaAPI(t, env, client, token, projID, "Main Task")
	targetTaskID := createTaskViaAPI(t, env, client, token, projID, "Target Task")

	validLinkTypes := []string{"blocks", "relates_to", "duplicates"}

	for _, linkType := range validLinkTypes {
		t.Run("create_link_type_"+linkType, func(t *testing.T) {
			body := jsonBody(t, map[string]any{
				"target_task_id": targetTaskID,
				"link_type":      linkType,
			})
			req := mustRequest(env.ctx, t, http.MethodPost,
				fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), body)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+token)
			resp := mustDo(t, client, req)
			defer func() { _ = resp.Body.Close() }()
			assertStatus(t, resp, http.StatusCreated)

			var env2 envelope
			decodeJSON(t, resp, &env2)
			data := assertDataMap(t, env2)

			if returnedType, _ := data["link_type"].(string); returnedType != linkType {
				t.Errorf("expected link_type %q, got %q", linkType, returnedType)
			}

			// Verify display_link_type is present
			if _, ok := data["display_link_type"].(string); !ok {
				t.Error("expected display_link_type to be present")
			}
		})
	}

	t.Run("list_links_shows_all_types", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, ok := data["items"].([]any)
		if !ok {
			t.Fatalf("expected items array, got %T", data["items"])
		}

		if len(items) != 3 {
			t.Errorf("expected 3 links (one for each type), got %d", len(items))
		}
	})
}

// TestE2ETaskLinkManagement_BidirectionalLinks tests that links can be viewed from both source and target
func TestE2ETaskLinkManagement_BidirectionalLinks(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "tasklink-bi-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "tasklinkbi1")
	client, token := taskMemberLogin(t, env, username, "tasklinkbi1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	taskID1 := createTaskViaAPI(t, env, client, token, projID, "Task A")
	taskID2 := createTaskViaAPI(t, env, client, token, projID, "Task B")

	// Create a link from task1 to task2
	t.Run("create_link_task1_to_task2", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"target_task_id": taskID2,
			"link_type":      "blocks",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID1), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
	})

	t.Run("list_links_from_source_task", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID1), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, ok := data["items"].([]any)
		if !ok {
			t.Fatalf("expected items array, got %T", data["items"])
		}

		if len(items) != 1 {
			t.Errorf("expected 1 link from task1, got %d", len(items))
		}

		link := items[0].(map[string]any)
		if sourceID, _ := link["source_task_id"].(string); sourceID != taskID1 {
			t.Errorf("expected source_task_id %q, got %q", taskID1, sourceID)
		}
		if targetID, _ := link["target_task_id"].(string); targetID != taskID2 {
			t.Errorf("expected target_task_id %q, got %q", taskID2, targetID)
		}
	})

	t.Run("list_links_from_target_task", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projID, taskID2), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)

		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, ok := data["items"].([]any)
		if !ok {
			t.Fatalf("expected items array, got %T", data["items"])
		}

		// The target task should also show the link (displayed as "is_blocked_by")
		if len(items) != 1 {
			t.Errorf("expected 1 link for task2, got %d", len(items))
		}

		link := items[0].(map[string]any)
		if displayType, _ := link["display_link_type"].(string); displayType == "" {
			t.Error("expected display_link_type to be present")
		}
	})
}
