package dto

import (
	"testing"
	"time"

	"github.com/google/uuid"

	roledom "github.com/Paca-AI/api/internal/domain/role"
	userdom "github.com/Paca-AI/api/internal/domain/user"
)

func TestUserFromEntity(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()

	u := &userdom.User{
		ID:        id,
		Username:  "alice",
		FullName:  "Alice",
		Roles:     []roledom.Summary{{ID: uuid.New(), Name: "USER"}},
		CreatedAt: now,
	}

	resp := UserFromEntity(u)
	if resp.ID != id {
		t.Fatalf("expected id %s, got %s", id, resp.ID)
	}
	if resp.Username != "alice" || resp.FullName != "Alice" || len(resp.Roles) != 1 || resp.Roles[0].Name != "USER" {
		t.Fatalf("unexpected mapped response: %+v", resp)
	}
	if !resp.CreatedAt.Equal(now) {
		t.Fatalf("expected created_at %v, got %v", now, resp.CreatedAt)
	}
}
