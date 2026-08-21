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
ON user_external_identities (issuer, subject);`
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
