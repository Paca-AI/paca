package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// listTasksPage issues GET /projects/:id/tasks with the given query params and
// asserts a 200 response, returning the decoded data map.
func listTasksPage(t *testing.T, env *e2eEnv, client *http.Client, token, projID string, q url.Values) map[string]any {
	t.Helper()
	reqURL := fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID)
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

// nextCursorStr extracts the next_cursor string from a list-tasks data map.
// Returns "" when next_cursor is absent or null.
func nextCursorStr(data map[string]any) string {
	if v, ok := data["next_cursor"]; ok && v != nil {
		s, _ := v.(string)
		return s
	}
	return ""
}

// totalCountFromData extracts total_count from a list-tasks data map as int64.
func totalCountFromData(data map[string]any) int64 {
	v, _ := data["total_count"].(float64)
	return int64(v)
}

// itemIDs extracts the "id" field from every item in a list-tasks data map.
func itemIDs(data map[string]any) []string {
	items, _ := data["items"].([]any)
	ids := make([]string, 0, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		id, _ := item["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// ---------------------------------------------------------------------------
// TestE2EListTaskPagination_CursorBased
// Tests the cursor-based pagination on the general ListTasks endpoint
// (GET /projects/:projectId/tasks).
// ---------------------------------------------------------------------------

func TestE2EListTaskPagination_CursorBased(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cursor-pag-user", "cursorpagpass1")
	client, token := taskMemberLogin(t, env, "cursor-pag-user", "cursorpagpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	// createBacklogTask creates a task with no sprint assignment.
	createBacklogTask := func(title string) string {
		t.Helper()
		body := jsonBody(t, map[string]any{"title": title})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		id, _ := data["id"].(string)
		return id
	}

	// Create 5 tasks for all sub-tests in this group.
	var allTaskIDs []string
	for i := 0; i < 5; i++ {
		allTaskIDs = append(allTaskIDs, createBacklogTask(fmt.Sprintf("Cursor Task %d", i+1)))
	}

	t.Run("first_page_returns_page_size_items_with_next_cursor", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"page_size": {"3"}})
		items, _ := data["items"].([]any)
		if len(items) != 3 {
			t.Errorf("expected 3 items on first page, got %d", len(items))
		}
		if nextCursorStr(data) == "" {
			t.Error("expected next_cursor to be set when more tasks exist beyond first page")
		}
		if got := totalCountFromData(data); got != 5 {
			t.Errorf("expected total_count=5 on first page, got %d", got)
		}
	})

	t.Run("second_page_via_cursor_has_remaining_items_and_no_cursor", func(t *testing.T) {
		firstPage := listTasksPage(t, env, client, token, projID, url.Values{"page_size": {"3"}})
		cursor := nextCursorStr(firstPage)
		if cursor == "" {
			t.Fatal("expected non-empty next_cursor from first page")
		}

		secondPage := listTasksPage(t, env, client, token, projID, url.Values{
			"page_size": {"3"},
			"cursor":    {cursor},
		})
		items, _ := secondPage["items"].([]any)
		if len(items) != 2 {
			t.Errorf("expected 2 remaining items on second page (5 total, 3 on first), got %d", len(items))
		}
		if nextCursorStr(secondPage) != "" {
			t.Error("expected next_cursor to be absent on the last page")
		}
		if got := totalCountFromData(secondPage); got != 5 {
			t.Errorf("expected total_count=5 on second page (cursor-independent), got %d", got)
		}
	})

	t.Run("no_next_cursor_when_all_tasks_fit_in_one_page", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"page_size": {"10"}})
		items, _ := data["items"].([]any)
		if len(items) != 5 {
			t.Errorf("expected 5 items when page_size exceeds task count, got %d", len(items))
		}
		if nextCursorStr(data) != "" {
			t.Error("expected no next_cursor when all tasks fit in one page")
		}
	})

	t.Run("full_traversal_returns_all_tasks_without_duplicates", func(t *testing.T) {
		seen := make(map[string]int)
		cursor := ""
		for {
			q := url.Values{"page_size": {"2"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			data := listTasksPage(t, env, client, token, projID, q)
			for _, id := range itemIDs(data) {
				seen[id]++
			}
			cursor = nextCursorStr(data)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 5 {
			t.Errorf("expected 5 unique tasks after full traversal, got %d", len(seen))
		}
		for _, id := range allTaskIDs {
			if seen[id] == 0 {
				t.Errorf("task %q was not returned during full traversal", id)
			}
			if seen[id] > 1 {
				t.Errorf("task %q was returned %d times (duplicate)", id, seen[id])
			}
		}
	})

	t.Run("page_size_zero_is_rejected", func(t *testing.T) {
		q := url.Values{"page_size": {"0"}}
		reqURL := fmt.Sprintf("%s/api/v1/projects/%s/tasks?%s", env.base, projID, q.Encode())
		req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "BAD_REQUEST")
	})

	t.Run("page_size_over_max_is_rejected", func(t *testing.T) {
		q := url.Values{"page_size": {"201"}}
		reqURL := fmt.Sprintf("%s/api/v1/projects/%s/tasks?%s", env.base, projID, q.Encode())
		req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "BAD_REQUEST")
	})

	t.Run("invalid_cursor_returns_error", func(t *testing.T) {
		q := url.Values{"cursor": {"not-a-valid-base64-cursor"}}
		reqURL := fmt.Sprintf("%s/api/v1/projects/%s/tasks?%s", env.base, projID, q.Encode())
		req := mustRequest(env.ctx, t, http.MethodGet, reqURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == http.StatusOK {
			t.Errorf("expected a non-200 error response for invalid cursor, got 200")
		}
	})
}

// ---------------------------------------------------------------------------
// TestE2EListTaskPagination_CursorWithSprintFilter
// Tests that cursor pagination works correctly when combined with a
// sprint_id filter — only sprint tasks are paginated; backlog tasks are excluded.
// ---------------------------------------------------------------------------

func TestE2EListTaskPagination_CursorWithSprintFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cursor-sprint-filter-user", "cursorsprintfilterpass1")
	client, token := taskMemberLogin(t, env, "cursor-sprint-filter-user", "cursorsprintfilterpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Pagination Sprint")

	createSprintTask := func(title string) string {
		t.Helper()
		body := jsonBody(t, map[string]any{"title": title, "sprint_id": sprintID})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		id, _ := data["id"].(string)
		return id
	}

	// 4 sprint tasks + 2 backlog tasks that must not appear in sprint results.
	var sprintTaskIDs []string
	for i := 0; i < 4; i++ {
		sprintTaskIDs = append(sprintTaskIDs, createSprintTask(fmt.Sprintf("Sprint Pag Task %d", i+1)))
	}
	for i := 0; i < 2; i++ {
		body := jsonBody(t, map[string]any{"title": fmt.Sprintf("Backlog Noise Task %d", i+1)})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		_ = resp.Body.Close()
		assertStatus(t, resp, http.StatusCreated)
	}

	t.Run("first_sprint_page_has_cursor_when_more_exist", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"sprint_id": {sprintID},
			"page_size": {"2"},
		})
		items, _ := data["items"].([]any)
		if len(items) != 2 {
			t.Errorf("expected 2 sprint tasks on first page, got %d", len(items))
		}
		if nextCursorStr(data) == "" {
			t.Error("expected next_cursor when more sprint tasks exist beyond first page")
		}
		if got := totalCountFromData(data); got != 4 {
			t.Errorf("expected total_count=4 (sprint tasks only), got %d", got)
		}
	})

	t.Run("full_sprint_traversal_returns_only_sprint_tasks_without_duplicates", func(t *testing.T) {
		seen := make(map[string]int)
		cursor := ""
		for {
			q := url.Values{"sprint_id": {sprintID}, "page_size": {"2"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			data := listTasksPage(t, env, client, token, projID, q)
			for _, id := range itemIDs(data) {
				seen[id]++
			}
			cursor = nextCursorStr(data)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 4 {
			t.Errorf("expected 4 sprint tasks from full traversal, got %d", len(seen))
		}
		for _, id := range sprintTaskIDs {
			if seen[id] == 0 {
				t.Errorf("sprint task %q was not returned during traversal", id)
			}
			if seen[id] > 1 {
				t.Errorf("sprint task %q appeared %d times (duplicate)", id, seen[id])
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestE2EListTaskPagination_CursorWithBacklogFilter
// Tests that cursor pagination works correctly with the sprint_id=null backlog
// filter — only backlog tasks are paginated; sprint tasks are excluded.
// ---------------------------------------------------------------------------

func TestE2EListTaskPagination_CursorWithBacklogFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cursor-backlog-filter-user", "cursorbacklogfilterpass1")
	client, token := taskMemberLogin(t, env, "cursor-backlog-filter-user", "cursorbacklogfilterpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint for Backlog Pagination Test")

	createTask := func(title string, inSprint bool) string {
		t.Helper()
		body := map[string]any{"title": title}
		if inSprint {
			body["sprint_id"] = sprintID
		}
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		id, _ := data["id"].(string)
		return id
	}

	// 4 backlog tasks + 2 sprint tasks that must not appear in backlog results.
	var backlogTaskIDs []string
	for i := 0; i < 4; i++ {
		backlogTaskIDs = append(backlogTaskIDs, createTask(fmt.Sprintf("Backlog Pag Task %d", i+1), false))
	}
	for i := 0; i < 2; i++ {
		createTask(fmt.Sprintf("Sprint Noise Task %d", i+1), true)
	}

	t.Run("first_backlog_page_has_cursor_when_more_exist", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"sprint_id": {"null"},
			"page_size": {"2"},
		})
		items, _ := data["items"].([]any)
		if len(items) != 2 {
			t.Errorf("expected 2 backlog tasks on first page, got %d", len(items))
		}
		if nextCursorStr(data) == "" {
			t.Error("expected next_cursor when more backlog tasks exist beyond first page")
		}
		if got := totalCountFromData(data); got != 4 {
			t.Errorf("expected total_count=4 (backlog tasks only), got %d", got)
		}
	})

	t.Run("full_backlog_traversal_returns_only_backlog_tasks_without_duplicates", func(t *testing.T) {
		seen := make(map[string]int)
		cursor := ""
		for {
			q := url.Values{"sprint_id": {"null"}, "page_size": {"2"}}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			data := listTasksPage(t, env, client, token, projID, q)
			for _, id := range itemIDs(data) {
				seen[id]++
			}
			cursor = nextCursorStr(data)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 4 {
			t.Errorf("expected 4 backlog tasks from full traversal, got %d", len(seen))
		}
		for _, id := range backlogTaskIDs {
			if seen[id] == 0 {
				t.Errorf("backlog task %q was not returned during traversal", id)
			}
			if seen[id] > 1 {
				t.Errorf("backlog task %q appeared %d times (duplicate)", id, seen[id])
			}
		}
	})
}

// ---------------------------------------------------------------------------
// TestE2EListTaskTotalCount
// Tests that the total_count field in list-task responses reflects the full
// matching set regardless of pagination, filter, or deletions.
// ---------------------------------------------------------------------------

func TestE2EListTaskTotalCount_NoFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "total-count-basic-user", "totalcountpass1")
	client, token := taskMemberLogin(t, env, "total-count-basic-user", "totalcountpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	for i := range 4 {
		body := jsonBody(t, map[string]any{"title": fmt.Sprintf("Task %d", i+1)})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		_ = resp.Body.Close()
		assertStatus(t, resp, http.StatusCreated)
	}

	data := listTasksPage(t, env, client, token, projID, nil)
	if got := totalCountFromData(data); got != 4 {
		t.Errorf("expected total_count=4, got %d", got)
	}
}

func TestE2EListTaskTotalCount_SprintFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "total-count-sprint-user", "totalcountpass2")
	client, token := taskMemberLogin(t, env, "total-count-sprint-user", "totalcountpass2")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "TC Sprint")

	createTask := func(title string, sprint *string) {
		t.Helper()
		body := map[string]any{"title": title}
		if sprint != nil {
			body["sprint_id"] = *sprint
		}
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		_ = resp.Body.Close()
		assertStatus(t, resp, http.StatusCreated)
	}

	// 3 sprint tasks, 2 backlog tasks.
	for i := range 3 {
		createTask(fmt.Sprintf("Sprint task %d", i+1), &sprintID)
	}
	for i := range 2 {
		createTask(fmt.Sprintf("Backlog task %d", i+1), nil)
	}

	// total_count when filtering by sprint must be 3, not 5.
	data := listTasksPage(t, env, client, token, projID, url.Values{"sprint_id": {sprintID}})
	if got := totalCountFromData(data); got != 3 {
		t.Errorf("expected total_count=3 for sprint filter, got %d", got)
	}
	items, _ := data["items"].([]any)
	if len(items) != 3 {
		t.Errorf("expected 3 items in sprint response, got %d", len(items))
	}
}

func TestE2EListTaskTotalCount_BacklogFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "total-count-backlog-user", "totalcountpass3")
	client, token := taskMemberLogin(t, env, "total-count-backlog-user", "totalcountpass3")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "TC Backlog Sprint")

	createTask := func(title string, sprint *string) {
		t.Helper()
		body := map[string]any{"title": title}
		if sprint != nil {
			body["sprint_id"] = *sprint
		}
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		_ = resp.Body.Close()
		assertStatus(t, resp, http.StatusCreated)
	}

	// 2 sprint tasks, 5 backlog tasks.
	for i := range 2 {
		createTask(fmt.Sprintf("Sprint task %d", i+1), &sprintID)
	}
	for i := range 5 {
		createTask(fmt.Sprintf("Backlog task %d", i+1), nil)
	}

	// total_count when filtering by backlog must be 5, not 7.
	data := listTasksPage(t, env, client, token, projID, url.Values{"sprint_id": {"null"}})
	if got := totalCountFromData(data); got != 5 {
		t.Errorf("expected total_count=5 for backlog filter, got %d", got)
	}
	items, _ := data["items"].([]any)
	if len(items) != 5 {
		t.Errorf("expected 5 items in backlog response, got %d", len(items))
	}
}

func TestE2EListTaskTotalCount_ExcludesDeleted(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "total-count-delete-user", "totalcountpass4")
	client, token := taskMemberLogin(t, env, "total-count-delete-user", "totalcountpass4")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createAndGetID := func(title string) string {
		t.Helper()
		body := jsonBody(t, map[string]any{"title": title})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		id, _ := data["id"].(string)
		return id
	}

	id1 := createAndGetID("Task 1")
	createAndGetID("Task 2")
	createAndGetID("Task 3")

	// Delete task 1.
	req := mustRequest(env.ctx, t, http.MethodDelete,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, id1), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	_ = resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	data := listTasksPage(t, env, client, token, projID, nil)
	if got := totalCountFromData(data); got != 2 {
		t.Errorf("expected total_count=2 after deleting 1 of 3 tasks, got %d", got)
	}
}

func TestE2EListTaskTotalCount_CursorDoesNotAffectCount(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "total-count-cursor-user", "totalcountpass5")
	client, token := taskMemberLogin(t, env, "total-count-cursor-user", "totalcountpass5")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	for i := range 6 {
		body := jsonBody(t, map[string]any{"title": fmt.Sprintf("Task %d", i+1)})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		_ = resp.Body.Close()
		assertStatus(t, resp, http.StatusCreated)
	}

	firstPage := listTasksPage(t, env, client, token, projID, url.Values{"page_size": {"2"}})
	if got := totalCountFromData(firstPage); got != 6 {
		t.Errorf("first page: expected total_count=6, got %d", got)
	}
	cursor := nextCursorStr(firstPage)
	if cursor == "" {
		t.Fatal("expected next_cursor on first page")
	}

	secondPage := listTasksPage(t, env, client, token, projID, url.Values{
		"page_size": {"2"},
		"cursor":    {cursor},
	})
	if got := totalCountFromData(secondPage); got != 6 {
		t.Errorf("second page: expected total_count=6 (cursor-independent), got %d", got)
	}
}

// ---------------------------------------------------------------------------
// fieldSumFromData helper
// ---------------------------------------------------------------------------

// fieldSumFromData extracts field_sum from a list-tasks data map.
// Returns (0, false) when field_sum is absent or null.
func fieldSumFromData(data map[string]any) (float64, bool) {
	v, ok := data["field_sum"]
	if !ok || v == nil {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

// createTaskWithCustomFieldViaAPI creates a task and sets a numeric custom field value.
func createTaskWithCustomFieldViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projID, title string, customFields map[string]any) {
	t.Helper()
	body := map[string]any{"title": title}
	if len(customFields) > 0 {
		body["custom_fields"] = customFields
	}
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	_ = resp.Body.Close()
	assertStatus(t, resp, http.StatusCreated)
}

// ---------------------------------------------------------------------------
// TestE2EFieldSum_CustomField — sum_field with a numeric custom task field
// ---------------------------------------------------------------------------

func TestE2EFieldSum_CustomField_BasicSum(t *testing.T) {
	t.Parallel()
	// Verifies that sum_field=<custom_key> returns the correct aggregate sum
	// across all matching tasks in the project.
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sum-field-basic-user", "sumfieldpass1")
	client, token := taskMemberLogin(t, env, "sum-field-basic-user", "sumfieldpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	// Register a numeric custom field definition so the field is discoverable.
	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "effort",
		"display_name": "Effort",
		"field_type":   "number",
	})

	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Task A", map[string]any{"effort": 3})
	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Task B", map[string]any{"effort": 5})
	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Task C", map[string]any{"effort": 7})
	// Task with no custom field — should contribute 0.
	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Task D (no effort)", nil)

	data := listTasksPage(t, env, client, token, projID, url.Values{"sum_field": {"effort"}})
	sum, ok := fieldSumFromData(data)
	if !ok {
		t.Fatal("expected field_sum in response, got null/absent")
	}
	if sum != 15 {
		t.Errorf("expected field_sum=15 (3+5+7), got %v", sum)
	}
}

func TestE2EFieldSum_CustomField_FilterBySprint(t *testing.T) {
	t.Parallel()
	// Verifies that sum_field=<custom_key> respects sprint_id filter.
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sum-field-sprint-user", "sumfieldpass2")
	client, token := taskMemberLogin(t, env, "sum-field-sprint-user", "sumfieldpass2")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Effort Sprint")

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "effort",
		"display_name": "Effort",
		"field_type":   "number",
	})

	createTaskWithSprintAndCustomField := func(title string, sprint *string, effort float64) {
		t.Helper()
		body := map[string]any{"title": title, "custom_fields": map[string]any{"effort": effort}}
		if sprint != nil {
			body["sprint_id"] = *sprint
		}
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		_ = resp.Body.Close()
		assertStatus(t, resp, http.StatusCreated)
	}

	// Sprint tasks: effort 4 + 6 = 10.
	createTaskWithSprintAndCustomField("Sprint A", &sprintID, 4)
	createTaskWithSprintAndCustomField("Sprint B", &sprintID, 6)
	// Backlog tasks: effort 20 + 30 (must not be included in sprint sum).
	createTaskWithSprintAndCustomField("Backlog A", nil, 20)
	createTaskWithSprintAndCustomField("Backlog B", nil, 30)

	data := listTasksPage(t, env, client, token, projID, url.Values{
		"sprint_id": {sprintID},
		"sum_field": {"effort"},
	})
	sum, ok := fieldSumFromData(data)
	if !ok {
		t.Fatal("expected field_sum in response")
	}
	if sum != 10 {
		t.Errorf("expected field_sum=10 (sprint tasks only), got %v", sum)
	}
}

func TestE2EFieldSum_CustomField_BacklogOnly(t *testing.T) {
	t.Parallel()
	// Verifies that sum_field=<custom_key> respects sprint_id=null (backlog) filter.
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sum-field-backlog-user", "sumfieldpass3")
	client, token := taskMemberLogin(t, env, "sum-field-backlog-user", "sumfieldpass3")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Effort Sprint")

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "effort",
		"display_name": "Effort",
		"field_type":   "number",
	})

	createTaskWithSprintAndCustomField := func(title string, sprint *string, effort float64) {
		t.Helper()
		body := map[string]any{"title": title, "custom_fields": map[string]any{"effort": effort}}
		if sprint != nil {
			body["sprint_id"] = *sprint
		}
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		_ = resp.Body.Close()
		assertStatus(t, resp, http.StatusCreated)
	}

	// Backlog tasks: effort 2 + 8 = 10.
	createTaskWithSprintAndCustomField("Backlog A", nil, 2)
	createTaskWithSprintAndCustomField("Backlog B", nil, 8)
	// Sprint task: effort 100 (must be excluded).
	createTaskWithSprintAndCustomField("Sprint task", &sprintID, 100)

	data := listTasksPage(t, env, client, token, projID, url.Values{
		"sprint_id": {"null"},
		"sum_field": {"effort"},
	})
	sum, ok := fieldSumFromData(data)
	if !ok {
		t.Fatal("expected field_sum in response")
	}
	if sum != 10 {
		t.Errorf("expected field_sum=10 (backlog tasks only), got %v", sum)
	}
}

func TestE2EFieldSum_CustomField_IgnoresCursor(t *testing.T) {
	t.Parallel()
	// Verifies that sum_field reflects all matching tasks regardless of cursor pagination.
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sum-field-cursor-user", "sumfieldpass4")
	client, token := taskMemberLogin(t, env, "sum-field-cursor-user", "sumfieldpass4")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "effort",
		"display_name": "Effort",
		"field_type":   "number",
	})

	for i := range 5 {
		createTaskWithCustomFieldViaAPI(t, env, client, token, projID,
			fmt.Sprintf("Task %d", i+1), map[string]any{"effort": 10})
	}

	// First page (2 items): field_sum must reflect all 5 tasks = 50.
	firstPage := listTasksPage(t, env, client, token, projID, url.Values{
		"page_size": {"2"},
		"sum_field": {"effort"},
	})
	sum1, ok1 := fieldSumFromData(firstPage)
	if !ok1 {
		t.Fatal("first page: expected field_sum in response")
	}
	if sum1 != 50 {
		t.Errorf("first page: expected field_sum=50 (5×10), got %v", sum1)
	}

	cursor := nextCursorStr(firstPage)
	if cursor == "" {
		t.Fatal("expected next_cursor on first page")
	}

	// Second page with cursor: field_sum must still equal 50.
	secondPage := listTasksPage(t, env, client, token, projID, url.Values{
		"page_size": {"2"},
		"cursor":    {cursor},
		"sum_field": {"effort"},
	})
	sum2, ok2 := fieldSumFromData(secondPage)
	if !ok2 {
		t.Fatal("second page: expected field_sum in response")
	}
	if sum2 != 50 {
		t.Errorf("second page: expected field_sum=50 (cursor-independent), got %v", sum2)
	}
}

func TestE2EFieldSum_CustomField_AbsentWhenNotRequested(t *testing.T) {
	t.Parallel()
	// Verifies that field_sum is null when sum_field query param is not provided.
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sum-field-absent-user", "sumfieldpass5")
	client, token := taskMemberLogin(t, env, "sum-field-absent-user", "sumfieldpass5")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Task", map[string]any{"effort": 99})

	data := listTasksPage(t, env, client, token, projID, nil)
	_, ok := fieldSumFromData(data)
	if ok {
		t.Error("expected field_sum to be null/absent when sum_field param is not set")
	}
}

// ---------------------------------------------------------------------------
// TestE2EListTaskPagination_ViewPositionSort
// Verifies that GET /projects/:id/tasks?view_id=<id> returns tasks in manual
// position order (not created_at order) and that cursor pagination correctly
// traverses all tasks without duplicates when sorted by view_position.
// ---------------------------------------------------------------------------

func TestE2EListTaskPagination_ViewPositionSort(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "view-pos-sort-user", "viewpossort1")
	client, token := taskMemberLogin(t, env, "view-pos-sort-user", "viewpossort1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	// Create a product-backlog view (no sprint needed).
	viewID := createBacklogViewViaAPI(t, env, client, token, projID, "Manual Sort View", "table")
	bulkPosURL := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions", env.base, projID, viewID)

	// Helper: create a backlog task and return its ID.
	createTask := func(title string) string {
		t.Helper()
		body := jsonBody(t, map[string]any{"title": title})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
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
			t.Fatal("missing task id in create response")
		}
		return id
	}

	// Create 5 tasks. Because they are inserted sequentially, their created_at
	// timestamps are ordered t1 < t2 < t3 < t4 < t5.
	t1 := createTask("Task 1")
	t2 := createTask("Task 2")
	t3 := createTask("Task 3")
	t4 := createTask("Task 4")
	t5 := createTask("Task 5")

	// Assign manual positions in a deliberately different order from created_at:
	//   position 10000 → Task 3
	//   position 20000 → Task 1
	//   position 30000 → Task 5
	// Tasks 2 and 4 are left without a saved position (fallback: created_at ASC).
	// Expected order: t3, t1, t5, t2, t4.
	savePositions := func(items []map[string]any) {
		t.Helper()
		body := jsonBody(t, map[string]any{"items": items})
		req := mustRequest(env.ctx, t, http.MethodPut, bulkPosURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	}
	savePositions([]map[string]any{
		{"task_id": t3, "position": 10000},
		{"task_id": t1, "position": 20000},
		{"task_id": t5, "position": 30000},
	})

	wantOrder := []string{t3, t1, t5, t2, t4}

	t.Run("all_tasks_returned_in_position_order", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"sprint_id": {"null"},
			"view_id":   {viewID},
			"page_size": {"10"},
		})
		ids := itemIDs(data)
		if len(ids) != 5 {
			t.Fatalf("expected 5 tasks, got %d: %v", len(ids), ids)
		}
		for i, want := range wantOrder {
			if ids[i] != want {
				t.Errorf("position %d: expected task %s, got %s (full order: %v)", i, want, ids[i], ids)
			}
		}
	})

	t.Run("cursor_pagination_traverses_all_in_position_order", func(t *testing.T) {
		// Page through all tasks 2 at a time and collect them in order.
		var collected []string
		var cursor string
		for {
			q := url.Values{
				"sprint_id": {"null"},
				"view_id":   {viewID},
				"page_size": {"2"},
			}
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			data := listTasksPage(t, env, client, token, projID, q)
			collected = append(collected, itemIDs(data)...)
			cursor = nextCursorStr(data)
			if cursor == "" {
				break
			}
		}
		if len(collected) != 5 {
			t.Fatalf("expected 5 tasks after full traversal, got %d: %v", len(collected), collected)
		}
		for i, want := range wantOrder {
			if collected[i] != want {
				t.Errorf("traversal position %d: expected %s, got %s (full: %v)", i, want, collected[i], collected)
			}
		}
		// No duplicates.
		seen := make(map[string]int, 5)
		for _, id := range collected {
			seen[id]++
		}
		for _, id := range wantOrder {
			if seen[id] != 1 {
				t.Errorf("task %s appeared %d times (expected exactly once)", id, seen[id])
			}
		}
	})

	t.Run("first_page_order_matches_position_sort", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"sprint_id": {"null"},
			"view_id":   {viewID},
			"page_size": {"3"},
		})
		ids := itemIDs(data)
		if len(ids) != 3 {
			t.Fatalf("expected 3 items on first page, got %d", len(ids))
		}
		for i, want := range wantOrder[:3] {
			if ids[i] != want {
				t.Errorf("first page[%d]: expected %s, got %s", i, want, ids[i])
			}
		}
		if nextCursorStr(data) == "" {
			t.Error("expected next_cursor on first page (5 tasks, page_size=3)")
		}
	})

	t.Run("second_page_continues_position_order", func(t *testing.T) {
		firstPage := listTasksPage(t, env, client, token, projID, url.Values{
			"sprint_id": {"null"},
			"view_id":   {viewID},
			"page_size": {"3"},
		})
		cursor := nextCursorStr(firstPage)
		if cursor == "" {
			t.Fatal("expected next_cursor on first page")
		}

		secondPage := listTasksPage(t, env, client, token, projID, url.Values{
			"sprint_id": {"null"},
			"view_id":   {viewID},
			"page_size": {"3"},
			"cursor":    {cursor},
		})
		ids := itemIDs(secondPage)
		if len(ids) != 2 {
			t.Fatalf("expected 2 items on second page (5 total, 3 on first), got %d", len(ids))
		}
		for i, want := range wantOrder[3:] {
			if ids[i] != want {
				t.Errorf("second page[%d]: expected %s, got %s", i, want, ids[i])
			}
		}
		if nextCursorStr(secondPage) != "" {
			t.Error("expected no next_cursor on last page")
		}
	})

	t.Run("without_view_id_no_position_sort", func(t *testing.T) {
		// Without view_id the default sort is created_at ASC: t1, t2, t3, t4, t5.
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"sprint_id": {"null"},
			"page_size": {"10"},
		})
		ids := itemIDs(data)
		if len(ids) != 5 {
			t.Fatalf("expected 5 tasks, got %d", len(ids))
		}
		// Default order must be creation order (t1…t5), NOT position order.
		defaultOrder := []string{t1, t2, t3, t4, t5}
		for i, want := range defaultOrder {
			if ids[i] != want {
				t.Errorf("default order[%d]: expected %s, got %s (full: %v)", i, want, ids[i], ids)
			}
		}
	})
}

// TestE2ECustomFieldFilters_FieldKeyReuseAfterDeleteDoesNotCrash is a
// regression test: deleting a custom field definition used to leave its
// stray values behind under that key in every task's custom_fields JSONB
// column. If a new field definition was then created reusing the same
// field_key but an incompatible type (e.g. the original field was "select"
// and the new one is "date"), filtering by the new field crashed the whole
// query with a Postgres cast error (SQLSTATE 22007: invalid input syntax for
// type date) on the leftover value, instead of just excluding that task.
func TestE2ECustomFieldFilters_FieldKeyReuseAfterDeleteDoesNotCrash(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-reuse-user", "cfreusepass1")
	client, token := taskMemberLogin(t, env, "cf-reuse-user", "cfreusepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	selectFieldID := createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "custom_1",
		"display_name": "Custom 1",
		"field_type":   "select",
		"options":      []string{"Test 3", "Other"},
	})
	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Stale select value",
		map[string]any{"custom_1": "Test 3"})

	delReq := mustRequest(env.ctx, t, http.MethodDelete,
		fmt.Sprintf("%s/api/v1/projects/%s/custom-fields/%s", env.base, projID, selectFieldID), nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	delResp := mustDo(t, client, delReq)
	_ = delResp.Body.Close()
	assertStatus(t, delResp, http.StatusOK)

	// Recreate a field with the same field_key but an incompatible type.
	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "custom_1",
		"display_name": "Custom 1 (date)",
		"field_type":   "date",
	})
	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Valid date value",
		map[string]any{"custom_1": "2024-03-15"})

	filters, err := json.Marshal(map[string]any{
		"custom_1": map[string]any{"after": "2024-01-01", "before": "2024-12-31"},
	})
	if err != nil {
		t.Fatalf("marshal filters: %v", err)
	}

	data := listTasksPage(t, env, client, token, projID, url.Values{
		"custom_field_filters": {string(filters)},
	})
	ids := itemIDs(data)
	if len(ids) != 1 {
		t.Fatalf("expected exactly 1 matching task (the valid date; the stale select value must be excluded, not crash the query), got %d: %v", len(ids), ids)
	}
}

// TestE2ECustomFieldFilters_InvalidStoredValueDoesNotCrash is a regression
// test for the more general case underlying the same bug class: custom_fields
// has no write-time type validation (task creation/update accepts any JSON
// value regardless of the field's declared type), so a task can end up with
// a value that doesn't match its field's type through any write path, not
// just field deletion + key reuse. Filtering or sorting by that field must
// exclude the invalid row instead of crashing the whole query.
func TestE2ECustomFieldFilters_InvalidStoredValueDoesNotCrash(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-invalid-user", "cfinvalidpass1")
	client, token := taskMemberLogin(t, env, "cf-invalid-user", "cfinvalidpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "due",
		"display_name": "Due",
		"field_type":   "date",
	})
	// No write-time type validation exists today, so this succeeds despite
	// "not-a-date" being an invalid value for a "date" field.
	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Bad value",
		map[string]any{"due": "not-a-date"})
	createTaskWithCustomFieldViaAPI(t, env, client, token, projID, "Good value",
		map[string]any{"due": "2024-03-15"})

	filters, err := json.Marshal(map[string]any{
		"due": map[string]any{"after": "2024-01-01", "before": "2024-12-31"},
	})
	if err != nil {
		t.Fatalf("marshal filters: %v", err)
	}

	data := listTasksPage(t, env, client, token, projID, url.Values{
		"custom_field_filters": {string(filters)},
	})
	ids := itemIDs(data)
	if len(ids) != 1 {
		t.Fatalf("expected exactly 1 matching task (invalid date value must be excluded, not crash the query), got %d: %v", len(ids), ids)
	}

	// Sorting by the same field must not crash either — it hits the same
	// unguarded-cast bug class in buildCFOrderBy.
	sortData := listTasksPage(t, env, client, token, projID, url.Values{"sort_by": {"due"}})
	if got := len(itemIDs(sortData)); got != 2 {
		t.Fatalf("expected sort_by=due to return both tasks without crashing, got %d: %v", got, itemIDs(sortData))
	}
}
