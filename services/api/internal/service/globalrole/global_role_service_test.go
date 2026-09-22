package globalrolesvc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	globalrolesvc "github.com/Paca-AI/api/internal/service/globalrole"
)

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
	findDefault        func(ctx context.Context) (*globalroledom.GlobalRole, error)
	setDefault         func(ctx context.Context, id uuid.UUID) error
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

func (r *stubRepo) FindDefault(ctx context.Context) (*globalroledom.GlobalRole, error) {
	if r.findDefault != nil {
		return r.findDefault(ctx)
	}
	return nil, globalroledom.ErrNoDefault
}

func (r *stubRepo) SetDefault(ctx context.Context, id uuid.UUID) error {
	if r.setDefault != nil {
		return r.setDefault(ctx, id)
	}
	return nil
}

// existingRole is a FindByID stub for the roles a Delete test acts on: an
// ordinary (non-default) role that exists.
func existingRole(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
	return &globalroledom.GlobalRole{ID: id, Name: "SOME_ROLE"}, nil
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

// stubAgentCounter is a minimal agentRoleCounter stub.
type stubAgentCounter struct {
	count func(ctx context.Context, id uuid.UUID) (int64, error)
}

func (a *stubAgentCounter) CountAgentsWithGlobalRole(ctx context.Context, id uuid.UUID) (int64, error) {
	if a.count != nil {
		return a.count(ctx, id)
	}
	return 0, nil
}

// newSvc builds a Service with no agentRoleCounter configured — sufficient
// for tests that don't exercise the agent-in-use guard (Delete treats a nil
// agents dependency as "no agents to check").
func newSvc(repo *stubRepo) *globalrolesvc.Service {
	return globalrolesvc.New(repo, nil)
}

func TestCreate_NameValidation(t *testing.T) {
	svc := newSvc(&stubRepo{})
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{Name: "   "})
	if !errors.Is(err, globalroledom.ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}

func TestCreate_NameTaken(t *testing.T) {
	svc := newSvc(&stubRepo{
		findByName: func(_ context.Context, _ string) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: uuid.New(), Name: "SUPER_ADMIN"}, nil
		},
	})
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{Name: "SUPER_ADMIN"})
	if !errors.Is(err, globalroledom.ErrNameTaken) {
		t.Fatalf("expected ErrNameTaken, got %v", err)
	}
}

func TestDelete_RejectedWhenUsersAssigned(t *testing.T) {
	roleID := uuid.New()
	svc := newSvc(&stubRepo{
		findByID: existingRole,
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

func TestDelete_RejectedWhenAgentAssigned(t *testing.T) {
	roleID := uuid.New()
	svc := globalrolesvc.New(&stubRepo{
		findByID:           existingRole,
		countUsersWithRole: func(_ context.Context, _ uuid.UUID) (int64, error) { return 0, nil },
	}, &stubAgentCounter{
		count: func(_ context.Context, id uuid.UUID) (int64, error) {
			if id != roleID {
				t.Fatalf("unexpected role id: %s", id)
			}
			return 1, nil // 1 global agent references this role
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
	svc := newSvc(&stubRepo{
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
	svc := newSvc(&stubRepo{
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

	roles, err := svc.ReplaceUserRoles(context.Background(), userID, []uuid.UUID{roleID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != roleID {
		t.Fatalf("unexpected roles result: %+v", roles)
	}
}

// The default role is what new users and agents start with, so it has to stay:
// deleting it would leave the next user creation with nothing to assign.
func TestDelete_RejectedForTheDefaultRole(t *testing.T) {
	roleID := uuid.New()
	svc := newSvc(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "USER", IsDefault: true}, nil
		},
		// Nobody holds it, which would otherwise let the delete through.
		countUsersWithRole: func(context.Context, uuid.UUID) (int64, error) { return 0, nil },
		delete: func(context.Context, uuid.UUID) error {
			t.Fatal("the default role must not be deleted")
			return nil
		},
	})

	err := svc.Delete(context.Background(), roleID)
	if !errors.Is(err, globalroledom.ErrIsDefault) {
		t.Fatalf("expected ErrIsDefault, got %v", err)
	}
}

func TestDelete_ReportsTheDefaultBeforeAssignedUsers(t *testing.T) {
	// The more specific reason wins: "make another role the default first" is
	// what the person has to do, whether or not users hold the role too.
	svc := newSvc(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "USER", IsDefault: true}, nil
		},
		countUsersWithRole: func(context.Context, uuid.UUID) (int64, error) { return 5, nil },
	})

	err := svc.Delete(context.Background(), uuid.New())
	if !errors.Is(err, globalroledom.ErrIsDefault) {
		t.Fatalf("expected ErrIsDefault, got %v", err)
	}
}

func TestDelete_UnknownRoleIsNotFound(t *testing.T) {
	svc := newSvc(&stubRepo{})

	err := svc.Delete(context.Background(), uuid.New())
	if !errors.Is(err, globalroledom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDelete_OldDefaultCanBeDeletedOnceAnotherIsDefault(t *testing.T) {
	deleted := false
	svc := newSvc(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			// No longer the default: SetDefault cleared the flag on it.
			return &globalroledom.GlobalRole{ID: id, Name: "USER", IsDefault: false}, nil
		},
		delete: func(context.Context, uuid.UUID) error {
			deleted = true
			return nil
		},
	})

	if err := svc.Delete(context.Background(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Fatal("expected the role to be deleted")
	}
}

func TestSetDefault_MakesTheRoleTheDefaultAndReturnsIt(t *testing.T) {
	roleID := uuid.New()
	var setID uuid.UUID
	svc := newSvc(&stubRepo{
		setDefault: func(_ context.Context, id uuid.UUID) error {
			setID = id
			return nil
		},
		findByID: func(_ context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error) {
			return &globalroledom.GlobalRole{ID: id, Name: "EDITOR", IsDefault: true}, nil
		},
	})

	got, err := svc.SetDefault(context.Background(), roleID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if setID != roleID {
		t.Fatalf("SetDefault reached the repository with %s, want %s", setID, roleID)
	}
	if got.ID != roleID || !got.IsDefault {
		t.Fatalf("expected the role back as the default, got %+v", got)
	}
}

func TestSetDefault_UnknownRole(t *testing.T) {
	svc := newSvc(&stubRepo{
		setDefault: func(context.Context, uuid.UUID) error { return globalroledom.ErrNotFound },
	})

	_, err := svc.SetDefault(context.Background(), uuid.New())
	if !errors.Is(err, globalroledom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFindDefault_ReturnsTheDefaultRole(t *testing.T) {
	want := &globalroledom.GlobalRole{ID: uuid.New(), Name: "USER", IsDefault: true}
	svc := newSvc(&stubRepo{
		findDefault: func(context.Context) (*globalroledom.GlobalRole, error) { return want, nil },
	})

	got, err := svc.FindDefault(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestFindDefault_NoneSet(t *testing.T) {
	svc := newSvc(&stubRepo{})

	_, err := svc.FindDefault(context.Background())
	if !errors.Is(err, globalroledom.ErrNoDefault) {
		t.Fatalf("expected ErrNoDefault, got %v", err)
	}
}

func TestCreate_NewRolesAreNeverTheDefault(t *testing.T) {
	var created *globalroledom.GlobalRole
	svc := newSvc(&stubRepo{
		findByName: func(context.Context, string) (*globalroledom.GlobalRole, error) {
			return nil, globalroledom.ErrNotFound
		},
		create: func(_ context.Context, role *globalroledom.GlobalRole) error {
			created = role
			return nil
		},
	})

	if _, err := svc.Create(context.Background(), globalroledom.CreateInput{Name: "EDITOR"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created == nil || created.IsDefault {
		t.Fatalf("a new role must not take the default from the current one, got %+v", created)
	}
}
