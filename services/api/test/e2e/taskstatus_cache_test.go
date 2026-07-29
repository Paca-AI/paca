package e2e_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/platform/cache"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
)

// TestE2ETaskStatus_CachedUpdatePreservesPositionAndCategory exercises the
// exact production wiring from bootstrap/app.go -- tasksvc.NewCachedService
// wrapping tasksvc.New(taskRepo) over real Postgres + real Redis -- which the
// shared e2e harness (common_env_test.go) does not: it wires the plain,
// uncached service directly. Filed as a regression check for GitHub issue
// #252, which claimed a PATCH to a task-status that omits position reverts
// it to a default under this cached path; that does not reproduce against
// the current code (both the direct return and a subsequent cached-list
// read correctly preserve position/category), but there was previously no
// test exercising UpdateTaskStatus through the cache decorator at all.
func TestE2ETaskStatus_CachedUpdatePreservesPositionAndCategory(t *testing.T) {
	env := newE2EEnv(t)
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	cacheStore := cache.NewStore(env.redisClient, "paca:")

	svc := tasksvc.NewCachedService(tasksvc.New(env.taskRepo), cacheStore, 5*time.Minute, log)

	var projectID string
	if err := env.db.QueryRowContext(env.ctx, `INSERT INTO projects (name) VALUES ($1) RETURNING id`,
		"taskstatus-cache-project").Scan(&projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	pid, err := uuid.Parse(projectID)
	if err != nil {
		t.Fatalf("parse project id: %v", err)
	}

	created, err := svc.CreateTaskStatus(env.ctx, taskdom.CreateTaskStatusInput{
		ProjectID: pid,
		Name:      "In Progress",
		Position:  7,
		Category:  taskdom.StatusCategoryInProgress,
	})
	if err != nil {
		t.Fatalf("create task status: %v", err)
	}

	// Populate the list cache, mirroring a GET /task-statuses right after creation.
	if _, err := svc.ListTaskStatuses(env.ctx, pid); err != nil {
		t.Fatalf("list task statuses (populate cache): %v", err)
	}

	// PATCH only the name; position/color/category are omitted and must survive.
	updated, err := svc.UpdateTaskStatus(env.ctx, pid, created.ID, taskdom.UpdateTaskStatusInput{
		Name: "In Progress (renamed)",
	})
	if err != nil {
		t.Fatalf("update task status: %v", err)
	}
	if updated.Position != 7 {
		t.Errorf("expected position to remain 7 after a name-only PATCH, got %d", updated.Position)
	}
	if updated.Category != taskdom.StatusCategoryInProgress {
		t.Errorf("expected category to remain %q, got %q", taskdom.StatusCategoryInProgress, updated.Category)
	}

	// Read back through the LIST path: proves UpdateTaskStatus's cache
	// invalidation actually clears the stale pre-update snapshot rather than
	// leaving readers to see it until TTL expiry.
	listedAfter, err := svc.ListTaskStatuses(env.ctx, pid)
	if err != nil {
		t.Fatalf("list task statuses (after patch): %v", err)
	}
	var found *taskdom.TaskStatus
	for _, s := range listedAfter {
		if s.ID == created.ID {
			found = s
		}
	}
	if found == nil {
		t.Fatalf("status not found in list after patch")
	}
	if found.Name != "In Progress (renamed)" {
		t.Errorf("expected cached list to reflect the rename, got %q (stale cache not invalidated?)", found.Name)
	}
	if found.Position != 7 {
		t.Errorf("expected position to remain 7 via the cached list read, got %d", found.Position)
	}
	if found.Category != taskdom.StatusCategoryInProgress {
		t.Errorf("expected category to remain %q via the cached list read, got %q", taskdom.StatusCategoryInProgress, found.Category)
	}
}
