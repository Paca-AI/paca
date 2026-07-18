package globalrolesvc_test

import (
	"context"
	"errors"
	"testing"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	"github.com/Paca-AI/api/internal/platform/authz"
	globalrolesvc "github.com/Paca-AI/api/internal/service/globalrole"
	"github.com/google/uuid"
)

// superAdmin is a caller permission set holding "*", which bypasses the grant
// ceiling. Used where a test exercises behavior unrelated to the ceiling.
func superAdmin() authz.PermissionSet {
	return authz.PermissionSet{authz.PermissionAll: {}}
}

// permSet builds a caller permission set from the given permission keys.
func permSet(perms ...authz.Permission) authz.PermissionSet {
	s := authz.PermissionSet{}
	for _, p := range perms {
		s[p] = struct{}{}
	}
	return s
}

type stubRepo struct {
	list               func(ctx context.Context) ([]*globalroledom.GlobalRole, error)
	findByID           func(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error)
	findByName         func(ctx context.Context, name string) (*globalroledom.GlobalRole, error)
	create             func(ctx context.Context, role *globalroledom.GlobalRole) error
	update             func(ctx context.Context, role *globalroledom.GlobalRole) error
	delete             func(ctx context.Context, id uuid.UUID) error
	replaceUserRoles   func(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error
	listUserRoles      func(ctx context.Context, userID uuid.UUID) ([]*globalroledom.GlobalRole, error)
	countUsersWithRole func(ctx context.Context, id uuid.UUID) (int64, error)
}

func (r *stubRepo) List(ctx context.Context) ([]*globalroledom.GlobalRole, error) {
	if r.list != nil {
		return r.list(ctx)
	}
	return nil, nil
}

func (r *stubRepo) FindByID(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
	if r.findByID != nil {
		return r.findByID(ctx, id)
	}
	return nil, globalroledom.ErrNotFound
}

func (r *stubRepo) FindByName(ctx context.Context, name string) (*globalroledom.GlobalRole, error) {
	if r.findByName != nil {
		return r.findByName(ctx, name)
	}
	return nil, globalroledom.ErrNotFound
}

func (r *stubRepo) Create(ctx context.Context, role *globalroledom.GlobalRole) error {
	if r.create != nil {
		return r.create(ctx, role)
	}
	return nil
}

func (r *stubRepo) Update(ctx context.Context, role *globalroledom.GlobalRole) error {
	if r.update != nil {
		return r.update(ctx, role)
	}
	return nil
}

func (r *stubRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if r.delete != nil {
		return r.delete(ctx, id)
	}
	return nil
}

func (r *stubRepo) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	if r.replaceUserRoles != nil {
		return r.replaceUserRoles(ctx, userID, roleIDs)
	}
	return nil
}

func (r *stubRepo) ListUserRoles(ctx context.Context, userID uuid.UUID) ([]*globalroledom.GlobalRole, error) {
	if r.listUserRoles != nil {
		return r.listUserRoles(ctx, userID)
	}
	return nil, nil
}

func (r *stubRepo) CountUsersWithRole(ctx context.Context, id uuid.UUID) (int64, error) {
	if r.countUsersWithRole != nil {
		return r.countUsersWithRole(ctx, id)
	}
	return 0, nil
}

func TestCreate_NameValidation(t *testing.T) {
	svc := globalrolesvc.New(&stubRepo{})
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{Name: "   "}, superAdmin())
	if !errors.Is(err, globalroledom.ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestCreate_NameTaken(t *testing.T) {
	svc := globalrolesvc.New(&stubRepo{
		findByName: func(_ context.Context, _ string) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: uuid.New(), Name: "SUPER_ADMIN"}, nil
		},
	})
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{Name: "SUPER_ADMIN"}, superAdmin())
	if !errors.Is(err, globalroledom.ErrNameTaken) {
		t.Fatalf("expected ErrNameTaken, got %v", err)
	}
}

// TestCreate_GrantCeiling_RejectsBroaderRole verifies PACA-3: a caller that
// lacks a permission cannot mint a global role that grants it. FAIL-BEFORE:
// before the fix Create ignored the caller entirely and always succeeded.
func TestCreate_GrantCeiling_RejectsBroaderRole(t *testing.T) {
	created := false
	svc := globalrolesvc.New(&stubRepo{
		create: func(_ context.Context, _ *globalroledom.GlobalRole) error {
			created = true
			return nil
		},
	})

	// Caller holds only global_roles.write — not "*" — but tries to mint a
	// role carrying the superuser wildcard.
	caller := permSet(authz.PermissionGlobalRolesWrite)
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{
		Name:        "backdoor",
		Permissions: map[string]any{string(authz.PermissionAll): true},
	}, caller)

	if !errors.Is(err, globalroledom.ErrPermissionCeilingExceeded) {
		t.Fatalf("expected ErrPermissionCeilingExceeded, got %v", err)
	}
	if created {
		t.Fatal("role must not be persisted when the ceiling is exceeded")
	}
}

// TestCreate_GrantCeiling_AllowsSubsetRole verifies the ceiling does not block
// a role that stays within the caller's own permissions.
func TestCreate_GrantCeiling_AllowsSubsetRole(t *testing.T) {
	created := false
	svc := globalrolesvc.New(&stubRepo{
		create: func(_ context.Context, _ *globalroledom.GlobalRole) error {
			created = true
			return nil
		},
	})

	caller := permSet(authz.PermissionUsersRead, authz.PermissionUsersWrite)
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{
		Name:        "reader",
		Permissions: map[string]any{string(authz.PermissionUsersRead): true},
	}, caller)
	if err != nil {
		t.Fatalf("expected success for subset role, got %v", err)
	}
	if !created {
		t.Fatal("expected role to be persisted")
	}
}

// TestCreate_GrantCeiling_SuperAdminBypasses verifies a caller holding "*" may
// create any role.
func TestCreate_GrantCeiling_SuperAdminBypasses(t *testing.T) {
	svc := globalrolesvc.New(&stubRepo{})
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{
		Name:        "anything",
		Permissions: map[string]any{string(authz.PermissionAll): true},
	}, superAdmin())
	if err != nil {
		t.Fatalf("super admin should bypass the ceiling, got %v", err)
	}
}

func TestDelete_RejectedWhenUsersAssigned(t *testing.T) {
	roleID := uuid.New()
	svc := globalrolesvc.New(&stubRepo{
		countUsersWithRole: func(_ context.Context, id uuid.UUID) (int64, error) {
			if id != roleID {
				t.Fatalf("unexpected role id: %s", id)
			}
			return 3, nil // 3 users reference this role
		},
	})

	err := svc.Delete(context.Background(), roleID)
	if !errors.Is(err, globalroledom.ErrHasAssignedUsers) {
		t.Fatalf("expected ErrHasAssignedUsers, got %v", err)
	}
}

func TestDelete_SucceedsWhenNoUsersAssigned(t *testing.T) {
	roleID := uuid.New()
	deleted := false
	svc := globalrolesvc.New(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "OLD"}, nil
		},
		countUsersWithRole: func(_ context.Context, _ uuid.UUID) (int64, error) { return 0, nil },
		delete: func(_ context.Context, _ uuid.UUID) error {
			deleted = true
			return nil
		},
	})

	if err := svc.Delete(context.Background(), roleID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Fatal("expected repo.Delete to be called")
	}
}

func TestReplaceUserRoles_ReturnsAssignedRoles(t *testing.T) {
	userID := uuid.New()
	roleID := uuid.New()
	svc := globalrolesvc.New(&stubRepo{
		replaceUserRoles: func(_ context.Context, gotUserID uuid.UUID, _ []uuid.UUID) error {
			if gotUserID != userID {
				t.Fatalf("unexpected user id: %s", gotUserID)
			}
			return nil
		},
		listUserRoles: func(_ context.Context, gotUserID uuid.UUID) ([]*globalroledom.GlobalRole, error) {
			if gotUserID != userID {
				t.Fatalf("unexpected user id: %s", gotUserID)
			}
			return []*globalroledom.GlobalRole{{ID: roleID, Name: "SUPER_ADMIN"}}, nil
		},
	})

	roles, err := svc.ReplaceUserRoles(context.Background(), userID, []uuid.UUID{roleID}, superAdmin())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != roleID {
		t.Fatalf("unexpected roles result: %+v", roles)
	}
}

// TestReplaceUserRoles_GrantCeiling_RejectsBroaderRole verifies PACA-3: a
// caller may not assign a role whose permission set exceeds their own.
// FAIL-BEFORE: before the fix ReplaceUserRoles never inspected the target
// roles' permissions and always assigned them.
func TestReplaceUserRoles_GrantCeiling_RejectsBroaderRole(t *testing.T) {
	roleID := uuid.New()
	replaced := false
	svc := globalrolesvc.New(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			// The target role grants the superuser wildcard.
			return &globalroledom.GlobalRole{
				ID:          id,
				Name:        "SUPER_ADMIN",
				Permissions: map[string]any{string(authz.PermissionAll): true},
			}, nil
		},
		replaceUserRoles: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID) error {
			replaced = true
			return nil
		},
	})

	// Caller can assign roles but is not a super admin.
	caller := permSet(authz.PermissionGlobalRolesAssign)
	_, err := svc.ReplaceUserRoles(context.Background(), uuid.New(), []uuid.UUID{roleID}, caller)
	if !errors.Is(err, globalroledom.ErrPermissionCeilingExceeded) {
		t.Fatalf("expected ErrPermissionCeilingExceeded, got %v", err)
	}
	if replaced {
		t.Fatal("assignment must not be persisted when the ceiling is exceeded")
	}
}

// TestReplaceUserRoles_GrantCeiling_AllowsSubset verifies a caller can assign a
// role that stays within their own permissions.
func TestReplaceUserRoles_GrantCeiling_AllowsSubset(t *testing.T) {
	roleID := uuid.New()
	svc := globalrolesvc.New(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{
				ID:          id,
				Name:        "reader",
				Permissions: map[string]any{string(authz.PermissionUsersRead): true},
			}, nil
		},
		listUserRoles: func(_ context.Context, _ uuid.UUID) ([]*globalroledom.GlobalRole, error) {
			return []*globalroledom.GlobalRole{{ID: roleID, Name: "reader"}}, nil
		},
	})

	caller := permSet(authz.PermissionUsersRead, authz.PermissionGlobalRolesAssign)
	roles, err := svc.ReplaceUserRoles(context.Background(), uuid.New(), []uuid.UUID{roleID}, caller)
	if err != nil {
		t.Fatalf("expected success for subset assignment, got %v", err)
	}
	if len(roles) != 1 || roles[0].ID != roleID {
		t.Fatalf("unexpected roles result: %+v", roles)
	}
}
