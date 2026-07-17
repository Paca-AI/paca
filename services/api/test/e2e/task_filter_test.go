package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// createTaskViaAPIWithBody creates a task from an arbitrary request body and
// returns the full decoded task response (id plus every field echoed back),
// so filter tests can set any combination of built-in/custom fields on one
// task and later reference its id.
func createTaskViaAPIWithBody(t *testing.T, env *e2eEnv, client *http.Client, token, projID string, body map[string]any) map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), jsonBody(t, body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	return assertDataMap(t, env2)
}

// idOf extracts the "id" field from a decoded task/entity response map.
func idOf(data map[string]any) string {
	s, _ := data["id"].(string)
	return s
}

// idsEqualSet reports whether got and want contain exactly the same set of
// ids, ignoring order and duplicates.
func idsEqualSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	set := make(map[string]bool, len(want))
	for _, w := range want {
		set[w] = true
	}
	for _, g := range got {
		if !set[g] {
			return false
		}
	}
	return true
}

// mustJSONString marshals v to a JSON string, for building the
// custom_field_filters / importance_ranges query parameter values.
func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(b)
}

// getTasksExpectBadRequest issues GET /tasks with the given raw query string
// and asserts a 400 BAD_REQUEST response.
func getTasksExpectBadRequest(t *testing.T, env *e2eEnv, client *http.Client, token, projID, rawQuery string) {
	t.Helper()
	path := fmt.Sprintf("%s/api/v1/projects/%s/tasks?%s", env.base, projID, rawQuery)
	req := mustRequest(env.ctx, t, http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusBadRequest)
	assertErrorCode(t, resp, "BAD_REQUEST")
}

// ---------------------------------------------------------------------------
// Custom field filters — select
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_CustomFieldSelect(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-select-filter-user", "cfselectpass1")
	client, token := taskMemberLogin(t, env, "cf-select-filter-user", "cfselectpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "priority",
		"display_name": "Priority",
		"field_type":   "select",
		"options":      []string{"Open", "Closed", "Blocked"},
	})

	open1 := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Open 1", "custom_fields": map[string]any{"priority": "Open"},
	})
	open2 := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Open 2", "custom_fields": map[string]any{"priority": "Open"},
	})
	closed1 := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Closed 1", "custom_fields": map[string]any{"priority": "Closed"},
	})
	blocked1 := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Blocked 1", "custom_fields": map[string]any{"priority": "Blocked"},
	})
	noValue := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "No Priority Set",
	})

	t.Run("single_value_matches_only_that_value", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"priority": map[string]any{"values": []string{"Open"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		want := []string{idOf(open1), idOf(open2)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("multiple_values_match_any", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"priority": map[string]any{"values": []string{"Closed", "Blocked"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		want := []string{idOf(closed1), idOf(blocked1)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("no_match_returns_empty", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"priority": map[string]any{"values": []string{"NoSuchOption"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); len(got) != 0 {
			t.Fatalf("expected no matches, got %v", got)
		}
	})

	t.Run("task_with_no_value_excluded_when_filter_active", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"priority": map[string]any{"values": []string{"Open", "Closed", "Blocked"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		got := itemIDs(data)
		for _, id := range got {
			if id == idOf(noValue) {
				t.Fatalf("expected task with no custom field value to be excluded, got it in results: %v", got)
			}
		}
		if len(got) != 4 {
			t.Fatalf("expected 4 tasks with any priority value, got %d: %v", len(got), got)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom field filters — multi_select
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_CustomFieldMultiSelect(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-multiselect-filter-user", "cfmultipass1")
	client, token := taskMemberLogin(t, env, "cf-multiselect-filter-user", "cfmultipass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key":    "labels",
		"display_name": "Labels",
		"field_type":   "multi_select",
		"options":      []string{"backend", "frontend", "urgent"},
	})

	backendUrgent := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Backend Urgent", "custom_fields": map[string]any{"labels": []string{"backend", "urgent"}},
	})
	frontendOnly := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Frontend Only", "custom_fields": map[string]any{"labels": []string{"frontend"}},
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "No Labels", "custom_fields": map[string]any{"labels": []string{}},
	})

	t.Run("matches_any_overlapping_value", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"labels": map[string]any{"values": []string{"urgent"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(backendUrgent)}) {
			t.Fatalf("expected only backendUrgent task, got %v", got)
		}
	})

	t.Run("multiple_filter_values_union_matches", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"labels": map[string]any{"values": []string{"backend", "frontend"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		want := []string{idOf(backendUrgent), idOf(frontendOnly)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("no_overlap_excluded", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"labels": map[string]any{"values": []string{"nonexistent"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); len(got) != 0 {
			t.Fatalf("expected no matches, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom field filters — boolean
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_CustomFieldBoolean(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-boolean-filter-user", "cfboolpass1")
	client, token := taskMemberLogin(t, env, "cf-boolean-filter-user", "cfboolpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "flag", "display_name": "Flag", "field_type": "boolean",
	})

	trueTask := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Flag True", "custom_fields": map[string]any{"flag": true},
	})
	falseTask := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Flag False", "custom_fields": map[string]any{"flag": false},
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Flag Unset",
	})

	t.Run("filters_true_only", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"flag": map[string]any{"values": []string{"true"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(trueTask)}) {
			t.Fatalf("expected only true task, got %v", got)
		}
	})

	t.Run("filters_false_only", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"flag": map[string]any{"values": []string{"false"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(falseTask)}) {
			t.Fatalf("expected only false task, got %v", got)
		}
	})

	t.Run("filters_both_excludes_unset", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"flag": map[string]any{"values": []string{"true", "false"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		want := []string{idOf(trueTask), idOf(falseTask)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom field filters — number
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_CustomFieldNumber(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-number-filter-user", "cfnumberpass1")
	client, token := taskMemberLogin(t, env, "cf-number-filter-user", "cfnumberpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "effort", "display_name": "Effort", "field_type": "number",
	})

	low := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Effort 3", "custom_fields": map[string]any{"effort": 3},
	})
	mid := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Effort 5", "custom_fields": map[string]any{"effort": 5},
	})
	high := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Effort 8", "custom_fields": map[string]any{"effort": 8},
	})
	noValue := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "No Effort",
	})

	t.Run("min_only", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"effort": map[string]any{"min": 5}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		want := []string{idOf(mid), idOf(high)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("max_only", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"effort": map[string]any{"max": 5}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		want := []string{idOf(low), idOf(mid)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("range_inclusive_exact_boundary", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"effort": map[string]any{"min": 3, "max": 3}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(low)}) {
			t.Fatalf("expected exact boundary match, got %v", got)
		}
	})

	t.Run("no_match_outside_range", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"effort": map[string]any{"min": 100}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); len(got) != 0 {
			t.Fatalf("expected no matches, got %v", got)
		}
	})

	t.Run("task_with_no_value_excluded", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"effort": map[string]any{"min": 0}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		got := itemIDs(data)
		for _, id := range got {
			if id == idOf(noValue) {
				t.Fatalf("expected task with no effort value to be excluded, got it: %v", got)
			}
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 tasks with an effort value, got %d: %v", len(got), got)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom field filters — date
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_CustomFieldDate(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-date-filter-user", "cfdatepass1")
	client, token := taskMemberLogin(t, env, "cf-date-filter-user", "cfdatepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "review_date", "display_name": "Review Date", "field_type": "date",
	})

	early := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Early", "custom_fields": map[string]any{"review_date": "2024-01-15"},
	})
	mid := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Mid", "custom_fields": map[string]any{"review_date": "2024-03-15"},
	})
	late := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Late", "custom_fields": map[string]any{"review_date": "2024-06-15"},
	})

	t.Run("after_only", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"review_date": map[string]any{"after": "2024-03-01"}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		want := []string{idOf(mid), idOf(late)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("before_only", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"review_date": map[string]any{"before": "2024-03-01"}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(early)}) {
			t.Fatalf("expected only early task, got %v", got)
		}
	})

	t.Run("range_inclusive_exact_day_boundary", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"review_date": map[string]any{"after": "2024-03-15", "before": "2024-03-15"}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(mid)}) {
			t.Fatalf("expected exact-day boundary match, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom field filters — text / url
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_CustomFieldTextAndURL(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-text-filter-user", "cftextpass1")
	client, token := taskMemberLogin(t, env, "cf-text-filter-user", "cftextpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "notes", "display_name": "Notes", "field_type": "text",
	})
	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "link", "display_name": "Link", "field_type": "url",
	})

	matchText := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Match Text", "custom_fields": map[string]any{"notes": "Needs URGENT review"},
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "No Match Text", "custom_fields": map[string]any{"notes": "All good here"},
	})
	matchURL := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Match URL", "custom_fields": map[string]any{"link": "https://example.com/docs"},
	})

	t.Run("text_contains_case_insensitive", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"notes": map[string]any{"contains": "urgent"}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(matchText)}) {
			t.Fatalf("expected only matchText task, got %v", got)
		}
	})

	t.Run("url_contains", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"link": map[string]any{"contains": "example.com"}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(matchURL)}) {
			t.Fatalf("expected only matchURL task, got %v", got)
		}
	})

	t.Run("no_match_returns_empty", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"notes": map[string]any{"contains": "nonexistent phrase"}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
		if got := itemIDs(data); len(got) != 0 {
			t.Fatalf("expected no matches, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom field filters — multiple fields combined (AND)
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_CustomFieldsCombinedAND(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-combined-filter-user", "cfcombinedpass1")
	client, token := taskMemberLogin(t, env, "cf-combined-filter-user", "cfcombinedpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "priority", "display_name": "Priority", "field_type": "select",
		"options": []string{"High", "Low"},
	})
	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "effort", "display_name": "Effort", "field_type": "number",
	})

	both := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "High + Effort 5", "custom_fields": map[string]any{"priority": "High", "effort": 5},
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "High + Effort 1", "custom_fields": map[string]any{"priority": "High", "effort": 1},
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Low + Effort 5", "custom_fields": map[string]any{"priority": "Low", "effort": 5},
	})

	raw := mustJSONString(t, map[string]any{
		"priority": map[string]any{"values": []string{"High"}},
		"effort":   map[string]any{"min": 3},
	})
	data := listTasksPage(t, env, client, token, projID, url.Values{"custom_field_filters": {raw}})
	if got := itemIDs(data); !idsEqualSet(got, []string{idOf(both)}) {
		t.Fatalf("expected only the task matching BOTH filters, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// Built-in filters — start_date
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_StartDateRange(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "startdate-filter-user", "startdatepass1")
	client, token := taskMemberLogin(t, env, "startdate-filter-user", "startdatepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	early := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Early", "start_date": "2024-01-15T00:00:00Z",
	})
	mid := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Mid", "start_date": "2024-03-15T00:00:00Z",
	})
	late := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Late", "start_date": "2024-06-15T00:00:00Z",
	})
	noDate := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "No Start Date",
	})

	t.Run("after_only", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"start_date_after": {"2024-03-01"}})
		want := []string{idOf(mid), idOf(late)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("before_only", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"start_date_before": {"2024-03-01"}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(early)}) {
			t.Fatalf("expected only early task, got %v", got)
		}
	})

	t.Run("range_exact_day_boundary", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"start_date_after":  {"2024-03-15"},
			"start_date_before": {"2024-03-15"},
		})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(mid)}) {
			t.Fatalf("expected exact-day boundary match, got %v", got)
		}
	})

	t.Run("task_with_no_start_date_excluded", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"start_date_after": {"2000-01-01"}})
		got := itemIDs(data)
		for _, id := range got {
			if id == idOf(noDate) {
				t.Fatalf("expected task with no start_date to be excluded, got it: %v", got)
			}
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 tasks with a start_date, got %d: %v", len(got), got)
		}
	})
}

// ---------------------------------------------------------------------------
// Built-in filters — due_date
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_DueDateRange(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "duedate-filter-user", "duedatepass1")
	client, token := taskMemberLogin(t, env, "duedate-filter-user", "duedatepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	early := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Early Due", "due_date": "2024-01-15T00:00:00Z",
	})
	late := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Late Due", "due_date": "2024-06-15T00:00:00Z",
	})

	t.Run("after_only", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"due_date_after": {"2024-03-01"}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(late)}) {
			t.Fatalf("expected only late task, got %v", got)
		}
	})

	t.Run("before_only", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"due_date_before": {"2024-03-01"}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(early)}) {
			t.Fatalf("expected only early task, got %v", got)
		}
	})

	t.Run("range_excludes_both_outliers", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"due_date_after":  {"2024-02-01"},
			"due_date_before": {"2024-05-01"},
		})
		if got := itemIDs(data); len(got) != 0 {
			t.Fatalf("expected no tasks in narrow middle range, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Built-in filters — story_points
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_StoryPointsRange(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "storypoints-filter-user", "spfilterpass1")
	client, token := taskMemberLogin(t, env, "storypoints-filter-user", "spfilterpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	sp1 := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "SP 1", "story_points": 1})
	sp5 := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "SP 5", "story_points": 5})
	sp13 := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "SP 13", "story_points": 13})
	noSP := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "No SP"})

	t.Run("min_only", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"story_points_min": {"5"}})
		want := []string{idOf(sp5), idOf(sp13)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("max_only", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"story_points_max": {"5"}})
		want := []string{idOf(sp1), idOf(sp5)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("range_exact_boundary", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"story_points_min": {"1"}, "story_points_max": {"1"},
		})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(sp1)}) {
			t.Fatalf("expected exact boundary match, got %v", got)
		}
	})

	t.Run("task_with_no_story_points_excluded", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"story_points_min": {"0"}})
		got := itemIDs(data)
		for _, id := range got {
			if id == idOf(noSP) {
				t.Fatalf("expected task with no story_points to be excluded, got it: %v", got)
			}
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 tasks with story_points set, got %d: %v", len(got), got)
		}
	})
}

// ---------------------------------------------------------------------------
// Built-in filters — importance_ranges
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_ImportanceRanges(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "importance-filter-user", "importancepass1")
	client, token := taskMemberLogin(t, env, "importance-filter-user", "importancepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	none := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "None", "importance": 0})
	low := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Low", "importance": 10})
	medium := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Medium", "importance": 35})
	high := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "High", "importance": 75})
	critical := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{"title": "Critical", "importance": 150})

	t.Run("single_range_matches_one_bucket", func(t *testing.T) {
		raw := mustJSONString(t, []map[string]any{{"min": 1, "max": 19}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"importance_ranges": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(low)}) {
			t.Fatalf("expected only low-importance task, got %v", got)
		}
	})

	t.Run("multiple_ranges_or_together_non_contiguous", func(t *testing.T) {
		// Selecting Low and Critical but skipping Medium/High must not
		// silently include the skipped buckets.
		raw := mustJSONString(t, []map[string]any{
			{"min": 1, "max": 19},
			{"min": 100, "max": 2147483647},
		})
		data := listTasksPage(t, env, client, token, projID, url.Values{"importance_ranges": {raw}})
		want := []string{idOf(low), idOf(critical)}
		got := itemIDs(data)
		if !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
		for _, id := range got {
			if id == idOf(medium) || id == idOf(high) {
				t.Fatalf("expected medium/high to be excluded from a non-contiguous selection, got %v", got)
			}
		}
	})

	t.Run("none_bucket_zero_zero", func(t *testing.T) {
		raw := mustJSONString(t, []map[string]any{{"min": 0, "max": 0}})
		data := listTasksPage(t, env, client, token, projID, url.Values{"importance_ranges": {raw}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(none)}) {
			t.Fatalf("expected only none-importance task, got %v", got)
		}
	})

	t.Run("invalid_json_returns_400", func(t *testing.T) {
		getTasksExpectBadRequest(t, env, client, token, projID,
			"importance_ranges="+url.QueryEscape("not-json"))
	})

	t.Run("min_greater_than_max_returns_400", func(t *testing.T) {
		raw := mustJSONString(t, []map[string]any{{"min": 50, "max": 10}})
		getTasksExpectBadRequest(t, env, client, token, projID,
			"importance_ranges="+url.QueryEscape(raw))
	})
}

// ---------------------------------------------------------------------------
// Built-in filters — tags
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_Tags(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "tags-filter-user", "tagsfilterpass1")
	client, token := taskMemberLogin(t, env, "tags-filter-user", "tagsfilterpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	urgentBug := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Urgent Bug", "tags": []string{"urgent", "bug"},
	})
	backend := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Backend Task", "tags": []string{"backend"},
	})
	needsReview := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Needs Review", "tags": []string{"needs review"},
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "No Tags",
	})

	t.Run("single_tag_matches", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"tags": {"urgent"}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(urgentBug)}) {
			t.Fatalf("expected only urgentBug task, got %v", got)
		}
	})

	t.Run("multiple_tags_match_any", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"tags": {"urgent,backend"}})
		want := []string{idOf(urgentBug), idOf(backend)}
		if got := itemIDs(data); !idsEqualSet(got, want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("tag_with_space_matches_exactly", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"tags": {"needs review"}})
		if got := itemIDs(data); !idsEqualSet(got, []string{idOf(needsReview)}) {
			t.Fatalf("expected only needsReview task, got %v", got)
		}
	})

	t.Run("no_match_returns_empty", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"tags": {"nonexistent"}})
		if got := itemIDs(data); len(got) != 0 {
			t.Fatalf("expected no matches, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Combined filters across dimensions (AND semantics)
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_CombinedAcrossDimensions(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "combined-filter-user", "combinedpass1")
	client, token := taskMemberLogin(t, env, "combined-filter-user", "combinedpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	statuses := listTaskStatusesViaAPI(t, env, client, token, projID)
	todoID := statusIDByName(statuses, "Todo")
	doneID := statusIDByName(statuses, "Done")
	if todoID == "" || doneID == "" {
		t.Fatalf("expected default Todo/Done statuses, got %+v", statuses)
	}

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "priority", "display_name": "Priority", "field_type": "select",
		"options": []string{"High", "Low"},
	})

	match := createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Matches everything", "status_id": todoID, "tags": []string{"urgent"},
		"custom_fields": map[string]any{"priority": "High"}, "start_date": "2024-03-01T00:00:00Z",
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Wrong status", "status_id": doneID, "tags": []string{"urgent"},
		"custom_fields": map[string]any{"priority": "High"}, "start_date": "2024-03-01T00:00:00Z",
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Wrong tag", "status_id": todoID, "tags": []string{"other"},
		"custom_fields": map[string]any{"priority": "High"}, "start_date": "2024-03-01T00:00:00Z",
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Wrong priority", "status_id": todoID, "tags": []string{"urgent"},
		"custom_fields": map[string]any{"priority": "Low"}, "start_date": "2024-03-01T00:00:00Z",
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Wrong date", "status_id": todoID, "tags": []string{"urgent"},
		"custom_fields": map[string]any{"priority": "High"}, "start_date": "2024-01-01T00:00:00Z",
	})

	raw := mustJSONString(t, map[string]any{"priority": map[string]any{"values": []string{"High"}}})
	data := listTasksPage(t, env, client, token, projID, url.Values{
		"status_ids":           {todoID},
		"tags":                 {"urgent"},
		"custom_field_filters": {raw},
		"start_date_after":     {"2024-02-01"},
		"start_date_before":    {"2024-04-01"},
	})
	if got := itemIDs(data); !idsEqualSet(got, []string{idOf(match)}) {
		t.Fatalf("expected only the task matching every filter dimension, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// total_count / field_sum respect active filters
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_TotalCountAndFieldSumRespectFilters(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "count-sum-filter-user", "countsumpass1")
	client, token := taskMemberLogin(t, env, "count-sum-filter-user", "countsumpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
		"field_key": "priority", "display_name": "Priority", "field_type": "select",
		"options": []string{"High", "Low"},
	})

	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "High 3", "story_points": 3, "custom_fields": map[string]any{"priority": "High"},
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "High 5", "story_points": 5, "custom_fields": map[string]any{"priority": "High"},
	})
	createTaskViaAPIWithBody(t, env, client, token, projID, map[string]any{
		"title": "Low 7", "story_points": 7, "custom_fields": map[string]any{"priority": "Low"},
	})

	t.Run("total_count_respects_story_points_filter", func(t *testing.T) {
		data := listTasksPage(t, env, client, token, projID, url.Values{"story_points_min": {"5"}})
		if got := totalCountFromData(data); got != 2 {
			t.Errorf("expected total_count=2, got %d", got)
		}
	})

	t.Run("field_sum_respects_custom_field_filter", func(t *testing.T) {
		raw := mustJSONString(t, map[string]any{"priority": map[string]any{"values": []string{"High"}}})
		data := listTasksPage(t, env, client, token, projID, url.Values{
			"sum_field":            {"story_points"},
			"custom_field_filters": {raw},
		})
		sum, ok := fieldSumFromData(data)
		if !ok {
			t.Fatal("expected field_sum in response")
		}
		if sum != 8 {
			t.Errorf("expected field_sum=8 (3+5, excluding the Low task), got %v", sum)
		}
	})
}

// ---------------------------------------------------------------------------
// Validation errors for malformed filter query params
// ---------------------------------------------------------------------------

func TestE2ETaskFilters_ValidationErrors(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "validation-filter-user", "validationpass1")
	client, token := taskMemberLogin(t, env, "validation-filter-user", "validationpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	cases := []struct {
		name  string
		query string
	}{
		{"invalid_start_date_after", "start_date_after=not-a-date"},
		{"invalid_start_date_before", "start_date_before=" + url.QueryEscape("2024/06/01")},
		{"invalid_due_date_after", "due_date_after=garbage"},
		{"invalid_due_date_before", "due_date_before=" + url.QueryEscape("01-01-2024")},
		{"invalid_story_points_min", "story_points_min=abc"},
		{"invalid_story_points_max", "story_points_max=" + url.QueryEscape("5.5")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			getTasksExpectBadRequest(t, env, client, token, projID, tc.query)
		})
	}
}
