package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"

	extiddom "github.com/Paca-AI/api/internal/domain/externalidentity"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

func openIdentityTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	schema := `
CREATE TABLE users (
id TEXT PRIMARY KEY,
username TEXT NOT NULL,
password_hash TEXT NOT NULL,
full_name TEXT NOT NULL DEFAULT '',
email TEXT,
role_id TEXT NOT NULL,
must_change_password INTEGER NOT NULL DEFAULT 0,
password_login_enabled INTEGER NOT NULL DEFAULT 1,
avatar_key TEXT,
avatar_thumb_key TEXT,
created_at DATETIME,
updated_at DATETIME,
deleted_at DATETIME
);
CREATE UNIQUE INDEX uni_users_username_active ON users (username) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uni_users_email_active ON users (email) WHERE deleted_at IS NULL AND email IS NOT NULL;
CREATE TABLE user_external_identities (
id TEXT PRIMARY KEY,
user_id TEXT NOT NULL,
provider TEXT NOT NULL DEFAULT 'oidc',
issuer TEXT NOT NULL,
subject TEXT NOT NULL,
created_at DATETIME,
last_login_at DATETIME
);
CREATE UNIQUE INDEX uni_external_identity_issuer_subject
ON user_external_identities (issuer, subject);
CREATE TABLE global_roles (
id TEXT PRIMARY KEY,
name TEXT NOT NULL,
description TEXT NOT NULL DEFAULT ''
);`
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return db
}

func newSSOUser(username string) *userdom.User {
	return &userdom.User{
		ID:                   uuid.New(),
		Username:             username,
		PasswordHash:         "random-hash",
		RoleID:               uuid.New(),
		Role:                 "USER",
		PasswordLoginEnabled: false,
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
}

func newIdentity(userID uuid.UUID, issuer, subject string) *extiddom.Identity {
	now := time.Now().UTC()
	return &extiddom.Identity{
		ID:          uuid.New(),
		UserID:      userID,
		Provider:    "oidc",
		Issuer:      issuer,
		Subject:     subject,
		CreatedAt:   now,
		LastLoginAt: now,
	}
}

func TestExternalIdentity_ProvisionWithUserCreatesBothRows(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := NewExternalIdentityRepository(db)
	ctx := context.Background()

	u := newSSOUser("alice")
	ident := newIdentity(u.ID, "https://id.example.com", "sub-1")
	if err := repo.ProvisionWithUser(ctx, u, ident); err != nil {
		t.Fatalf("provision: %v", err)
	}

	var userCount, identCount int
	_ = db.GetContext(ctx, &userCount, "SELECT COUNT(*) FROM users WHERE id = ?", u.ID.String())
	_ = db.GetContext(ctx, &identCount, "SELECT COUNT(*) FROM user_external_identities WHERE user_id = ?", u.ID.String())
	if userCount != 1 || identCount != 1 {
		t.Fatalf("expected 1 user + 1 identity, got %d/%d", userCount, identCount)
	}

	found, err := repo.FindByIssuerSubject(ctx, "https://id.example.com", "sub-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found.UserID != u.ID {
		t.Fatalf("expected binding to user %s, got %s", u.ID, found.UserID)
	}
}

func TestExternalIdentity_FindByIssuerSubjectNotFound(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := NewExternalIdentityRepository(db)

	if _, err := repo.FindByIssuerSubject(context.Background(), "https://id.example.com", "ghost"); err != extiddom.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestExternalIdentity_TouchLastLogin(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := NewExternalIdentityRepository(db)
	ctx := context.Background()

	u := newSSOUser("alice")
	ident := newIdentity(u.ID, "https://id.example.com", "sub-1")
	if err := repo.ProvisionWithUser(ctx, u, ident); err != nil {
		t.Fatalf("provision: %v", err)
	}

	if err := repo.TouchLastLogin(ctx, ident.ID); err != nil {
		t.Fatalf("touch: %v", err)
	}
}

// The SSO-only startup guard must only trust admins bound to the currently
// configured issuer — an admin on a retired IdP must not satisfy it.
func TestExternalIdentity_HasSSOUserWithRole(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := NewExternalIdentityRepository(db)
	ctx := context.Background()

	seed := func(username, roleName string) *userdom.User {
		u := newSSOUser(username)
		u.Role = roleName
		var roleID string
		if err := db.GetContext(ctx, &roleID, "SELECT id FROM global_roles WHERE name = ?", roleName); err != nil {
			t.Fatalf("lookup role %s: %v", roleName, err)
		}
		u.RoleID = uuid.MustParse(roleID)
		if _, err := db.ExecContext(ctx,
			"INSERT INTO users (id, username, password_hash, full_name, role_id, must_change_password, password_login_enabled, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)",
			u.ID.String(), u.Username, u.PasswordHash, u.FullName, u.RoleID.String(), 0, 1, u.CreatedAt, u.UpdatedAt); err != nil {
			t.Fatalf("seed user: %v", err)
		}
		return u
	}
	bind := func(u *userdom.User, issuer, subject string) {
		ident := newIdentity(u.ID, issuer, subject)
		if _, err := db.ExecContext(ctx,
			"INSERT INTO user_external_identities (id, user_id, provider, issuer, subject, created_at, last_login_at) VALUES (?,?,?,?,?,?,?)",
			ident.ID.String(), ident.UserID.String(), ident.Provider, ident.Issuer, ident.Subject, ident.CreatedAt, ident.LastLoginAt); err != nil {
			t.Fatalf("seed identity: %v", err)
		}
	}

	currentIssuer := "https://id.example.com"
	oldIssuer := "https://old-id.example.com"

	// Seed the role rows the guard joins against.
	for name := range map[string]bool{"USER": true, "ADMIN": true, "SUPER_ADMIN": true} {
		if _, err := db.ExecContext(ctx, "INSERT INTO global_roles (id, name, description) VALUES (?,?,?)", uuid.NewString(), name, ""); err != nil {
			t.Fatalf("seed role %s: %v", name, err)
		}
	}

	// An admin bound to the CURRENT issuer.
	admin := seed("admin", "ADMIN")
	bind(admin, currentIssuer, "admin-sub")
	// A plain user on the current issuer.
	user := seed("bob", "USER")
	bind(user, currentIssuer, "bob-sub")

	ok, err := repo.HasSSOUserWithRole(ctx, currentIssuer, []string{"ADMIN", "SUPER_ADMIN"})
	if err != nil || !ok {
		t.Fatalf("expected an SSO admin on the current issuer, got ok=%v err=%v", ok, err)
	}
	// No privileged binding on the other issuer — must NOT satisfy the guard.
	ok, err = repo.HasSSOUserWithRole(ctx, oldIssuer, []string{"ADMIN", "SUPER_ADMIN"})
	if err != nil || ok {
		t.Fatalf("an admin on a different issuer must not satisfy the guard, got ok=%v err=%v", ok, err)
	}
	// A deleted admin no longer counts.
	if _, err := db.ExecContext(ctx, "UPDATE users SET deleted_at = ? WHERE id = ?", time.Now().UTC(), admin.ID.String()); err != nil {
		t.Fatalf("soft-delete admin: %v", err)
	}
	ok, err = repo.HasSSOUserWithRole(ctx, currentIssuer, []string{"ADMIN", "SUPER_ADMIN"})
	if err != nil || ok {
		t.Fatalf("a deleted admin must not satisfy the guard, got ok=%v err=%v", ok, err)
	}
}

func TestExternalIdentity_ProvisionUsernameCollisionFailsAtomic(t *testing.T) {
	db := openIdentityTestDB(t)
	repo := NewExternalIdentityRepository(db)
	ctx := context.Background()

	u1 := newSSOUser("alice")
	if err := repo.ProvisionWithUser(ctx, u1, newIdentity(u1.ID, "https://id.example.com", "sub-1")); err != nil {
		t.Fatalf("provision 1: %v", err)
	}

	u2 := newSSOUser("alice")
	err := repo.ProvisionWithUser(ctx, u2, newIdentity(u2.ID, "https://id.example.com", "sub-2"))
	if err == nil {
		t.Fatal("expected a username-collision error")
	}
	// The transaction rolled back: no orphan user, no orphan identity.
	var userCount, identCount int
	_ = db.GetContext(ctx, &userCount, "SELECT COUNT(*) FROM users WHERE id = ?", u2.ID.String())
	_ = db.GetContext(ctx, &identCount, "SELECT COUNT(*) FROM user_external_identities WHERE issuer = ? AND subject = ?", "https://id.example.com", "sub-2")
	if userCount != 0 || identCount != 0 {
		t.Fatalf("expected rollback to leave no orphan rows, got %d users / %d identities", userCount, identCount)
	}
}
