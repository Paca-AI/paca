package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

func openGlobalRoleRepoTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema := `
		CREATE TABLE global_roles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
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
		);`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func testGlobalRole(id uuid.UUID, name string) *globalroledom.GlobalRole {
	now := time.Now().UTC().Truncate(time.Second)
	return &globalroledom.GlobalRole{
		ID:          id,
		Name:        name,
		Permissions: map[string]any{"manage_users": true},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

func TestGlobalRoleRepository_CreateAndList(t *testing.T) {
	db := openGlobalRoleRepoTestDB(t)
	repo := NewGlobalRoleRepository(db)
	ctx := context.Background()

	if err := repo.Create(ctx, testGlobalRole(uuid.New(), "SUPER_ADMIN")); err != nil {
		t.Fatalf("create role: %v", err)
	}
	if err := repo.Create(ctx, testGlobalRole(uuid.New(), "AUDITOR")); err != nil {
		t.Fatalf("create role: %v", err)
	}

	roles, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(roles))
	}
	if roles[0].Name != "AUDITOR" || roles[1].Name != "SUPER_ADMIN" {
		t.Fatalf("expected roles sorted by name, got %q then %q", roles[0].Name, roles[1].Name)
	}
}

func TestGlobalRoleRepository_ReplaceUserRoles(t *testing.T) {
	db := openGlobalRoleRepoTestDB(t)
	repo := NewGlobalRoleRepository(db)
	ctx := context.Background()

	now := time.Now()
	userID := uuid.New()
	roleA := testGlobalRole(uuid.New(), "SUPER_ADMIN")
	roleB := testGlobalRole(uuid.New(), "AUDITOR")
	if err := repo.Create(ctx, roleA); err != nil {
		t.Fatalf("create roleA: %v", err)
	}
	if err := repo.Create(ctx, roleB); err != nil {
		t.Fatalf("create roleB: %v", err)
	}

	// Seed user with roleA as initial role_id (SQLite ignores FK constraints).
	db.MustExec(
		`INSERT INTO users (id, username, password_hash, full_name, role_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID.String(), "alice", "hash", "Alice", roleA.ID.String(), now, now,
	)

	// With single-role schema, exactly one role ID is required.
	if err := repo.ReplaceUserRoles(ctx, userID, []uuid.UUID{roleB.ID}); err != nil {
		t.Fatalf("replace user roles: %v", err)
	}

	assigned, err := repo.ListUserRoles(ctx, userID)
	if err != nil {
		t.Fatalf("list user roles: %v", err)
	}
	if len(assigned) != 1 {
		t.Fatalf("expected 1 assigned role, got %d", len(assigned))
	}
	if assigned[0].ID != roleB.ID {
		t.Fatalf("expected roleB to be assigned, got %v", assigned[0].Name)
	}
}

func TestGlobalRoleRepository_ReplaceUserRoles_UserNotFound(t *testing.T) {
	db := openGlobalRoleRepoTestDB(t)
	repo := NewGlobalRoleRepository(db)
	ctx := context.Background()

	// Seed a real role so the validation passes role-check but fails user-check.
	role := testGlobalRole(uuid.New(), "SOME_ROLE")
	if err := repo.Create(ctx, role); err != nil {
		t.Fatalf("create role: %v", err)
	}

	err := repo.ReplaceUserRoles(context.Background(), uuid.New(), []uuid.UUID{role.ID})
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected user ErrNotFound, got %v", err)
	}
}
