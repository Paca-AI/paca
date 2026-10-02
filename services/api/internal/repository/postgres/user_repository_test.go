package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	userdom "github.com/Paca-AI/api/internal/domain/user"
)

// openUserRepoTestDB sets up an in-memory SQLite DB for user repository tests.
// It creates the necessary schema, seeds a "USER" global role so FK constraints
// are satisfied, and returns the DB plus the seeded role's UUID.
func openUserRepoTestDB(t *testing.T) (*sqlx.DB, uuid.UUID) {
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
			email TEXT,
			role_id TEXT NOT NULL,
			must_change_password INTEGER NOT NULL DEFAULT 0,
			avatar_key TEXT,
			avatar_thumb_key TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		);
		CREATE UNIQUE INDEX uni_users_username_active ON users (username) WHERE deleted_at IS NULL;
		CREATE UNIQUE INDEX uni_users_email_active ON users (email) WHERE deleted_at IS NULL AND email IS NOT NULL;`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	// Seed a global role so foreign-key constraints are satisfied.
	roleID := uuid.New()
	now := time.Now()
	db.MustExec(
		`INSERT INTO global_roles (id, name, permissions, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		roleID.String(), userdom.RoleUser, []byte("{}"), now, now,
	)
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

	// Verify deleted_at was set via raw query (bypassing soft-delete filter).
	var rec userRecord
	if err := db.GetContext(ctx, &rec, "SELECT id, username, password_hash, full_name, role_id, must_change_password, created_at, updated_at, deleted_at FROM users WHERE id = $1", u.ID.String()); err != nil {
		t.Fatalf("query deleted row: %v", err)
	}
	if rec.DeletedAt == nil {
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

func TestUserRepository_Create_AllowsUsernameReuseAfterDelete(t *testing.T) {
	db, roleID := openUserRepoTestDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	original := testUser(uuid.New(), roleID)
	if err := repo.Create(ctx, original); err != nil {
		t.Fatalf("create original user: %v", err)
	}
	if err := repo.Delete(ctx, original.ID); err != nil {
		t.Fatalf("delete original user: %v", err)
	}

	replacement := testUser(uuid.New(), roleID) // same "alice" username as original
	if err := repo.Create(ctx, replacement); err != nil {
		t.Fatalf("expected username reuse after delete to succeed, got: %v", err)
	}

	got, err := repo.FindByUsername(ctx, replacement.Username)
	if err != nil {
		t.Fatalf("find by username: %v", err)
	}
	if got.ID != replacement.ID {
		t.Fatalf("expected active user to be the replacement %s, got %s", replacement.ID, got.ID)
	}
}

// --- uniqueViolationConstraint / userRepoErr --------------------------------
//
// The SQLite-backed harness above can't produce a real *pgconn.PgError (its
// driver reports constraint violations differently), so these construct one
// directly to test the Postgres-specific mapping in isolation: it's what
// makes Create/Update race-safe against usersvc.Service's
// check-then-write uniqueness pre-check for username/email (two concurrent
// requests can both pass the pre-check for the same value; the database's
// unique index is the actual guarantee, and its violation must still come
// back as the same domain sentinel the pre-check itself returns).

func TestUniqueViolationConstraint_MatchesPgUniqueViolation(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "uni_users_email_active"}
	wrapped := fmt.Errorf("insert: %w", pgErr)

	constraint, ok := uniqueViolationConstraint(wrapped)
	if !ok {
		t.Fatal("expected a unique violation to be recognized")
	}
	if constraint != "uni_users_email_active" {
		t.Errorf("expected constraint uni_users_email_active, got %q", constraint)
	}
}

func TestUniqueViolationConstraint_IgnoresOtherPgErrorCodes(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23503", ConstraintName: "users_role_id_fkey"} // FK violation, not unique
	if _, ok := uniqueViolationConstraint(fmt.Errorf("insert: %w", pgErr)); ok {
		t.Error("expected a non-unique-violation PgError not to be recognized")
	}
}

func TestUniqueViolationConstraint_IgnoresNonPgErrors(t *testing.T) {
	if _, ok := uniqueViolationConstraint(errors.New("boom")); ok {
		t.Error("expected a plain error not to be recognized as a unique violation")
	}
}

func TestUserRepoErr_MapsUsernameConstraint(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "uni_users_username_active"}
	err := userRepoErr("create", fmt.Errorf("insert: %w", pgErr))
	if !errors.Is(err, userdom.ErrUsernameTaken) {
		t.Errorf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestUserRepoErr_MapsEmailConstraint(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", ConstraintName: "uni_users_email_active"}
	err := userRepoErr("update", fmt.Errorf("update: %w", pgErr))
	if !errors.Is(err, userdom.ErrEmailTaken) {
		t.Errorf("expected ErrEmailTaken, got %v", err)
	}
}

func TestUserRepoErr_WrapsUnrelatedErrorsGenerically(t *testing.T) {
	err := userRepoErr("create", errors.New("connection reset"))
	if err == nil {
		t.Fatal("expected a non-nil wrapped error")
	}
	if errors.Is(err, userdom.ErrUsernameTaken) || errors.Is(err, userdom.ErrEmailTaken) {
		t.Errorf("unrelated error should not map to a uniqueness sentinel, got %v", err)
	}
}

func TestUserRepository_List_SearchRoleAndOrder(t *testing.T) {
	db, userRoleID := openUserRepoTestDB(t)
	adminRoleID := uuid.New()
	now := time.Now()
	db.MustExec(
		`INSERT INTO global_roles (id, name, permissions, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		adminRoleID.String(), "ADMIN", []byte("{}"), now, now,
	)
	repo := NewUserRepository(db)
	ctx := context.Background()

	email := func(s string) *string { return &s }
	seed := []struct {
		username, fullName string
		email              *string
		roleID             uuid.UUID
	}{
		{"zed", "Alice Smith", email("zed@corp.io"), userRoleID},
		{"bob", "Bob Jones", email("bob@corp.io"), adminRoleID},
		{"carol", "carol 100%", nil, userRoleID},
		{"dave_x", "Dave", email("dave@other.io"), userRoleID},
	}
	for _, s := range seed {
		u := testUser(uuid.New(), s.roleID)
		u.Username, u.FullName, u.Email = s.username, s.fullName, s.email
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("create %s: %v", s.username, err)
		}
	}

	names := func(f userdom.ListFilter) []string {
		t.Helper()
		users, total, err := repo.List(ctx, 0, 10, f)
		if err != nil {
			t.Fatalf("List(%+v): %v", f, err)
		}
		if int(total) != len(users) {
			t.Fatalf("total = %d, len = %d", total, len(users))
		}
		out := make([]string, 0, len(users))
		for _, u := range users {
			out = append(out, u.Username)
		}
		return out
	}
	eq := func(got, want []string) {
		t.Helper()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	eq(names(userdom.ListFilter{}), []string{"zed", "bob", "carol", "dave_x"})
	eq(names(userdom.ListFilter{Search: "ALICE"}), []string{"zed"})
	eq(names(userdom.ListFilter{Search: "smith alice"}), []string{"zed"}) // all words, any order
	eq(names(userdom.ListFilter{Search: "corp.io"}), []string{"zed", "bob"})
	eq(names(userdom.ListFilter{Search: "100%"}), []string{"carol"})
	eq(names(userdom.ListFilter{Search: "%"}), []string{"carol"}) // wildcard is literal
	eq(names(userdom.ListFilter{Search: "dave_"}), []string{"dave_x"})
	eq(names(userdom.ListFilter{Search: "a_e"}), []string{}) // "_" is literal, not any-char
	eq(names(userdom.ListFilter{Role: "ADMIN"}), []string{"bob"})
	eq(names(userdom.ListFilter{Role: "ADMIN", Search: "alice"}), []string{})

	// total reflects the filter, not just the page.
	users, total, err := repo.List(ctx, 0, 1, userdom.ListFilter{Search: "corp.io"})
	if err != nil || total != 2 || len(users) != 1 {
		t.Fatalf("paged: users=%d total=%d err=%v", len(users), total, err)
	}
}
