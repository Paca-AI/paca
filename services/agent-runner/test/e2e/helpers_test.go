package e2e_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/moby/moby/client"
	"github.com/redis/go-redis/v9"

	"github.com/Paca-AI/agent-runner/internal/sandbox/docker"
	"github.com/Paca-AI/agent-runner/internal/secret"
)

// newE2ERedisClient is newE2EEnv's lighter counterpart for tests that only
// need the shared Valkey container, not Postgres — skips the per-test
// database creation and migrations entirely.
func newE2ERedisClient(t *testing.T) *redis.Client {
	t.Helper()
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)
	client := redis.NewClient(&redis.Options{Addr: sharedRedisAddr})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// runMigrations executes every *.sql file in migrationsDir, in lexicographic
// order, each inside its own transaction — a duplicate of services/api's own
// internal/platform/database.RunMigrations. Not imported: that package is
// under services/api/internal, unreachable from this sibling module (see
// internal/repository/postgres/db.go's doc comment on the same boundary).
func runMigrations(db *sql.DB, migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", migrationsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		path := filepath.Join(migrationsDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", path, err)
		}
		if err := execMigrationTx(db, string(data)); err != nil {
			return fmt.Errorf("exec migration %q: %w", path, err)
		}
	}
	return nil
}

func execMigrationTx(db *sql.DB, sqlText string) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// seedBaseProject inserts one throwaway user/project/project_role/
// project_member chain — the minimum a fresh testcontainers Postgres needs
// before any ported test's own agent/agent_conversations seeding (copied
// near-verbatim from each source livecheck program) can run, since a real
// dev DB always already had at least one project (see e.g.
// cmd/agent-runner/livecheck's seedThrowawayRows: "is there at least one
// project in this DB?"), but a freshly-migrated container never does.
func seedBaseProject(t *testing.T, db *sqlx.DB) (projectID, memberID, userID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	userID = uuid.New()
	suffix := userID.String()[:8]
	if _, err := db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role_id)
		SELECT $1, $2, '', id FROM global_roles WHERE name = 'USER'
	`, userID, "e2e-user-"+suffix); err != nil {
		t.Fatalf("seed base user: %v", err)
	}

	projectID = uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO projects (id, name, task_id_prefix, created_by) VALUES ($1, $2, 'E2E', $3)
	`, projectID, "E2E Project "+suffix, userID); err != nil {
		t.Fatalf("seed base project: %v", err)
	}

	projectRoleID := uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_roles (id, project_id, role_name, permissions) VALUES ($1, $2, 'MEMBER', '{}'::jsonb)
	`, projectRoleID, projectID); err != nil {
		t.Fatalf("seed base project role: %v", err)
	}

	memberID = uuid.New()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO project_members (id, project_id, user_id, project_role_id) VALUES ($1, $2, $3, $4)
	`, memberID, projectID, userID, projectRoleID); err != nil {
		t.Fatalf("seed base project member: %v", err)
	}

	return projectID, memberID, userID
}

// portAllocSeq hands each docker.Manager a non-overlapping 100-wide host
// port range: docker.Manager.acquirePort does no OS-level collision
// detection, and every Docker-heavy test file in this package shares one go
// test process (see sandbox_manager_test.go and friends).
var portAllocSeq atomic.Int64

func newSandboxManager(t *testing.T) *docker.Manager {
	t.Helper()
	n := portAllocSeq.Add(1) - 1
	start := 20000 + int(n)*100
	mgr, err := docker.NewManager(start, 100)
	if err != nil {
		t.Fatalf("new sandbox manager: %v", err)
	}
	return mgr
}

// newEncryptor builds a secret.Encryptor from a fresh random key — every
// ported test gets its own key rather than reusing one of the original
// livecheck programs' hardcoded throwaway hex strings, since nothing here
// needs a stable/reproducible key across runs.
func newEncryptor(t *testing.T) *secret.Encryptor {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generate encryption key: %v", err)
	}
	enc, err := secret.NewEncryptor(raw)
	if err != nil {
		t.Fatalf("new encryptor: %v", err)
	}
	return enc
}

// dockerBridgeGatewayIP returns the host address sandbox containers use to
// reach the fake LLM / MCP-permissions servers in this test process. Docker
// Desktop's bridge gateway lives inside its Linux VM, so it cannot route back
// to a listener on the macOS or Windows host; Docker Desktop's documented
// host.docker.internal name is required there. Native Linux uses the default
// bridge gateway resolved at runtime rather than assuming 172.17.0.1.
func dockerBridgeGatewayIP(ctx context.Context, t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return "host.docker.internal"
	}
	dockerClient, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("create docker client: %v", err)
	}
	defer func() { _ = dockerClient.Close() }()

	info, err := dockerClient.NetworkInspect(ctx, "bridge", client.NetworkInspectOptions{})
	if err != nil {
		t.Fatalf("inspect docker bridge network: %v", err)
	}
	for _, cfg := range info.Network.IPAM.Config {
		if cfg.Gateway.IsValid() {
			return cfg.Gateway.String()
		}
	}
	t.Fatal("docker bridge network has no IPAM gateway configured")
	return ""
}

var dockerfileFromLineRe = regexp.MustCompile(`(?m)^FROM\s+(\S+)`)

// rawUpstreamGooseImage reads the pinned upstream goose digest straight out
// of services/agent-server/Dockerfile's own FROM line, rather than a second
// hardcoded copy of that digest — so rotating the pin in one place (per that
// Dockerfile's own doc comment on when it must be re-verified) automatically
// flows into every test that only needs the Node-less base image (no MCP),
// with no second edit to forget.
func rawUpstreamGooseImage(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	dockerfilePath := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "agent-server", "Dockerfile")
	data, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("read %s: %v", dockerfilePath, err)
	}
	m := dockerfileFromLineRe.FindSubmatch(data)
	if m == nil {
		t.Fatalf("no FROM line found in %s", dockerfilePath)
	}
	return string(m[1])
}

// agentServerImage returns the Node+MCP-enabled sandbox image the CI
// test-e2e job builds fresh as AGENT_SERVER_IMAGE (see
// .github/workflows/agent-runner-pr-ci.yml) — skips the test rather than
// failing when unset, so a contributor running `go test ./test/e2e/...`
// locally without first building that image gets a clear reason, not a
// confusing container-start failure.
func agentServerImage(t *testing.T) string {
	t.Helper()
	image := os.Getenv("AGENT_SERVER_IMAGE")
	if image == "" {
		t.Skip("set AGENT_SERVER_IMAGE to a built paca-agent-server-goose image to run this test " +
			"(docker build -f services/agent-server/Dockerfile -t paca-agent-server-goose:e2e services/agent-server)")
	}
	return image
}

func conversationStatus(ctx context.Context, db *sqlx.DB, convID uuid.UUID) (string, error) {
	var status string
	if err := db.GetContext(ctx, &status, `SELECT status FROM agent_conversations WHERE id = $1`, convID); err != nil {
		return "", fmt.Errorf("query conversation status: %w", err)
	}
	return status, nil
}

func waitForStatus(ctx context.Context, db *sqlx.DB, convID uuid.UUID, want string) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got, err := conversationStatus(ctx, db, convID)
		if err == nil && got == want {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	got, _ := conversationStatus(ctx, db, convID)
	return fmt.Errorf("status never reached %q, last seen %q", want, got)
}

func waitForEventCount(ctx context.Context, db *sqlx.DB, convID uuid.UUID, want int) error {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := db.GetContext(ctx, &count,
			`SELECT COUNT(*) FROM agent_conversation_events WHERE conversation_id = $1`, convID); err == nil && count >= want {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("event count for conversation %s never reached %d", convID, want)
}
