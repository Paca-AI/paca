package e2e_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Paca-AI/api/internal/platform/database"
)

// ---------------------------------------------------------------------------
// database.RunMigrations — real Postgres coverage
// ---------------------------------------------------------------------------
//
// RunMigrations/RunMigrationsFS use Postgres-specific features (advisory
// locks, and every real migration file's own dialect), so unlike most of
// this package's other helpers this one can't be meaningfully exercised
// against an in-memory fake — these tests run against a fresh per-test
// database on the shared e2e Postgres container instead (see
// newMigrationsTestDB). In particular, TestRunMigrations_SecondRunSkipsAlreadyApplied
// and TestRunMigrations_NewFileAddedLaterOnlyRunsNewFile use deliberately
// non-idempotent SQL (a bare INSERT with no ON CONFLICT, an ADD COLUMN with
// no IF NOT EXISTS) — exactly the kind of statement the old always-rerun
// convention required every migration to avoid — to prove a file only ever
// executes once.

// newMigrationsTestDB provisions a fresh, empty per-test Postgres database
// on the shared e2e container — the same per-test isolation newE2EEnv uses,
// without building the rest of the service stack, since these tests
// exercise database.RunMigrations directly.
func newMigrationsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv("PACA_E2E") != "1" {
		t.Skip("set PACA_E2E=1 to run e2e tests (requires Docker)")
	}
	checkDockerAvailable(t)

	ctx := context.Background()
	seq := testDBSeq.Add(1)
	testDBName := fmt.Sprintf("migtest_%04d", seq)

	quietLog := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	adminDB, err := database.Open(database.Config{DSN: sharedPGDSN}, quietLog)
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

	testDSN := strings.Replace(sharedPGDSN, "/testdb?", "/"+testDBName+"?", 1)
	db, err := database.Open(database.Config{DSN: testDSN}, quietLog)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db.DB
}

// writeMigrationFile writes one migration file into dir, named so it sorts
// after any other file already written with a smaller n.
func writeMigrationFile(t *testing.T, dir string, n int, name, sqlBody string) {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("%04d_%s.sql", n, name))
	if err := os.WriteFile(path, []byte(sqlBody), 0o644); err != nil {
		t.Fatalf("write migration file %q: %v", path, err)
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n); err != nil {
		t.Fatalf("count rows in %q: %v", table, err)
	}
	return n
}

func TestRunMigrations_AppliesAllFilesInOrder(t *testing.T) {
	db := newMigrationsTestDB(t)
	dir := t.TempDir()
	writeMigrationFile(t, dir, 1, "create", "CREATE TABLE widgets(id serial PRIMARY KEY, name text);")
	writeMigrationFile(t, dir, 2, "seed", "INSERT INTO widgets(name) VALUES ('alice');")

	if err := database.RunMigrations(db, dir); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	if got := countRows(t, db, "widgets"); got != 1 {
		t.Fatalf("expected 1 seeded widget, got %d", got)
	}
}

func TestRunMigrations_MissingDir_Rejected(t *testing.T) {
	db := newMigrationsTestDB(t)

	err := database.RunMigrations(db, filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for missing migration dir")
	}
	if !strings.Contains(err.Error(), "migrations: read dir") {
		t.Fatalf("expected read dir error, got %v", err)
	}
}

// TestRunMigrations_SecondRunSkipsAlreadyApplied is the core regression test
// for the ledger: migration 0001 is a bare INSERT with no ON CONFLICT guard,
// so replaying it (the old, always-rerun behavior) would either double the
// row count or violate a unique constraint. Calling RunMigrations twice
// against the same directory and database must leave exactly one row.
func TestRunMigrations_SecondRunSkipsAlreadyApplied(t *testing.T) {
	db := newMigrationsTestDB(t)
	dir := t.TempDir()
	writeMigrationFile(t, dir, 1, "create",
		"CREATE TABLE singleton(id int PRIMARY KEY);")
	writeMigrationFile(t, dir, 2, "seed",
		"INSERT INTO singleton(id) VALUES (1);") // no ON CONFLICT — errors if replayed

	if err := database.RunMigrations(db, dir); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}
	if err := database.RunMigrations(db, dir); err != nil {
		t.Fatalf("second RunMigrations (should be a no-op, not a replay): %v", err)
	}

	if got := countRows(t, db, "singleton"); got != 1 {
		t.Fatalf("expected exactly 1 row (migration 0002 must not have re-run), got %d", got)
	}
}

// TestRunMigrations_NewFileAddedLaterOnlyRunsNewFile proves migrations are
// tracked per-file, not "has this directory been fully processed before":
// after an initial run, adding a new file to the same directory and running
// again must apply only the new file, leaving the earlier one untouched
// (its own non-idempotent INSERT must not fire a second time).
func TestRunMigrations_NewFileAddedLaterOnlyRunsNewFile(t *testing.T) {
	db := newMigrationsTestDB(t)
	dir := t.TempDir()
	writeMigrationFile(t, dir, 1, "create",
		"CREATE TABLE events(id serial PRIMARY KEY, label text);")
	writeMigrationFile(t, dir, 2, "seed_one",
		"INSERT INTO events(label) VALUES ('first');")

	if err := database.RunMigrations(db, dir); err != nil {
		t.Fatalf("first RunMigrations: %v", err)
	}
	if got := countRows(t, db, "events"); got != 1 {
		t.Fatalf("expected 1 row after first run, got %d", got)
	}

	writeMigrationFile(t, dir, 3, "seed_two",
		"INSERT INTO events(label) VALUES ('second');")

	if err := database.RunMigrations(db, dir); err != nil {
		t.Fatalf("second RunMigrations: %v", err)
	}

	if got := countRows(t, db, "events"); got != 2 {
		t.Fatalf("expected 2 rows total (only the new file's insert should have run), got %d", got)
	}
}

// TestRunMigrations_FailedFileNotRecorded ensures a file that errors is not
// marked applied, so it's retried (not silently skipped) on the next run.
func TestRunMigrations_FailedFileNotRecorded(t *testing.T) {
	db := newMigrationsTestDB(t)
	dir := t.TempDir()
	writeMigrationFile(t, dir, 1, "bad", "this is not valid sql;")

	if err := database.RunMigrations(db, dir); err == nil {
		t.Fatal("expected error from invalid SQL")
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE filename = '0001_bad.sql'").Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed migration must not be recorded as applied, found %d row(s)", count)
	}
}
