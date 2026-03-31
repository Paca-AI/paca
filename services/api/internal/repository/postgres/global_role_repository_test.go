package postgres

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	userdom "github.com/paca/api/internal/domain/user"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openGlobalRoleRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "global-role-repo-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&userRecord{}, &globalRoleRecord{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
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
	user := &userdom.User{
		ID:           userID,
		Username:     "alice",
		PasswordHash: "hash",
		FullName:     "Alice",
		RoleID:       roleA.ID,
		Role:         userdom.RoleAdmin,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := db.Create(entityToRecord(user)).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

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
