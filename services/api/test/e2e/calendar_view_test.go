package e2e_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"
)

// patchViewConfigViaAPI saves config as the view's shared config and returns
// the config the API echoes back.
func patchViewConfigViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, viewID string, config map[string]any) map[string]any {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projectID, viewID),
		jsonBody(t, map[string]any{"config": config}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	cfg, _ := assertDataMap(t, env2)["config"].(map[string]any)
	return cfg
}

// A Calendar view can be created in the sprint, backlog and timeline contexts,
// and its mode and date field are kept, with unknown values dropped.
func TestE2ECalendarView_CreateAndConfigure(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "calendar-view-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "calendarpass1")
	client, token := taskMemberLogin(t, env, username, "calendarpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Calendar Sprint")

	views := map[string]string{
		"sprint":   createViewViaAPI(t, env, client, token, projID, sprintID, "Calendar", "calendar"),
		"backlog":  createBacklogViewViaAPI(t, env, client, token, projID, "Calendar", "calendar"),
		"timeline": createTimelineViewViaAPI(t, env, client, token, projID, "Calendar", "calendar"),
	}

	for viewContext, viewID := range views {
		t.Run(viewContext, func(t *testing.T) {
			cfg := patchViewConfigViaAPI(t, env, client, token, projID, viewID, map[string]any{
				"calendar_mode":       "week",
				"calendar_date_field": "due_date",
			})
			if cfg["calendar_mode"] != "week" || cfg["calendar_date_field"] != "due_date" {
				t.Fatalf("config = %v, want calendar_mode week and calendar_date_field due_date", cfg)
			}

			cfg = patchViewConfigViaAPI(t, env, client, token, projID, viewID, map[string]any{
				"calendar_mode":       "fortnight",
				"calendar_date_field": "created_at",
			})
			if _, ok := cfg["calendar_mode"]; ok {
				t.Errorf("an unknown calendar_mode was stored: %v", cfg)
			}
			if _, ok := cfg["calendar_date_field"]; ok {
				t.Errorf("an unknown calendar_date_field was stored: %v", cfg)
			}
		})
	}
}

// The schedule filter lists the tasks a calendar places in a window of days,
// or the tasks it places nowhere, for each choice of dates.
func TestE2ECalendarView_ScheduleFilter(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "calendar-filter-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "calendarpass1")
	client, token := taskMemberLogin(t, env, username, "calendarpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	create := func(title, start, due string) string {
		body := map[string]any{"title": title}
		if start != "" {
			body["start_date"] = start + "T00:00:00Z"
		}
		if due != "" {
			body["due_date"] = due + "T00:00:00Z"
		}
		return idOf(createTaskViaAPIWithBody(t, env, client, token, projID, body))
	}
	// The window used below is the week of 2026-03-09 to 2026-03-15.
	inside := create("Inside", "2026-03-10", "2026-03-11")
	spanning := create("Spanning", "2026-03-01", "2026-03-31")
	startsInside := create("Starts inside", "2026-03-15", "2026-03-20")
	endsInside := create("Ends inside", "2026-03-02", "2026-03-09")
	dueOnly := create("Due only", "", "2026-03-12")
	startOnly := create("Start only", "2026-03-13", "")
	create("Before", "2026-03-01", "2026-03-08")
	after := create("After", "2026-03-16", "2026-03-18")
	undated := create("Undated", "", "")

	week := url.Values{"scheduled_from": {"2026-03-09"}, "scheduled_to": {"2026-03-15"}}
	with := func(v url.Values, extra ...string) url.Values {
		out := url.Values{}
		for k, vs := range v {
			out[k] = vs
		}
		for i := 0; i+1 < len(extra); i += 2 {
			out.Set(extra[i], extra[i+1])
		}
		return out
	}
	cases := []struct {
		name  string
		query url.Values
		want  []string
	}{
		{
			name:  "start_and_due_window",
			query: week,
			want:  []string{inside, spanning, startsInside, endsInside, dueOnly, startOnly},
		},
		{
			name:  "start_date_window",
			query: with(week, "scheduled_by", "start_date"),
			want:  []string{inside, startsInside, startOnly},
		},
		{
			name:  "due_date_window",
			query: with(week, "scheduled_by", "due_date"),
			want:  []string{inside, endsInside, dueOnly},
		},
		{
			name:  "unscheduled_start_and_due",
			query: url.Values{"unscheduled": {"true"}},
			want:  []string{undated},
		},
		{
			name:  "unscheduled_due_date",
			query: url.Values{"unscheduled": {"true"}, "scheduled_by": {"due_date"}},
			want:  []string{startOnly, undated},
		},
		{
			name:  "open_ended_window",
			query: url.Values{"scheduled_from": {"2026-03-16"}},
			want:  []string{spanning, startsInside, after},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := listTasksPage(t, env, client, token, projID, with(tc.query, "page_size", "50"))
			if got := itemIDs(data); !idsEqualSet(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("rejects_invalid_params", func(t *testing.T) {
		for _, query := range []string{
			"scheduled_by=created_at&scheduled_from=2026-03-01",
			"scheduled_from=not-a-date",
			"scheduled_from=2026-04-01&scheduled_to=2026-03-31",
			"unscheduled=true&scheduled_from=2026-03-01",
		} {
			getTasksExpectBadRequest(t, env, client, token, projID, query)
		}
	})
}

// Migration 000099 can run again over a database it already migrated, and the
// constraint it leaves accepts calendar but still rejects unknown types.
func TestE2ECalendarView_MigrationIsRerunnable(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	username := "calendar-migrate-" + uuid.NewString()
	seedTaskMemberUser(t, env, username, "calendarpass1")
	client, token := taskMemberLogin(t, env, username, "calendarpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	viewID := createBacklogViewViaAPI(t, env, client, token, projID, "Calendar", "calendar")

	_, thisFile, _, _ := runtime.Caller(0)
	migration, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations", "000099_add_calendar_view_type.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.ExecContext(env.ctx, string(migration)); err != nil {
		t.Fatalf("second run of the migration: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `UPDATE sprint_views SET view_type = 'calendar' WHERE id = $1`, viewID); err != nil {
		t.Fatalf("the constraint must accept calendar: %v", err)
	}
	if _, err := env.db.ExecContext(env.ctx, `UPDATE sprint_views SET view_type = 'agenda' WHERE id = $1`, viewID); err == nil {
		t.Fatal("the constraint must reject an unknown view_type")
	}
}
