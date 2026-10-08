package userdom

import (
	"testing"
	"time"

	"github.com/google/uuid"

	roledom "github.com/Paca-AI/api/internal/domain/role"
)

func TestRoleClaim(t *testing.T) {
	u := User{Roles: []roledom.Summary{{ID: uuid.New(), Name: "ADMIN"}, {ID: uuid.New(), Name: "Support"}}}
	if got := u.RoleClaim(); got != "ADMIN,Support" {
		t.Fatalf("RoleClaim = %q", got)
	}
	if got := (&User{}).RoleClaim(); got != "" {
		t.Fatalf("empty RoleClaim = %q", got)
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
		Roles:        []roledom.Summary{{ID: uuid.New(), Name: "USER"}},
		CreatedAt:    now,
		UpdatedAt:    now,
		DeletedAt:    &deletedAt,
	}

	if u.Username != "alice" || u.FullName != "Alice" || len(u.Roles) != 1 || u.Roles[0].Name != "USER" {
		t.Fatalf("unexpected user entity values: %+v", u)
	}
	if u.DeletedAt == nil || !u.DeletedAt.Equal(deletedAt) {
		t.Fatal("expected deleted timestamp to be set")
	}
}
