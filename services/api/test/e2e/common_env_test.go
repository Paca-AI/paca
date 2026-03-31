// Package e2e_test contains end-to-end smoke tests for the Paca API service.
// Tests spin up real Postgres and Redis containers via testcontainers-go,
// apply migrations, wire the full service stack, and exercise the complete
// HTTP request flow against an in-process httptest.Server.
//
// Run with: PACA_E2E=1 go test ./test/e2e/... -v -timeout 120s
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
	"testing"
	"time"

	"github.com/paca/api/internal/platform/authz"
	"github.com/paca/api/internal/platform/cache"
	"github.com/paca/api/internal/platform/database"
	jwttoken "github.com/paca/api/internal/platform/token"
	pgRepo "github.com/paca/api/internal/repository/postgres"
	redisRepo "github.com/paca/api/internal/repository/redis"
	authsvc "github.com/paca/api/internal/service/auth"
	globalrolesvc "github.com/paca/api/internal/service/globalrole"
	usersvc "github.com/paca/api/internal/service/user"
	"github.com/paca/api/internal/transport/http/handler"
	"github.com/paca/api/internal/transport/http/router"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	e2eJWTSecret         = "e2e-test-secret-value-that-is-at-least-32-chars"
	e2eAccessTTL         = 15 * time.Minute
	e2eRefreshTTL        = 48 * time.Hour
	e2eRefreshSessionTTL = 24 * time.Hour
)

type e2eEnv struct {
	ctx         context.Context
	base        string
	client      *http.Client
	userService *usersvc.Service
	userRepo    *pgRepo.UserRepository
	roleRepo    *pgRepo.GlobalRoleRepository
}

func newE2EEnv(t *testing.T) *e2eEnv {
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	ctx := t.Context()

	pgC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
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
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	redisC, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "valkey/valkey:8-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() { _ = redisC.Terminate(context.Background()) })

	pgHost, err := pgC.Host(ctx)
	if err != nil {
		t.Fatalf("get postgres host: %v", err)
	}
	pgPort, err := pgC.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatalf("get postgres port: %v", err)
	}
	if pgHost == "localhost" {
		pgHost = "127.0.0.1"
	}
	pgDSN := fmt.Sprintf("postgresql://test:test@%s:%s/testdb?sslmode=disable", pgHost, pgPort.Port())

	redisHost, err := redisC.Host(ctx)
	if err != nil {
		t.Fatalf("get redis host: %v", err)
	}
	redisPort, err := redisC.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("get redis port: %v", err)
	}
	if redisHost == "localhost" {
		redisHost = "127.0.0.1"
	}
	redisURL := fmt.Sprintf("redis://%s:%s/0", redisHost, redisPort.Port())

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	db, err := database.Open(pgDSN, log)
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
			db, err = database.Open(pgDSN, log)
			if err == nil {
				break
			}
		}
		if err != nil {
			t.Fatalf("open database: %v", err)
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get underlying sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
	if err := database.RunMigrations(db, migrationsDir); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	redisClient, err := cache.NewClient(redisURL, log)
	if err != nil {
		t.Fatalf("open redis client: %v", err)
	}
	t.Cleanup(func() { _ = redisClient.Close() })

	tm := jwttoken.New(e2eJWTSecret, e2eAccessTTL, e2eRefreshTTL)
	userRepo := pgRepo.NewUserRepository(db)
	roleRepo := pgRepo.NewGlobalRoleRepository(db)
	authzStore := pgRepo.NewAuthzPermissionStore(db)
	refreshStore := redisRepo.NewRefreshTokenStore(redisClient)
	authService := authsvc.New(userRepo, tm, refreshStore, e2eRefreshTTL, e2eRefreshSessionTTL)
	userService := usersvc.New(userRepo, authzStore, roleRepo)
	globalRoleService := globalrolesvc.New(roleRepo)

	cookieCfg := handler.CookieConfig{
		Secure:            false,
		AccessTTL:         e2eAccessTTL,
		RefreshTTL:        e2eRefreshTTL,
		RefreshSessionTTL: e2eRefreshSessionTTL,
	}
	engine := router.New(router.Deps{
		TokenManager: tm,
		Authorizer:   authz.NewAuthorizer(authzStore),
		Health:       handler.NewHealthHandler(),
		Auth:         handler.NewAuthHandler(authService, cookieCfg),
		User:         handler.NewUserHandler(userService),
		GlobalRole:   handler.NewGlobalRoleHandler(globalRoleService),
		Log:          log,
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
		userService: userService,
		userRepo:    userRepo,
		roleRepo:    roleRepo,
	}
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
