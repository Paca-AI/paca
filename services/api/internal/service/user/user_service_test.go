package usersvc_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	globalroledom "github.com/Paca-AI/api/internal/domain/globalrole"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/platform/authz"
	usersvc "github.com/Paca-AI/api/internal/service/user"
)

// ---------------------------------------------------------------------------
// stub avatar service
// ---------------------------------------------------------------------------

// stubAvatarService is a bare-bones attachmentdom.AvatarService double —
// CompleteAvatarUpload always returns nextKeys, and DeleteAvatarObjects
// records what it was asked to delete so tests can assert the *previous*
// avatar's keys were cleaned up after a replace.
type stubAvatarService struct {
	mu          sync.Mutex
	nextKeys    *attachmentdom.AvatarKeys
	deletedKeys []string
}

func (s *stubAvatarService) InitiateAvatarUpload(context.Context, attachmentdom.AvatarUploadInput) (*attachmentdom.UploadSession, error) {
	return &attachmentdom.UploadSession{FileID: uuid.New(), UploadURL: "https://fake/upload"}, nil
}

func (s *stubAvatarService) CompleteAvatarUpload(context.Context, attachmentdom.AvatarCompleteInput) (*attachmentdom.AvatarKeys, error) {
	return s.nextKeys, nil
}

func (s *stubAvatarService) ResolveAvatarURL(context.Context, *string) (*string, error) {
	return nil, nil
}

func (s *stubAvatarService) DeleteAvatarObjects(_ context.Context, keys ...*string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		if k != nil && *k != "" {
			s.deletedKeys = append(s.deletedKeys, *k)
		}
	}
}

// ---------------------------------------------------------------------------
// stub repository
// ---------------------------------------------------------------------------

type stubRepo struct {
	findByID                       func(ctx context.Context, id uuid.UUID) (*userdom.User, error)
	findByUsername                 func(ctx context.Context, username string) (*userdom.User, error)
	findByEmail                    func(ctx context.Context, email string) (*userdom.User, error)
	findByUsernameIncludingDeleted func(ctx context.Context, username string) (*userdom.User, error)
	create                         func(ctx context.Context, u *userdom.User) error
	update                         func(ctx context.Context, u *userdom.User) error
	delete                         func(ctx context.Context, id uuid.UUID) error
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
func (r *stubRepo) FindByEmail(ctx context.Context, email string) (*userdom.User, error) {
	if r.findByEmail != nil {
		return r.findByEmail(ctx, email)
	}
	return nil, userdom.ErrNotFound
}
func (r *stubRepo) FindByUsernameIncludingDeleted(ctx context.Context, username string) (*userdom.User, error) {
	if r.findByUsernameIncludingDeleted != nil {
		return r.findByUsernameIncludingDeleted(ctx, username)
	}
	if r.findByUsername != nil {
		return r.findByUsername(ctx, username)
	}
	return nil, userdom.ErrNotFound
}
func (r *stubRepo) List(_ context.Context, _, _ int) ([]*userdom.User, int64, error) {
	return nil, 0, nil
}
func (r *stubRepo) CountUsers(_ context.Context) (int64, error) { return 0, nil }
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

func TestCreate_AllowsUsernameReuseAfterSoftDelete(t *testing.T) {
	roleID := uuid.New()
	deletedUser := &userdom.User{ID: uuid.New(), Username: "alice"}
	svc := usersvc.New(
		&stubRepo{
			// Active lookup finds nothing — the only "alice" on record is soft-deleted.
			findByUsername: func(_ context.Context, _ string) (*userdom.User, error) {
				return nil, userdom.ErrNotFound
			},
			findByUsernameIncludingDeleted: func(_ context.Context, _ string) (*userdom.User, error) {
				return deletedUser, nil
			},
		},
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
		t.Fatalf("expected username reuse after soft-delete to succeed, got: %v", err)
	}
	if got.Username != "alice" {
		t.Errorf("unexpected username: %s", got.Username)
	}
	if got.ID == deletedUser.ID {
		t.Fatal("expected a new user ID, not the deleted user's ID")
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
// SetPasswordWithToken
// ---------------------------------------------------------------------------

type stubTokenRepo struct {
	findActiveByTokenHash func(ctx context.Context, hash string) (*userdom.PasswordSetToken, error)
	markUsedCalled        bool
	markUsedClaimed       *bool
	createCalled          bool
	created               *userdom.PasswordSetToken
}

func (r *stubTokenRepo) Create(_ context.Context, t *userdom.PasswordSetToken) error {
	r.createCalled = true
	r.created = t
	return nil
}
func (r *stubTokenRepo) FindActiveByTokenHash(ctx context.Context, hash string) (*userdom.PasswordSetToken, error) {
	if r.findActiveByTokenHash != nil {
		return r.findActiveByTokenHash(ctx, hash)
	}
	return nil, userdom.ErrPasswordSetTokenInvalid
}
func (r *stubTokenRepo) MarkUsed(_ context.Context, _ uuid.UUID) (bool, error) {
	r.markUsedCalled = true
	if r.markUsedClaimed != nil {
		return *r.markUsedClaimed, nil
	}
	return true, nil
}

// TestIssuePasswordSetToken_UserNotFound pins IssuePasswordSetToken checking
// the user exists up front: the plugin host function
// (registerPasswordSetTokenFunction) forwards this error's message straight
// back to the calling plugin, so a nonexistent user_id must surface as the
// clean userdom.ErrNotFound sentinel — not a raw FK-violation string from
// the token insert, which would otherwise leak internal DB detail. No token
// row should be created for a user that doesn't exist.
func TestIssuePasswordSetToken_UserNotFound(t *testing.T) {
	tokenRepo := &stubTokenRepo{}
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) {
			return nil, userdom.ErrNotFound
		},
	}).WithPasswordSetTokenRepo(tokenRepo)

	_, _, err := svc.IssuePasswordSetToken(context.Background(), uuid.New())
	if !errors.Is(err, userdom.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if tokenRepo.createCalled {
		t.Error("expected no token to be created for a nonexistent user")
	}
}

// TestIssuePasswordSetToken_Success guards the happy path: a token is
// created scoped to the requested user, and the raw token returned to the
// caller hashes to what was persisted (so FindActiveByTokenHash can find it
// later).
func TestIssuePasswordSetToken_Success(t *testing.T) {
	userID := uuid.New()
	tokenRepo := &stubTokenRepo{}
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*userdom.User, error) {
			return &userdom.User{ID: id}, nil
		},
	}).WithPasswordSetTokenRepo(tokenRepo)

	raw, expiresAt, err := svc.IssuePasswordSetToken(context.Background(), userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw == "" {
		t.Fatal("expected a non-empty raw token")
	}
	if !tokenRepo.createCalled || tokenRepo.created == nil {
		t.Fatal("expected a token to be persisted")
	}
	if tokenRepo.created.UserID != userID {
		t.Errorf("expected token scoped to user %s, got %s", userID, tokenRepo.created.UserID)
	}
	if !expiresAt.After(time.Now()) {
		t.Errorf("expected expiresAt in the future, got %v", expiresAt)
	}
}

func TestSetPasswordWithToken_Success_WhenMustChangePassword(t *testing.T) {
	userID := uuid.New()
	var savedHash string
	tokenRepo := &stubTokenRepo{
		findActiveByTokenHash: func(_ context.Context, _ string) (*userdom.PasswordSetToken, error) {
			return &userdom.PasswordSetToken{ID: uuid.New(), UserID: userID}, nil
		},
	}
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) {
			return &userdom.User{ID: userID, Role: userdom.RoleUser, PasswordHash: "oldhash", MustChangePassword: true}, nil
		},
		update: func(_ context.Context, u *userdom.User) error {
			savedHash = u.PasswordHash
			if u.MustChangePassword {
				t.Fatalf("expected MustChangePassword cleared after setting password")
			}
			return nil
		},
	}).WithPasswordSetTokenRepo(tokenRepo)

	if err := svc.SetPasswordWithToken(context.Background(), "raw-token", "newpassword123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if savedHash == "oldhash" || savedHash == "newpassword123" {
		t.Fatalf("expected bcrypt hash, got %q", savedHash)
	}
	if !tokenRepo.markUsedCalled {
		t.Fatal("expected token to be marked used")
	}
}

// TestSetPasswordWithToken_RejectsWhenPasswordAlreadySet pins the fix for a
// token that was issued (e.g. via a plugin's welcome email) but never
// consumed because the recipient instead logged in with their temporary
// password and changed it through the normal UI: the still-unused,
// unexpired token must no longer be redeemable once the account has left
// the must-change-password state, or anyone who later gets hold of the old
// invite link could reset the account's password without ever knowing its
// current one.
func TestSetPasswordWithToken_RejectsWhenPasswordAlreadySet(t *testing.T) {
	userID := uuid.New()
	updateCalled := false
	tokenRepo := &stubTokenRepo{
		findActiveByTokenHash: func(_ context.Context, _ string) (*userdom.PasswordSetToken, error) {
			return &userdom.PasswordSetToken{ID: uuid.New(), UserID: userID}, nil
		},
	}
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) {
			return &userdom.User{ID: userID, Role: userdom.RoleUser, PasswordHash: "oldhash", MustChangePassword: false}, nil
		},
		update: func(_ context.Context, _ *userdom.User) error {
			updateCalled = true
			return nil
		},
	}).WithPasswordSetTokenRepo(tokenRepo)

	err := svc.SetPasswordWithToken(context.Background(), "raw-token", "newpassword123")
	if !errors.Is(err, userdom.ErrPasswordSetTokenInvalid) {
		t.Fatalf("expected ErrPasswordSetTokenInvalid, got %v", err)
	}
	if updateCalled {
		t.Fatal("expected no password update when account isn't in must-change-password state")
	}
	if tokenRepo.markUsedCalled {
		t.Fatal("expected token left unused on rejection")
	}
}

// TestSetPasswordWithToken_RejectsLostRace pins the fix for the redemption
// race: MarkUsed atomically claims the token, and losing that claim (a
// concurrent request already redeemed it) must reject the request without
// writing a password, even though FindActiveByTokenHash and the
// must-change-password check both still passed.
func TestSetPasswordWithToken_RejectsLostRace(t *testing.T) {
	userID := uuid.New()
	updateCalled := false
	lost := false
	tokenRepo := &stubTokenRepo{
		findActiveByTokenHash: func(_ context.Context, _ string) (*userdom.PasswordSetToken, error) {
			return &userdom.PasswordSetToken{ID: uuid.New(), UserID: userID}, nil
		},
		markUsedClaimed: &lost,
	}
	svc := usersvc.New(&stubRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) {
			return &userdom.User{ID: userID, Role: userdom.RoleUser, PasswordHash: "oldhash", MustChangePassword: true}, nil
		},
		update: func(_ context.Context, _ *userdom.User) error {
			updateCalled = true
			return nil
		},
	}).WithPasswordSetTokenRepo(tokenRepo)

	err := svc.SetPasswordWithToken(context.Background(), "raw-token", "newpassword123")
	if !errors.Is(err, userdom.ErrPasswordSetTokenInvalid) {
		t.Fatalf("expected ErrPasswordSetTokenInvalid, got %v", err)
	}
	if updateCalled {
		t.Fatal("expected no password update when the claim was lost to a concurrent redemption")
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

// ---------------------------------------------------------------------------
// Avatar
// ---------------------------------------------------------------------------

func TestInitiateAvatarUpload_NoAvatarService_ReturnsError(t *testing.T) {
	svc := usersvc.New(&stubRepo{}) // WithAvatarService never called
	_, err := svc.InitiateAvatarUpload(context.Background(), uuid.New(), "me.png", "image/png", 1024)
	if !errors.Is(err, usersvc.ErrAvatarServiceRequired) {
		t.Fatalf("expected ErrAvatarServiceRequired, got %v", err)
	}
}

func TestCompleteAvatarUpload_SwapsKeysAndDeletesOld(t *testing.T) {
	oldKey, oldThumbKey := "avatars/users/u1/old/full.png", "avatars/users/u1/old/thumb.png"
	userID := uuid.New()
	var updated *userdom.User
	repo := &stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*userdom.User, error) {
			return &userdom.User{ID: id, AvatarKey: &oldKey, AvatarThumbKey: &oldThumbKey}, nil
		},
		update: func(_ context.Context, u *userdom.User) error {
			updated = u
			return nil
		},
	}
	avatarSvc := &stubAvatarService{
		nextKeys: &attachmentdom.AvatarKeys{Key: "avatars/users/u1/new/full.png", ThumbKey: "avatars/users/u1/new/thumb.png"},
	}
	svc := usersvc.New(repo).WithAvatarService(avatarSvc)

	u, err := svc.CompleteAvatarUpload(context.Background(), userID, uuid.New())
	if err != nil {
		t.Fatalf("CompleteAvatarUpload: %v", err)
	}
	if u.AvatarKey == nil || *u.AvatarKey != avatarSvc.nextKeys.Key {
		t.Errorf("expected AvatarKey %q, got %v", avatarSvc.nextKeys.Key, u.AvatarKey)
	}
	if updated == nil || updated.AvatarKey == nil || *updated.AvatarKey != avatarSvc.nextKeys.Key {
		t.Errorf("expected repo.Update to persist the new AvatarKey, got %v", updated)
	}

	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	if len(avatarSvc.deletedKeys) != 2 {
		t.Fatalf("expected the two old keys to be deleted, got %v", avatarSvc.deletedKeys)
	}
}

func TestRemoveAvatar_NoExistingAvatar_NoOps(t *testing.T) {
	updateCalled := false
	repo := &stubRepo{
		findByID: func(_ context.Context, id uuid.UUID) (*userdom.User, error) {
			return &userdom.User{ID: id}, nil
		},
		update: func(context.Context, *userdom.User) error {
			updateCalled = true
			return nil
		},
	}
	avatarSvc := &stubAvatarService{}
	svc := usersvc.New(repo).WithAvatarService(avatarSvc)

	if _, err := svc.RemoveAvatar(context.Background(), uuid.New()); err != nil {
		t.Fatalf("RemoveAvatar: %v", err)
	}
	if updateCalled {
		t.Error("expected repo.Update not to be called when the user has no avatar")
	}
	avatarSvc.mu.Lock()
	defer avatarSvc.mu.Unlock()
	if len(avatarSvc.deletedKeys) != 0 {
		t.Errorf("expected no delete calls when user has no avatar, got %v", avatarSvc.deletedKeys)
	}
}
