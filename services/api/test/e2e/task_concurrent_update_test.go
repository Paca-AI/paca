package e2e_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

// TestE2ETaskUpdate_ConcurrentPatchesKeepEveryChange guards against the
// lost-update race (issue #539): PATCH used to write every column back from a
// stale read, so overlapping saves of different fields silently reverted each
// other. Each round fires one PATCH per field at the same time and then checks
// that every change survived.
func TestE2ETaskUpdate_ConcurrentPatchesKeepEveryChange(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "task-race-user", "taskracepass1")
	client, token := taskMemberLogin(t, env, "task-race-user", "taskracepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	for _, key := range []string{"cf_one", "cf_two"} {
		createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
			"field_key":    key,
			"display_name": key,
			"field_type":   "text",
			"is_required":  false,
		})
	}

	const rounds = 15
	for round := 0; round < rounds; round++ {
		taskID := createTaskViaAPI(t, env, client, token, projID, "orig")

		title := fmt.Sprintf("race-%d", round)
		due := fmt.Sprintf("2026-11-%02dT00:00:00Z", round+1)
		points := round + 10
		importance := round%4 + 1
		tags := []string{fmt.Sprintf("tag-%d", round)}
		cfOne := fmt.Sprintf("one-%d", round)
		cfTwo := fmt.Sprintf("two-%d", round)

		patches := []map[string]any{
			{"title": title},
			{"due_date": due},
			{"story_points": points},
			{"importance": importance},
			{"tags": tags},
			{"custom_fields": map[string]any{"cf_one": cfOne}},
			{"custom_fields": map[string]any{"cf_two": cfTwo}},
		}

		var wg sync.WaitGroup
		errs := make(chan string, len(patches))
		for _, p := range patches {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := mustRequest(env.ctx, t, http.MethodPatch,
					fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), jsonBody(t, p))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := client.Do(req)
				if err != nil {
					errs <- fmt.Sprintf("patch %v: %v", p, err)
					return
				}
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusOK {
					errs <- fmt.Sprintf("patch %v: status %d", p, resp.StatusCode)
				}
			}()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			t.Fatalf("round %d: %s", round, e)
		}

		got := getTaskViaAPI(t, env, client, token, projID, taskID)
		if got["title"] != title {
			t.Errorf("round %d: title = %v, want %q", round, got["title"], title)
		}
		if d, _ := got["due_date"].(string); d != due {
			t.Errorf("round %d: due_date = %v, want %q", round, got["due_date"], due)
		}
		if sp, _ := got["story_points"].(float64); int(sp) != points {
			t.Errorf("round %d: story_points = %v, want %d", round, got["story_points"], points)
		}
		if im, _ := got["importance"].(float64); int(im) != importance {
			t.Errorf("round %d: importance = %v, want %d", round, got["importance"], importance)
		}
		if !taskHasTag(got, tags[0]) {
			t.Errorf("round %d: tags = %v, want %v", round, got["tags"], tags)
		}
		cf, _ := got["custom_fields"].(map[string]any)
		if cf["cf_one"] != cfOne || cf["cf_two"] != cfTwo {
			t.Errorf("round %d: custom_fields = %v, want cf_one=%q cf_two=%q", round, cf, cfOne, cfTwo)
		}
	}
}

// TestE2ETaskUpdate_CustomFieldsMergeByKey checks that a PATCH carrying one
// custom field key leaves the task's other custom fields in place.
func TestE2ETaskUpdate_CustomFieldsMergeByKey(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "task-cfmerge-user", "taskcfmergepass1")
	client, token := taskMemberLogin(t, env, "task-cfmerge-user", "taskcfmergepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	for _, key := range []string{"cf_a", "cf_b"} {
		createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
			"field_key":    key,
			"display_name": key,
			"field_type":   "text",
			"is_required":  false,
		})
	}
	taskID := createTaskViaAPI(t, env, client, token, projID, "cf-merge")

	patchTaskFieldsViaAPI(t, env, client, token, projID, taskID,
		map[string]any{"custom_fields": map[string]any{"cf_a": "A1", "cf_b": "B1"}})
	patchTaskFieldsViaAPI(t, env, client, token, projID, taskID,
		map[string]any{"custom_fields": map[string]any{"cf_b": "B2"}})

	got := getTaskViaAPI(t, env, client, token, projID, taskID)
	cf, _ := got["custom_fields"].(map[string]any)
	if cf["cf_a"] != "A1" || cf["cf_b"] != "B2" {
		t.Errorf("custom_fields = %v, want cf_a=A1 cf_b=B2", cf)
	}
}
