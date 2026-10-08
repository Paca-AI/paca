package postgres

import (
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
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
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
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
	})
	if err := database.RunMigrationsFS(db.DB, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

func pgExec(t *testing.T, db *sqlx.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
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

func (f *iamFx) user(deleted bool) uuid.UUID {
	id := uuid.New()
	var del any
	if deleted {
		del = "2026-01-01T00:00:00Z"
	}
	pgExec(f.t, f.db, `INSERT INTO users (id, username, password_hash, deleted_at) VALUES ($1, $2, 'x', $3)`,
		id, "u_"+id.String()[:8], del)
	return id
}

func (f *iamFx) agent(project *uuid.UUID, deleted bool) uuid.UUID {
	id := uuid.New()
	var del any
	if deleted {
		del = "2026-01-01T00:00:00Z"
	}
	scope := "project"
	if project == nil {
		scope = "global"
	}
	pgExec(f.t, f.db, `INSERT INTO agents (id, project_id, name, handle, llm_provider, llm_model, llm_api_key_secret, agent_scope, deleted_at)
		VALUES ($1, $2, 'A', $3, 'openai', 'gpt', 'k', $4, $5)`, id, project, "h-"+id.String()[:8], scope, del)
	return id
}

// member adds a project_members row for a user (agent=false) or an agent.
func (f *iamFx) member(project, principal uuid.UUID, agent, deleted bool) uuid.UUID {
	id := uuid.New()
	var del any
	if deleted {
		del = "2026-01-01T00:00:00Z"
	}
	if agent {
		pgExec(f.t, f.db, `INSERT INTO project_members (id, project_id, agent_id, member_type, deleted_at)
			VALUES ($1, $2, $3, 'agent', $4)`, id, project, principal, del)
	} else {
		pgExec(f.t, f.db, `INSERT INTO project_members (id, project_id, user_id, member_type, deleted_at)
			VALUES ($1, $2, $3, 'human', $4)`, id, project, principal, del)
	}
	return id
}

func (f *iamFx) role(policy string) uuid.UUID {
	id := uuid.New()
	pgExec(f.t, f.db, `INSERT INTO roles (id, name, policy) VALUES ($1, $2, $3::jsonb)`, id, "fx-"+id.String()[:8], policy)
	return id
}

func (f *iamFx) attach(role uuid.UUID, ptype string, principal uuid.UUID, project *uuid.UUID) {
	pgExec(f.t, f.db, `INSERT INTO role_attachments (role_id, principal_type, principal_id, project_id) VALUES ($1, $2, $3, $4)`,
		role, ptype, principal, project)
}
