package e2e_test

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
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
// Task CRUD
// ---------------------------------------------------------------------------

func TestE2ETaskManagement_CRUD(t *testing.T) {
	env := newE2EEnv(t)
	seedTaskMemberUser(t, env, "task-crud-user", "taskpass1")
	client, token := taskMemberLogin(t, env, "task-crud-user", "taskpass1")
	projID := createProjectForTasksViaAPI(t, env, client, token)

	var taskID string

	t.Run("create_task", func(t *testing.T) {
		body := jsonBody(t, map[string]any{
			"title":       "Implement feature X",
			"description": "As a user I want feature X",
			"importance":  3,
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
		total, _ := data["total"].(float64)
		if total < 1 {
			t.Errorf("expected total >= 1, got %v", total)
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
	env := newE2EEnv(t)
	projID := uuid.New().String()
	req := mustRequest(env.ctx, t, http.MethodGet,
		fmt.Sprintf("%s/api/v1/projects/%s/tasks", env.base, projID), nil)
	resp := mustDo(t, env.client, req)
	defer func() { _ = resp.Body.Close() }()
	assertStatus(t, resp, http.StatusUnauthorized)
}

func TestE2ESprint_Unauthenticated(t *testing.T) {
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
			fmt.Sprintf("%s/api/v1/projects/%s/sprints/%s/tasks", env.base, projID, sprintID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		total, _ := data["total"].(float64)
		if total != 1 {
			t.Errorf("expected 1 sprint task, got %v", total)
		}
	})
}

func TestE2ESprintView_Backlog(t *testing.T) {
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
			fmt.Sprintf("%s/api/v1/projects/%s/product-backlog", env.base, projID), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp := mustDo(t, client, req)
		defer func() { _ = resp.Body.Close() }()
		assertStatus(t, resp, http.StatusOK)
		var env2 envelope
		decodeJSON(t, resp, &env2)
		data := assertDataMap(t, env2)
		total, _ := data["total"].(float64)
		if total != 2 {
			t.Errorf("expected 2 backlog tasks, got %v", total)
		}
	})
}
