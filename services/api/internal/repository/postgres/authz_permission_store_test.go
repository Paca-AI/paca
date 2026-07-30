package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	"github.com/Paca-AI/api/internal/platform/authz"
)

func openAuthzStoreTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema := `
		CREATE TABLE global_roles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			permissions BLOB NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		);
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			full_name TEXT NOT NULL,
			role_id TEXT NOT NULL,
			must_change_password INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		);
		CREATE TABLE project_roles (
			id TEXT PRIMARY KEY,
			project_id TEXT,
			role_name TEXT NOT NULL,
			permissions BLOB NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		);
		CREATE TABLE project_members (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			project_role_id TEXT NOT NULL
		);`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func TestAuthzPermissionStore_ListGlobalPermissions(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)
	ctx := context.Background()

	roleID := uuid.New().String()
	now := time.Now()
	db.MustExec(
		`INSERT INTO global_roles (id, name, permissions, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		roleID, "ADMIN", []byte(`{"users.delete":true,"global_roles.write":true}`), now, now,
	)

	userID := uuid.New()
	db.MustExec(
		`INSERT INTO users (id, username, password_hash, full_name, role_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID.String(), "alice", "hash", "Alice", roleID, now, now,
	)

	perms, err := store.ListGlobalPermissions(ctx, userID)
	if err != nil {
		t.Fatalf("list global permissions: %v", err)
	}
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d (%v)", len(perms), perms)
	}
}

func TestAuthzPermissionStore_ListProjectPermissions(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)
	ctx := context.Background()

	userID := uuid.New()
	projectID := uuid.New()
	roleID := uuid.New()
	now := time.Now()

	// Seed a global role for the user's role_id FK
	globalRoleID := uuid.New().String()
	db.MustExec(
		`INSERT INTO global_roles (id, name, permissions, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		globalRoleID, "USER", []byte(`{}`), now, now,
	)
	db.MustExec(
		`INSERT INTO users (id, username, password_hash, full_name, role_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID.String(), "alice", "hash", "Alice", globalRoleID, now, now,
	)
	db.MustExec(
		`INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at) VALUES ($1, NULL, $2, $3, $4, $5)`,
		roleID.String(), "PROJECT_MANAGER", []byte(`{"tasks.write":true,"tasks.read":true}`), now, now,
	)
	db.MustExec(
		`INSERT INTO project_members (id, project_id, user_id, project_role_id) VALUES ($1, $2, $3, $4)`,
		uuid.New().String(), projectID.String(), userID.String(), roleID.String(),
	)

	perms, err := store.ListProjectPermissions(ctx, userID, projectID)
	if err != nil {
		t.Fatalf("list project permissions: %v", err)
	}

	foundRead := false
	foundWrite := false
	for _, p := range perms {
		if p == authz.PermissionTasksRead {
			foundRead = true
		}
		if p == authz.PermissionTasksWrite {
			foundWrite = true
		}
	}
	if !foundRead || !foundWrite {
		t.Fatalf("expected tasks.read and tasks.write, got %v", perms)
	}
}
