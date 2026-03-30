package globalrolesvc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	globalrolesvc "github.com/paca/api/internal/service/globalrole"
)

type stubRepo struct {
	list             func(ctx context.Context) ([]*globalroledom.GlobalRole, error)
	findByID         func(ctx context.Context, id uuid.UUID) (*globalroledom.GlobalRole, error)
	findByName       func(ctx context.Context, name string) (*globalroledom.GlobalRole, error)
	create           func(ctx context.Context, role *globalroledom.GlobalRole) error
	update           func(ctx context.Context, role *globalroledom.GlobalRole) error
	delete           func(ctx context.Context, id uuid.UUID) error
	replaceUserRoles func(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error
	listUserRoles    func(ctx context.Context, userID uuid.UUID) ([]*globalroledom.GlobalRole, error)
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

func TestCreate_NameValidation(t *testing.T) {
	svc := globalrolesvc.New(&stubRepo{})
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{Name: "   "})
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
	_, err := svc.Create(context.Background(), globalroledom.CreateInput{Name: "SUPER_ADMIN"})
	if !errors.Is(err, globalroledom.ErrNameTaken) {
		t.Fatalf("expected ErrNameTaken, got %v", err)
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

	roles, err := svc.ReplaceUserRoles(context.Background(), userID, []uuid.UUID{roleID})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != 1 || roles[0].ID != roleID {
		t.Fatalf("unexpected roles result: %+v", roles)
	}
}
