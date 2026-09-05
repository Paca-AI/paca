package e2e_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
)

func seedTaskMemberUser(t *testing.T, env *e2eEnv, username, password string) {
	t.Helper()
	seedUser(t, env, username, password, "Task Member")
	roleName := "TASK_MEMBER_" + uuid.NewString()
	if err := env.roleRepo.Create(env.ctx, &globalroledom.GlobalRole{
		ID:   uuid.New(),
		Name: roleName,
		Permissions: map[string]any{
			"projects.create": true,
			"projects.read":   true,
			"projects.write":  true,
			"projects.delete": true,
			"tasks.read":      true,
			"tasks.write":     true,
			"sprints.read":    true,
			"sprints.write":   true,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create task-member role: %v", err)
	}
	assignGlobalRolesByName(t, env, username, roleName)
}

func taskMemberLogin(t *testing.T, env *e2eEnv, username, password string) (*http.Client, string) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp := login(env.ctx, t, client, env.base, username, password)
	defer func() { _ = resp.Body.Close() }()
	token := cookieValue(resp, "access_token")
	return client, token
}

func createProjectForTasksViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token string) string {
	t.Helper()
	body := jsonBody(t, map[string]any{"name": "task-project-" + uuid.NewString(), "description": ""})
	req := mustRequest(env.ctx, t, http.MethodPost, env.base+"/api/v1/projects", body)
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

func createSprintViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID, name string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/projects/%s/sprints", env.base, projectID)
	body := jsonBody(t, map[string]any{
		"name":   name,
		"status": "planned",
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
	return id
}

// ---------------------------------------------------------------------------
// Sprint CRUD
// ---------------------------------------------------------------------------

func TestE2ESprintManagement_CRUD(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sprint-crud-user", "sprintpass1")
	client, token := taskMemberLogin(t, env, "sprint-crud-user", "sprintpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	var sprintID string

	t.Run("create_sprint", func(t *testing.T) {
		sprintID = createSprintViaAPI(t, env, client, token, projID, "Sprint 1")
		if sprintID == "" {
			t.Fatal("expected non-empty sprint id")
		}
	})

	t.Run("list_sprints", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/sprints", env.base, projID), nil)
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
		if len(items) < 1 {
			t.Errorf("expected at least 1 sprint, got %d", len(items))
		}
	})

	t.Run("update_sprint", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"name":   "Sprint 1 Updated",
			"status": "active",
		})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s", env.base, projID, sprintID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if name, _ := data["name"].(string); name != "Sprint 1 Updated" {
			t.Errorf("expected updated name, got %q", name)
		}
	})

	t.Run("delete_sprint", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete,
			fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s", env.base, projID, sprintID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})
}

// ---------------------------------------------------------------------------
// Sprint partial-update semantics
//
// Regression coverage: PATCH /sprints/:sprintId used to unconditionally
// overwrite start_date/end_date/goal with whatever the request carried, even
// when the client omitted those fields entirely — silently wiping them on
// any PATCH that only meant to change e.g. the name. See UpdateSprintInput
// in internal/domain/sprint/service.go.
// ---------------------------------------------------------------------------

func TestE2ESprintManagement_PartialUpdatePreservesFields(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sprint-patch-user", "sprintpatchpass1")
	client, token := taskMemberLogin(t, env, "sprint-patch-user", "sprintpatchpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	sprintsURL := fmt.Sprintf("%s/api/v1/projects/%s/sprints", env.base, projID)

	const (
		startDate = "2026-01-01T00:00:00Z"
		endDate   = "2026-01-15T00:00:00Z"
		goal      = "Ship the thing"
	)
	var sprintID string

	t.Run("create_sprint_with_dates_and_goal", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"name":       "Sprint With Dates",
			"status":     "planned",
			"start_date": startDate,
			"end_date":   endDate,
			"goal":       goal,
		})
		req := mustRequest(env.ctx, t, http.MethodPost, sprintsURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		sprintID, _ = data["id"].(string)
		if sprintID == "" {
			t.Fatal("expected non-empty sprint id")
		}
		if g, _ := data["goal"].(string); g != goal {
			t.Fatalf("expected goal %q at creation, got %v", goal, data["goal"])
		}
	})

	t.Run("patch_name_only_preserves_dates_and_goal", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": "Sprint With Dates Renamed"})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/%s", sprintsURL, sprintID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)

		if name, _ := data["name"].(string); name != "Sprint With Dates Renamed" {
			t.Errorf("expected renamed sprint, got %q", name)
		}
		if g, _ := data["goal"].(string); g != goal {
			t.Errorf("expected goal to remain %q after a name-only PATCH, got %v", goal, data["goal"])
		}
		if gotStart, _ := data["start_date"].(string); gotStart == "" {
			t.Error("expected start_date to remain set after a name-only PATCH, got empty")
		}
		if gotEnd, _ := data["end_date"].(string); gotEnd == "" {
			t.Error("expected end_date to remain set after a name-only PATCH, got empty")
		}
	})

	t.Run("explicit_null_clears_goal_without_touching_dates", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"goal": nil})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/%s", sprintsURL, sprintID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)

		if g := data["goal"]; g != nil {
			t.Errorf("expected goal to be cleared, got %v", g)
		}
		if gotStart, _ := data["start_date"].(string); gotStart == "" {
			t.Error("expected start_date to remain set after clearing goal")
		}
	})
}

// ---------------------------------------------------------------------------
// Task type partial-update semantics
//
// Regression coverage: PATCH /task-types/:typeId used to unconditionally
// overwrite icon/color/description with whatever the request carried, even
// when the client omitted those fields entirely. See UpdateTaskTypeInput in
// internal/domain/task/service.go.
// ---------------------------------------------------------------------------

func TestE2ETaskTypeManagement_PartialUpdatePreservesFields(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "task-type-patch-user", "tasktypepass1")
	client, token := taskMemberLogin(t, env, "task-type-patch-user", "tasktypepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	taskTypesURL := fmt.Sprintf("%s/api/v1/projects/%s/task-types", env.base, projID)

	const (
		icon        = "rocket"
		color       = "#ff0000"
		description = "A feature request"
	)
	var typeID string

	t.Run("create_task_type_with_icon_color_description", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"name":        "Feature",
			"icon":        icon,
			"color":       color,
			"description": description,
		})
		req := mustRequest(env.ctx, t, http.MethodPost, taskTypesURL, body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		typeID, _ = data["id"].(string)
		if typeID == "" {
			t.Fatal("expected non-empty task type id")
		}
	})

	t.Run("patch_name_only_preserves_icon_color_description", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"name": "Feature Request"})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/%s", taskTypesURL, typeID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)

		if name, _ := data["name"].(string); name != "Feature Request" {
			t.Errorf("expected renamed task type, got %q", name)
		}
		if i, _ := data["icon"].(string); i != icon {
			t.Errorf("expected icon to remain %q after a name-only PATCH, got %v", icon, data["icon"])
		}
		if c, _ := data["color"].(string); c != color {
			t.Errorf("expected color to remain %q after a name-only PATCH, got %v", color, data["color"])
		}
		if d, _ := data["description"].(string); d != description {
			t.Errorf("expected description to remain %q after a name-only PATCH, got %v", description, data["description"])
		}
	})

	t.Run("explicit_null_clears_icon_without_touching_color_or_description", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"icon": nil})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/%s", taskTypesURL, typeID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)

		if icon := data["icon"]; icon != nil {
			t.Errorf("expected icon to be cleared, got %v", icon)
		}
		if c, _ := data["color"].(string); c != color {
			t.Errorf("expected color to remain %q, got %v", color, data["color"])
		}
		if d, _ := data["description"].(string); d != description {
			t.Errorf("expected description to remain %q, got %v", description, data["description"])
		}
	})
}

// ---------------------------------------------------------------------------
// Task CRUD
// ---------------------------------------------------------------------------

func TestE2ETaskManagement_CRUD(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "task-crud-user", "taskpass1")
	client, token := taskMemberLogin(t, env, "task-crud-user", "taskpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	var taskID string
	var taskNumber float64

	t.Run("create_task", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title": "Implement feature X",
			"description": []map[string]any{
				{
					"id":       "1",
					"type":     "paragraph",
					"props":    map[string]any{"textColor": "default", "backgroundColor": "default", "textAlignment": "left"},
					"content":  []map[string]any{{"type": "text", "text": "As a user I want feature X", "styles": map[string]any{}}},
					"children": []any{},
				},
			},
			"importance":   3,
			"story_points": 5,
		})
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
		taskID, _ = data["id"].(string)
		if taskID == "" {
			t.Fatal("expected non-empty task id")
		}
		taskNumber, _ = data["task_number"].(float64)
		if taskNumber < 1 {
			t.Errorf("expected task_number >= 1, got %v", taskNumber)
		}
		if sp, _ := data["story_points"].(float64); sp != 5 {
			t.Errorf("expected story_points=5 in create response, got %v", data["story_points"])
		}
	})

	t.Run("list_tasks", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) < 1 {
			t.Errorf("expected at least 1 task, got %d", len(items))
		}
		totalCount, _ := data["total_count"].(float64)
		if int(totalCount) < 1 {
			t.Errorf("expected total_count >= 1, got %v", totalCount)
		}
		if int(totalCount) != len(items) {
			t.Errorf("expected total_count=%d to match items length=%d", int(totalCount), len(items))
		}
	})

	t.Run("get_task", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if id, _ := data["id"].(string); id != taskID {
			t.Errorf("expected id %q, got %q", taskID, id)
		}
	})

	t.Run("update_task", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"title": "Implement feature X (updated)"})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if title, _ := data["title"].(string); title != "Implement feature X (updated)" {
			t.Errorf("expected updated title, got %q", title)
		}
	})

	t.Run("get_task_by_number", func(t *testing.T) {
		if taskNumber < 1 {
			t.Skip("task_number not available (create_task may have failed)")
		}
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/by-number/%d", env.base, projID, int64(taskNumber)), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if id, _ := data["id"].(string); id != taskID {
			t.Errorf("get by number: expected id %q, got %q", taskID, id)
		}
		if num, _ := data["task_number"].(float64); num != taskNumber {
			t.Errorf("get by number: expected task_number=%v, got %v", taskNumber, num)
		}
	})

	t.Run("delete_task", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("get_deleted_task_returns_not_found", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "TASK_NOT_FOUND")
	})
}

// ---------------------------------------------------------------------------
// Unauthenticated access
// ---------------------------------------------------------------------------

func TestE2ETask_Unauthenticated(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	projID := uuid.New().String()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), nil)
	resp := mustDo(t, env.client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestE2ESprint_Unauthenticated(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	projID := uuid.New().String()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/sprints", env.base, projID), nil)
	resp := mustDo(t, env.client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

// ---------------------------------------------------------------------------
// Insufficient permissions
// ---------------------------------------------------------------------------

func TestE2ETask_InsufficientPermissions(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	// Seed a plain user with no task permissions.
	seedUser(t, env, "no-task-perm-user", "plainpass1", "No Task Perm")
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp := login(env.ctx, t, client, env.base, "no-task-perm-user", "plainpass1")
	token := cookieValue(resp, "access_token")
	_ = resp.Body.Close()

	projID := uuid.New().String()

	t.Run("create_task_forbidden", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"title": "should-fail"})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusForbidden)
		assertErrorCode(t, resp, "FORBIDDEN")
	})
}

// ---------------------------------------------------------------------------
// Sprint view — GetSprint, GetSprintTasks, Backlog
// ---------------------------------------------------------------------------

func TestE2ESprintManagement_GetSprint(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "get-sprint-user", "getsprintpass1")
	client, token := taskMemberLogin(t, env, "get-sprint-user", "getsprintpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint View 1")

	t.Run("get_sprint_by_id", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s", env.base, projID, sprintID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if id, _ := data["id"].(string); id != sprintID {
			t.Errorf("expected sprint id %q, got %q", sprintID, id)
		}
		if name, _ := data["name"].(string); name != "Sprint View 1" {
			t.Errorf("expected name 'Sprint View 1', got %q", name)
		}
	})

	t.Run("get_sprint_not_found", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s", env.base, projID, uuid.New()), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "SPRINT_NOT_FOUND")
	})
}

func TestE2ESprintView_SprintTasks(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sprint-tasks-user", "sprinttaskspass1")
	client, token := taskMemberLogin(t, env, "sprint-tasks-user", "sprinttaskspass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint Tasks View")

	// Create a task assigned to the sprint
	t.Run("setup_sprint_task", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title":     "Sprint task",
			"sprint_id": sprintID,
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
	})

	// Create a backlog task (no sprint)
	t.Run("setup_backlog_task", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"title": "Backlog task"})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
	})

	t.Run("get_sprint_tasks_returns_only_sprint_tasks", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=%s", env.base, projID, sprintID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 1 {
			t.Errorf("expected 1 sprint task, got %d", len(items))
		}
		if tc, _ := data["total_count"].(float64); int(tc) != 1 {
			t.Errorf("expected total_count=1 for sprint filter, got %v", tc)
		}
	})
}

func TestE2ESprintView_Backlog(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "backlog-view-user", "backlogpass1")
	client, token := taskMemberLogin(t, env, "backlog-view-user", "backlogpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint for Backlog Test")

	// Create tasks: 1 sprint task + 2 backlog tasks
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
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusCreated)
	}

	createTask("In-sprint task", &sprintID)
	createTask("Backlog task 1", nil)
	createTask("Backlog task 2", nil)

	t.Run("backlog_returns_only_sprintless_tasks", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=null", env.base, projID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) != 2 {
			t.Errorf("expected 2 backlog tasks, got %d", len(items))
		}
		if tc, _ := data["total_count"].(float64); int(tc) != 2 {
			t.Errorf("expected total_count=2 for backlog filter, got %v", tc)
		}
	})
}

// ---------------------------------------------------------------------------
// ListTasks with view_id — view position enrichment
// ---------------------------------------------------------------------------

// TestE2ETaskList_WithViewID verifies that GET /tasks?view_id=<id> returns
// view_position and view_group_key on each task that has a recorded manual
// position in that view.
func TestE2ETaskList_WithViewID(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "view-task-pos-user", "viewpospass1")
	client, token := taskMemberLogin(t, env, "view-task-pos-user", "viewpospass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint ViewPos")

	// Create a view for the sprint.
	viewID := createViewViaAPI(t, env, client, token, projID, sprintID, "Position View", "table")

	// Create two tasks assigned to the sprint.
	createTaskInSprint := func(title string) string {
		t.Helper()
		body := jsonBody(t, map[string]any{
			"title":     title,
			"sprint_id": sprintID,
		})
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

	task1ID := createTaskInSprint("ViewPos Task Alpha")
	task2ID := createTaskInSprint("ViewPos Task Beta")

	// Assign manual positions in the view.
	moveTask := func(taskID string, position int, groupKey *string) {
		t.Helper()
		body := map[string]any{"position": position}
		if groupKey != nil {
			body["group_key"] = *groupKey
		}
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions/%s",
			env.base, projID, viewID, taskID)
		req := mustRequest(env.ctx, t, http.MethodPut, url, jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	}

	gk := "status-col-1"
	moveTask(task1ID, 5, &gk)
	moveTask(task2ID, 15, nil)

	t.Run("without_view_id_no_view_position", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=%s", env.base, projID, sprintID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if _, ok := item["view_position"]; ok {
				t.Error("expected no view_position field without view_id query param")
			}
		}
	})

	t.Run("with_view_id_positions_enriched", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=%s&view_id=%s",
				env.base, projID, sprintID, viewID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		if len(items) < 2 {
			t.Fatalf("expected at least 2 tasks, got %d", len(items))
		}
		posMap := make(map[string]float64)
		groupMap := make(map[string]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			id, _ := item["id"].(string)
			if pos, ok := item["view_position"]; ok {
				posMap[id] = pos.(float64)
			}
			if gk, ok := item["view_group_key"]; ok {
				groupMap[id] = gk
			}
		}
		if posMap[task1ID] != 5 {
			t.Errorf("expected task1 view_position=5, got %v", posMap[task1ID])
		}
		if posMap[task2ID] != 15 {
			t.Errorf("expected task2 view_position=15, got %v", posMap[task2ID])
		}
		if groupMap[task1ID] != "status-col-1" {
			t.Errorf("expected task1 view_group_key=status-col-1, got %v", groupMap[task1ID])
		}
		if _, hasGroup := groupMap[task2ID]; hasGroup {
			t.Errorf("expected no view_group_key for task2, got %v", groupMap[task2ID])
		}
	})

	t.Run("invalid_view_id_returns_400", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?view_id=not-a-uuid", env.base, projID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("unknown_view_id_returns_404", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?view_id=%s", env.base, projID, uuid.New()), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "VIEW_NOT_FOUND")
	})
}

// ---------------------------------------------------------------------------
// GetSprintTasks with view_id — view position enrichment
// ---------------------------------------------------------------------------

// TestE2ESprintTasks_WithViewID verifies that
// GET /sprints/:sprintId/tasks?view_id=<id> returns view_position and
// view_group_key for tasks that have a recorded position in that view.
func TestE2ESprintTasks_WithViewID(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "sprint-viewid-user", "sprintviewpass1")
	client, token := taskMemberLogin(t, env, "sprint-viewid-user", "sprintviewpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint ViewID")

	viewID := createViewViaAPI(t, env, client, token, projID, sprintID, "Sprint Pos View", "table")

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

	task1ID := createSprintTask("Sprint Pos Alpha")
	task2ID := createSprintTask("Sprint Pos Beta")

	moveSprintTask := func(taskID string, position int, groupKey *string) {
		t.Helper()
		body := map[string]any{"position": position}
		if groupKey != nil {
			body["group_key"] = *groupKey
		}
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions/%s",
			env.base, projID, viewID, taskID)
		req := mustRequest(env.ctx, t, http.MethodPut, url, jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	}

	gk := "sprint-col"
	moveSprintTask(task1ID, 10, &gk)
	moveSprintTask(task2ID, 20, nil)

	t.Run("with_view_id_positions_returned", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=%s&view_id=%s", env.base, projID, sprintID, viewID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		posMap := make(map[string]float64)
		groupMap := make(map[string]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			id, _ := item["id"].(string)
			if pos, ok := item["view_position"]; ok {
				posMap[id] = pos.(float64)
			}
			if gk, ok := item["view_group_key"]; ok {
				groupMap[id] = gk
			}
		}
		if posMap[task1ID] != 10 {
			t.Errorf("expected task1 view_position=10, got %v", posMap[task1ID])
		}
		if posMap[task2ID] != 20 {
			t.Errorf("expected task2 view_position=20, got %v", posMap[task2ID])
		}
		if groupMap[task1ID] != "sprint-col" {
			t.Errorf("expected task1 view_group_key=sprint-col, got %v", groupMap[task1ID])
		}
	})

	t.Run("without_view_id_no_positions", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=%s", env.base, projID, sprintID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if _, ok := item["view_position"]; ok {
				t.Error("expected no view_position without view_id param")
			}
		}
	})

	t.Run("invalid_view_id_returns_400", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=%s&view_id=not-a-uuid", env.base, projID, sprintID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("unknown_view_id_returns_404", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=%s&view_id=%s", env.base, projID, sprintID, uuid.New()), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "VIEW_NOT_FOUND")
	})
}

// ---------------------------------------------------------------------------
// Backlog tasks with view_id — view position enrichment
// ---------------------------------------------------------------------------

// TestE2EBacklog_WithViewID verifies that
// GET /tasks?sprint_id=null&view_id=<id> returns view_position and view_group_key
// for backlog tasks that have a recorded position in that view.
func TestE2EBacklog_WithViewID(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "backlog-viewid-user", "backlogviewpass1")
	client, token := taskMemberLogin(t, env, "backlog-viewid-user", "backlogviewpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	viewID := createBacklogViewViaAPI(t, env, client, token, projID, "Backlog Pos View", "table")

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

	task1ID := createBacklogTask("Backlog Pos Alpha")
	task2ID := createBacklogTask("Backlog Pos Beta")

	moveBacklogTask := func(taskID string, position int, groupKey *string) {
		t.Helper()
		body := map[string]any{"position": position}
		if groupKey != nil {
			body["group_key"] = *groupKey
		}
		url := fmt.Sprintf("%s/api/v1/projects/%s/views/%s/task-positions/%s",
			env.base, projID, viewID, taskID)
		req := mustRequest(env.ctx, t, http.MethodPut, url, jsonBody(t, body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNoContent)
	}

	gk := "backlog-col"
	moveBacklogTask(task1ID, 3, &gk)
	moveBacklogTask(task2ID, 7, nil)

	t.Run("with_view_id_positions_returned", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=null&view_id=%s", env.base, projID, viewID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		posMap := make(map[string]float64)
		groupMap := make(map[string]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			id, _ := item["id"].(string)
			if pos, ok := item["view_position"]; ok {
				posMap[id] = pos.(float64)
			}
			if gk, ok := item["view_group_key"]; ok {
				groupMap[id] = gk
			}
		}
		if posMap[task1ID] != 3 {
			t.Errorf("expected task1 view_position=3, got %v", posMap[task1ID])
		}
		if posMap[task2ID] != 7 {
			t.Errorf("expected task2 view_position=7, got %v", posMap[task2ID])
		}
		if groupMap[task1ID] != "backlog-col" {
			t.Errorf("expected task1 view_group_key=backlog-col, got %v", groupMap[task1ID])
		}
	})

	t.Run("without_view_id_no_positions", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=null", env.base, projID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if _, ok := item["view_position"]; ok {
				t.Error("expected no view_position without view_id param")
			}
		}
	})

	t.Run("invalid_view_id_returns_400", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=null&view_id=not-a-uuid", env.base, projID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
	})

	t.Run("unknown_view_id_returns_404", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=null&view_id=%s", env.base, projID, uuid.New()), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "VIEW_NOT_FOUND")
	})
}

// ---------------------------------------------------------------------------
// Task dates and tags
// ---------------------------------------------------------------------------

func TestE2ETaskManagement_DatesAndTags(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "dates-tags-user", "datestagpass1")
	client, token := taskMemberLogin(t, env, "dates-tags-user", "datestagpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	var taskID string

	t.Run("create_task_with_dates_and_tags", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title":      "Task with dates and tags",
			"start_date": "2026-05-01T00:00:00Z",
			"due_date":   "2026-05-31T00:00:00Z",
			"tags":       []string{"alpha", "beta"},
		})
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
		taskID, _ = data["id"].(string)
		if taskID == "" {
			t.Fatal("expected non-empty task id")
		}
		if data["start_date"] == nil {
			t.Error("expected start_date in create response")
		}
		if data["due_date"] == nil {
			t.Error("expected due_date in create response")
		}
		tags, _ := data["tags"].([]any)
		if len(tags) != 2 {
			t.Errorf("expected 2 tags, got %d", len(tags))
		}
	})

	t.Run("get_task_returns_dates_and_tags", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if data["start_date"] == nil {
			t.Error("expected start_date in get response")
		}
		if data["due_date"] == nil {
			t.Error("expected due_date in get response")
		}
		tags, _ := data["tags"].([]any)
		if len(tags) != 2 {
			t.Errorf("expected 2 tags in get response, got %d", len(tags))
		}
	})

	t.Run("update_task_replaces_tags_and_clears_dates", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title":      "Task with dates and tags",
			"start_date": nil,
			"due_date":   nil,
			"tags":       []string{"gamma"},
		})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if _, hasStart := data["start_date"]; hasStart {
			t.Error("expected start_date to be absent after clearing")
		}
		if _, hasDue := data["due_date"]; hasDue {
			t.Error("expected due_date to be absent after clearing")
		}
		tags, _ := data["tags"].([]any)
		if len(tags) != 1 {
			t.Errorf("expected 1 tag after update, got %d", len(tags))
		}
	})

	t.Run("update_task_clears_tags", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title": "Task with dates and tags",
			"tags":  []string{},
		})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		tags, _ := data["tags"].([]any)
		if len(tags) != 0 {
			t.Errorf("expected 0 tags after clearing, got %d", len(tags))
		}
	})
}

// ---------------------------------------------------------------------------
// Multiple assignees
// ---------------------------------------------------------------------------

func TestE2ETaskManagement_MultipleAssignees(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	ownerUsername := "multi-assignee-owner-" + uuid.NewString()
	seedTaskMemberUser(t, env, ownerUsername, "multiassigneeowner1")
	client, token := taskMemberLogin(t, env, ownerUsername, "multiassigneeowner1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	memberA := addProjectMemberWithAutomationPerms(t, env, client, token, projID, "multi-assignee-a-"+uuid.NewString(), "multiassigneea1")
	memberB := addProjectMemberWithAutomationPerms(t, env, client, token, projID, "multi-assignee-b-"+uuid.NewString(), "multiassigneeb1")
	memberC := addProjectMemberWithAutomationPerms(t, env, client, token, projID, "multi-assignee-c-"+uuid.NewString(), "multiassigneec1")

	var taskID string

	assigneeIDs := func(data map[string]any) []string {
		raw, _ := data["assignee_ids"].([]any)
		out := make([]string, len(raw))
		for i, v := range raw {
			out[i], _ = v.(string)
		}
		return out
	}
	containsAll := func(got []string, want ...string) bool {
		set := make(map[string]bool, len(got))
		for _, g := range got {
			set[g] = true
		}
		for _, w := range want {
			if !set[w] {
				return false
			}
		}
		return true
	}

	t.Run("create_task_with_multiple_assignees", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title":        "Task with multiple assignees",
			"assignee_ids": []string{memberA, memberB},
		})
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
		taskID, _ = data["id"].(string)
		if taskID == "" {
			t.Fatal("expected non-empty task id")
		}
		got := assigneeIDs(data)
		if len(got) != 2 || !containsAll(got, memberA, memberB) {
			t.Fatalf("expected assignee_ids=[%s,%s], got %v", memberA, memberB, got)
		}
	})

	t.Run("get_task_returns_both_assignees", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		got := assigneeIDs(data)
		if len(got) != 2 || !containsAll(got, memberA, memberB) {
			t.Fatalf("expected assignee_ids=[%s,%s], got %v", memberA, memberB, got)
		}
	})

	t.Run("filter_by_assignee_ids_matches_any_assignee", func(t *testing.T) {
		// Filtering by memberB alone should still surface the task since it
		// matches ANY of its assignees, not all of them.
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?assignee_ids=%s", env.base, projID, memberB), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		found := false
		for _, item := range items {
			m, _ := item.(map[string]any)
			if id, _ := m["id"].(string); id == taskID {
				found = true
			}
		}
		if !found {
			t.Errorf("expected task %s to be included when filtering by assignee_ids=%s", taskID, memberB)
		}
	})

	t.Run("filter_by_unrelated_assignee_excludes_task", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks?assignee_ids=%s", env.base, projID, memberC), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		items, _ := data["items"].([]any)
		for _, item := range items {
			m, _ := item.(map[string]any)
			if id, _ := m["id"].(string); id == taskID {
				t.Errorf("did not expect task %s when filtering by unrelated assignee_ids=%s", taskID, memberC)
			}
		}
	})

	t.Run("update_task_removes_one_and_adds_another", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"assignee_ids": []string{memberB, memberC}})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		got := assigneeIDs(data)
		if len(got) != 2 || !containsAll(got, memberB, memberC) {
			t.Fatalf("expected assignee_ids=[%s,%s], got %v", memberB, memberC, got)
		}
	})

	t.Run("update_task_clears_all_assignees", func(t *testing.T) {
		body := jsonBody(t, map[string]any{"assignee_ids": []string{}})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/api/v1/projects/%s/tasks/%s", env.base, projID, taskID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		got := assigneeIDs(data)
		if len(got) != 0 {
			t.Fatalf("expected no assignees after clearing, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Custom field definition helpers
// ---------------------------------------------------------------------------

// customFieldOptions builds the wire shape for a select/multi_select custom
// field's options list: an array of {"value": ...} objects (each may also
// carry an optional "color", not needed by these tests).
func customFieldOptions(values ...string) []map[string]any {
	opts := make([]map[string]any, len(values))
	for i, v := range values {
		opts[i] = map[string]any{"value": v}
	}
	return opts
}

func createCustomFieldViaAPI(t *testing.T, env *e2eEnv, client *http.Client, token, projectID string, body map[string]any) string {
	t.Helper()
	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/custom-fields", env.base, projectID),
		jsonBody(t, body))
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
		t.Fatal("expected non-empty custom field id")
	}
	return id
}

// ---------------------------------------------------------------------------
// Custom field management E2E tests
// ---------------------------------------------------------------------------

func TestE2ECustomFieldManagement_CRUD(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "cf-crud-user", "cfcrudpass1")
	client, token := taskMemberLogin(t, env, "cf-crud-user", "cfcrudpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	var fieldID string

	t.Run("create_custom_field", func(t *testing.T) {
		fieldID = createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
			"field_key":    "priority_level",
			"display_name": "Priority Level",
			"field_type":   "text",
			"is_required":  false,
		})
	})

	t.Run("list_custom_fields", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/custom-fields", env.base, projID), nil)
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
		if len(items) < 1 {
			t.Errorf("expected at least 1 custom field, got %d", len(items))
		}
	})

	t.Run("get_custom_field", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/custom-fields/%s", env.base, projID, fieldID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if id, _ := data["id"].(string); id != fieldID {
			t.Errorf("expected id %q, got %q", fieldID, id)
		}
		if key, _ := data["field_key"].(string); key != "priority_level" {
			t.Errorf("expected field_key 'priority_level', got %q", key)
		}
		if ft, _ := data["field_type"].(string); ft != "text" {
			t.Errorf("expected field_type 'text', got %q", ft)
		}
	})

	t.Run("update_custom_field", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"display_name": "Priority Level (Updated)",
		})
		req := mustRequest(env.ctx, t, http.MethodPatch,
			fmt.Sprintf("%s/api/v1/projects/%s/custom-fields/%s", env.base, projID, fieldID), body)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		if name, _ := data["display_name"].(string); name != "Priority Level (Updated)" {
			t.Errorf("expected updated display_name, got %q", name)
		}
	})

	t.Run("select_type_with_options", func(t *testing.T) {
		selectFieldID := createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
			"field_key":    "status_tag",
			"display_name": "Status Tag",
			"field_type":   "select",
			"options":      customFieldOptions("open", "in_progress", "done"),
			"is_required":  true,
		})
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/custom-fields/%s", env.base, projID, selectFieldID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		options, ok := data["options"].([]any)
		if !ok {
			t.Fatalf("expected options array, got %T", data["options"])
		}
		if len(options) != 3 {
			t.Errorf("expected 3 options, got %d", len(options))
		}
		if isRequired, _ := data["is_required"].(bool); !isRequired {
			t.Error("expected is_required to be true")
		}
	})

	t.Run("duplicate_key_returns_409", func(t *testing.T) {
		createCustomFieldViaAPI(t, env, client, token, projID, map[string]any{
			"field_key":    "unique_key",
			"display_name": "First Field",
			"field_type":   "text",
		})
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/custom-fields", env.base, projID),
			jsonBody(t, map[string]any{
				"field_key":    "unique_key",
				"display_name": "Duplicate Field",
				"field_type":   "number",
			}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusConflict)
		assertErrorCode(t, resp, "CUSTOM_FIELD_KEY_TAKEN")
	})

	t.Run("invalid_type_returns_400", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodPost,
			fmt.Sprintf("%s/api/v1/projects/%s/custom-fields", env.base, projID),
			jsonBody(t, map[string]any{
				"field_key":    "bad_type_field",
				"display_name": "Bad Type",
				"field_type":   "not_a_real_type",
			}))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusBadRequest)
		assertErrorCode(t, resp, "CUSTOM_FIELD_TYPE_INVALID")
	})

	t.Run("delete_custom_field", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodDelete,
			fmt.Sprintf("%s/api/v1/projects/%s/custom-fields/%s", env.base, projID, fieldID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
	})

	t.Run("get_deleted_field_returns_not_found", func(t *testing.T) {
		req := mustRequest(env.ctx, t, http.MethodGet,
			fmt.Sprintf("%s/api/v1/projects/%s/custom-fields/%s", env.base, projID, fieldID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusNotFound)
		assertErrorCode(t, resp, "CUSTOM_FIELD_NOT_FOUND")
	})
}

func TestE2ECustomFieldManagement_Unauthorized(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedUser(t, env, "cf-noperm-user", "cfnopermpass1", "No Perm CF")
	jar, _ := cookiejar.New(nil)
	noPermClient := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	resp := login(env.ctx, t, noPermClient, env.base, "cf-noperm-user", "cfnopermpass1")
	token := cookieValue(resp, "access_token")
	_ = resp.Body.Close()

	projID := uuid.New().String()

	req := mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/custom-fields", env.base, projID),
		jsonBody(t, map[string]any{
			"field_key":    "should_fail",
			"display_name": "Should Fail",
			"field_type":   "text",
		}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp = mustDo(t, noPermClient, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusForbidden)
	assertErrorCode(t, resp, "FORBIDDEN")
}

// ---------------------------------------------------------------------------
// Complete sprint
// ---------------------------------------------------------------------------

func TestE2ECompleteSprint_MovesToBacklog(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "complete-sprint-user", "completepass1")
	client, token := taskMemberLogin(t, env, "complete-sprint-user", "completepass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	// Create and activate the sprint.
	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint To Complete")
	activateBody := jsonBody(t, map[string]any{"status": "active"})
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s", env.base, projID, sprintID), activateBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	_ = resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	// Assign a task to the sprint.
	taskBody := jsonBody(t, map[string]any{"title": "incomplete task", "sprint_id": sprintID})
	req = mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), taskBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp = mustDo(t, client, req)
	_ = resp.Body.Close()
	assertStatus(t, resp, http.StatusCreated)

	// Complete the sprint — no destination means backlog.
	completeBody := jsonBody(t, map[string]any{})
	req = mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s/complete", env.base, projID, sprintID), completeBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp = mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env2 envelope
	decodeJSON(t, resp, &env2)
	data := assertDataMap(t, env2)
	if status, _ := data["status"].(string); status != "completed" {
		t.Errorf("expected sprint status=completed, got %q", status)
	}

	// Verify the task moved to backlog.
	req = mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks?sprint_id=null", env.base, projID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp = mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusOK)
	var env3 envelope
	decodeJSON(t, resp, &env3)
	d := assertDataMap(t, env3)
	if items, _ := d["items"].([]any); len(items) < 1 {
		t.Errorf("expected at least 1 backlog task after sprint completion, got 0")
	}
}

func TestE2ECompleteSprint_AlreadyCompleted(t *testing.T) {
	t.Parallel()
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "complete-sprint-dup-user", "completepass2")
	client, token := taskMemberLogin(t, env, "complete-sprint-dup-user", "completepass2")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	sprintID := createSprintViaAPI(t, env, client, token, projID, "Sprint Already Done")

	// Activate then immediately complete via the bulk endpoint.
	activateBody := jsonBody(t, map[string]any{"status": "active"})
	req := mustRequest(env.ctx, t, http.MethodPatch,
		fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s", env.base, projID, sprintID), activateBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp := mustDo(t, client, req)
	_ = resp.Body.Close()

	completeBody := jsonBody(t, map[string]any{})
	req = mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s/complete", env.base, projID, sprintID), completeBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp = mustDo(t, client, req)
	_ = resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	// A second call must return 409.
	req = mustRequest(env.ctx, t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s/complete", env.base, projID, sprintID), jsonBody(t, map[string]any{}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp = mustDo(t, client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusConflict)
	assertErrorCode(t, resp, "SPRINT_ALREADY_COMPLETE")
}
