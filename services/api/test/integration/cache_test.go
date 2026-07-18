package integration_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/cache"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
)

// ---------------------------------------------------------------------------
// helpers: cache-aware router builder
// ---------------------------------------------------------------------------

// buildCachedTaskRouter builds an httptest-compatible gin.Engine wired with
// a CachedTaskService backed by an in-memory (miniredis) cache store.  The
// miniredis instance is automatically stopped when t ends.
func buildCachedTaskRouter(t *testing.T, taskRepo *fakeTaskRepo, store *projectPermStore) (http.Handler, *cache.Store) {
	t.Helper()

	// Stand up miniredis and build a cache.Store.
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cacheStore := cache.NewStore(client, "paca:")

	tm := jwttoken.New(testSecret, 15*time.Minute, 168*time.Hour)
	refreshStore := &fakeRefreshStore{}
	userRepo := newFakeUserRepo()
	authService := authsvc.New(userRepo, tm, refreshStore, 168*time.Hour, 24*time.Hour)
	userService := usersvc.New(userRepo)
	projectRepo := newFakeProjectRepo()
	projectService := projectsvc.New(projectRepo, taskRepo)

	// Wrap the real task service with the caching decorator.
	realTaskSvc := tasksvc.New(taskRepo)
	cachedTaskSvc := tasksvc.NewCachedService(
		realTaskSvc,
		cacheStore,
		5*time.Minute,
		slog.New(slog.NewTextHandler(os.Stdout, nil)),
	)
	viewService := sprintsvc.NewViewService(newFakeViewRepoIT())
	activityService := tasksvc.NewActivityService(newFakeTaskActivityRepo(), &fakeActivityMemberRepo{}, nil)
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	r := router.New(router.Deps{
		TokenManager:         tm,
		Authorizer:           authz.NewAuthorizer(store),
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, testCookieCfg),
		User:                 handler.NewUserHandler(userService),
		GlobalRole:           handler.NewGlobalRoleHandler(&fakeGlobalRoleService{}, authz.NewAuthorizer(store)),
		Project:              handler.NewProjectHandler(projectService, authz.NewAuthorizer(store)),
		Task:                 handler.NewTaskHandler(cachedTaskSvc, viewService, activityService),
		Log:                  log,
	})
	return r, cacheStore
}

// ---------------------------------------------------------------------------
// test: task-type cache via HTTP
// ---------------------------------------------------------------------------

// TestCacheIntegration_TaskTypes_HitAndInvalidation verifies, end-to-end through
// the HTTP layer, that:
//
//  1. The first GET populates the cache (stub repo is called once).
//  2. A subsequent GET hits the cache (stub repo is NOT called again).
//  3. After a POST (create), the cache is invalidated and the next GET hits the
//     repo again.
func TestCacheIntegration_TaskTypes_HitAndInvalidation(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
	r, _ := buildCachedTaskRouter(t, taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-types", projectID)

	// --- 1. First GET: cache miss — creates default task types (returns 200). ---
	w1 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("first GET: expected 200, got %d (%s)", w1.Code, w1.Body.String())
	}
	firstItems := decodeTaskTypeItems(t, w1.Body.Bytes())

	// --- 2. Second GET: cache hit — should return identical items. ---
	w2 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("second GET (cache hit): expected 200, got %d (%s)", w2.Code, w2.Body.String())
	}
	secondItems := decodeTaskTypeItems(t, w2.Body.Bytes())

	if len(firstItems) != len(secondItems) {
		t.Fatalf("cache hit: item count mismatch — first=%d, second=%d", len(firstItems), len(secondItems))
	}
	// The IDs must be identical (same objects served from cache).
	for i := range firstItems {
		got, _ := secondItems[i]["id"].(string)
		want, _ := firstItems[i]["id"].(string)
		if got != want {
			t.Errorf("item[%d] id mismatch: want %q, got %q", i, want, got)
		}
	}

	// --- 3. POST: create a new type — should invalidate the cache. ---
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name": "Integration Test Type",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("POST create: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}

	// --- 4. Third GET after invalidation: cache miss — should now include new type. ---
	w3 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w3.Code != http.StatusOK {
		t.Fatalf("third GET (after invalidation): expected 200, got %d (%s)", w3.Code, w3.Body.String())
	}
	thirdItems := decodeTaskTypeItems(t, w3.Body.Bytes())

	if len(thirdItems) != len(firstItems)+1 {
		t.Fatalf("after create+invalidation: expected %d items, got %d", len(firstItems)+1, len(thirdItems))
	}
}

// TestCacheIntegration_TaskStatuses_HitAndInvalidation verifies cache
// hit/miss/invalidation for task statuses via HTTP.
func TestCacheIntegration_TaskStatuses_HitAndInvalidation(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectID := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectID: {
				authz.PermissionTasksRead,
				authz.PermissionTasksWrite,
			},
		},
	}
	r, _ := buildCachedTaskRouter(t, taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())
	base := fmt.Sprintf("/api/v1/projects/%s/task-statuses", projectID)

	// First GET: populate cache.
	w1 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("first GET: expected 200, got %d (%s)", w1.Code, w1.Body.String())
	}
	initialCount := countListItems(t, w1.Body.Bytes())

	// Second GET: cache hit — count must match.
	w2 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("second GET (cache hit): expected 200, got %d (%s)", w2.Code, w2.Body.String())
	}
	if got := countListItems(t, w2.Body.Bytes()); got != initialCount {
		t.Fatalf("cache hit count mismatch: want %d, got %d", initialCount, got)
	}

	// POST: create a new status — invalidates cache.
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, base, tok, map[string]any{
		"name":     "In Review",
		"category": "inprogress",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("POST create: expected 201, got %d (%s)", createW.Code, createW.Body.String())
	}

	// Third GET: cache invalidated — must see the new status.
	w3 := serve(r, authedJSONReq(t.Context(), http.MethodGet, base, tok, nil))
	if w3.Code != http.StatusOK {
		t.Fatalf("third GET (after invalidation): expected 200, got %d (%s)", w3.Code, w3.Body.String())
	}
	if got := countListItems(t, w3.Body.Bytes()); got != initialCount+1 {
		t.Fatalf("after invalidation: expected %d items, got %d", initialCount+1, got)
	}
}

// TestCacheIntegration_PerProject_CacheIsolation verifies that invalidating
// the cache for project A does not evict project B's cached task types.
func TestCacheIntegration_PerProject_CacheIsolation(t *testing.T) {
	taskRepo := newFakeTaskRepoIT()
	projectA := uuid.New()
	projectB := uuid.New()
	store := &projectPermStore{
		projectPerms: map[uuid.UUID][]authz.Permission{
			projectA: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
			projectB: {authz.PermissionTasksRead, authz.PermissionTasksWrite},
		},
	}
	r, _ := buildCachedTaskRouter(t, taskRepo, store)
	tok := issueTaskToken(t, uuid.NewString())

	baseA := fmt.Sprintf("/api/v1/projects/%s/task-types", projectA)
	baseB := fmt.Sprintf("/api/v1/projects/%s/task-types", projectB)

	// Populate cache for both projects.
	if w := serve(r, authedJSONReq(t.Context(), http.MethodGet, baseA, tok, nil)); w.Code != http.StatusOK {
		t.Fatalf("GET project A: %d (%s)", w.Code, w.Body.String())
	}
	if w := serve(r, authedJSONReq(t.Context(), http.MethodGet, baseB, tok, nil)); w.Code != http.StatusOK {
		t.Fatalf("GET project B: %d (%s)", w.Code, w.Body.String())
	}

	// Record project B's initial item count.
	wB1 := serve(r, authedJSONReq(t.Context(), http.MethodGet, baseB, tok, nil))
	countB := countListItems(t, wB1.Body.Bytes())

	// Invalidate project A's cache by creating a new type there.
	createW := serve(r, authedJSONReq(t.Context(), http.MethodPost, baseA, tok, map[string]any{"name": "A-Only Type"}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("POST project A: %d (%s)", createW.Code, createW.Body.String())
	}

	// Project B's cache must be untouched: item count unchanged after a fresh GET.
	wB2 := serve(r, authedJSONReq(t.Context(), http.MethodGet, baseB, tok, nil))
	if got := countListItems(t, wB2.Body.Bytes()); got != countB {
		t.Fatalf("project B cache was evicted by project A invalidation: want %d items, got %d", countB, got)
	}
}

// ---------------------------------------------------------------------------
// decode helpers
// ---------------------------------------------------------------------------

func decodeTaskTypeItems(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	var env struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decodeTaskTypeItems: %v — body: %s", err, string(body))
	}
	return env.Data.Items
}

func countListItems(t *testing.T, body []byte) int {
	t.Helper()
	return len(decodeTaskTypeItems(t, body))
}
