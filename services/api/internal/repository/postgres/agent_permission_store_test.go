package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/authz"
)

// TestAuthzPermissionStore_ListAgentProjectPermissions_GrantsProjectsReadForAnyMember
// mirrors TestAuthzPermissionStore_ListProjectPermissions_GrantsProjectsReadForAnyMember
// for an agent project member: a role whose own stored permissions omit
// projects.read must not leave an agent unable to fetch the project it's a
// member of (e.g. via the MCP server's list_projects/get_project tools).
func TestAuthzPermissionStore_ListAgentProjectPermissions_GrantsProjectsReadForAnyMember(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)
	ctx := context.Background()

	agentID := uuid.New()
	projectID := uuid.New()
	roleID := uuid.New()
	now := time.Now()

	db.MustExec(
		`INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at) VALUES ($1, NULL, $2, $3, $4, $5)`,
		roleID.String(), "CUSTOM_NO_PROJECT_READ", []byte(`{"tasks.read":true}`), now, now,
	)
	db.MustExec(
		`INSERT INTO project_members (id, project_id, agent_id, project_role_id) VALUES ($1, $2, $3, $4)`,
		uuid.New().String(), projectID.String(), agentID.String(), roleID.String(),
	)

	perms, err := store.ListAgentProjectPermissions(ctx, agentID, projectID)
	if err != nil {
		t.Fatalf("list agent project permissions: %v", err)
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

// TestAuthzPermissionStore_ListAgentProjectPermissions_NonMemberGetsNothing
// guards the other direction: an agent with no project_members row gets no
// permissions at all, not even projects.read.
func TestAuthzPermissionStore_ListAgentProjectPermissions_NonMemberGetsNothing(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)
	ctx := context.Background()

	perms, err := store.ListAgentProjectPermissions(ctx, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("list agent project permissions: %v", err)
	}
	if len(perms) != 0 {
		t.Fatalf("expected no permissions for a non-member agent, got %v", perms)
	}
}

// TestAuthzPermissionStore_ListAgentProjectPermissions_ExcludesSoftDeletedMembership
// confirms the already-correct deleted_at filter (unlike the human query,
// this one already had it) keeps excluding a removed agent member.
func TestAuthzPermissionStore_ListAgentProjectPermissions_ExcludesSoftDeletedMembership(t *testing.T) {
	db := openAuthzStoreTestDB(t)
	store := NewAuthzPermissionStore(db)
	ctx := context.Background()

	agentID := uuid.New()
	projectID := uuid.New()
	roleID := uuid.New()
	now := time.Now()

	db.MustExec(
		`INSERT INTO project_roles (id, project_id, role_name, permissions, created_at, updated_at) VALUES ($1, NULL, $2, $3, $4, $5)`,
		roleID.String(), "PROJECT_MANAGER", []byte(`{"tasks.write":true,"tasks.read":true}`), now, now,
	)
	db.MustExec(
		`INSERT INTO project_members (id, project_id, agent_id, project_role_id, deleted_at) VALUES ($1, $2, $3, $4, $5)`,
		uuid.New().String(), projectID.String(), agentID.String(), roleID.String(), now,
	)

	perms, err := store.ListAgentProjectPermissions(ctx, agentID, projectID)
	if err != nil {
		t.Fatalf("list agent project permissions: %v", err)
	}
	if len(perms) != 0 {
		t.Fatalf("expected no permissions for a removed (soft-deleted) agent member, got %v", perms)
	}
}
