package usersvc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	userdom "github.com/paca/api/internal/domain/user"
	usersvc "github.com/paca/api/internal/service/user"
)

// ---------------------------------------------------------------------------
// stub repository
// ---------------------------------------------------------------------------

type stubRepo struct {
	findByID    func(ctx context.Context, id uuid.UUID) (*userdom.User, error)
	findByEmail func(ctx context.Context, email string) (*userdom.User, error)
	create      func(ctx context.Context, u *userdom.User) error
	update      func(ctx context.Context, u *userdom.User) error
	delete      func(ctx context.Context, id uuid.UUID) error
}

func (r *stubRepo) FindByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	if r.findByID != nil {
		return r.findByID(ctx, id)
	}
	return nil, userdom.ErrNotFound
}
func (r *stubRepo) FindByEmail(ctx context.Context, email string) (*userdom.User, error) {
	if r.findByEmail != nil {
		return r.findByEmail(ctx, email)
	}
	return nil, userdom.ErrNotFound
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
	want := &userdom.User{ID: id, Email: "a@example.com", Role: userdom.RoleUser}
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

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func TestCreate_Success(t *testing.T) {
	svc := usersvc.New(&stubRepo{})

	got, err := svc.Create(context.Background(), userdom.CreateInput{
		Email:    "new@example.com",
		Password: "password123",
		Name:     "Alice",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Email != "new@example.com" {
		t.Errorf("unexpected email: %s", got.Email)
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

func TestCreate_DuplicateEmail(t *testing.T) {
	existing := &userdom.User{ID: uuid.New(), Email: "dup@example.com"}
	svc := usersvc.New(&stubRepo{
		findByEmail: func(_ context.Context, _ string) (*userdom.User, error) { return existing, nil },
	})

	_, err := svc.Create(context.Background(), userdom.CreateInput{
		Email:    "dup@example.com",
		Password: "password123",
		Name:     "Bob",
	})
	if !errors.Is(err, userdom.ErrEmailTaken) {
		t.Fatalf("expected ErrEmailTaken, got %v", err)
	}
}

func TestCreate_RepoError(t *testing.T) {
	repoErr := errors.New("insert failed")
	svc := usersvc.New(&stubRepo{
		create: func(_ context.Context, _ *userdom.User) error { return repoErr },
	})

	_, err := svc.Create(context.Background(), userdom.CreateInput{
		Email:    "new@example.com",
		Password: "password123",
		Name:     "Alice",
	})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected wrapped repo error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func TestUpdate_Success(t *testing.T) {
	id := uuid.New()
	original := &userdom.User{ID: id, Email: "a@example.com", Name: "Old Name", Role: userdom.RoleUser}
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return original, nil },
	})

	got, err := svc.Update(context.Background(), id, userdom.UpdateInput{Name: "New Name"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "New Name" {
		t.Fatalf("expected name 'New Name', got %q", got.Name)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	svc := usersvc.New(&stubRepo{})
	_, err := svc.Update(context.Background(), uuid.New(), userdom.UpdateInput{Name: "X"})
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
