package usersvc_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	globalroledom "github.com/paca/api/internal/domain/globalrole"
	userdom "github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/authz"
	usersvc "github.com/paca/api/internal/service/user"
)

// ---------------------------------------------------------------------------
// stub repository
// ---------------------------------------------------------------------------

type stubRepo struct {
	findByID       func(ctx context.Context, id uuid.UUID) (*userdom.User, error)
	findByUsername func(ctx context.Context, username string) (*userdom.User, error)
	create         func(ctx context.Context, u *userdom.User) error
	update         func(ctx context.Context, u *userdom.User) error
	delete         func(ctx context.Context, id uuid.UUID) error
}

type stubPermissionReader struct {
	listGlobalPermissions func(ctx context.Context, userID uuid.UUID) ([]authz.Permission, error)
}

func (r *stubPermissionReader) ListGlobalPermissions(ctx context.Context, userID uuid.UUID) ([]authz.Permission, error) {
	if r.listGlobalPermissions != nil {
		return r.listGlobalPermissions(ctx, userID)
	}
	return nil, nil
}

// stubRoleRepo implements usersvc.RoleByNameFinder.
type stubRoleRepo struct {
	findByName func(ctx context.Context, name string) (*globalroledom.GlobalRole, error)
}

func (r *stubRoleRepo) FindByName(ctx context.Context, name string) (*globalroledom.GlobalRole, error) {
	if r.findByName != nil {
		return r.findByName(ctx, name)
	}
	return nil, globalroledom.ErrNotFound
}

func (r *stubRepo) FindByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	if r.findByID != nil {
		return r.findByID(ctx, id)
	}
	return nil, userdom.ErrNotFound
}
func (r *stubRepo) FindByUsername(ctx context.Context, username string) (*userdom.User, error) {
	if r.findByUsername != nil {
		return r.findByUsername(ctx, username)
	}
	return nil, userdom.ErrNotFound
}
func (r *stubRepo) List(_ context.Context, _, _ int) ([]*userdom.User, int64, error) {
	return nil, 0, nil
}
func (r *stubRepo) Create(ctx context.Context, u *userdom.User) error {
	if r.create != nil {
		return r.create(ctx, u)
	}
	return nil
}
func (r *stubRepo) Update(ctx context.Context, u *userdom.User) error {
	if r.update != nil {
		return r.update(ctx, u)
	}
	return nil
}
func (r *stubRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if r.delete != nil {
		return r.delete(ctx, id)
	}
	return nil
}

// verify *usersvc.Service satisfies the domain interface
var _ userdom.Service = (*usersvc.Service)(nil)

// ---------------------------------------------------------------------------
// GetByID
// ---------------------------------------------------------------------------

func TestGetByID_Found(t *testing.T) {
	id := uuid.New()
	want := &userdom.User{ID: id, Username: "alice", Role: userdom.RoleUser}
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return want, nil },
	})

	got, err := svc.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != id {
		t.Errorf("expected id %v, got %v", id, got.ID)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	svc := usersvc.New(&stubRepo{})
	_, err := svc.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListGlobalPermissions_LegacyOnly(t *testing.T) {
	id := uuid.New()
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, got uuid.UUID) (*userdom.User, error) {
			if got != id {
				t.Fatalf("unexpected id: %v", got)
			}
			return &userdom.User{ID: id, Role: userdom.RoleUser}, nil
		},
	})

	got, err := svc.ListGlobalPermissions(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{string(authz.PermissionUsersRead)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected permissions: want %v got %v", want, got)
	}
}

func TestListGlobalPermissions_MergesAndDedupes(t *testing.T) {
	id := uuid.New()
	svc := usersvc.New(
		&stubRepo{
			findByID: func(_ context.Context, got uuid.UUID) (*userdom.User, error) {
				if got != id {
					t.Fatalf("unexpected id: %v", got)
				}
				return &userdom.User{ID: id, Role: userdom.RoleUser}, nil
			},
		},
		&stubPermissionReader{
			listGlobalPermissions: func(_ context.Context, got uuid.UUID) ([]authz.Permission, error) {
				if got != id {
					t.Fatalf("unexpected id: %v", got)
				}
				return []authz.Permission{authz.PermissionUsersRead, authz.PermissionGlobalRolesRead}, nil
			},
		},
	)

	got, err := svc.ListGlobalPermissions(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{string(authz.PermissionGlobalRolesRead), string(authz.PermissionUsersRead)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected permissions: want %v got %v", want, got)
	}
}

func TestListGlobalPermissions_UserNotFound(t *testing.T) {
	svc := usersvc.New(&stubRepo{})

	_, err := svc.ListGlobalPermissions(context.Background(), uuid.New())
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListGlobalPermissions_ReaderError(t *testing.T) {
	id := uuid.New()
	wantErr := errors.New("permission store failed")

	svc := usersvc.New(
		&stubRepo{
			findByID: func(_ context.Context, got uuid.UUID) (*userdom.User, error) {
				if got != id {
					t.Fatalf("unexpected id: %v", got)
				}
				return &userdom.User{ID: id, Role: userdom.RoleUser}, nil
			},
		},
		&stubPermissionReader{
			listGlobalPermissions: func(_ context.Context, _ uuid.UUID) ([]authz.Permission, error) {
				return nil, wantErr
			},
		},
	)

	_, err := svc.ListGlobalPermissions(context.Background(), id)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected reader error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestCreate_Success(t *testing.T) {
	roleID := uuid.New()
	svc := usersvc.New(
		&stubRepo{},
		&stubRoleRepo{
			findByName: func(_ context.Context, _ string) (*globalroledom.GlobalRole, error) {
				return &globalroledom.GlobalRole{ID: roleID, Name: userdom.RoleUser}, nil
			},
		},
	)

	got, err := svc.Create(context.Background(), userdom.CreateInput{
		Username: "alice",
		Password: "password123",
		FullName: "Alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Username != "alice" {
		t.Errorf("unexpected username: %s", got.Username)
	}
	if got.FullName != "Alice" {
		t.Errorf("unexpected full name: %s", got.FullName)
	}
	if got.Role != userdom.RoleUser {
		t.Errorf("expected role USER, got %s", got.Role)
	}
	if got.PasswordHash == "password123" {
		t.Fatal("password must be hashed, not stored in plain text")
	}
	if got.ID == uuid.Nil {
		t.Fatal("expected non-nil UUID")
	}
}

func TestCreate_DuplicateUsername(t *testing.T) {
	existing := &userdom.User{ID: uuid.New(), Username: "alice"}
	svc := usersvc.New(&stubRepo{
		findByUsername: func(_ context.Context, _ string) (*userdom.User, error) { return existing, nil },
	})

	_, err := svc.Create(context.Background(), userdom.CreateInput{
		Username: "alice",
		Password: "password123",
		FullName: "Alice",
	})
	if !errors.Is(err, userdom.ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

func TestCreate_RepoError(t *testing.T) {
	repoErr := errors.New("insert failed")
	roleID := uuid.New()
	svc := usersvc.New(
		&stubRepo{
			create: func(_ context.Context, _ *userdom.User) error { return repoErr },
		},
		&stubRoleRepo{
			findByName: func(_ context.Context, _ string) (*globalroledom.GlobalRole, error) {
				return &globalroledom.GlobalRole{ID: roleID, Name: userdom.RoleUser}, nil
			},
		},
	)

	_, err := svc.Create(context.Background(), userdom.CreateInput{
		Username: "alice",
		Password: "password123",
		FullName: "Alice",
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected wrapped repo error, got %v", err)
	}
}

func TestCreate_RequiresRoleResolver(t *testing.T) {
	svc := usersvc.New(&stubRepo{})

	_, err := svc.Create(context.Background(), userdom.CreateInput{
		Username: "alice",
		Password: "password123",
		FullName: "Alice",
	})
	if !errors.Is(err, usersvc.ErrRoleResolverRequired) {
		t.Fatalf("expected ErrRoleResolverRequired, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// AdminUpdate
// ---------------------------------------------------------------------------

func TestAdminUpdate_SetsRoleAndRoleID(t *testing.T) {
	id := uuid.New()
	roleID := uuid.New()
	repoUser := &userdom.User{ID: id, Username: "alice", FullName: "Alice", Role: userdom.RoleUser}

	svc := usersvc.New(
		&stubRepo{
			findByID: func(_ context.Context, got uuid.UUID) (*userdom.User, error) {
				if got != id {
					t.Fatalf("unexpected id: %v", got)
				}
				return repoUser, nil
			},
			update: func(_ context.Context, u *userdom.User) error {
				if u.Role != userdom.RoleAdmin {
					t.Fatalf("expected role %q, got %q", userdom.RoleAdmin, u.Role)
				}
				if u.RoleID != roleID {
					t.Fatalf("expected roleID %v, got %v", roleID, u.RoleID)
				}
				return nil
			},
		},
		&stubRoleRepo{
			findByName: func(_ context.Context, name string) (*globalroledom.GlobalRole, error) {
				if name != userdom.RoleAdmin {
					t.Fatalf("unexpected role lookup: %q", name)
				}
				return &globalroledom.GlobalRole{ID: roleID, Name: userdom.RoleAdmin}, nil
			},
		},
	)

	got, err := svc.AdminUpdate(context.Background(), id, userdom.AdminUpdateInput{Role: userdom.RoleAdmin})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Role != userdom.RoleAdmin {
		t.Fatalf("expected role %q, got %q", userdom.RoleAdmin, got.Role)
	}
	if got.RoleID != roleID {
		t.Fatalf("expected roleID %v, got %v", roleID, got.RoleID)
	}
}

func TestAdminUpdate_RoleChangeRequiresRoleResolver(t *testing.T) {
	id := uuid.New()
	svc := usersvc.New(
		&stubRepo{
			findByID: func(_ context.Context, got uuid.UUID) (*userdom.User, error) {
				if got != id {
					t.Fatalf("unexpected id: %v", got)
				}
				return &userdom.User{ID: id, Username: "alice", Role: userdom.RoleUser}, nil
			},
		},
	)

	_, err := svc.AdminUpdate(context.Background(), id, userdom.AdminUpdateInput{Role: userdom.RoleAdmin})
	if !errors.Is(err, usersvc.ErrRoleResolverRequired) {
		t.Fatalf("expected ErrRoleResolverRequired, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// UpdateProfile
// ---------------------------------------------------------------------------

func TestUpdateProfile_Success(t *testing.T) {
	id := uuid.New()
	original := &userdom.User{ID: id, Username: "alice", FullName: "Old Name", Role: userdom.RoleUser}
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return original, nil },
	})

	got, err := svc.UpdateProfile(context.Background(), id, userdom.UpdateProfileInput{FullName: "New Name"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.FullName != "New Name" {
		t.Fatalf("expected full name 'New Name', got %q", got.FullName)
	}
}

func TestUpdateProfile_NotFound(t *testing.T) {
	svc := usersvc.New(&stubRepo{})
	_, err := svc.UpdateProfile(context.Background(), uuid.New(), userdom.UpdateProfileInput{FullName: "X"})
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// ResetPassword
// ---------------------------------------------------------------------------

func TestResetPassword_Success(t *testing.T) {
	id := uuid.New()
	var savedHash string
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) {
			return &userdom.User{ID: id, Role: userdom.RoleUser, PasswordHash: "oldhash"}, nil
		},
		update: func(_ context.Context, u *userdom.User) error {
			savedHash = u.PasswordHash
			return nil
		},
	})

	if err := svc.ResetPassword(context.Background(), id, "newpassword123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedHash == "oldhash" || savedHash == "newpassword123" {
		t.Fatalf("expected bcrypt hash, got %q", savedHash)
	}
}

func TestResetPassword_UserNotFound(t *testing.T) {
	svc := usersvc.New(&stubRepo{})
	err := svc.ResetPassword(context.Background(), uuid.New(), "newpassword123")
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestDelete_Success(t *testing.T) {
	deleted := false
	svc := usersvc.New(&stubRepo{
		delete: func(_ context.Context, _ uuid.UUID) error {
			deleted = true
			return nil
		},
	})

	if err := svc.Delete(context.Background(), uuid.New()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !deleted {
		t.Fatal("expected repo.Delete to be called")
	}
}

func TestDelete_RepoError(t *testing.T) {
	repoErr := errors.New("delete failed")
	svc := usersvc.New(&stubRepo{
		delete: func(_ context.Context, _ uuid.UUID) error { return repoErr },
	})

	err := svc.Delete(context.Background(), uuid.New())
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error, got %v", err)
	}
}
