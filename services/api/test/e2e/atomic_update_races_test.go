package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
)

// Regression tests for the read-then-write races fixed across tasks (issue
// #550), documents, doc folders, sprints and views: validation now runs under
// the row lock, writes touch only live rows, and overlapping saves of
// different fields no longer revert each other.

// apiCall performs one JSON request and returns the status, the decoded
// envelope and any transport error. It never calls t.Fatal, so it is safe to
// use from goroutines.
func apiCall(env *e2eEnv, t *testing.T, client *http.Client, token, method, url string, body any) (int, envelope, error) {
	t.Helper()
	buf := jsonBody(t, map[string]any{})
	if body != nil {
		buf = jsonBody(t, body)
	}
	req := mustRequest(env.ctx, t, method, url, buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return 0, envelope{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var e envelope
	_ = json.NewDecoder(resp.Body).Decode(&e)
	return resp.StatusCode, e, nil
}

// fireConcurrently runs every fn at the same moment and returns their results
// in order.
func fireConcurrently[T any](fns ...func() T) []T {
	out := make([]T, len(fns))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = fn()
		}()
	}
	close(start)
	wg.Wait()
	return out
}

type callResult struct {
	status int
	env    envelope
	err    error
}

func (c callResult) mustHaveResponse(t *testing.T) {
	t.Helper()
	if c.err != nil {
		t.Fatalf("request failed: %v", c.err)
	}
}

func patchAsync(env *e2eEnv, t *testing.T, client *http.Client, token, url string, body any) func() callResult {
	return func() callResult {
		s, e, err := apiCall(env, t, client, token, http.MethodPatch, url, body)
		return callResult{s, e, err}
	}
}

// ---------------------------------------------------------------------------
// Tasks
// ---------------------------------------------------------------------------

// A save setting a parent and a save changing the type to Epic must not both
// win: the task would end up an Epic with a parent.
func TestE2ETaskUpdate_ConcurrentParentAndEpicTypeNeverCombine(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "task-epic-race-user", "taskepicracepass1")
	client, token := taskMemberLogin(t, env, "task-epic-race-user", "taskepicracepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	epicTypeID := taskTypeIDByName(listTaskTypesViaAPI(t, env, client, token, projID), "Epic")
	if epicTypeID == "" {
		t.Fatal("project has no Epic task type")
	}
	base := fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID)

	for round := 0; round < 20; round++ {
		parentID := createTaskViaAPI(t, env, client, token, projID, "parent")
		taskID := createTaskViaAPI(t, env, client, token, projID, "child")
		url := fmt.Sprintf("%s/%s", base, taskID)

		results := fireConcurrently(
			patchAsync(env, t, client, token, url, map[string]any{"parent_task_id": parentID}),
			patchAsync(env, t, client, token, url, map[string]any{"task_type_id": epicTypeID}),
		)
		for _, r := range results {
			r.mustHaveResponse(t)
		}
		if results[0].status == http.StatusOK && results[1].status == http.StatusOK {
			t.Fatalf("round %d: both saves succeeded", round)
		}

		got := getTaskViaAPI(t, env, client, token, projID, taskID)
		isEpic := got["task_type_id"] == epicTypeID
		hasParent := got["parent_task_id"] != nil && got["parent_task_id"] != ""
		if isEpic && hasParent {
			t.Fatalf("round %d: task is an Epic with parent %v", round, got["parent_task_id"])
		}
	}
}

func TestE2ETaskUpdate_EpicCannotHaveParentAndNoSelfParent(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "task-epic-rule-user", "taskepicrulepass1")
	client, token := taskMemberLogin(t, env, "task-epic-rule-user", "taskepicrulepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	epicTypeID := taskTypeIDByName(listTaskTypesViaAPI(t, env, client, token, projID), "Epic")
	base := fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID)

	parentID := createTaskViaAPI(t, env, client, token, projID, "parent")
	childID := createTaskViaAPI(t, env, client, token, projID, "child")

	status, e, err := apiCall(env, t, client, token, http.MethodPatch, base+"/"+childID, map[string]any{"parent_task_id": childID})
	if err != nil || status != http.StatusBadRequest || e.ErrorCode != "TASK_CANNOT_BE_OWN_PARENT" {
		t.Errorf("self parent: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}

	patchTaskFieldsViaAPI(t, env, client, token, projID, childID, map[string]any{"parent_task_id": parentID})
	status, e, err = apiCall(env, t, client, token, http.MethodPatch, base+"/"+childID, map[string]any{"task_type_id": epicTypeID})
	if err != nil || status != http.StatusBadRequest || e.ErrorCode != "TASK_EPIC_CANNOT_HAVE_PARENT" {
		t.Errorf("epic with parent: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}

	status, e, err = apiCall(env, t, client, token, http.MethodPatch, base+"/"+parentID, map[string]any{"parent_task_id": childID})
	if err != nil || status != http.StatusBadRequest || e.ErrorCode != "TASK_PARENT_CYCLE_DETECTED" {
		t.Errorf("cycle: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}
}

// Saving a deleted task reports not-found and never touches the row.
func TestE2ETaskUpdate_DeletedTaskIsNotFound(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "task-deleted-user", "taskdeletedpass1")
	client, token := taskMemberLogin(t, env, "task-deleted-user", "taskdeletedpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	taskID := createTaskViaAPI(t, env, client, token, projID, "to-delete")
	url := fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID)

	if status, _, err := apiCall(env, t, client, token, http.MethodDelete, url, nil); err != nil || status >= 300 {
		t.Fatalf("delete: status=%d err=%v", status, err)
	}
	// Both the locked path (type/parent) and the partial-update path.
	for _, body := range []map[string]any{{"title": "after delete"}, {"parent_task_id": nil, "story_points": 3}} {
		status, e, err := apiCall(env, t, client, token, http.MethodPatch, url, body)
		if err != nil || status != http.StatusNotFound || e.ErrorCode != "TASK_NOT_FOUND" {
			t.Errorf("patch %v: status=%d code=%q err=%v", body, status, e.ErrorCode, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Doc folders and documents
// ---------------------------------------------------------------------------

func createFolderViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projID, name string, parentID any) string {
	t.Helper()
	body := map[string]any{"name": name}
	if parentID != nil {
		body["parent_id"] = parentID
	}
	status, e, err := apiCall(env, t, client, token, http.MethodPost, fmt.Sprintf("%s/api/v1/projects/%s/docs/folders", env.base, projID), body)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create folder: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}
	id, _ := assertDataMap(t, e)["id"].(string)
	return id
}

func createDocViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projID, title string) string {
	t.Helper()
	status, e, err := apiCall(env, t, client, token, http.MethodPost, fmt.Sprintf("%s/api/v1/projects/%s/docs", env.base, projID), map[string]any{"title": title})
	if err != nil || status != http.StatusCreated {
		t.Fatalf("create doc: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}
	id, _ := assertDataMap(t, e)["id"].(string)
	return id
}

func TestE2EDocFolder_MoveIntoOwnSubtreeIsRejected(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "folder-cycle-user", "foldercyclepass1")
	client, token := taskMemberLogin(t, env, "folder-cycle-user", "foldercyclepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	base := fmt.Sprintf("%s/api/v1/projects/%s/docs/folders", env.base, projID)

	a := createFolderViaAPI(t, env, client, token, projID, "A", nil)
	b := createFolderViaAPI(t, env, client, token, projID, "B", a)
	c := createFolderViaAPI(t, env, client, token, projID, "C", b)

	status, e, err := apiCall(env, t, client, token, http.MethodPatch, base+"/"+a, map[string]any{"parent_id": c})
	if err != nil || status != http.StatusBadRequest || e.ErrorCode != "DOC_FOLDER_CYCLE" {
		t.Errorf("A under C: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}
	status, e, err = apiCall(env, t, client, token, http.MethodPatch, base+"/"+a, map[string]any{"parent_id": a})
	if err != nil || status != http.StatusBadRequest || e.ErrorCode != "DOC_FOLDER_SELF_PARENT" {
		t.Errorf("A under A: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}
	// A legitimate move still works: C to the root.
	status, e, err = apiCall(env, t, client, token, http.MethodPatch, base+"/"+c, map[string]any{"parent_id": nil})
	if err != nil || status != http.StatusOK {
		t.Errorf("C to root: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}
}

// Two opposite moves (A under B, B under A) must not both succeed.
func TestE2EDocFolder_ConcurrentOppositeMovesNeverFormCycle(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "folder-race-user", "folderracepass1")
	client, token := taskMemberLogin(t, env, "folder-race-user", "folderracepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	base := fmt.Sprintf("%s/api/v1/projects/%s/docs/folders", env.base, projID)

	for round := 0; round < 20; round++ {
		a := createFolderViaAPI(t, env, client, token, projID, fmt.Sprintf("A%d", round), nil)
		b := createFolderViaAPI(t, env, client, token, projID, fmt.Sprintf("B%d", round), nil)
		results := fireConcurrently(
			patchAsync(env, t, client, token, base+"/"+a, map[string]any{"parent_id": b}),
			patchAsync(env, t, client, token, base+"/"+b, map[string]any{"parent_id": a}),
		)
		ok := 0
		for _, r := range results {
			r.mustHaveResponse(t)
			if r.status == http.StatusOK {
				ok++
			}
		}
		if ok != 1 {
			t.Fatalf("round %d: %d of 2 opposite moves succeeded (statuses %d, %d), want exactly 1",
				round, ok, results[0].status, results[1].status)
		}
	}
}

// Saving a deleted document must not bring it back.
func TestE2EDocument_UpdateAfterDeleteDoesNotResurrect(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "doc-resurrect-user", "docresurrectpass1")
	client, token := taskMemberLogin(t, env, "doc-resurrect-user", "docresurrectpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	url := func(id string) string { return fmt.Sprintf("%s/api/v1/projects/%s/docs/%s", env.base, projID, id) }

	docID := createDocViaAPI(t, env, client, token, projID, "doomed")
	if status, _, err := apiCall(env, t, client, token, http.MethodDelete, url(docID), nil); err != nil || status >= 300 {
		t.Fatalf("delete: status=%d err=%v", status, err)
	}
	status, e, err := apiCall(env, t, client, token, http.MethodPatch, url(docID), map[string]any{"title": "back from the dead"})
	if err != nil || status != http.StatusNotFound || e.ErrorCode != "DOC_NOT_FOUND" {
		t.Errorf("patch deleted: status=%d code=%q err=%v", status, e.ErrorCode, err)
	}
	if status, _, err = apiCall(env, t, client, token, http.MethodGet, url(docID), nil); err != nil || status != http.StatusNotFound {
		t.Errorf("get after patch: status=%d err=%v, want 404", status, err)
	}
}

// A racing delete and save may resolve either way, but the document must end
// up deleted if the delete succeeded.
func TestE2EDocument_ConcurrentSaveAndDeleteStaysDeleted(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "doc-delrace-user", "docdelracepass1")
	client, token := taskMemberLogin(t, env, "doc-delrace-user", "docdelracepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	url := func(id string) string { return fmt.Sprintf("%s/api/v1/projects/%s/docs/%s", env.base, projID, id) }

	for round := 0; round < 20; round++ {
		docID := createDocViaAPI(t, env, client, token, projID, "racing")
		results := fireConcurrently(
			patchAsync(env, t, client, token, url(docID), map[string]any{"title": fmt.Sprintf("edit-%d", round)}),
			func() callResult {
				s, e, err := apiCall(env, t, client, token, http.MethodDelete, url(docID), nil)
				return callResult{s, e, err}
			},
		)
		for _, r := range results {
			r.mustHaveResponse(t)
		}
		if status, _, err := apiCall(env, t, client, token, http.MethodGet, url(docID), nil); err != nil || status != http.StatusNotFound {
			t.Fatalf("round %d: doc readable after delete (status=%d err=%v; patch=%d delete=%d)",
				round, status, err, results[0].status, results[1].status)
		}
	}
}

// Overlapping saves of different document fields all survive.
func TestE2EDocument_ConcurrentPatchesKeepEveryChange(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "doc-race-user", "docracepass1")
	client, token := taskMemberLogin(t, env, "doc-race-user", "docracepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	url := func(id string) string { return fmt.Sprintf("%s/api/v1/projects/%s/docs/%s", env.base, projID, id) }

	for round := 0; round < 15; round++ {
		folderID := createFolderViaAPI(t, env, client, token, projID, fmt.Sprintf("F%d", round), nil)
		docID := createDocViaAPI(t, env, client, token, projID, "orig")
		title := fmt.Sprintf("title-%d", round)
		results := fireConcurrently(
			patchAsync(env, t, client, token, url(docID), map[string]any{"title": title}),
			patchAsync(env, t, client, token, url(docID), map[string]any{"position": round + 5}),
			patchAsync(env, t, client, token, url(docID), map[string]any{"folder_id": folderID}),
		)
		for _, r := range results {
			r.mustHaveResponse(t)
			if r.status != http.StatusOK {
				t.Fatalf("round %d: patch status %d (%s)", round, r.status, r.env.ErrorCode)
			}
		}
		status, e, err := apiCall(env, t, client, token, http.MethodGet, url(docID), nil)
		if err != nil || status != http.StatusOK {
			t.Fatalf("get: status=%d err=%v", status, err)
		}
		got := assertDataMap(t, e)
		if got["title"] != title {
			t.Errorf("round %d: title = %v, want %q", round, got["title"], title)
		}
		if p, _ := got["position"].(float64); int(p) != round+5 {
			t.Errorf("round %d: position = %v, want %d", round, got["position"], round+5)
		}
		if got["folder_id"] != folderID {
			t.Errorf("round %d: folder_id = %v, want %s", round, got["folder_id"], folderID)
		}
	}
}

// ---------------------------------------------------------------------------
// Sprints and views
// ---------------------------------------------------------------------------

func TestE2ESprintUpdate_ConcurrentPatchesKeepEveryChange(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sprint-race-user", "sprintracepass1")
	client, token := taskMemberLogin(t, env, "sprint-race-user", "sprintracepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	for round := 0; round < 15; round++ {
		sprintID := createSprintViaAPI(t, env, client, token, projID, "orig")
		url := fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s", env.base, projID, sprintID)
		name := fmt.Sprintf("name-%d", round)
		goal := fmt.Sprintf("goal-%d", round)
		results := fireConcurrently(
			patchAsync(env, t, client, token, url, map[string]any{"name": name}),
			patchAsync(env, t, client, token, url, map[string]any{"goal": goal}),
			patchAsync(env, t, client, token, url, map[string]any{"status": "active"}),
		)
		for _, r := range results {
			r.mustHaveResponse(t)
			if r.status != http.StatusOK {
				t.Fatalf("round %d: patch status %d (%s)", round, r.status, r.env.ErrorCode)
			}
		}
		status, e, err := apiCall(env, t, client, token, http.MethodGet, url, nil)
		if err != nil || status != http.StatusOK {
			t.Fatalf("get: status=%d err=%v", status, err)
		}
		got := assertDataMap(t, e)
		if got["name"] != name || got["goal"] != goal || got["status"] != "active" {
			t.Errorf("round %d: got name=%v goal=%v status=%v, want %q %q active", round, got["name"], got["goal"], got["status"], name, goal)
		}
	}
}

// Two simultaneous completions: exactly one succeeds, the other is a conflict.
func TestE2ECompleteSprint_ConcurrentCompletionsOnlyOneWins(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sprint-complete-race-user", "sprintcompleteracepass1")
	client, token := taskMemberLogin(t, env, "sprint-complete-race-user", "sprintcompleteracepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	for round := 0; round < 10; round++ {
		sprintID := createSprintViaAPI(t, env, client, token, projID, "to-complete")
		url := fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s/complete", env.base, projID, sprintID)
		post := func() callResult {
			s, e, err := apiCall(env, t, client, token, http.MethodPost, url, map[string]any{})
			return callResult{s, e, err}
		}
		results := fireConcurrently(post, post)
		ok, conflict := 0, 0
		for _, r := range results {
			r.mustHaveResponse(t)
			switch r.status {
			case http.StatusOK:
				ok++
			case http.StatusConflict:
				conflict++
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("round %d: statuses %d and %d, want one 200 and one 409", round, results[0].status, results[1].status)
		}
	}
}

func TestE2EViewUpdate_ConcurrentPatchesKeepEveryChange(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "view-race-user", "viewracepass1")
	client, token := taskMemberLogin(t, env, "view-race-user", "viewracepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "view-race")

	for round := 0; round < 15; round++ {
		viewID := createViewViaAPI(t, env, client, token, projID, sprintID, "orig", "table")
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s", env.base, projID, viewID)
		name := fmt.Sprintf("view-%d", round)
		results := fireConcurrently(
			patchAsync(env, t, client, token, url, map[string]any{"name": name}),
			patchAsync(env, t, client, token, url, map[string]any{"position": round + 7}),
		)
		for _, r := range results {
			r.mustHaveResponse(t)
			if r.status != http.StatusOK {
				t.Fatalf("round %d: patch status %d (%s)", round, r.status, r.env.ErrorCode)
			}
		}
		status, e, err := apiCall(env, t, client, token, http.MethodGet, url, nil)
		if err != nil || status != http.StatusOK {
			t.Fatalf("get: status=%d err=%v", status, err)
		}
		got := assertDataMap(t, e)
		if got["name"] != name {
			t.Errorf("round %d: name = %v, want %q", round, got["name"], name)
		}
		if p, _ := got["position"].(float64); int(p) != round+7 {
			t.Errorf("round %d: position = %v, want %d", round, got["position"], round+7)
		}
	}
}
