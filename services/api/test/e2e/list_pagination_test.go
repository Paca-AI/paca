package e2e_test

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Shared helpers for the list endpoints that page by cursor or by number.
// ---------------------------------------------------------------------------

// listPage is a decoded page of a paginated list.
type listPage struct {
	Items      []map[string]any
	PageSize   int
	NextCursor string
	// TotalCount is -1 when the response carries no total_count.
	TotalCount int
	// Page is the echoed page number (0 in cursor mode).
	Page int
}

func decodeListPage(data map[string]any) listPage {
	p := listPage{TotalCount: -1}
	if raw, ok := data["items"].([]any); ok {
		for _, it := range raw {
			if m, ok := it.(map[string]any); ok {
				p.Items = append(p.Items, m)
			}
		}
	}
	if v, ok := data["page_size"].(float64); ok {
		p.PageSize = int(v)
	}
	if v, ok := data["total_count"].(float64); ok {
		p.TotalCount = int(v)
	}
	if v, ok := data["page"].(float64); ok {
		p.Page = int(v)
	}
	if s, ok := data["next_cursor"].(string); ok {
		p.NextCursor = s
	}
	return p
}

// fetchPage GETs path with q and returns the status and, on 200, the page.
func fetchPage(t *testing.T, env *e2eEnv, client *http.Client, path string, q url.Values) (int, listPage) {
	t.Helper()
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	status, out := doJSON(t, env, client, http.MethodGet, path, nil)
	if status != http.StatusOK {
		return status, listPage{}
	}
	data, ok := out.Data.(map[string]any)
	if !ok {
		t.Fatalf("GET %s: expected an object page, got %T", path, out.Data)
	}
	return status, decodeListPage(data)
}

func mustFetchPage(t *testing.T, env *e2eEnv, client *http.Client, path string, q url.Values) listPage {
	t.Helper()
	status, p := fetchPage(t, env, client, path, q)
	if status != http.StatusOK {
		t.Fatalf("GET %s?%s: want 200, got %d", path, q.Encode(), status)
	}
	return p
}

func withQuery(q url.Values, kv ...string) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = slices.Clone(v)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out.Set(kv[i], kv[i+1])
	}
	return out
}

// walkCursor follows next_cursor from the first page until the last, asserting
// that no item repeats and that every page but the last is full. wantTotal, if
// not negative, is checked against the first page's total_count.
func walkCursor(t *testing.T, env *e2eEnv, client *http.Client, path string, q url.Values, pageSize, wantTotal int) []map[string]any {
	t.Helper()
	var all []map[string]any
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 200; pages++ {
		params := withQuery(q, "page_size", fmt.Sprint(pageSize))
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		p := mustFetchPage(t, env, client, path, params)
		if pages == 0 && wantTotal >= 0 && p.TotalCount != wantTotal {
			t.Fatalf("GET %s: first page total_count = %d, want %d", path, p.TotalCount, wantTotal)
		}
		if p.PageSize != 0 && p.PageSize != pageSize {
			t.Fatalf("GET %s: page_size echoed as %d, want %d", path, p.PageSize, pageSize)
		}
		if p.NextCursor != "" && len(p.Items) != pageSize {
			t.Fatalf("GET %s: a page with a next_cursor must be full, got %d of %d", path, len(p.Items), pageSize)
		}
		for _, it := range p.Items {
			id, _ := it["id"].(string)
			if id != "" {
				if seen[id] {
					t.Fatalf("GET %s: item %s appeared on two pages", path, id)
				}
				seen[id] = true
			}
			all = append(all, it)
		}
		if p.NextCursor == "" {
			return all
		}
		cursor = p.NextCursor
	}
	t.Fatalf("GET %s: did not reach the last page", path)
	return nil
}

func stringField(items []map[string]any, key string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, _ := it[key].(string)
		out = append(out, s)
	}
	return out
}

func assertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

// assertBadList asserts that GET path with q is refused with 400.
func assertBadList(t *testing.T, env *e2eEnv, client *http.Client, path string, q url.Values) {
	t.Helper()
	if status, _ := fetchPage(t, env, client, path, q); status != http.StatusBadRequest {
		t.Fatalf("GET %s?%s: want 400, got %d", path, q.Encode(), status)
	}
}

// assertCommonListValidation checks the parameter validation every list
// shares: page_size bounds and a malformed cursor.
func assertCommonListValidation(t *testing.T, env *e2eEnv, client *http.Client, path string, q url.Values) {
	t.Helper()
	assertListValidation(t, env, client, path, q, 100)
}

// assertListValidation is assertCommonListValidation for a list whose largest
// page size is maxPageSize.
func assertListValidation(t *testing.T, env *e2eEnv, client *http.Client, path string, q url.Values, maxPageSize int) {
	t.Helper()
	assertBadList(t, env, client, path, withQuery(q, "page_size", "0"))
	assertBadList(t, env, client, path, withQuery(q, "page_size", fmt.Sprint(maxPageSize+1)))
	assertBadList(t, env, client, path, withQuery(q, "page_size", "abc"))
	assertBadList(t, env, client, path, withQuery(q, "cursor", "!!not-a-cursor"))
}

// assertNumberedPages checks page-number paging of a list whose items sort as
// wantOrder (item ids): pages echo their number, carry total_count, have no
// next_cursor and together cover the list; pages past the end are empty.
func assertNumberedPages(t *testing.T, env *e2eEnv, client *http.Client, path string, q url.Values, pageSize int, wantOrder []string) {
	t.Helper()
	var got []string
	totalPages := (len(wantOrder) + pageSize - 1) / pageSize
	for page := 1; page <= totalPages; page++ {
		p := mustFetchPage(t, env, client, path, withQuery(q, "page", fmt.Sprint(page), "page_size", fmt.Sprint(pageSize)))
		if p.Page != page || p.PageSize != pageSize {
			t.Fatalf("page %d: echoed page=%d page_size=%d", page, p.Page, p.PageSize)
		}
		if p.TotalCount != len(wantOrder) {
			t.Fatalf("page %d: total_count = %d, want %d", page, p.TotalCount, len(wantOrder))
		}
		if p.NextCursor != "" {
			t.Fatalf("page %d: numbered pages must not carry a next_cursor", page)
		}
		want := min(pageSize, len(wantOrder)-(page-1)*pageSize)
		if len(p.Items) != want {
			t.Fatalf("page %d: %d items, want %d", page, len(p.Items), want)
		}
		got = append(got, stringField(p.Items, "id")...)
	}
	assertStrings(t, "numbered pages together", got, wantOrder)

	past := mustFetchPage(t, env, client, path, withQuery(q, "page", fmt.Sprint(totalPages+1), "page_size", fmt.Sprint(pageSize)))
	if len(past.Items) != 0 || past.TotalCount != len(wantOrder) {
		t.Fatalf("a page past the end must be empty and still report the total: %d items, total %d", len(past.Items), past.TotalCount)
	}
	assertBadList(t, env, client, path, withQuery(q, "page", "0"))
	assertBadList(t, env, client, path, withQuery(q, "page", "abc"))
}

// superAdminClient seeds a SUPER_ADMIN user and returns its logged-in client
// and access token.
func superAdminClient(t *testing.T, env *e2eEnv, username string) (*http.Client, string) {
	t.Helper()
	seedUser(t, env, username, "pagpass123!", "Pagination Admin")
	assignPlatformRole(t, env, username, "SUPER_ADMIN")
	return loginUser(t, env, username, "pagpass123!")
}

func mustCreate(t *testing.T, env *e2eEnv, client *http.Client, path string, body any) map[string]any {
	t.Helper()
	status, out := doJSON(t, env, client, http.MethodPost, path, body)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("POST %s: want 201, got %d (%s)", path, status, out.ErrorCode)
	}
	data, _ := out.Data.(map[string]any)
	return data
}

// ---------------------------------------------------------------------------
// API keys
// ---------------------------------------------------------------------------

func TestE2EListPagination_APIKeys(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := apiKeyUserLogin(t, env, "pag-apikey-user", "keypass1!")
	const path = "/api/v1/users/me/api-keys"

	var ids []string // newest first
	for i := 1; i <= 5; i++ {
		id, _ := createAPIKey(t, env, client, token, fmt.Sprintf("pag-key-%d", i))
		ids = append([]string{id}, ids...)
		time.Sleep(5 * time.Millisecond) // distinct created_at
	}

	t.Run("cursor_walk_is_newest_first_with_total", func(t *testing.T) {
		got := walkCursor(t, env, client, path, nil, 2, 5)
		assertStrings(t, "keys", stringField(got, "id"), ids)
	})
	t.Run("page_numbers", func(t *testing.T) {
		assertNumberedPages(t, env, client, path, nil, 2, ids)
	})
	t.Run("validation", func(t *testing.T) {
		assertCommonListValidation(t, env, client, path, nil)
	})
	t.Run("a_revoked_key_leaves_the_list_and_the_total", func(t *testing.T) {
		if status, _ := doJSON(t, env, client, http.MethodDelete, path+"/"+ids[0], nil); status != http.StatusNoContent {
			t.Fatalf("revoke: want 204, got %d", status)
		}
		p := mustFetchPage(t, env, client, path, url.Values{"page": {"1"}, "page_size": {"10"}})
		if p.TotalCount != 4 || len(p.Items) != 4 {
			t.Fatalf("after revoking: %d items, total %d, want 4 and 4", len(p.Items), p.TotalCount)
		}
	})
}

// ---------------------------------------------------------------------------
// Service accounts and their API keys
// ---------------------------------------------------------------------------

func TestE2EListPagination_ServiceAccounts(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, _ := superAdminClient(t, env, "pag-sa-admin")
	const path = "/api/v1/admin/service-accounts"

	// Created out of order and in mixed case: the list sorts by lowercase name.
	idByName := map[string]string{}
	for _, name := range []string{"Pag-SA-b", "pag-sa-a", "pag-sa-d", "PAG-SA-c", "pag-sa-e"} {
		d := mustCreate(t, env, client, path, map[string]any{"name": name, "description": "d " + name})
		idByName[strings.ToLower(name)], _ = d["id"].(string)
	}
	order := []string{"pag-sa-a", "pag-sa-b", "pag-sa-c", "pag-sa-d", "pag-sa-e"}
	var wantIDs []string
	for _, n := range order {
		wantIDs = append(wantIDs, idByName[n])
	}

	t.Run("cursor_walk_sorted_by_lowercase_name", func(t *testing.T) {
		got := walkCursor(t, env, client, path, nil, 2, 5)
		assertStrings(t, "accounts", stringField(got, "id"), wantIDs)
	})
	t.Run("page_numbers", func(t *testing.T) {
		assertNumberedPages(t, env, client, path, nil, 2, wantIDs)
	})
	t.Run("search_matches_words_case_insensitively", func(t *testing.T) {
		p := mustFetchPage(t, env, client, path, url.Values{"search": {"SA-D"}, "page": {"1"}, "page_size": {"10"}})
		assertStrings(t, "search hits", stringField(p.Items, "id"), []string{idByName["pag-sa-d"]})
		if p.TotalCount != 1 {
			t.Fatalf("search total_count = %d, want 1", p.TotalCount)
		}
	})
	t.Run("validation", func(t *testing.T) {
		assertCommonListValidation(t, env, client, path, nil)
	})

	t.Run("an_accounts_api_keys_page_the_same_way", func(t *testing.T) {
		accountID := idByName["pag-sa-a"]
		keysPath := path + "/" + accountID + "/api-keys"
		var keyIDs []string // newest first
		for i := 1; i <= 3; i++ {
			d := mustCreate(t, env, client, keysPath, map[string]any{"name": fmt.Sprintf("sa-key-%d", i)})
			id, _ := d["id"].(string)
			keyIDs = append([]string{id}, keyIDs...)
			time.Sleep(5 * time.Millisecond)
		}
		got := walkCursor(t, env, client, keysPath, nil, 2, 3)
		assertStrings(t, "service account keys", stringField(got, "id"), keyIDs)
		assertNumberedPages(t, env, client, keysPath, nil, 2, keyIDs)
		assertCommonListValidation(t, env, client, keysPath, nil)
	})
}

// ---------------------------------------------------------------------------
// Roles (platform and project)
// ---------------------------------------------------------------------------

func TestE2EListPagination_Roles(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := superAdminClient(t, env, "pag-roles-admin")
	const path = "/api/v1/admin/roles"

	var roleIDs []string
	for _, suffix := range []string{"C", "a", "E", "B", "d"} {
		roleIDs = append(roleIDs, createPlatformRole(t, env, "PAG_ROLE_"+suffix, "tasks:read").String())
	}
	sorted := []string{roleIDs[1], roleIDs[3], roleIDs[0], roleIDs[4], roleIDs[2]} // a B C d E
	q := url.Values{"search": {"PAG_ROLE_"}}

	t.Run("cursor_walk_sorted_by_lowercase_name", func(t *testing.T) {
		got := walkCursor(t, env, client, path, q, 2, 5)
		assertStrings(t, "roles", stringField(got, "id"), sorted)
	})
	t.Run("page_numbers", func(t *testing.T) {
		assertNumberedPages(t, env, client, path, q, 2, sorted)
	})
	t.Run("ids_is_a_batch_lookup", func(t *testing.T) {
		p := mustFetchPage(t, env, client, path, url.Values{"ids": {roleIDs[4] + "," + roleIDs[1]}})
		assertStrings(t, "ids", stringField(p.Items, "id"), []string{roleIDs[1], roleIDs[4]})
	})
	t.Run("validation", func(t *testing.T) {
		assertCommonListValidation(t, env, client, path, q)
		assertBadList(t, env, client, path, url.Values{"ids": {"not-a-uuid"}})
	})

	t.Run("a_project_lists_its_own_roles_in_pages", func(t *testing.T) {
		projID := createProjectViaAPI(t, env, client, token, "pag-roles-project-"+uuid.NewString(), "")
		rolesPath := "/api/v1/projects/" + projID + "/roles"
		for _, name := range []string{"Pag Zeta", "Pag Alpha", "Pag Mid"} {
			createProjectRoleViaAPI(t, env, client, token, projID, name, "tasks:read")
		}
		all := mustFetchPage(t, env, client, rolesPath, url.Values{"page_size": {"100"}})
		if all.TotalCount < 7 || len(all.Items) != all.TotalCount {
			t.Fatalf("project roles: %d items, total %d, want the shipped four plus three more", len(all.Items), all.TotalCount)
		}
		got := walkCursor(t, env, client, rolesPath, nil, 2, all.TotalCount)
		assertStrings(t, "project roles", stringField(got, "id"), stringField(all.Items, "id"))
		assertNumberedPages(t, env, client, rolesPath, nil, 4, stringField(all.Items, "id"))
		names := stringField(got, "name")
		sortedNames := slices.SortedFunc(slices.Values(names), func(a, b string) int {
			return strings.Compare(strings.ToLower(a), strings.ToLower(b))
		})
		assertStrings(t, "project role names (own roles sort by name)", names, sortedNames)

		search := mustFetchPage(t, env, client, rolesPath, url.Values{"search": {"alpha"}})
		if len(search.Items) != 1 {
			t.Fatalf("search alpha: %d hits, want 1", len(search.Items))
		}
	})
}

// ---------------------------------------------------------------------------
// Project members
// ---------------------------------------------------------------------------

func TestE2EListPagination_ProjectMembers(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := superAdminClient(t, env, "pag-members-admin")
	projID := createProjectViaAPI(t, env, client, token, "pag-members-project-"+uuid.NewString(), "")
	editorID := projectRoleIDByName(t, env, client, token, projID, "Editor")
	path := "/api/v1/projects/" + projID + "/members"

	for i := 1; i <= 5; i++ {
		username := fmt.Sprintf("pag-member-%d", i)
		seedUser(t, env, username, "memberpass1", fmt.Sprintf("Pag Member %d", i))
		u, err := env.userRepo.FindByUsername(env.ctx, username)
		if err != nil {
			t.Fatalf("find user: %v", err)
		}
		addMemberViaAPI(t, env, client, token, projID, u.ID.String(), editorID)
	}

	all := listProjectMembersViaAPI(t, env, client, token, projID)
	if len(all) < 6 {
		t.Fatalf("expected the creator and five members, got %d", len(all))
	}
	var allIDs []string
	for _, m := range all {
		allIDs = append(allIDs, m["id"].(string))
	}

	t.Run("cursor_walk_covers_every_member_once", func(t *testing.T) {
		got := walkCursor(t, env, client, path, nil, 2, len(all))
		if !slices.Equal(slices.Sorted(slices.Values(stringField(got, "id"))), slices.Sorted(slices.Values(allIDs))) {
			t.Fatalf("paged members differ from the full list")
		}
	})
	t.Run("search_narrows_and_reports_the_matching_total", func(t *testing.T) {
		p := mustFetchPage(t, env, client, path, url.Values{"search": {"pag-member-3"}})
		if len(p.Items) != 1 || p.TotalCount != 1 {
			t.Fatalf("search: %d items, total %d, want 1 and 1", len(p.Items), p.TotalCount)
		}
	})
	t.Run("ids_is_a_batch_lookup", func(t *testing.T) {
		want := []string{allIDs[0], allIDs[2]}
		p := mustFetchPage(t, env, client, path, url.Values{"ids": {strings.Join(want, ",")}})
		got := stringField(p.Items, "id")
		slices.Sort(got)
		slices.Sort(want)
		assertStrings(t, "ids", got, want)
	})
	t.Run("validation", func(t *testing.T) {
		assertCommonListValidation(t, env, client, path, nil)
	})
}

// ---------------------------------------------------------------------------
// Sprints
// ---------------------------------------------------------------------------

func TestE2EListPagination_Sprints(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := superAdminClient(t, env, "pag-sprint-admin")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	path := "/api/v1/projects/" + projID + "/sprints"

	idByName := map[string]string{}
	for _, name := range []string{"Echo", "alpha", "Delta", "bravo", "Charlie"} {
		idByName[name] = createSprintViaAPI(t, env, client, token, projID, name)
	}
	// alpha, bravo: active; Charlie, Delta: completed (Delta touched last);
	// Echo: planned.
	set := func(name, status string, updated time.Duration) {
		if _, err := env.db.ExecContext(env.ctx, `UPDATE sprints SET status = $2, updated_at = now() + $3::interval WHERE id = $1`,
			idByName[name], status, fmt.Sprintf("%d seconds", int(updated.Seconds()))); err != nil {
			t.Fatalf("set sprint status: %v", err)
		}
	}
	set("alpha", "active", 0)
	set("bravo", "active", 0)
	set("Charlie", "completed", 10*time.Second)
	set("Delta", "completed", 20*time.Second)
	ids := func(names ...string) []string {
		out := make([]string, len(names))
		for i, n := range names {
			out[i] = idByName[n]
		}
		return out
	}

	t.Run("default_order_is_case_insensitive_name", func(t *testing.T) {
		got := walkCursor(t, env, client, path, nil, 2, 5)
		assertStrings(t, "sprints", stringField(got, "id"), ids("alpha", "bravo", "Charlie", "Delta", "Echo"))
	})
	t.Run("status_filter_takes_several_statuses", func(t *testing.T) {
		got := walkCursor(t, env, client, path, url.Values{"status": {"active,planned"}}, 2, 3)
		assertStrings(t, "open sprints", stringField(got, "id"), ids("alpha", "bravo", "Echo"))
	})
	t.Run("recent_sort_puts_the_last_touched_first", func(t *testing.T) {
		got := walkCursor(t, env, client, path, url.Values{"status": {"completed"}, "sort": {"recent"}}, 1, 2)
		assertStrings(t, "completed", stringField(got, "id"), ids("Delta", "Charlie"))
	})
	t.Run("search_and_ids", func(t *testing.T) {
		p := mustFetchPage(t, env, client, path, url.Values{"search": {"ALP"}})
		assertStrings(t, "search", stringField(p.Items, "id"), ids("alpha"))
		p = mustFetchPage(t, env, client, path, url.Values{"ids": {idByName["Echo"] + "," + idByName["bravo"]}})
		assertStrings(t, "ids", stringField(p.Items, "id"), ids("bravo", "Echo"))
		if p.TotalCount != 2 {
			t.Fatalf("ids total_count = %d, want 2", p.TotalCount)
		}
	})
	t.Run("validation", func(t *testing.T) {
		assertCommonListValidation(t, env, client, path, nil)
		assertBadList(t, env, client, path, url.Values{"status": {"nonsense"}})
		assertBadList(t, env, client, path, url.Values{"sort": {"sideways"}})
		assertBadList(t, env, client, path, url.Values{"ids": {"not-a-uuid"}})
	})
}

// ---------------------------------------------------------------------------
// Task links and attachments
// ---------------------------------------------------------------------------

func TestE2EListPagination_TaskLinksAndAttachments(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := superAdminClient(t, env, "pag-task-admin")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	hub := createTaskViaAPI(t, env, client, token, projID, "hub task")
	tasksPath := "/api/v1/projects/" + projID + "/tasks/" + hub

	t.Run("links", func(t *testing.T) {
		var linked []string // oldest first
		for i := 1; i <= 5; i++ {
			other := createTaskViaAPI(t, env, client, token, projID, fmt.Sprintf("linked %d", i))
			mustCreate(t, env, client, tasksPath+"/links", map[string]any{"target_task_id": other, "link_type": "relates_to"})
			linked = append(linked, other)
			time.Sleep(5 * time.Millisecond)
		}
		got := walkCursor(t, env, client, tasksPath+"/links", nil, 2, 5)
		var gotTasks []string
		for _, l := range got {
			lt, _ := l["linked_task"].(map[string]any)
			id, _ := lt["id"].(string)
			gotTasks = append(gotTasks, id)
		}
		assertStrings(t, "linked tasks (oldest link first)", gotTasks, linked)
		assertCommonListValidation(t, env, client, tasksPath+"/links", nil)
	})

	t.Run("attachments", func(t *testing.T) {
		base := time.Now().UTC().Add(-time.Hour)
		var want []string // oldest first
		for i := 0; i < 5; i++ {
			fileID, attID := uuid.New(), uuid.New()
			if _, err := env.db.ExecContext(env.ctx, `INSERT INTO files (id, storage_key, bucket, file_name, upload_status) VALUES ($1,$2,'b',$3,'uploaded')`,
				fileID, "pag/"+fileID.String(), fmt.Sprintf("pag-%d.txt", i)); err != nil {
				t.Fatalf("insert file: %v", err)
			}
			if _, err := env.db.ExecContext(env.ctx, `INSERT INTO task_attachments (id, task_id, file_id, created_at) VALUES ($1,$2,$3,$4)`,
				attID, hub, fileID, base.Add(time.Duration(i)*time.Minute)); err != nil {
				t.Fatalf("insert attachment: %v", err)
			}
			want = append(want, attID.String())
		}
		got := walkCursor(t, env, client, tasksPath+"/attachments", nil, 2, 5)
		assertStrings(t, "attachments (oldest first)", stringField(got, "id"), want)
		for _, a := range got {
			if f, _ := a["file"].(map[string]any); f == nil {
				t.Fatalf("each attachment must carry its file: %v", a)
			}
		}
		assertCommonListValidation(t, env, client, tasksPath+"/attachments", nil)

		// One attachment is readable by id, without paging the list.
		status, out := doJSON(t, env, client, http.MethodGet, tasksPath+"/attachments/"+want[2], nil)
		one, _ := out.Data.(map[string]any)
		if status != http.StatusOK || one["id"] != want[2] {
			t.Fatalf("GET attachment: status=%d body=%v", status, out.Data)
		}
		if status, _ := doJSON(t, env, client, http.MethodGet, tasksPath+"/attachments/"+uuid.NewString(), nil); status != http.StatusNotFound {
			t.Fatalf("unknown attachment: want 404, got %d", status)
		}
	})
}

// ---------------------------------------------------------------------------
// Project exports (numbered pages)
// ---------------------------------------------------------------------------

func TestE2EListPagination_ProjectExports(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := superAdminClient(t, env, "pag-export-admin")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	path := "/api/v1/projects/" + projID + "/exports"

	base := time.Now().UTC().Add(-time.Hour)
	var newestFirst []string
	for i := 0; i < 5; i++ {
		id := uuid.New()
		if _, err := env.db.ExecContext(env.ctx, `INSERT INTO project_exports (id, project_id, status, created_at) VALUES ($1,$2,'completed',$3)`,
			id, projID, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert export: %v", err)
		}
		newestFirst = append([]string{id.String()}, newestFirst...)
	}

	t.Run("numbered_pages_newest_first", func(t *testing.T) {
		assertNumberedPages(t, env, client, path, nil, 2, newestFirst)
	})
	t.Run("defaults_to_the_first_page", func(t *testing.T) {
		p := mustFetchPage(t, env, client, path, nil)
		if p.Page != 1 || p.PageSize != 20 || len(p.Items) != 5 || p.TotalCount != 5 {
			t.Fatalf("defaults: page=%d size=%d items=%d total=%d", p.Page, p.PageSize, len(p.Items), p.TotalCount)
		}
	})
	t.Run("validation", func(t *testing.T) {
		assertBadList(t, env, client, path, url.Values{"page_size": {"0"}})
		assertBadList(t, env, client, path, url.Values{"page_size": {"101"}})
		assertBadList(t, env, client, path, url.Values{"page": {"0"}})
	})
}

// ---------------------------------------------------------------------------
// Docs tree
// ---------------------------------------------------------------------------

func TestE2EListPagination_DocsTree(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := superAdminClient(t, env, "pag-docs-admin")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	base := "/api/v1/projects/" + projID + "/docs"

	var folderIDs []string
	for i := 1; i <= 5; i++ {
		d := mustCreate(t, env, client, base+"/folders", map[string]any{"name": fmt.Sprintf("Pag Folder %d", i)})
		id, _ := d["id"].(string)
		folderIDs = append(folderIDs, id)
	}
	// Three documents inside the first folder, two at the top level.
	for i := 1; i <= 3; i++ {
		mustCreate(t, env, client, base, map[string]any{"title": fmt.Sprintf("Inside %d", i), "folder_id": folderIDs[0]})
	}
	for i := 1; i <= 2; i++ {
		mustCreate(t, env, client, base, map[string]any{"title": fmt.Sprintf("Top %d", i)})
	}

	t.Run("folders_page_by_cursor", func(t *testing.T) {
		all := mustFetchPage(t, env, client, base+"/folders", url.Values{"page_size": {"100"}})
		got := walkCursor(t, env, client, base+"/folders", nil, 2, -1)
		assertStrings(t, "folders", stringField(got, "id"), stringField(all.Items, "id"))
		if len(got) != 5 {
			t.Fatalf("expected 5 folders, got %d", len(got))
		}
	})
	t.Run("documents_page_by_cursor_per_folder", func(t *testing.T) {
		in := walkCursor(t, env, client, base, url.Values{"folder_id": {folderIDs[0]}}, 2, -1)
		if len(in) != 3 {
			t.Fatalf("folder documents: %d, want 3", len(in))
		}
		top := walkCursor(t, env, client, base, url.Values{"folder_id": {"null"}}, 1, -1)
		if len(top) != 2 {
			t.Fatalf("top-level documents: %d, want 2", len(top))
		}
		all := walkCursor(t, env, client, base, nil, 2, -1)
		if len(all) != 5 {
			t.Fatalf("all documents: %d, want 5", len(all))
		}
	})
	t.Run("exact_name_and_title_lookups_never_match_longer_names", func(t *testing.T) {
		f := mustFetchPage(t, env, client, base+"/folders", url.Values{"name": {"pag folder 3"}})
		if len(f.Items) != 1 || f.Items[0]["name"] != "Pag Folder 3" {
			t.Fatalf("folder by name: %v", f.Items)
		}
		if f := mustFetchPage(t, env, client, base+"/folders", url.Values{"name": {"Pag Folder"}}); len(f.Items) != 0 {
			t.Fatalf("a partial name must not match a folder, got %d", len(f.Items))
		}
		d := mustFetchPage(t, env, client, base, url.Values{"title": {"TOP 1"}, "folder_id": {"null"}})
		if len(d.Items) != 1 || d.Items[0]["title"] != "Top 1" {
			t.Fatalf("document by title: %v", d.Items)
		}
		if d := mustFetchPage(t, env, client, base, url.Values{"title": {"Top"}}); len(d.Items) != 0 {
			t.Fatalf("a partial title must not match a document, got %d", len(d.Items))
		}
	})
	t.Run("search_and_validation", func(t *testing.T) {
		p := mustFetchPage(t, env, client, base, url.Values{"search": {"inside 2"}})
		if len(p.Items) != 1 {
			t.Fatalf("search: %d hits, want 1", len(p.Items))
		}
		assertListValidation(t, env, client, base, nil, 200)
		assertListValidation(t, env, client, base+"/folders", nil, 200)
		assertBadList(t, env, client, base, url.Values{"folder_id": {"not-a-uuid"}})
	})
}

// ---------------------------------------------------------------------------
// Project activity feed and the admin users table
// ---------------------------------------------------------------------------

func TestE2EListPagination_ProjectActivities(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, token := superAdminClient(t, env, "pag-activity-admin")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	path := "/api/v1/projects/" + projID + "/activities"

	// Seed the feed directly: the e2e task service records no activity.
	base := time.Now().UTC().Add(-time.Hour)
	var newestFirst []string
	for i := 0; i < 7; i++ {
		id := uuid.New()
		typ := "task.created"
		if i%3 == 0 {
			typ = "sprint.completed"
		}
		if _, err := env.db.ExecContext(env.ctx, `INSERT INTO activities (id, project_id, entity_type, activity_type, created_at) VALUES ($1,$2,'task',$3,$4)`,
			id, projID, typ, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("insert activity: %v", err)
		}
		newestFirst = append([]string{id.String()}, newestFirst...)
	}

	t.Run("cursor_walk_is_newest_first_and_complete", func(t *testing.T) {
		got := walkCursor(t, env, client, path, nil, 3, -1)
		assertStrings(t, "feed", stringField(got, "id"), newestFirst)
	})
	t.Run("a_filter_pages_over_the_matching_rows_only", func(t *testing.T) {
		got := walkCursor(t, env, client, path, url.Values{"activity_type": {"sprint.completed"}}, 2, -1)
		if len(got) != 3 {
			t.Fatalf("sprint.completed activities: %d, want 3", len(got))
		}
	})
	t.Run("validation", func(t *testing.T) {
		assertListValidation(t, env, client, path, nil, 200)
	})
}

func TestE2EListPagination_AdminUsersTable(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	client, _ := superAdminClient(t, env, "pag-users-admin")
	for i := 1; i <= 6; i++ {
		seedUser(t, env, fmt.Sprintf("pag-listed-%d", i), "listpass123", fmt.Sprintf("Listed %d", i))
	}
	status, out := doJSON(t, env, client, http.MethodGet, "/api/v1/admin/users?page=2&page_size=3&search=pag-listed", nil)
	data, _ := out.Data.(map[string]any)
	items, _ := data["items"].([]any)
	if status != http.StatusOK || len(items) != 3 || int(data["total"].(float64)) != 6 || int(data["page"].(float64)) != 2 || int(data["page_size"].(float64)) != 3 {
		t.Fatalf("users page 2: status=%d items=%d data=%v", status, len(items), data)
	}
	if status, _ := doJSON(t, env, client, http.MethodGet, "/api/v1/admin/users?page_size=101", nil); status != http.StatusBadRequest {
		t.Fatalf("page_size=101: want 400, got %d", status)
	}
}
