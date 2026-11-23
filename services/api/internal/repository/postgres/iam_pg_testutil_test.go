package postgres

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"

	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/migrations"
)

// newIAMPGTestDB creates a fresh database on PACA_TEST_PG_DSN's server with
// every embedded migration applied, and drops it on cleanup. Skipped when the
// env var is unset.
func newIAMPGTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("PACA_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("PACA_TEST_PG_DSN not set; skipping Postgres-backed IAM test")
	}
	admin, err := sqlx.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })

	name := "iam_pg_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("PACA_TEST_PG_DSN must be a URL: %v", err)
	}
	u.Path = "/" + name
	db, err := sqlx.Open("pgx", u.String())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		// Not t.Context(): that is already cancelled by the time cleanups run, which
		// made this drop silently fail and leak a database per test.
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	if err := database.RunMigrationsFS(db.DB, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

func pgExec(t *testing.T, db *sqlx.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// iamFx offers small row builders over the real schema.
type iamFx struct {
	t  *testing.T
	db *sqlx.DB
}

func newIAMFx(t *testing.T, db *sqlx.DB) *iamFx {
	t.Helper()
	return &iamFx{t: t, db: db}
}

func (f *iamFx) project() uuid.UUID {
	id := uuid.New()
	pgExec(f.t, f.db, `INSERT INTO projects (id, name) VALUES ($1, $2)`, id, "P-"+id.String()[:8])
	return id
}

// account inserts the accounts row every user and agent hangs off.
func (f *iamFx) account(id uuid.UUID, kind, name string, deleted bool) {
	var del any
	if deleted {
		del = "2026-01-01T00:00:00Z"
	}
	pgExec(f.t, f.db, `INSERT INTO accounts (id, kind, name, deleted_at) VALUES ($1, $2, $3, $4)`, id, kind, name, del)
}

func (f *iamFx) user(deleted bool) uuid.UUID {
	id := uuid.New()
	f.account(id, "user", "u_"+id.String()[:8], deleted)
	pgExec(f.t, f.db, `INSERT INTO users (id, username, password_hash) VALUES ($1, $2, 'x')`, id, "u_"+id.String()[:8])
	return id
}

func (f *iamFx) agent(project *uuid.UUID, deleted bool) uuid.UUID {
	id := uuid.New()
	f.account(id, "agent", "A", deleted)
	pgExec(f.t, f.db, `INSERT INTO agents (id, project_id, handle, llm_provider, llm_model, llm_api_key_secret)
		VALUES ($1, $2, $3, 'openai', 'gpt', 'k')`, id, project, "h-"+id.String()[:8])
	return id
}

// serviceAccount inserts a service account (an accounts row only).
func (f *iamFx) serviceAccount(deleted bool) uuid.UUID {
	id := uuid.New()
	f.account(id, "service_account", "SA", deleted)
	return id
}

// member adds a project_members row for any account (user, agent or service account).
func (f *iamFx) member(project, account uuid.UUID, deleted bool) uuid.UUID {
	id := uuid.New()
	var del any
	if deleted {
		del = "2026-01-01T00:00:00Z"
	}
	pgExec(f.t, f.db, `INSERT INTO project_members (id, project_id, account_id, deleted_at) VALUES ($1, $2, $3, $4)`, id, project, account, del)
	return id
}

func (f *iamFx) role(policy string) uuid.UUID {
	id := uuid.New()
	pgExec(f.t, f.db, `INSERT INTO roles (id, name, policy) VALUES ($1, $2, $3::jsonb)`, id, "fx-"+id.String()[:8], policy)
	return id
}

func (f *iamFx) attach(role, account uuid.UUID, project *uuid.UUID) {
	pgExec(f.t, f.db, `INSERT INTO role_attachments (role_id, account_id, project_id) VALUES ($1, $2, $3)`,
		role, account, project)
}
