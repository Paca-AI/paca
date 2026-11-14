package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"
)

// Migration 000090 turns every roadmap view into a gantt view, keeping its
// config, and renames a view only while it still has the seeded name.
func TestE2EGanttView_MigrationRenamesRoadmapViews(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "gantt-migrate-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "ganttpass1")
	client, token := taskMemberLogin(t, env, username, "ganttpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	seeded := createTimelineViewViaAPI(t, env, client, token, projID, "Roadmap", "gantt")
	named := createTimelineViewViaAPI(t, env, client, token, projID, "Launch plan", "gantt")
	table := createTimelineViewViaAPI(t, env, client, token, projID, "Roadmap", "table")

	// Put the two timeline views back to how they look before the migration.
	tx, err := env.db.BeginTxx(env.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE sprint_views DROP CONSTRAINT IF EXISTS sprint_views_view_type_check`,
		`UPDATE sprint_views SET view_type = 'roadmap', config = '{"sort_by":"due_date","subtask_mode":"expanded"}'::jsonb WHERE id IN ('` + seeded + `', '` + named + `')`,
	} {
		if _, err := tx.ExecContext(env.ctx, q); err != nil {
			_ = tx.Rollback()
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	migration, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "000090_rename_roadmap_view_to_gantt.sql"))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Name     string `db:"name"`
		ViewType string `db:"view_type"`
		Config   string `db:"config"`
	}
	read := func(id string) row {
		t.Helper()
		var r row
		if err := env.db.GetContext(env.ctx, &r, `SELECT name, view_type, config::text AS config FROM sprint_views WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
		return r
	}
	for run := 1; run <= 2; run++ {
		if _, err := env.db.ExecContext(env.ctx, string(migration)); err != nil {
			t.Fatalf("run %d of the migration: %v", run, err)
		}
		if got := read(seeded); got.Name != "Gantt" || got.ViewType != "gantt" {
			t.Fatalf("run %d: seeded view = %+v, want name Gantt and type gantt", run, got)
		}
		if got := read(named); got.Name != "Launch plan" || got.ViewType != "gantt" {
			t.Fatalf("run %d: named view = %+v, want its own name and type gantt", run, got)
		}
		if got := read(seeded); got.Config != `{"sort_by": "due_date", "subtask_mode": "expanded"}` {
			t.Fatalf("run %d: config = %s, want it kept", run, got.Config)
		}
		if got := read(table); got.Name != "Roadmap" || got.ViewType != "table" {
			t.Fatalf("run %d: a table view named Roadmap changed: %+v", run, got)
		}
	}

	// The constraint is back and no longer allows the old type.
	if _, err := env.db.ExecContext(env.ctx, `UPDATE sprint_views SET view_type = 'roadmap' WHERE id = $1`, seeded); err == nil {
		t.Fatal("the view_type constraint must reject roadmap after the migration")
	}
}

// createTaskLinkViaAPI links source to target with linkType and returns the link id.
func createTaskLinkViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, source, target, linkType string) string {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s/links", env.base, projectID, source),
		jsonBody(t, map[string]any{"target_task_id": target, "link_type": linkType}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusCreated)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	id, _ := assertDataMap(t, env2)["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty link id")
	}
	return id
}

// listProjectTaskLinksViaAPI fetches one page of GET /projects/:id/task-links.
func listProjectTaskLinksViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, query string, wantStatus int) map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/task-links?%s", env.base, projectID, query), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, wantStatus)
	if wantStatus != http.StatusOK {
		return nil
	}
	var env2 envelope
	decodeJSON(t, resp, &env2)
	return assertDataMap(t, env2)
}

// The project-wide link listing returns the links of one type between live
// tasks, oldest first, a page at a time.
func TestE2EGanttView_ProjectTaskLinks(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "gantt-links-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "ganttpass1")
	client, token := taskMemberLogin(t, env, username, "ganttpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	design := createTaskViaAPI(t, env, client, token, projID, "Design")
	build := createTaskViaAPI(t, env, client, token, projID, "Build")
	launch := createTaskViaAPI(t, env, client, token, projID, "Launch")
	gone := createTaskViaAPI(t, env, client, token, projID, "Dropped")

	first := createTaskLinkViaAPI(t, env, client, token, projID, design, build, "blocks")
	second := createTaskLinkViaAPI(t, env, client, token, projID, build, launch, "blocks")
	createTaskLinkViaAPI(t, env, client, token, projID, design, launch, "relates_to")
	createTaskLinkViaAPI(t, env, client, token, projID, gone, launch, "blocks")

	// A link to a deleted task is not listed.
	req := mustRequest(env.ctx, t, http.MethodDelete,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, gone), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("delete task: status %d", resp.StatusCode)
	}

	t.Run("lists_blocks_links_between_live_tasks", func(t *testing.T) {
		data := listProjectTaskLinksViaAPI(t, env, client, token, projID, "link_type=blocks", http.StatusOK)
		items, _ := data["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("expected 2 blocks links, got %d: %v", len(items), items)
		}
		want := []struct{ id, source, target string }{{first, design, build}, {second, build, launch}}
		for i, w := range want {
			item, _ := items[i].(map[string]any)
			if item["id"] != w.id || item["source_task_id"] != w.source || item["target_task_id"] != w.target || item["link_type"] != "blocks" {
				t.Errorf("item %d = %v, want link %s from %s to %s", i, item, w.id, w.source, w.target)
			}
			if _, ok := item["linked_task"]; ok {
				t.Errorf("item %d must not carry a linked task summary: %v", i, item)
			}
		}
		if data["next_cursor"] != nil {
			t.Errorf("expected no next_cursor, got %v", data["next_cursor"])
		}
	})

	t.Run("filters_by_link_type", func(t *testing.T) {
		data := listProjectTaskLinksViaAPI(t, env, client, token, projID, "link_type=relates_to", http.StatusOK)
		if items, _ := data["items"].([]any); len(items) != 1 {
			t.Fatalf("expected 1 relates_to link, got %d", len(items))
		}
	})

	t.Run("pages_with_a_cursor", func(t *testing.T) {
		page1 := listProjectTaskLinksViaAPI(t, env, client, token, projID, "link_type=blocks&page_size=1", http.StatusOK)
		items1, _ := page1["items"].([]any)
		cursor, _ := page1["next_cursor"].(string)
		if len(items1) != 1 || cursor == "" {
			t.Fatalf("page 1 = %v, want one item and a next_cursor", page1)
		}
		page2 := listProjectTaskLinksViaAPI(t, env, client, token, projID, "link_type=blocks&page_size=1&cursor="+cursor, http.StatusOK)
		items2, _ := page2["items"].([]any)
		if len(items2) != 1 || page2["next_cursor"] != nil {
			t.Fatalf("page 2 = %v, want the last item and no next_cursor", page2)
		}
		if items2[0].(map[string]any)["id"] != second {
			t.Errorf("page 2 item = %v, want link %s", items2[0], second)
		}
	})

	t.Run("rejects_a_missing_or_unknown_link_type", func(t *testing.T) {
		listProjectTaskLinksViaAPI(t, env, client, token, projID, "", http.StatusBadRequest)
		listProjectTaskLinksViaAPI(t, env, client, token, projID, "link_type=is_blocked_by", http.StatusBadRequest)
	})

	t.Run("rejects_a_bad_cursor", func(t *testing.T) {
		listProjectTaskLinksViaAPI(t, env, client, token, projID, "link_type=blocks&cursor=!!", http.StatusBadRequest)
	})

	t.Run("requires_project_access", func(t *testing.T) {
		outsider := "gantt-outsider-" + uuid.NewString()
		seedTaskMemberUser(t, env, outsider, "ganttpass1")
		otherClient, otherToken := taskMemberLogin(t, env, outsider, "ganttpass1")
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/task-links?link_type=blocks", env.base, projID), nil)
		req.Header.Set("Authorization", "Bearer "+otherToken)
		resp := mustDo(t, otherClient, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
	})
}
