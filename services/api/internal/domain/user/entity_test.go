package userdom

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRoleConstants(t *testing.T) {
	if RoleUser == "" || RoleAdmin == "" {
		t.Fatal("role constants must be non-empty")
	}
	if RoleUser == RoleAdmin {
		t.Fatal("role constants must be distinct")
	}
}

func TestUserEntityFields(t *testing.T) {
	now := time.Now().UTC()
	deletedAt := now.Add(time.Hour)
	u := User{
		ID:           uuid.New(),
		Username:     "alice",
		PasswordHash: "hash",
		FullName:     "Alice",
		Role:         RoleUser,
		CreatedAt:    now,
		UpdatedAt:    now,
		DeletedAt:    &deletedAt,
	}

	if u.Username != "alice" || u.FullName != "Alice" || u.Role != RoleUser {
		t.Fatalf("unexpected user entity values: %+v", u)
	}
	if u.DeletedAt == nil || !u.DeletedAt.Equal(deletedAt) {
		t.Fatal("expected deleted timestamp to be set")
	}
}
