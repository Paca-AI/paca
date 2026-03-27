package dto

import (
	"testing"
	"time"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
)

func TestUserFromEntity(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()

	u := &userdom.User{
		ID:        id,
		Username:  "alice",
		FullName:  "Alice",
		Role:      userdom.RoleUser,
		CreatedAt: now,
	}

	resp := UserFromEntity(u)
	if resp.ID != id {
		t.Fatalf("expected id %s, got %s", id, resp.ID)
	}
	if resp.Username != "alice" || resp.FullName != "Alice" || resp.Role != userdom.RoleUser {
		t.Fatalf("unexpected mapped response: %+v", resp)
	}
	if !resp.CreatedAt.Equal(now) {
		t.Fatalf("expected created_at %v, got %v", now, resp.CreatedAt)
	}
}
