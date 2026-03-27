package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	domainauth "github.com/paca/api/internal/domain/auth"
	userdom "github.com/paca/api/internal/domain/user"
	jwttoken "github.com/paca/api/internal/platform/token"
	authsvc "github.com/paca/api/internal/service/auth"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// stubs
// ---------------------------------------------------------------------------

type stubUserRepo struct {
	findByUsername func(ctx context.Context, username string) (*userdom.User, error)
}

func (r *stubUserRepo) FindByID(_ context.Context, _ uuid.UUID) (*userdom.User, error) {
	return nil, userdom.ErrNotFound
}
func (r *stubUserRepo) FindByUsername(ctx context.Context, username string) (*userdom.User, error) {
	if r.findByUsername != nil {
		return r.findByUsername(ctx, username)
	}
	return nil, userdom.ErrNotFound
}
func (r *stubUserRepo) Create(_ context.Context, _ *userdom.User) error { return nil }
func (r *stubUserRepo) Update(_ context.Context, _ *userdom.User) error { return nil }
func (r *stubUserRepo) Delete(_ context.Context, _ uuid.UUID) error     { return nil }

type stubRefreshStore struct {
	recordFirstUse  func(ctx context.Context, jti string, ttl time.Duration) (*time.Time, error)
	revokeFamily    func(ctx context.Context, familyID string, ttl time.Duration) error
	isFamilyRevoked func(ctx context.Context, familyID string) (bool, error)
}

func (s *stubRefreshStore) RecordFirstUse(ctx context.Context, jti string, ttl time.Duration) (*time.Time, error) {
	if s.recordFirstUse != nil {
		return s.recordFirstUse(ctx, jti, ttl)
	}
	return nil, nil // first use by default
}
func (s *stubRefreshStore) RevokeFamily(ctx context.Context, familyID string, ttl time.Duration) error {
	if s.revokeFamily != nil {
		return s.revokeFamily(ctx, familyID, ttl)
	}
	return nil
}
func (s *stubRefreshStore) IsFamilyRevoked(ctx context.Context, familyID string) (bool, error) {
	if s.isFamilyRevoked != nil {
		return s.isFamilyRevoked(ctx, familyID)
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

func newAuthSvc(repo *stubUserRepo, store *stubRefreshStore) *authsvc.Service {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	return authsvc.New(repo, tm, store, 7*24*time.Hour, 24*time.Hour)
}

// verify that *authsvc.Service satisfies the domain interface
var _ domainauth.Service = (*authsvc.Service)(nil)

// ---------------------------------------------------------------------------
// Login
// ---------------------------------------------------------------------------

func TestLogin_Success(t *testing.T) {
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "alice",
		Role:         userdom.RoleUser,
		PasswordHash: hashedPassword(t, "secret123"),
	}
	svc := newAuthSvc(&stubUserRepo{
		findByUsername: func(_ context.Context, _ string) (*userdom.User, error) { return u, nil },
	}, &stubRefreshStore{})

	pair, err := svc.Login(context.Background(), "alice", "secret123", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected non-empty token pair")
	}
}

func TestLogin_UserNotFound(t *testing.T) {
	svc := newAuthSvc(&stubUserRepo{}, &stubRefreshStore{})
	_, err := svc.Login(context.Background(), "ghost", "pass1234", true)
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "alice",
		Role:         userdom.RoleUser,
		PasswordHash: hashedPassword(t, "correct12"),
	}
	svc := newAuthSvc(&stubUserRepo{
		findByUsername: func(_ context.Context, _ string) (*userdom.User, error) { return u, nil },
	}, &stubRefreshStore{})

	_, err := svc.Login(context.Background(), "alice", "wrongpass", true)
	if !errors.Is(err, domainauth.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_RepoError(t *testing.T) {
	repoErr := errors.New("db down")
	svc := newAuthSvc(&stubUserRepo{
		findByUsername: func(_ context.Context, _ string) (*userdom.User, error) { return nil, repoErr },
	}, &stubRefreshStore{})

	_, err := svc.Login(context.Background(), "alice", "pass1234", true)
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Refresh
// ---------------------------------------------------------------------------

func TestRefresh_Success(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubRefreshStore{}, 7*24*time.Hour, 24*time.Hour)

	refresh, err := tm.IssueRefresh("sub123", "alice", userdom.RoleUser, "fam1")
	if err != nil {
		t.Fatalf("IssueRefresh: %v", err)
	}

	pair, err := svc.Refresh(context.Background(), refresh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected non-empty token pair")
	}
}

func TestRefresh_WrongKind(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubRefreshStore{}, 7*24*time.Hour, 24*time.Hour)

	// Pass an access token where a refresh token is expected.
	access, _ := tm.IssueAccess("sub", "alice", userdom.RoleUser, "fam1")
	_, err := svc.Refresh(context.Background(), access)
	if !errors.Is(err, domainauth.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestRefresh_FamilyRevoked(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	store := &stubRefreshStore{
		isFamilyRevoked: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	svc := authsvc.New(&stubUserRepo{}, tm, store, 7*24*time.Hour, 24*time.Hour)

	refresh, _ := tm.IssueRefresh("sub", "alice", userdom.RoleUser, "fam1")
	_, err := svc.Refresh(context.Background(), refresh)
	if !errors.Is(err, domainauth.ErrSessionInvalidated) {
		t.Fatalf("expected ErrSessionInvalidated, got %v", err)
	}
}

func TestRefresh_ReuseWithinGrace_RejectsWithoutRevokingFamily(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)

	familyRevoked := false
	// Simulate a token that was already used just 1 second ago.
	usedAt := time.Now().Add(-1 * time.Second)
	store := &stubRefreshStore{
		recordFirstUse: func(_ context.Context, _ string, _ time.Duration) (*time.Time, error) {
			return &usedAt, nil // already used
		},
		revokeFamily: func(_ context.Context, _ string, _ time.Duration) error {
			familyRevoked = true
			return nil
		},
	}
	svc := authsvc.New(&stubUserRepo{}, tm, store, 7*24*time.Hour, 24*time.Hour)

	refresh, _ := tm.IssueRefresh("sub", "alice", userdom.RoleUser, "fam1")
	_, err := svc.Refresh(context.Background(), refresh)
	if !errors.Is(err, domainauth.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
	if familyRevoked {
		t.Fatal("family must NOT be revoked within the grace period")
	}
}

func TestRefresh_ReuseOutsideGrace_RevokesFamily(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)

	familyRevoked := false
	// Simulate token used 10 seconds ago (outside the 5s grace period).
	usedAt := time.Now().Add(-10 * time.Second)
	store := &stubRefreshStore{
		recordFirstUse: func(_ context.Context, _ string, _ time.Duration) (*time.Time, error) {
			return &usedAt, nil
		},
		revokeFamily: func(_ context.Context, _ string, _ time.Duration) error {
			familyRevoked = true
			return nil
		},
	}
	svc := authsvc.New(&stubUserRepo{}, tm, store, 7*24*time.Hour, 24*time.Hour)

	refresh, _ := tm.IssueRefresh("sub", "alice", userdom.RoleUser, "fam1")
	_, err := svc.Refresh(context.Background(), refresh)
	if !errors.Is(err, domainauth.ErrSessionInvalidated) {
		t.Fatalf("expected ErrSessionInvalidated, got %v", err)
	}
	if !familyRevoked {
		t.Fatal("family must be revoked when reuse is detected outside grace period")
	}
}

func TestRefresh_ReuseOutsideGrace_RevokeFamilyFailure(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)

	revokeErr := errors.New("redis unavailable")
	// Simulate token used 10 seconds ago (outside the 5s grace period).
	usedAt := time.Now().Add(-10 * time.Second)
	store := &stubRefreshStore{
		recordFirstUse: func(_ context.Context, _ string, _ time.Duration) (*time.Time, error) {
			return &usedAt, nil
		},
		revokeFamily: func(_ context.Context, _ string, _ time.Duration) error {
			return revokeErr
		},
	}
	svc := authsvc.New(&stubUserRepo{}, tm, store, 7*24*time.Hour, 24*time.Hour)

	refresh, _ := tm.IssueRefresh("sub", "alice", userdom.RoleUser, "fam1")
	_, err := svc.Refresh(context.Background(), refresh)
	if err == nil {
		t.Fatal("expected error when family revocation fails")
	}
	if !errors.Is(err, revokeErr) {
		t.Fatalf("expected revoke error to be wrapped, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Logout
// ---------------------------------------------------------------------------

func TestLogout_RevokesFamily(t *testing.T) {
	revoked := false
	store := &stubRefreshStore{
		revokeFamily: func(_ context.Context, _ string, _ time.Duration) error {
			revoked = true
			return nil
		},
	}
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, store, 7*24*time.Hour, 24*time.Hour)

	if err := svc.Logout(context.Background(), "some-family-id"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !revoked {
		t.Fatal("expected RevokeFamily to be called")
	}
}

func TestLogout_EmptyFamilyID_NoOp(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubRefreshStore{}, 7*24*time.Hour, 24*time.Hour)
	if err := svc.Logout(context.Background(), ""); err != nil {
		t.Fatalf("unexpected error for empty familyID: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Remember Me — Login TTL selection
// ---------------------------------------------------------------------------

func TestLogin_RememberMe_True_UsesLongTTL(t *testing.T) {
	const refreshTTL = 7 * 24 * time.Hour
	const sessionTTL = 24 * time.Hour

	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "alice",
		Role:         userdom.RoleUser,
		PasswordHash: hashedPassword(t, "secret123"),
	}
	tm := jwttoken.New("test-secret", 15*time.Minute, refreshTTL)
	svc := authsvc.New(&stubUserRepo{
		findByUsername: func(_ context.Context, _ string) (*userdom.User, error) { return u, nil },
	}, tm, &stubRefreshStore{}, refreshTTL, sessionTTL)

	pair, err := svc.Login(context.Background(), "alice", "secret123", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.RefreshTTL != refreshTTL {
		t.Errorf("expected RefreshTTL=%v (long), got %v", refreshTTL, pair.RefreshTTL)
	}

	// Confirm RememberMe is embedded in the refresh token JWT.
	claims, err := tm.Verify(pair.RefreshToken)
	if err != nil {
		t.Fatalf("verify refresh token: %v", err)
	}
	if !claims.RememberMe {
		t.Error("expected RememberMe=true in refresh token claims")
	}
}

func TestLogin_RememberMe_False_UsesSessionTTL(t *testing.T) {
	const refreshTTL = 7 * 24 * time.Hour
	const sessionTTL = 24 * time.Hour

	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "alice",
		Role:         userdom.RoleUser,
		PasswordHash: hashedPassword(t, "secret123"),
	}
	tm := jwttoken.New("test-secret", 15*time.Minute, refreshTTL)
	svc := authsvc.New(&stubUserRepo{
		findByUsername: func(_ context.Context, _ string) (*userdom.User, error) { return u, nil },
	}, tm, &stubRefreshStore{}, refreshTTL, sessionTTL)

	pair, err := svc.Login(context.Background(), "alice", "secret123", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.RefreshTTL != sessionTTL {
		t.Errorf("expected RefreshTTL=%v (session), got %v", sessionTTL, pair.RefreshTTL)
	}

	// Confirm RememberMe=false is embedded in the refresh token JWT.
	claims, err := tm.Verify(pair.RefreshToken)
	if err != nil {
		t.Fatalf("verify refresh token: %v", err)
	}
	if claims.RememberMe {
		t.Error("expected RememberMe=false in refresh token claims")
	}
}

// ---------------------------------------------------------------------------
// Remember Me — Refresh rotation preserves preference
// ---------------------------------------------------------------------------

func TestRefresh_RememberMe_True_PreservesLongTTL(t *testing.T) {
	const refreshTTL = 7 * 24 * time.Hour
	const sessionTTL = 24 * time.Hour

	tm := jwttoken.New("test-secret", 15*time.Minute, refreshTTL)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubRefreshStore{}, refreshTTL, sessionTTL)

	// Issue a persistent-session refresh token.
	origRefresh, err := tm.IssueRefreshWithTTL("sub", "alice", userdom.RoleUser, "fam1", true, refreshTTL)
	if err != nil {
		t.Fatalf("IssueRefreshWithTTL: %v", err)
	}

	pair, err := svc.Refresh(context.Background(), origRefresh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.RefreshTTL != refreshTTL {
		t.Errorf("expected rotated RefreshTTL=%v, got %v", refreshTTL, pair.RefreshTTL)
	}

	// Confirm the rotated token carries RememberMe=true.
	claims, err := tm.Verify(pair.RefreshToken)
	if err != nil {
		t.Fatalf("verify rotated token: %v", err)
	}
	if !claims.RememberMe {
		t.Error("expected RememberMe=true to be preserved through rotation")
	}
}

func TestRefresh_RememberMe_False_PreservesSessionTTL(t *testing.T) {
	const refreshTTL = 7 * 24 * time.Hour
	const sessionTTL = 24 * time.Hour

	tm := jwttoken.New("test-secret", 15*time.Minute, refreshTTL)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubRefreshStore{}, refreshTTL, sessionTTL)

	// Issue a session-only refresh token.
	origRefresh, err := tm.IssueRefreshWithTTL("sub", "alice", userdom.RoleUser, "fam1", false, sessionTTL)
	if err != nil {
		t.Fatalf("IssueRefreshWithTTL: %v", err)
	}

	pair, err := svc.Refresh(context.Background(), origRefresh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.RefreshTTL != sessionTTL {
		t.Errorf("expected rotated RefreshTTL=%v, got %v", sessionTTL, pair.RefreshTTL)
	}

	// Confirm the rotated token carries RememberMe=false.
	claims, err := tm.Verify(pair.RefreshToken)
	if err != nil {
		t.Fatalf("verify rotated token: %v", err)
	}
	if claims.RememberMe {
		t.Error("expected RememberMe=false to be preserved through rotation")
	}
}
