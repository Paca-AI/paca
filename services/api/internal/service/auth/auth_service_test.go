package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	domainauth "github.com/paca/api/internal/domain/auth"
	"github.com/paca/api/internal/domain/user"
	"github.com/paca/api/internal/platform/token"
	authsvc "github.com/paca/api/internal/service/auth"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// stubs
// ---------------------------------------------------------------------------

type stubUserRepo struct {
	findByEmail func(ctx context.Context, email string) (*user.User, error)
}

func (r *stubUserRepo) FindByID(_ context.Context, _ uuid.UUID) (*user.User, error) {
	return nil, user.ErrNotFound
}
func (r *stubUserRepo) FindByEmail(ctx context.Context, email string) (*user.User, error) {
	if r.findByEmail != nil {
		return r.findByEmail(ctx, email)
	}
	return nil, user.ErrNotFound
}
func (r *stubUserRepo) Create(_ context.Context, _ *user.User) error { return nil }
func (r *stubUserRepo) Update(_ context.Context, _ *user.User) error { return nil }
func (r *stubUserRepo) Delete(_ context.Context, _ uuid.UUID) error  { return nil }

type stubBlacklist struct {
	revoke    func(ctx context.Context, jti string, ttl time.Duration) error
	isRevoked func(ctx context.Context, jti string) (bool, error)
}

func (b *stubBlacklist) Revoke(ctx context.Context, jti string, ttl time.Duration) error {
	if b.revoke != nil {
		return b.revoke(ctx, jti, ttl)
	}
	return nil
}
func (b *stubBlacklist) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if b.isRevoked != nil {
		return b.isRevoked(ctx, jti)
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func hashedPassword(t *testing.T, plain string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(h)
}

func newAuthSvc(repo *stubUserRepo, bl *stubBlacklist) *authsvc.Service {
	tm := token.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	return authsvc.New(repo, tm, bl, 7*24*time.Hour)
}

// verify that *authsvc.Service satisfies the domain interface
var _ domainauth.Service = (*authsvc.Service)(nil)

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	u := &user.User{
		ID:           uuid.New(),
		Email:        "alice@example.com",
		Role:         user.RoleUser,
		PasswordHash: hashedPassword(t, "secret123"),
	}
	svc := newAuthSvc(&stubUserRepo{
		findByEmail: func(_ context.Context, _ string) (*user.User, error) { return u, nil },
	}, &stubBlacklist{})

	pair, err := svc.Login(context.Background(), "alice@example.com", "secret123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected non-empty token pair")
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	svc := newAuthSvc(&stubUserRepo{}, &stubBlacklist{})
	_, err := svc.Login(context.Background(), "ghost@example.com", "pass1234")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	u := &user.User{
		ID:           uuid.New(),
		Email:        "alice@example.com",
		Role:         user.RoleUser,
		PasswordHash: hashedPassword(t, "correct12"),
	}
	svc := newAuthSvc(&stubUserRepo{
		findByEmail: func(_ context.Context, _ string) (*user.User, error) { return u, nil },
	}, &stubBlacklist{})

	_, err := svc.Login(context.Background(), "alice@example.com", "wrongpass")
	if err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
}

func TestLogin_RepoError(t *testing.T) {
	repoErr := errors.New("db down")
	svc := newAuthSvc(&stubUserRepo{
		findByEmail: func(_ context.Context, _ string) (*user.User, error) { return nil, repoErr },
	}, &stubBlacklist{})

	_, err := svc.Login(context.Background(), "a@b.com", "pass1234")
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

func TestRefresh_Success(t *testing.T) {
	tm := token.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubBlacklist{}, 7*24*time.Hour)

	refresh, err := tm.IssueRefresh("sub123", "a@b.com", user.RoleUser)
	if err != nil {
		t.Fatalf("IssueRefresh: %v", err)
	}

	access, err := svc.Refresh(context.Background(), refresh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if access == "" {
		t.Fatal("expected non-empty access token")
	}
}

func TestRefresh_WrongKind(t *testing.T) {
	tm := token.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubBlacklist{}, 7*24*time.Hour)

	// Pass an access token where a refresh token is expected.
	access, _ := tm.IssueAccess("sub", "a@b.com", user.RoleUser)
	_, err := svc.Refresh(context.Background(), access)
	if err == nil {
		t.Fatal("expected error for access token used as refresh, got nil")
	}
}

func TestRefresh_Revoked(t *testing.T) {
	tm := token.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	bl := &stubBlacklist{
		isRevoked: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	svc := authsvc.New(&stubUserRepo{}, tm, bl, 7*24*time.Hour)

	refresh, _ := tm.IssueRefresh("sub", "a@b.com", user.RoleUser)
	_, err := svc.Refresh(context.Background(), refresh)
	if err == nil {
		t.Fatal("expected error for revoked token, got nil")
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

func TestLogout_CallsRevoke(t *testing.T) {
	revoked := false
	bl := &stubBlacklist{
		revoke: func(_ context.Context, _ string, _ time.Duration) error {
			revoked = true
			return nil
		},
	}
	tm := token.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, bl, 7*24*time.Hour)

	if err := svc.Logout(context.Background(), "some-jti"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Fatal("expected Revoke to be called")
	}
}
