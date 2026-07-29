package e2e_test

import (
	"fmt"
	"net/http"
	"testing"
)

// createViewViaAPI creates a sprint view and returns its ID.
func createViewViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, sprintID, name, viewType string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views?sprint_id=%s", env.base, projectID, sprintID)
	body := jsonBody(t, map[string]any{
		"name":      name,
		"view_type": viewType,
	})
	req := mustRequest(env.ctx, t, http.MethodPost, url, body)
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
		t.Fatal("missing view id in response")
	}
	return id
}

// listViewIDsViaAPI returns all view IDs for a sprint.
func listViewIDsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, sprintID string) []string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views?sprint_id=%s", env.base, projectID, sprintID)
	req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	items, _ := data["items"].([]any)
	var ids []string
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if id, ok := m["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// deleteViewViaAPI deletes a view, ignoring 404 (already gone).
func deleteViewViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, _ string, viewID string) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projectID, viewID)
	req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		t.Errorf("deleteViewViaAPI: unexpected status %d", resp.StatusCode)
	}
}

func TestE2EViewManagement_CRUD(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "view-crud-user", "viewpass1")
	client, token := taskMemberLogin(t, env, "view-crud-user", "viewpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint for Views")

	// Sprint creation auto-seeds default views; record their IDs so we can
	// clean them up before testing the "last view" guard.
	autoSeededViewIDs := listViewIDsViaAPI(t, env, client, token, projID, sprintID)

	var viewID string
	var view2ID string

	t.Run("create_view", func(t *testing.T) {
		viewID = createViewViaAPI(t, env, client, token, projID, sprintID, "Board View", "board")
		if viewID == "" {
			t.Fatal("expected non-empty view id")
		}
	})

	t.Run("list_views", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views?sprint_id=%s", env.base, projID, sprintID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) == 0 {
			t.Error("expected at least 1 view in list")
		}
	})

	t.Run("get_view", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if data["id"] != viewID {
			t.Errorf("expected id=%s, got %v", viewID, data["id"])
		}
	})

	t.Run("update_name", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		body := jsonBody(t, map[string]any{"name": "Renamed Board"})
		req := mustRequest(env.ctx, t, http.MethodPatch, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if data["name"] != "Renamed Board" {
			t.Errorf("expected name=Renamed Board, got %v", data["name"])
		}
	})

	t.Run("update_config", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		body := jsonBody(t, map[string]any{
			"config": map[string]any{
				"column_by": "status",
				"swimlanes": "assignee",
			},
		})
		req := mustRequest(env.ctx, t, http.MethodPatch, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		cfg, _ := data["config"].(map[string]any)
		if cfg["column_by"] != "status" {
			t.Errorf("expected column_by=status, got %v", cfg["column_by"])
		}
	})

	t.Run("create_second_view", func(t *testing.T) {
		view2ID = createViewViaAPI(t, env, client, token, projID, sprintID, "Table View", "table")
	})

	t.Run("delete_first_view", func(t *testing.T) {
		// Delete the manually created first view.
		deleteViewViaAPI(t, env, client, token, projID, sprintID, viewID)
		// Also delete every auto-seeded view (created when the sprint was
		// opened) so that view2ID becomes the sole remaining view, allowing
		// the next subtest to exercise the "last view" guard.
		for _, aid := range autoSeededViewIDs {
			deleteViewViaAPI(t, env, client, token, projID, sprintID, aid)
		}
	})

	t.Run("delete_last_view_rejected", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, view2ID)
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusConflict)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		if env2.ErrorCode != "VIEW_IS_LAST_VIEW" {
			t.Errorf("expected VIEW_IS_LAST_VIEW, got %q", env2.ErrorCode)
		}
	})
}

func TestE2ETaskPositionManagement(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "view-pos-user", "pospass1")
	client, token := taskMemberLogin(t, env, "view-pos-user", "pospass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint for Positions")
	viewID := createViewViaAPI(t, env, client, token, projID, sprintID, "Position View", "table")

	// Use a fixed task UUID (doesn't need to exist in DB for position tracking)
	taskID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	t.Run("move_task", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions/%s",
			env.base, projID, viewID, taskID)
		body := jsonBody(t, map[string]any{
			"position":  2,
			"group_key": "in-progress",
		})
		req := mustRequest(env.ctx, t, http.MethodPut, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("list_positions", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions",
			env.base, projID, viewID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 position, got %d", len(items))
		}
		pos, _ := items[0].(map[string]any)
		if pos["task_id"] != taskID {
			t.Errorf("expected task_id=%s, got %v", taskID, pos["task_id"])
		}
	})

	t.Run("move_to_different_group", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions/%s",
			env.base, projID, viewID, taskID)
		body := jsonBody(t, map[string]any{
			"position":  0,
			"group_key": "done",
		})
		req := mustRequest(env.ctx, t, http.MethodPut, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)

		// Verify updated
		listURL := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions",
			env.base, projID, viewID)
		req2 := mustRequest(env.ctx, t, http.MethodGet, listURL, nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		resp2 := mustDo(t, client, req2)
		defer func() { _ = resp2.Body.Close() }()
		assertStatus(t, resp2, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp2, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 position after upsert, got %d", len(items))
		}
		pos, _ := items[0].(map[string]any)
		if pos["group_key"] != "done" {
			t.Errorf("expected group_key=done, got %v", pos["group_key"])
		}
	})
}

// ---------------------------------------------------------------------------
// Product-backlog view helpers
// ---------------------------------------------------------------------------

// createBacklogViewViaAPI creates a product-backlog view and returns its ID.
func createBacklogViewViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, name, viewType string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views?context=backlog", env.base, projectID)
	body := jsonBody(t, map[string]any{
		"name":      name,
		"view_type": viewType,
	})
	req := mustRequest(env.ctx, t, http.MethodPost, url, body)
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
		t.Fatal("missing view id in backlog view response")
	}
	return id
}

// listBacklogViewIDsViaAPI returns all view IDs for the product backlog.
func listBacklogViewIDsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string) []string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views?context=backlog", env.base, projectID)
	req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	items, _ := data["items"].([]any)
	var ids []string
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if id, ok := m["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// deleteBacklogViewViaAPI deletes a product-backlog view, ignoring 404.
func deleteBacklogViewViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, viewID string) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projectID, viewID)
	req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		t.Errorf("deleteBacklogViewViaAPI: unexpected status %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Product-backlog view e2e tests
// ---------------------------------------------------------------------------

func TestE2EBacklogViewManagement_CRUD(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "backlog-view-crud-user", "backlogpass1")
	client, token := taskMemberLogin(t, env, "backlog-view-crud-user", "backlogpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	// Project creation auto-seeds default backlog views; record their IDs.
	autoSeededViewIDs := listBacklogViewIDsViaAPI(t, env, client, token, projID)

	var viewID string
	var view2ID string

	t.Run("create_view", func(t *testing.T) {
		viewID = createBacklogViewViaAPI(t, env, client, token, projID, "Backlog Board", "board")
		if viewID == "" {
			t.Fatal("expected non-empty view id")
		}
	})

	t.Run("response_has_no_sprint_id", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if sid, ok := data["sprint_id"]; ok && sid != nil {
			t.Errorf("expected sprint_id absent/null in backlog view response, got %v", sid)
		}
		if pid, _ := data["project_id"].(string); pid != projID {
			t.Errorf("expected project_id=%s, got %q", projID, pid)
		}
	})

	t.Run("list_views", func(t *testing.T) {
		ids := listBacklogViewIDsViaAPI(t, env, client, token, projID)
		if len(ids) == 0 {
			t.Error("expected at least 1 backlog view in list")
		}
	})

	t.Run("update_name", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		body := jsonBody(t, map[string]any{"name": "Renamed Backlog Board"})
		req := mustRequest(env.ctx, t, http.MethodPatch, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if data["name"] != "Renamed Backlog Board" {
			t.Errorf("expected name=Renamed Backlog Board, got %v", data["name"])
		}
	})

	t.Run("create_second_view", func(t *testing.T) {
		view2ID = createBacklogViewViaAPI(t, env, client, token, projID, "Backlog Table", "table")
	})

	t.Run("delete_first_view", func(t *testing.T) {
		deleteBacklogViewViaAPI(t, env, client, token, projID, viewID)
		for _, aid := range autoSeededViewIDs {
			deleteBacklogViewViaAPI(t, env, client, token, projID, aid)
		}
	})

	t.Run("delete_last_view_rejected", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, view2ID)
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusConflict)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		if env2.ErrorCode != "VIEW_IS_LAST_VIEW" {
			t.Errorf("expected VIEW_IS_LAST_VIEW, got %q", env2.ErrorCode)
		}
	})
}

func TestE2EBacklogTaskPositionManagement(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "backlog-pos-user", "backlogpospass1")
	client, token := taskMemberLogin(t, env, "backlog-pos-user", "backlogpospass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	viewID := createBacklogViewViaAPI(t, env, client, token, projID, "Backlog Position View", "table")

	taskID := "cccccccc-dddd-eeee-ffff-aaaaaaaaaaaa"

	t.Run("move_task", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions/%s",
			env.base, projID, viewID, taskID)
		body := jsonBody(t, map[string]any{
			"position":  1,
			"group_key": "todo",
		})
		req := mustRequest(env.ctx, t, http.MethodPut, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("list_positions", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions",
			env.base, projID, viewID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 position, got %d", len(items))
		}
		pos, _ := items[0].(map[string]any)
		if pos["task_id"] != taskID {
			t.Errorf("expected task_id=%s, got %v", taskID, pos["task_id"])
		}
	})

	t.Run("update_group", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions/%s",
			env.base, projID, viewID, taskID)
		body := jsonBody(t, map[string]any{
			"position":  0,
			"group_key": "in-progress",
		})
		req := mustRequest(env.ctx, t, http.MethodPut, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)

		listURL := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions",
			env.base, projID, viewID)
		req2 := mustRequest(env.ctx, t, http.MethodGet, listURL, nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		resp2 := mustDo(t, client, req2)
		defer func() { _ = resp2.Body.Close() }()
		assertStatus(t, resp2, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp2, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("expected 1 position, got %d", len(items))
		}
		pos, _ := items[0].(map[string]any)
		if pos["group_key"] != "in-progress" {
			t.Errorf("expected group_key=in-progress, got %v", pos["group_key"])
		}
	})
}

func TestE2EBulkTaskPositionManagement(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "bulk-pos-user", "bulkpass1")
	client, token := taskMemberLogin(t, env, "bulk-pos-user", "bulkpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint for Bulk Positions")
	viewID := createViewViaAPI(t, env, client, token, projID, sprintID, "Bulk Position View", "table")

	// Fixed UUIDs — do not need to exist as actual tasks for position tracking
	task1 := "11111111-1111-1111-1111-111111111111"
	task2 := "22222222-2222-2222-2222-222222222222"
	task3 := "33333333-3333-3333-3333-333333333333"

	bulkURL := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions",
		env.base, projID, viewID)

	t.Run("bulk_move_three_tasks", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"items": []map[string]any{
				{"task_id": task1, "position": 65536, "group_key": "todo"},
				{"task_id": task2, "position": 131072, "group_key": "todo"},
				{"task_id": task3, "position": 196608, "group_key": "in-progress"},
			},
		})
		req := mustRequest(env.ctx, t, http.MethodPut, bulkURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("list_shows_all_three", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, bulkURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 3 {
			t.Fatalf("expected 3 positions, got %d", len(items))
		}
	})

	t.Run("bulk_upsert_overwrites_position", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"items": []map[string]any{
				{"task_id": task1, "position": 32768, "group_key": "done"},
			},
		})
		req := mustRequest(env.ctx, t, http.MethodPut, bulkURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)

		// Verify task1 was updated and the other two still exist
		req2 := mustRequest(env.ctx, t, http.MethodGet, bulkURL, nil)
		req2.Header.Set("Authorization", "Bearer "+token)
		resp2 := mustDo(t, client, req2)
		defer func() { _ = resp2.Body.Close() }()
		assertStatus(t, resp2, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp2, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 3 {
			t.Fatalf("expected 3 positions after upsert, got %d", len(items))
		}
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["task_id"] == task1 {
				if item["group_key"] != "done" {
					t.Errorf("task1 group_key: expected done, got %v", item["group_key"])
				}
			}
		}
	})

	t.Run("empty_items_rejected", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"items": []any{}})
		req := mustRequest(env.ctx, t, http.MethodPut, bulkURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})
}

func TestE2EBulkBacklogTaskPositionManagement(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "bulk-backlog-user", "bulkblpass1")
	client, token := taskMemberLogin(t, env, "bulk-backlog-user", "bulkblpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	viewID := createBacklogViewViaAPI(t, env, client, token, projID, "Bulk Backlog View", "table")

	task1 := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	task2 := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"

	bulkURL := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions",
		env.base, projID, viewID)

	t.Run("bulk_move", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"items": []map[string]any{
				{"task_id": task1, "position": 65536, "group_key": "todo"},
				{"task_id": task2, "position": 131072},
			},
		})
		req := mustRequest(env.ctx, t, http.MethodPut, bulkURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("list_shows_both", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet, bulkURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("expected 2 backlog positions, got %d", len(items))
		}
	})
}

// ---------------------------------------------------------------------------
// Timeline view helpers
// ---------------------------------------------------------------------------

// createTimelineViewViaAPI creates a timeline view and returns its ID.
func createTimelineViewViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, name, viewType string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views?context=timeline", env.base, projectID)
	body := jsonBody(t, map[string]any{
		"name":      name,
		"view_type": viewType,
	})
	req := mustRequest(env.ctx, t, http.MethodPost, url, body)
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
		t.Fatal("missing view id in timeline view response")
	}
	return id
}

// listTimelineViewIDsViaAPI returns all view IDs for the timeline.
func listTimelineViewIDsViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string) []string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views?context=timeline", env.base, projectID)
	req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	items, _ := data["items"].([]any)
	var ids []string
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if id, ok := m["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// deleteTimelineViewViaAPI deletes a timeline view, ignoring 404.
func deleteTimelineViewViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, viewID string) {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projectID, viewID)
	req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		t.Errorf("deleteTimelineViewViaAPI: unexpected status %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Timeline view e2e tests
// ---------------------------------------------------------------------------

func TestE2ETimelineViewManagement_CRUD(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "timeline-crud-user", "timelinepass1")
	client, token := taskMemberLogin(t, env, "timeline-crud-user", "timelinepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	var viewID string

	t.Run("create_view", func(t *testing.T) {
		viewID = createTimelineViewViaAPI(t, env, client, token, projID, "Roadmap", "roadmap")
		if viewID == "" {
			t.Fatal("expected non-empty view id")
		}
	})

	t.Run("list_views", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views?context=timeline", env.base, projID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) == 0 {
			t.Error("expected at least 1 view in list")
		}
	})

	t.Run("get_view", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if data["id"] != viewID {
			t.Errorf("expected id=%s, got %v", viewID, data["id"])
		}
	})

	t.Run("update_name", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		body := jsonBody(t, map[string]any{"name": "Renamed Roadmap"})
		req := mustRequest(env.ctx, t, http.MethodPatch, url, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if data["name"] != "Renamed Roadmap" {
			t.Errorf("expected name=Renamed Roadmap, got %v", data["name"])
		}
	})

	t.Run("create_second_view", func(t *testing.T) {
		_ = createTimelineViewViaAPI(t, env, client, token, projID, "Timeline Table", "table")
	})

	t.Run("delete_first_view", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	})

	t.Run("deleted_view_returns_404", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		req := mustRequest(env.ctx, t, http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
	})
}

func TestE2ETimelineViewManagement_DeleteLastViewRejected(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "timeline-last-user", "timelinepass2")
	client, token := taskMemberLogin(t, env, "timeline-last-user", "timelinepass2")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	// Remove all auto-seeded timeline views first so we can control the count.
	for _, id := range listTimelineViewIDsViaAPI(t, env, client, token, projID) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, id)
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		_ = resp.Body.Close()
		// Tolerate 409 (last view) — we'll just use the remaining one.
		if resp.StatusCode == http.StatusConflict {
			break
		}
	}

	remaining := listTimelineViewIDsViaAPI(t, env, client, token, projID)
	var onlyViewID string
	if len(remaining) == 1 {
		onlyViewID = remaining[0]
	} else {
		// Create a fresh one, delete all but one.
		onlyViewID = createTimelineViewViaAPI(t, env, client, token, projID, "Sole Timeline", "roadmap")
		for _, id := range remaining {
			deleteTimelineViewViaAPI(t, env, client, token, projID, id)
		}
	}

	t.Run("delete_last_view_returns_409", func(t *testing.T) {
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, onlyViewID)
		req := mustRequest(env.ctx, t, http.MethodDelete, url, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusConflict)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		if env2.ErrorCode != "VIEW_IS_LAST_VIEW" {
			t.Errorf("expected error code VIEW_IS_LAST_VIEW, got %q", env2.ErrorCode)
		}
	})
}

func TestE2ETimelineViewManagement_ContextIsolation(t *testing.T) {
	t.Parallel()
	// Verifies that creating timeline and backlog views for the same project
	// does not mix them up in their respective list endpoints.
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "timeline-isolation-user", "timelinepass3")
	client, token := taskMemberLogin(t, env, "timeline-isolation-user", "timelinepass3")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	tlViewID := createTimelineViewViaAPI(t, env, client, token, projID, "My Roadmap", "roadmap")
	blViewID := createBacklogViewViaAPI(t, env, client, token, projID, "My Backlog", "table")

	t.Run("timeline_list_excludes_backlog", func(t *testing.T) {
		ids := listTimelineViewIDsViaAPI(t, env, client, token, projID)
		for _, id := range ids {
			if id == blViewID {
				t.Errorf("timeline list contains backlog view id %s", blViewID)
			}
		}
		found := false
		for _, id := range ids {
			if id == tlViewID {
				found = true
			}
		}
		if !found {
			t.Errorf("timeline list missing expected timeline view %s", tlViewID)
		}
	})

	t.Run("backlog_list_excludes_timeline", func(t *testing.T) {
		ids := listBacklogViewIDsViaAPI(t, env, client, token, projID)
		for _, id := range ids {
			if id == tlViewID {
				t.Errorf("backlog list contains timeline view id %s", tlViewID)
			}
		}
		found := false
		for _, id := range ids {
			if id == blViewID {
				found = true
			}
		}
		if !found {
			t.Errorf("backlog list missing expected backlog view %s", blViewID)
		}
	})
}

// ---------------------------------------------------------------------------
// View filter persistence E2E tests
// ---------------------------------------------------------------------------

// TestE2EViewFilters_TaskTypesRoundtrip verifies that a view's
// config.filters.task_types FilterConfig (using the "normal" virtual group)
// persists through PATCH and is returned by a subsequent GET.
func TestE2EViewFilters_TaskTypesRoundtrip(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "vf-mode-user", "vfmodepass1")
	client, token := taskMemberLogin(t, env, "vf-mode-user", "vfmodepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Filter Mode Sprint")
	viewID := createViewViaAPI(t, env, client, token, projID, sprintID, "FilterTestView", "board")

	// PATCH the view config with a task_types FilterConfig using the "normal" group.
	patchURL := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
	body := jsonBody(t, map[string]any{
		"config": map[string]any{
			"filters": map[string]any{
				"task_types": map[string]any{
					"all":   false,
					"items": map[string]any{"normal": map[string]any{"all": true}},
				},
			},
		},
	})
	req := mustRequest(env.ctx, t, http.MethodPatch, patchURL, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	patchResp := mustDo(t, client, req)
	defer func() { _ = patchResp.Body.Close() }()
	assertStatus(t, patchResp, http.StatusOK)

	// GET the view back and verify task_types FilterConfig is preserved.
	getURL := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
	req2 := mustRequest(env.ctx, t, http.MethodGet, getURL, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	getResp := mustDo(t, client, req2)
	defer func() { _ = getResp.Body.Close() }()
	assertStatus(t, getResp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, getResp, &env2)
	data := assertDataMap(t, env2)
	cfg, _ := data["config"].(map[string]any)
	if cfg == nil {
		t.Fatal("expected non-nil config in view response")
	}
	filters, _ := cfg["filters"].(map[string]any)
	if filters == nil {
		t.Fatal("expected non-nil config.filters in view response")
	}
	taskTypes, _ := filters["task_types"].(map[string]any)
	if taskTypes == nil {
		t.Fatalf("expected task_types in filters, got %v", filters)
	}
	items, _ := taskTypes["items"].(map[string]any)
	normalGroup, _ := items["normal"].(map[string]any)
	if normalGroup == nil || normalGroup["all"] != true {
		t.Errorf("expected task_types.items.normal={all:true}, got %v", items["normal"])
	}
}

// TestE2EViewFilters_SprintIDsRoundtrip verifies that sprint_ids saved in
// config.filters persist correctly (covers the previously-missing SprintIDs
// DTO field serialisation bug).
func TestE2EViewFilters_SprintIDsRoundtrip(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "vf-sprintids-user", "vfsprintpass1")
	client, token := taskMemberLogin(t, env, "vf-sprintids-user", "vfsprintpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "SprintIDs Filter Sprint")
	viewID := createViewViaAPI(t, env, client, token, projID, sprintID, "SprintFilterView", "table")

	// PATCH with sprints FilterConfig.
	fakeSprintID1 := "00000000-0000-0000-0000-000000000001"
	fakeSprintID2 := "00000000-0000-0000-0000-000000000002"
	patchURL := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
	body := jsonBody(t, map[string]any{
		"config": map[string]any{
			"filters": map[string]any{
				"sprints": map[string]any{
					"all": false,
					"items": map[string]any{
						fakeSprintID1: true,
						fakeSprintID2: true,
					},
				},
			},
		},
	})
	req := mustRequest(env.ctx, t, http.MethodPatch, patchURL, body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	patchResp := mustDo(t, client, req)
	defer func() { _ = patchResp.Body.Close() }()
	assertStatus(t, patchResp, http.StatusOK)

	// GET the view back and verify the sprints FilterConfig is preserved.
	req2 := mustRequest(env.ctx, t, http.MethodGet, patchURL, nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	getResp := mustDo(t, client, req2)
	defer func() { _ = getResp.Body.Close() }()
	assertStatus(t, getResp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, getResp, &env2)
	data := assertDataMap(t, env2)
	cfg, _ := data["config"].(map[string]any)
	if cfg == nil {
		t.Fatal("expected non-nil config in view response")
	}
	filters, _ := cfg["filters"].(map[string]any)
	if filters == nil {
		t.Fatal("expected non-nil config.filters in view response")
	}
	sprintsFilter, _ := filters["sprints"].(map[string]any)
	if sprintsFilter == nil {
		t.Fatalf("expected sprints filter in config.filters, got %v", filters)
	}
	sprintItems, _ := sprintsFilter["items"].(map[string]any)
	if len(sprintItems) != 2 {
		t.Errorf("expected 2 sprint items, got %d: %v", len(sprintItems), sprintItems)
	}
}
