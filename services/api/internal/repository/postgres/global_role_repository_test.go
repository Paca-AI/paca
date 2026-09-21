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
			is_default INTEGER NOT NULL DEFAULT 0,
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

// TestIsUniqueViolation_NilErrorDoesNotPanic guards a real regression: a
// caller that forgets the outer `if err != nil` guard (every existing call
// site has one, but AgentRepository.AddAgentAccessGrant briefly didn't)
// passes a nil err straight through on the success path. A nil `error`
// interface's Error() method panics (invalid memory address / nil pointer
// dereference) rather than returning "", so this must be checked explicitly
// rather than relying on err.Error() to behave like a typed nil would.
func TestGlobalRoleRepository_IsDefaultRoundTrips(t *testing.T) {
	repo := NewGlobalRoleRepository(openGlobalRoleRepoTestDB(t))
	ctx := context.Background()

	plain := testGlobalRole(uuid.New(), "EDITOR")
	standard := testGlobalRole(uuid.New(), "USER")
	standard.IsDefault = true
	for _, role := range []*globalroledom.GlobalRole{plain, standard} {
		if err := repo.Create(ctx, role); err != nil {
			t.Fatalf("create %s: %v", role.Name, err)
		}
	}

	byID, err := repo.FindByID(ctx, standard.ID)
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if !byID.IsDefault {
		t.Fatal("the default flag was not stored")
	}
	byName, err := repo.FindByName(ctx, "EDITOR")
	if err != nil {
		t.Fatalf("find by name: %v", err)
	}
	if byName.IsDefault {
		t.Fatal("an ordinary role came back as the default")
	}

	roles, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defaults := 0
	for _, role := range roles {
		if role.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("expected exactly one default in the list, got %d", defaults)
	}
}

func TestGlobalRoleRepository_FindDefault(t *testing.T) {
	repo := NewGlobalRoleRepository(openGlobalRoleRepoTestDB(t))
	ctx := context.Background()

	if _, err := repo.FindDefault(ctx); !errors.Is(err, globalroledom.ErrNoDefault) {
		t.Fatalf("with no default set, expected ErrNoDefault, got %v", err)
	}

	standard := testGlobalRole(uuid.New(), "USER")
	standard.IsDefault = true
	if err := repo.Create(ctx, testGlobalRole(uuid.New(), "EDITOR")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Create(ctx, standard); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.FindDefault(ctx)
	if err != nil {
		t.Fatalf("find default: %v", err)
	}
	if got.ID != standard.ID || got.Name != "USER" || !got.IsDefault {
		t.Fatalf("expected the USER role as the default, got %+v", got)
	}
}

func TestGlobalRoleRepository_UpdateKeepsTheDefaultFlag(t *testing.T) {
	// Renaming or re-permissioning a role must not change whether it is the
	// default: only SetDefault does that.
	repo := NewGlobalRoleRepository(openGlobalRoleRepoTestDB(t))
	ctx := context.Background()
	role := testGlobalRole(uuid.New(), "USER")
	role.IsDefault = true
	if err := repo.Create(ctx, role); err != nil {
		t.Fatalf("create: %v", err)
	}

	edited := *role
	edited.Name = "MEMBER"
	edited.IsDefault = false // an update carrying a stale value must not clear it
	if err := repo.Update(ctx, &edited); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, err := repo.FindByID(ctx, role.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.Name != "MEMBER" || !got.IsDefault {
		t.Fatalf("expected the renamed role to stay the default, got %+v", got)
	}
}

func TestIsUniqueViolation_NilErrorDoesNotPanic(t *testing.T) {
	if isUniqueViolation(nil) {
		t.Fatal("expected false for a nil error")
	}
}

func TestIsUniqueViolation_MatchesUniqueConstraintMessage(t *testing.T) {
	if !isUniqueViolation(errors.New(`pq: duplicate key value violates unique constraint "uq_agent_access_grants_agent_member"`)) {
		t.Fatal("expected true for a unique-constraint error message")
	}
}

func TestIsUniqueViolation_FalseForUnrelatedError(t *testing.T) {
	if isUniqueViolation(errors.New("connection refused")) {
		t.Fatal("expected false for an unrelated error")
	}
}
