// Package e2e_test contains end-to-end tests for services/agent-runner.
// Tests spin up real Postgres and Valkey containers via testcontainers-go
// (shared for the whole suite) plus, where needed, real Docker sandbox
// containers via this service's own docker.Manager — the same
// container-lifecycle code cmd/agent-runner's production binary uses, not a
// second, testcontainers-driven reimplementation of it (see
// helpers_test.go's rawUpstreamGooseImage doc comment).
//
// Run with: PACA_E2E=1 go test ./test/e2e/... -v -timeout 600s
//
// This suite replaces the former cmd/agent-runner/livecheck* and
// internal/*/livecheck manually-run programs — each ported test file's own
// doc comment names which one it replaces.
package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

// sharedPGDSN and sharedRedisAddr are populated once by TestMain so every
// newE2EEnv call reuses the same two containers. agent-runner has no import
// path to services/api/test/e2e/common_env_test.go (separate Go module),
// so this — and the Docker-detection helpers below — are a deliberate,
// near-verbatim duplication of that file's own TestMain/checkDockerAvailable
// pattern rather than a shared import.
var (
	sharedPGDSN     string
	sharedRedisAddr string

	// testDBSeq generates a unique per-test Postgres database name, so seed
	// data from one test (e.g. a throwaway project) can never collide with
	// another's.
	testDBSeq atomic.Int64
)

// e2eEnv is one test's isolated slice of the shared Postgres/Valkey
// containers: its own throwaway database (migrated fresh) plus one seeded
// project/user/member chain every ported test's own agent/conversation
// seeding builds on top of. A fresh testcontainers Postgres has no projects
// the way the real dev DB the original livecheck programs ran against
// always did — their own comments literally ask "is there at least one
// project in this DB?".
type e2eEnv struct {
	db          *sqlx.DB
	redisClient *redis.Client
	redisAddr   string
	projectID   uuid.UUID
	memberID    uuid.UUID
	userID      uuid.UUID
}

func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	ctx := context.Background()

	seq := testDBSeq.Add(1)
	testDBName := fmt.Sprintf("e2e_ar_%04d", seq)

	adminDB, err := postgres.Open(sharedPGDSN)
	if err != nil {
		t.Fatalf("open admin db for test isolation: %v", err)
	}
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %q`, testDBName)); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create per-test database %q: %v", testDBName, err)
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

	// Postgres in a freshly-started container can briefly accept TCP before
	// it's actually ready to serve — retry rather than fail on the first
	// connection attempt (postgres.Open itself has no retry of its own).
	var db *sqlx.DB
	deadline := time.Now().Add(15 * time.Second)
	for {
		db, err = postgres.Open(pgDSN)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("open per-test database %q: %v", testDBName, err)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "api", "migrations")
	if err := runMigrations(db.DB, migrationsDir); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	projectID, memberID, userID := seedBaseProject(t, db)

	redisClient := redis.NewClient(&redis.Options{Addr: sharedRedisAddr})
	t.Cleanup(func() { _ = redisClient.Close() })

	return &e2eEnv{
		db:          db,
		redisClient: redisClient,
		redisAddr:   sharedRedisAddr,
		projectID:   projectID,
		memberID:    memberID,
		userID:      userID,
	}
}

// TestMain starts a single Postgres and Valkey container pair for the whole
// suite rather than once per test — see services/api/test/e2e's identical
// TestMain for the pattern this mirrors.
func TestMain(m *testing.M) {
	if os.Getenv("PACA_E2E") != "1" {
		// Guard not set – individual tests self-skip via newE2EEnv; just run them.
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
	sharedRedisAddr = fmt.Sprintf("%s:%s", redisHost, redisPort.Port())

	code := m.Run()

	_ = pgC.Terminate(bgCtx)
	_ = redisC.Terminate(bgCtx)
	os.Exit(code)
}

// setupDockerEnvForMain mirrors checkDockerAvailable but operates outside a
// *testing.T context, using os.Setenv instead of t.Setenv. Returns false
// when no Docker socket can be found.
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

// checkDockerAvailable is a read-only reachability guard: it never mutates
// the environment. TestMain's setupDockerEnvForMain already exported
// DOCKER_HOST/TESTCONTAINERS_RYUK_DISABLED process-wide (via os.Setenv,
// before any test starts) if Docker was found at all — nothing left for an
// individual test to configure.
func checkDockerAvailable(t *testing.T) {
	t.Helper()

	if dh := os.Getenv("DOCKER_HOST"); dh != "" {
		if !strings.Contains(dh, "://") || strings.HasPrefix(dh, "unix://") {
			socket := strings.TrimPrefix(dh, "unix://")
			if _, err := os.Stat(socket); err == nil {
				return
			}
			t.Skipf("DOCKER_HOST=%s set but socket not found; is Docker running?", dh)
		}
		return
	}

	if socket := socketFromDockerContext(); socket != "" {
		if _, err := os.Stat(socket); err == nil {
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
