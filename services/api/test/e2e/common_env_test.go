// Package e2e_test contains end-to-end smoke tests for the Paca API service.
// Tests spin up real Postgres and Redis containers via testcontainers-go,
// apply migrations, wire the full service stack, and exercise the complete
// HTTP request flow against an in-process httptest.Server.
//
// Run with: PACA_E2E=1 go test ./test/e2e/... -v -timeout 300s
package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/platform/cache"
	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/internal/platform/messaging"
	"github.com/Paca-AI/api/internal/platform/storage"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	pgRepo "github.com/Paca-AI/api/internal/repository/postgres"
	redisRepo "github.com/Paca-AI/api/internal/repository/redis"
	apikeysvc "github.com/Paca-AI/api/internal/service/apikey"
	attachmentsvc "github.com/Paca-AI/api/internal/service/attachment"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
	globalrolesvc "github.com/Paca-AI/api/internal/service/globalrole"
	projectsvc "github.com/Paca-AI/api/internal/service/project"
	sprintsvc "github.com/Paca-AI/api/internal/service/sprint"
	tasksvc "github.com/Paca-AI/api/internal/service/task"
	usersvc "github.com/Paca-AI/api/internal/service/user"
	workflowsvc "github.com/Paca-AI/api/internal/service/workflow"
	"github.com/Paca-AI/api/internal/transport/http/handler"
	"github.com/Paca-AI/api/internal/transport/http/router"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	e2eJWTSecret         = "e2e-test-secret-value-that-is-at-least-32-chars"
	e2eAccessTTL         = 15 * time.Minute
	e2eRefreshTTL        = 48 * time.Hour
	e2eRefreshSessionTTL = 24 * time.Hour
)

// sharedPGDSN, sharedRedisURL, and sharedMinIOEndpoint are populated once by
// TestMain so that every newE2EEnv call reuses the same containers.
var (
	sharedPGDSN         string
	sharedRedisURL      string
	sharedMinIOEndpoint string // host:port reachable from the test process

	// testDBSeq is incremented for each newE2EEnv call to generate a unique
	// per-test Postgres database name. This ensures seed data (e.g. users)
	// created in one test cannot conflict with another.
	testDBSeq atomic.Int64
)

type e2eEnv struct {
	ctx            context.Context
	base           string
	client         *http.Client
	userService    *usersvc.Service
	userRepo       *pgRepo.UserRepository
	roleRepo       *pgRepo.GlobalRoleRepository
	projectRepo    *pgRepo.ProjectRepository
	projectSvc     *projectsvc.Service
	taskRepo       *pgRepo.TaskRepository
	taskSvc        *tasksvc.Service
	sprintRepo     *pgRepo.SprintRepository
	sprintSvc      *sprintsvc.Service
	viewRepo       *pgRepo.ViewRepository
	viewSvc        *sprintsvc.ViewService
	attachmentRepo *pgRepo.AttachmentRepository
	attachmentSvc  *attachmentsvc.Service
	apiKeyRepo     *pgRepo.APIKeyRepository
	apiKeySvc      *apikeysvc.Service
	workflowRepo   *pgRepo.WorkflowRepository
	workflowSvc    *workflowsvc.Service
	activitySvc    *tasksvc.ActivitySvc
	redisClient    *redis.Client // shared Valkey client, for per-test worker/consumer wiring
	db             *sqlx.DB      // raw connection for per-test service wiring
}

func newE2EEnv(t *testing.T) *e2eEnv {
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	ctx := t.Context()

	redisURL := sharedRedisURL

	// Provision a per-test Postgres database so that seed data from one test
	// (e.g. a user with a hardcoded username) cannot conflict with another.
	seq := testDBSeq.Add(1)
	testDBName := fmt.Sprintf("e2e_%04d", seq)
	adminLog := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	adminDB, adminErr := database.Open(database.Config{DSN: sharedPGDSN}, adminLog)
	if adminErr != nil {
		t.Fatalf("open admin db for test isolation: %v", adminErr)
	}
	if _, createErr := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %q`, testDBName)); createErr != nil {
		_ = adminDB.Close()
		t.Fatalf("create per-test database %q: %v", testDBName, createErr)
	}
	t.Cleanup(func() {
		bgCtx := context.Background()
		_, _ = adminDB.ExecContext(bgCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			testDBName,
		)
		_, _ = adminDB.ExecContext(bgCtx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q`, testDBName))
		_ = adminDB.Close()
	})
	pgDSN := strings.Replace(sharedPGDSN, "/testdb?", "/"+testDBName+"?", 1)

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	dbCfg := database.Config{DSN: pgDSN}
	db, err := database.Open(dbCfg, log)
	if err != nil {
		// Postgres in fresh containers can briefly accept TCP before it is fully ready.
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				t.Fatalf("open database: context canceled while waiting for postgres: %v", ctx.Err())
			default:
			}

			time.Sleep(300 * time.Millisecond)
			db, err = database.Open(dbCfg, log)
			if err == nil {
				break
			}
		}
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
	}
	sqlDB := db.DB

	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if err := database.RunMigrations(sqlDB, migrationsDir); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	redisClient, err := cache.NewClient(redisURL, log)
	if err != nil {
		t.Fatalf("open redis client: %v", err)
	}

	// Cleanup must close sqlDB BEFORE dropping the database to avoid blocking
	// the DROP DATABASE command with active connections
	// The cleanup order is important: LIFO (Last-In, First-Out)
	// 1. redisClient.Close() - closes Redis connection
	// 2. sqlDB.Close() - closes test DB connection (must happen before DROP)
	// 3. DROP DATABASE - drops the test DB (registered earlier)
	t.Cleanup(func() { _ = redisClient.Close() })
	t.Cleanup(func() {
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})

	tm := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL)
	userRepo := pgRepo.NewUserRepository(db)
	roleRepo := pgRepo.NewGlobalRoleRepository(db)
	authzStore := pgRepo.NewAuthzPermissionStore(db)
	refreshStore := redisRepo.NewRefreshTokenStore(redisClient)
	authService := authsvc.New(userRepo, tm, refreshStore, e2eRefreshTTL, e2eRefreshSessionTTL)
	userService := usersvc.New(userRepo, authzStore, roleRepo)
	globalRoleService := globalrolesvc.New(roleRepo)
	projectRepo := pgRepo.NewProjectRepository(db)
	taskRepo := pgRepo.NewTaskRepository(db)
	projectService := projectsvc.New(projectRepo, taskRepo)
	taskService := tasksvc.New(taskRepo)
	sprintRepo := pgRepo.NewSprintRepository(db)
	sprintService := sprintsvc.New(sprintRepo, taskRepo)
	viewRepo := pgRepo.NewViewRepository(db)
	viewService := sprintsvc.NewViewService(viewRepo)
	attachmentRepo := pgRepo.NewAttachmentRepository(db)
	apiKeyRepo := pgRepo.NewAPIKeyRepository(db)
	apiKeyService := apikeysvc.New(apiKeyRepo)
	// A real (non-nil) publisher is required so that task.updated activity
	// events actually reach StreamTaskActivities — otherwise a per-test
	// worker.WorkflowConsumer would never observe task status changes made
	// through the HTTP API. This is a cheap, fire-and-forget XAdd for every
	// other e2e test too (no consumer group drains it unless a test starts
	// one), so it doesn't affect their behavior or teardown time.
	publisher := messaging.NewPublisher(redisClient, log)
	activityRepo := pgRepo.NewTaskActivityRepository(db)
	activityService := tasksvc.NewActivityService(activityRepo, projectRepo, publisher)
	workflowRepo := pgRepo.NewWorkflowRepository(db)
	workflowService := workflowsvc.New(workflowRepo, taskRepo, projectRepo, publisher)
	var attachmentService *attachmentsvc.Service
	if sharedMinIOEndpoint != "" {
		minIOEndpoint := sharedMinIOEndpoint
		storageClient, storageErr := storage.NewS3Client(ctx, storage.S3Config{
			Endpoint:        "http://" + minIOEndpoint,
			Region:          "us-east-1",
			AccessKeyID:     "minioadmin",
			SecretAccessKey: "minioadmin",
			ForcePathStyle:  true,
		})
		if storageErr != nil {
			t.Fatalf("init storage client: %v", storageErr)
		}
		const attachBucket = "paca-attachments-e2e"
		if err := storageClient.EnsureBucket(ctx, attachBucket); err != nil {
			t.Fatalf("ensure bucket: %v", err)
		}
		attachmentService = attachmentsvc.New(attachmentRepo, attachmentsvc.NewTaskOwnerChecker(taskRepo), storageClient, attachBucket)
	}

	cookieCfg := handler.CookieConfig{
		Secure:            false,
		AccessTTL:         e2eAccessTTL,
		RefreshTTL:        e2eRefreshTTL,
		RefreshSessionTTL: e2eRefreshSessionTTL,
	}
	engine := router.New(router.Deps{
		TokenManager:         tm,
		APIKeyAuth:           apiKeyService,
		Authorizer:           authz.NewAuthorizer(authzStore),
		ProjectVisibilitySvc: projectService,
		Health:               handler.NewHealthHandler(),
		Auth:                 handler.NewAuthHandler(authService, cookieCfg),
		User:                 handler.NewUserHandler(userService),
		GlobalRole:           handler.NewGlobalRoleHandler(globalRoleService, authz.NewAuthorizer(authzStore)),
		Project:              handler.NewProjectHandler(projectService, authz.NewAuthorizer(authzStore)),
		Task:                 handler.NewTaskHandler(taskService, viewService, activityService),
		Sprint:               handler.NewSprintHandler(sprintService, viewService),
		View:                 handler.NewViewHandler(viewService),
		Attachment:           handler.NewAttachmentHandler(attachmentService),
		APIKey:               handler.NewAPIKeyHandler(apiKeyService),
		Workflow:             handler.NewWorkflowHandler(workflowService),
		Log:                  log,
	})

	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}

	return &e2eEnv{
		ctx:  ctx,
		base: srv.URL,
		client: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
		userService:    userService,
		userRepo:       userRepo,
		roleRepo:       roleRepo,
		projectRepo:    projectRepo,
		projectSvc:     projectService,
		taskRepo:       taskRepo,
		taskSvc:        taskService,
		sprintRepo:     sprintRepo,
		sprintSvc:      sprintService,
		viewRepo:       viewRepo,
		viewSvc:        viewService,
		attachmentRepo: attachmentRepo,
		attachmentSvc:  attachmentService,
		apiKeyRepo:     apiKeyRepo,
		apiKeySvc:      apiKeyService,
		workflowRepo:   workflowRepo,
		workflowSvc:    workflowService,
		activitySvc:    activityService,
		redisClient:    redisClient,
		db:             db,
	}
}

// TestMain starts a single Postgres and Valkey container pair for the whole
// E2E suite rather than once per test function. This keeps total container
// startup time proportional to O(1) instead of O(N tests).
func TestMain(m *testing.M) {
	if os.Getenv("PACA_E2E") != "1" {
		// Guard not set – individual tests will self-skip; just run them.
		os.Exit(m.Run())
	}

	if !setupDockerEnvForMain() {
		// Docker unavailable – individual tests will skip via checkDockerAvailable.
		os.Exit(m.Run())
	}

	bgCtx := context.Background()

	pgC, err := testcontainers.GenericContainer(bgCtx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "postgres:16-alpine",
			Env: map[string]string{
				"POSTGRES_USER":     "test",
				"POSTGRES_PASSWORD": "test",
				"POSTGRES_DB":       "testdb",
			},
			ExposedPorts: []string{"5432/tcp"},
			WaitingFor:   wait.ForLog("database system is ready to accept connections").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: start postgres container: %v\n", err)
		os.Exit(1)
	}

	redisC, err := testcontainers.GenericContainer(bgCtx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "valkey/valkey:8-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		_ = pgC.Terminate(bgCtx)
		fmt.Fprintf(os.Stderr, "FATAL: start valkey container: %v\n", err)
		os.Exit(1)
	}

	pgHost, _ := pgC.Host(bgCtx)
	pgPort, _ := pgC.MappedPort(bgCtx, "5432/tcp")
	if pgHost == "localhost" {
		pgHost = "127.0.0.1"
	}
	sharedPGDSN = fmt.Sprintf("postgresql://test:test@%s:%s/testdb?sslmode=disable", pgHost, pgPort.Port())

	redisHost, _ := redisC.Host(bgCtx)
	redisPort, _ := redisC.MappedPort(bgCtx, "6379/tcp")
	if redisHost == "localhost" {
		redisHost = "127.0.0.1"
	}
	sharedRedisURL = fmt.Sprintf("redis://%s:%s/0", redisHost, redisPort.Port())

	minioC, err := testcontainers.GenericContainer(bgCtx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image: "minio/minio:latest",
			Env: map[string]string{
				"MINIO_ROOT_USER":     "minioadmin",
				"MINIO_ROOT_PASSWORD": "minioadmin",
			},
			Cmd:          []string{"server", "/data"},
			ExposedPorts: []string{"9000/tcp"},
			WaitingFor:   wait.ForHTTP("/minio/health/live").WithPort("9000/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		_ = pgC.Terminate(bgCtx)
		_ = redisC.Terminate(bgCtx)
		fmt.Fprintf(os.Stderr, "WARN: start minio container: %v – attachment tests will be skipped\n", err)
	} else {
		minioHost, _ := minioC.Host(bgCtx)
		minioPort, _ := minioC.MappedPort(bgCtx, "9000/tcp")
		if minioHost == "localhost" {
			minioHost = "127.0.0.1"
		}
		sharedMinIOEndpoint = fmt.Sprintf("%s:%s", minioHost, minioPort.Port())
	}

	code := m.Run()

	_ = pgC.Terminate(bgCtx)
	_ = redisC.Terminate(bgCtx)
	if minioC != nil {
		_ = minioC.Terminate(bgCtx)
	}

	os.Exit(code)
}

// setupDockerEnvForMain mirrors checkDockerAvailable but operates outside a
// *testing.T context, using os.Setenv instead of t.Setenv.
// Returns false when no Docker socket can be found.
func setupDockerEnvForMain() bool {
	if dh := os.Getenv("DOCKER_HOST"); dh != "" {
		if !strings.Contains(dh, "://") || strings.HasPrefix(dh, "unix://") {
			socket := strings.TrimPrefix(dh, "unix://")
			if _, err := os.Stat(socket); err == nil {
				_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
				return true
			}
			return false
		}
		return true
	}

	if socket := socketFromDockerContext(); socket != "" {
		if _, err := os.Stat(socket); err == nil {
			_ = os.Setenv("DOCKER_HOST", "unix://"+socket)
			_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
			return true
		}
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		"/var/run/docker.sock",
		filepath.Join(home, ".docker/run/docker.sock"),
		filepath.Join(home, ".docker/desktop/docker.sock"),
		filepath.Join(home, ".colima/default/docker.sock"),
		filepath.Join(home, ".colima/docker.sock"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			_ = os.Setenv("DOCKER_HOST", "unix://"+p)
			_ = os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
			return true
		}
	}
	return false
}

func checkDockerAvailable(t *testing.T) {
	t.Helper()

	if dh := os.Getenv("DOCKER_HOST"); dh != "" {
		if !strings.Contains(dh, "://") || strings.HasPrefix(dh, "unix://") {
			socket := strings.TrimPrefix(dh, "unix://")
			if _, err := os.Stat(socket); err == nil {
				t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
				return
			}
			t.Skipf("DOCKER_HOST=%s set but socket not found; is Docker running?", dh)
		}
		return
	}

	if socket := socketFromDockerContext(); socket != "" {
		if _, err := os.Stat(socket); err == nil {
			t.Setenv("DOCKER_HOST", "unix://"+socket)
			t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
			return
		}
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		"/var/run/docker.sock",
		filepath.Join(home, ".docker/run/docker.sock"),
		filepath.Join(home, ".docker/desktop/docker.sock"),
		filepath.Join(home, ".colima/default/docker.sock"),
		filepath.Join(home, ".colima/docker.sock"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			t.Setenv("DOCKER_HOST", "unix://"+p)
			t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
			return
		}
	}

	t.Skip("Docker socket not found; install Docker Desktop or Colima and retry with PACA_E2E=1")
}

func socketFromDockerContext() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	type dockerConfig struct {
		CurrentContext string `json:"currentContext"`
	}
	cfgData, err := os.ReadFile(filepath.Join(home, ".docker", "config.json"))
	if err != nil {
		return ""
	}
	var cfg dockerConfig
	if err := json.Unmarshal(cfgData, &cfg); err != nil || cfg.CurrentContext == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(cfg.CurrentContext))
	metaPath := filepath.Join(home, ".docker", "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")

	type contextEndpoint struct {
		Host string `json:"Host"`
	}
	type contextMeta struct {
		Endpoints map[string]contextEndpoint `json:"Endpoints"`
	}
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return ""
	}
	var meta contextMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return ""
	}

	host := meta.Endpoints["docker"].Host
	return strings.TrimPrefix(host, "unix://")
}
