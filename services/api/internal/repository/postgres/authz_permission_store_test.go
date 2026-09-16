package postgres

import (
	"context"
	"encoding/json"
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
			user_id TEXT,
			agent_id TEXT,
			project_role_id TEXT NOT NULL,
			deleted_at DATETIME
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

// TestAuthzPermissionStore_ListProjectPermissions_GrantsProjectsReadForAnyMember
// covers the "membership implies projects.read" fix: a role whose own
// stored permissions omit projects.read entirely (e.g. a hand-edited
// custom role) must not leave an actual project member unable to fetch the
// project they belong to.
func TestAuthzPermissionStore_ListProjectPermissions_GrantsProjectsReadForAnyMember(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)
	ctx := context.Background()

	userID := uuid.New()
	projectID := uuid.New()
	roleID := uuid.New()
	now := time.Now()

	globalRoleID := uuid.New().String()
	db.MustExec(
		`INSERT INTO global_roles (id, name, permissions, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		globalRoleID, "USER", []byte(`{}`), now, now,
	)
	db.MustExec(
		`INSERT INTO users (id, username, password_hash, full_name, role_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID.String(), "alice", "hash", "Alice", globalRoleID, now, now,
	)
	// A custom role that never granted projects.read — only tasks.read.
	db.MustExec(
		`INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at) VALUES ($1, NULL, $2, $3, $4, $5)`,
		roleID.String(), "CUSTOM_NO_PROJECT_READ", []byte(`{"tasks.read":true}`), now, now,
	)
	db.MustExec(
		`INSERT INTO project_members (id, project_id, user_id, project_role_id) VALUES ($1, $2, $3, $4)`,
		uuid.New().String(), projectID.String(), userID.String(), roleID.String(),
	)

	perms, err := store.ListProjectPermissions(ctx, userID, projectID)
	if err != nil {
		t.Fatalf("list project permissions: %v", err)
	}

	found := false
	for _, p := range perms {
		if p == authz.PermissionProjectsRead {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected projects.read to be implied by membership, got %v", perms)
	}
}

// TestAuthzPermissionStore_ListProjectPermissions_NonMemberGetsNothing guards
// the other direction of the fix above: someone with no project_members row
// at all must not get projects.read (or anything else) — the auto-grant is
// keyed on actual membership, not applied unconditionally.
func TestAuthzPermissionStore_ListProjectPermissions_NonMemberGetsNothing(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)
	ctx := context.Background()

	perms, err := store.ListProjectPermissions(ctx, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("list project permissions: %v", err)
	}
	if len(perms) != 0 {
		t.Fatalf("expected no permissions for a non-member, got %v", perms)
	}
}

// TestAuthzPermissionStore_ListProjectPermissions_ExcludesSoftDeletedMembership
// is a regression test for a separate bug found alongside the fix above:
// this query had no deleted_at filter, so a removed member (RemoveMember
// soft-deletes the project_members row rather than deleting it) kept
// resolving their former role's permissions indefinitely.
func TestAuthzPermissionStore_ListProjectPermissions_ExcludesSoftDeletedMembership(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)
	ctx := context.Background()

	userID := uuid.New()
	projectID := uuid.New()
	roleID := uuid.New()
	now := time.Now()

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
		`INSERT INTO project_members (id, project_id, user_id, project_role_id, deleted_at) VALUES ($1, $2, $3, $4, $5)`,
		uuid.New().String(), projectID.String(), userID.String(), roleID.String(), now,
	)

	perms, err := store.ListProjectPermissions(ctx, userID, projectID)
	if err != nil {
		t.Fatalf("list project permissions: %v", err)
	}
	if len(perms) != 0 {
		t.Fatalf("expected no permissions for a removed (soft-deleted) member, got %v", perms)
	}
}

// TestPermissionsFromJSON_MatchesAuthzParser pins the store's own view of a
// persisted permissions blob to the parser the authorization guards use
// (authz.PermissionsGrantAll). The two must agree on whitespace-padded keys:
// the store trims them before granting, so " *" is PermissionAll here — if a
// guard disagreed, an ADMIN could persist a padded wildcard that resolves to
// god mode at request time while sailing past the guard. Kept as a direct
// parser test (no database) so it runs everywhere.
func TestPermissionsFromJSON_MatchesAuthzParser(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []authz.Permission
	}{
		{"wildcard", `{"*": true}`, []authz.Permission{authz.PermissionAll}},
		{"leading-space wildcard", `{" *": true}`, []authz.Permission{authz.PermissionAll}},
		{"trailing-space wildcard", `{"* ": true}`, []authz.Permission{authz.PermissionAll}},
		{"tab-padded wildcard", "{\"\\t*\": true}", []authz.Permission{authz.PermissionAll}},
		{"false wildcard", `{"*": false}`, []authz.Permission{}},
		{"named permissions", `{"users.read": true}`, []authz.Permission{authz.PermissionUsersRead}},
		{"array shape", `[" * "]`, []authz.Permission{authz.PermissionAll}},
		{"empty", ``, nil},
		{"malformed", `{`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := permissionsFromJSON([]byte(tc.raw))
			if len(got) != len(tc.want) {
				t.Fatalf("permissionsFromJSON(%s) = %v, want %v", tc.raw, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("permissionsFromJSON(%s) = %v, want %v", tc.raw, got, tc.want)
				}
			}

			// The guard's view must match: same blob, same verdict.
			wantAll := false
			for _, p := range tc.want {
				if p == authz.PermissionAll {
					wantAll = true
				}
			}
			var payload any
			if len(tc.raw) > 0 && json.Unmarshal([]byte(tc.raw), &payload) == nil {
				if authz.PermissionsGrantAll(payload) != wantAll {
					t.Fatalf("authz.PermissionsGrantAll(%s) disagrees with the store's parser", tc.raw)
				}
			}
		})
	}
}
