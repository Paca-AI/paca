package postgres

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// openUserRepoTestDB sets up an in-memory SQLite DB for user repository tests.
// It auto-migrates both globalRoleRecord and userRecord, seeds a "USER" global
// role, and returns the db plus the seeded role's UUID.
func openUserRepoTestDB(t *testing.T) (*gorm.DB, uuid.UUID) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "user-repo-test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&globalRoleRecord{}, &userRecord{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	// Seed a global role so foreign-key constraints are satisfied.
	roleID := uuid.New()
	if err := db.Create(&globalRoleRecord{
		ID:          roleID.String(),
		Name:        userdom.RoleUser,
		Permissions: []byte("{}"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed global role: %v", err)
	}
	return db, roleID
}

func testUser(id, roleID uuid.UUID) *userdom.User {
	now := time.Now().UTC().Truncate(time.Second)
	return &userdom.User{
		ID:           id,
		Username:     "alice",
		PasswordHash: "hashed",
		FullName:     "Alice",
		RoleID:       roleID,
		Role:         userdom.RoleUser,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func TestUserRepository_CreateAndFind(t *testing.T) {
	db, roleID := openUserRepoTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	id := uuid.New()
	u := testUser(id, roleID)
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	byID, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if byID.Username != u.Username {
		t.Fatalf("expected username %q, got %q", u.Username, byID.Username)
	}

	byUsername, err := repo.FindByUsername(ctx, u.Username)
	if err != nil {
		t.Fatalf("find by username: %v", err)
	}
	if byUsername.ID != id {
		t.Fatalf("expected id %s, got %s", id, byUsername.ID)
	}
}

func TestUserRepository_FindNotFound(t *testing.T) {
	db, _ := openUserRepoTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	_, err := repo.FindByID(ctx, uuid.New())
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	_, err = repo.FindByUsername(ctx, "missing")
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUserRepository_Update(t *testing.T) {
	db, roleID := openUserRepoTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	u := testUser(uuid.New(), roleID)
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	u.FullName = "Alice Updated"
	if err := repo.Update(ctx, u); err != nil {
		t.Fatalf("update user: %v", err)
	}

	got, err := repo.FindByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("find updated user: %v", err)
	}
	if got.FullName != "Alice Updated" {
		t.Fatalf("expected full name updated, got %q", got.FullName)
	}
}

func TestUserRepository_DeleteSoftDelete(t *testing.T) {
	db, roleID := openUserRepoTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	u := testUser(uuid.New(), roleID)
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := repo.Delete(ctx, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	_, err := repo.FindByID(ctx, u.ID)
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	var rec userRecord
	if err := db.Unscoped().First(&rec, "id = ?", u.ID.String()).Error; err != nil {
		t.Fatalf("query unscoped deleted row: %v", err)
	}
	if !rec.DeletedAt.Valid {
		t.Fatal("expected deleted_at to be set")
	}
}

func TestUserRepository_FindByUsernameIncludingDeleted_FindsSoftDeleted(t *testing.T) {
	db, roleID := openUserRepoTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	u := testUser(uuid.New(), roleID)
	if err := repo.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Delete(ctx, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	_, err := repo.FindByUsername(ctx, u.Username)
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected FindByUsername to ignore soft-deleted user, got %v", err)
	}

	got, err := repo.FindByUsernameIncludingDeleted(ctx, u.Username)
	if err != nil {
		t.Fatalf("find by username including deleted: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("expected id %s, got %s", u.ID, got.ID)
	}
}
