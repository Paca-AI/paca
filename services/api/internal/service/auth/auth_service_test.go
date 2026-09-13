package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	authsvc "github.com/Paca-AI/api/internal/service/auth"
)

// ---------------------------------------------------------------------------
// stubs
// ---------------------------------------------------------------------------

type stubUserRepo struct {
	findByID       func(ctx context.Context, id uuid.UUID) (*userdom.User, error)
	findByUsername func(ctx context.Context, username string) (*userdom.User, error)
}

func (r *stubUserRepo) FindByID(ctx context.Context, id uuid.UUID) (*userdom.User, error) {
	if r.findByID != nil {
		return r.findByID(ctx, id)
	}
	return nil, userdom.ErrNotFound
}
func (r *stubUserRepo) FindByUsername(ctx context.Context, username string) (*userdom.User, error) {
	if r.findByUsername != nil {
		return r.findByUsername(ctx, username)
	}
	return nil, userdom.ErrNotFound
}
func (r *stubUserRepo) FindByUsernameIncludingDeleted(ctx context.Context, username string) (*userdom.User, error) {
	return r.FindByUsername(ctx, username)
}
func (r *stubUserRepo) FindByEmail(_ context.Context, _ string) (*userdom.User, error) {
	return nil, userdom.ErrNotFound
}
func (r *stubUserRepo) List(_ context.Context, _, _ int) ([]*userdom.User, int64, error) {
	return nil, 0, nil
}
func (r *stubUserRepo) CountUsers(_ context.Context) (int64, error)     { return 0, nil }
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

func TestLogin_IssuesAnnotationPair(t *testing.T) {
	u := &userdom.User{
		ID:           uuid.New(),
		Username:     "alice",
		Role:         userdom.RoleUser,
		PasswordHash: hashedPassword(t, "secret123"),
	}
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{
		findByUsername: func(_ context.Context, _ string) (*userdom.User, error) { return u, nil },
	}, tm, &stubRefreshStore{}, 7*24*time.Hour, 24*time.Hour)

	pair, err := svc.Login(context.Background(), "alice", "secret123", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AnnotationAccessToken == "" || pair.AnnotationRefreshToken == "" {
		t.Fatal("expected non-empty annotation token pair")
	}

	accessClaims, err := tm.Verify(pair.AnnotationAccessToken)
	if err != nil {
		t.Fatalf("verify annotation access token: %v", err)
	}
	if accessClaims.Scope != domainauth.ScopeAnnotation {
		t.Errorf("annotation access token Scope = %q, want %q", accessClaims.Scope, domainauth.ScopeAnnotation)
	}
	if accessClaims.Kind != "access" {
		t.Errorf("annotation access token Kind = %q, want %q", accessClaims.Kind, "access")
	}

	refreshClaims, err := tm.Verify(pair.AnnotationRefreshToken)
	if err != nil {
		t.Fatalf("verify annotation refresh token: %v", err)
	}
	if refreshClaims.Scope != domainauth.ScopeAnnotation {
		t.Errorf("annotation refresh token Scope = %q, want %q", refreshClaims.Scope, domainauth.ScopeAnnotation)
	}

	// The main pair must stay full-scope -- adding the annotation pair
	// must not narrow what Login's original tokens can do.
	mainAccessClaims, err := tm.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("verify main access token: %v", err)
	}
	if mainAccessClaims.Scope != "" {
		t.Errorf("main access token Scope = %q, want empty (full scope)", mainAccessClaims.Scope)
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
	userID := uuid.New()
	u := &userdom.User{
		ID:       userID,
		Username: "alice",
		Role:     userdom.RoleUser,
	}
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	repo := &stubUserRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return u, nil },
	}
	svc := authsvc.New(repo, tm, &stubRefreshStore{
		isFamilyRevoked: func(_ context.Context, _ string) (bool, error) { return false, nil },
		recordFirstUse:  func(_ context.Context, _ string, _ time.Duration) (*time.Time, error) { return nil, nil },
	}, 7*24*time.Hour, 24*time.Hour)

	refresh, err := tm.IssueRefresh(userID.String(), "alice", userdom.RoleUser, "fam1")
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

// TestRefresh_ReissuesAnnotationPair confirms the piggybacking behavior
// this whole change is for: refreshing the main session also hands back a
// fresh, valid domainauth.ScopeAnnotation pair, not just the main one.
func TestRefresh_ReissuesAnnotationPair(t *testing.T) {
	userID := uuid.New()
	u := &userdom.User{ID: userID, Username: "alice", Role: userdom.RoleUser}
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	repo := &stubUserRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return u, nil },
	}
	svc := authsvc.New(repo, tm, &stubRefreshStore{
		isFamilyRevoked: func(_ context.Context, _ string) (bool, error) { return false, nil },
		recordFirstUse:  func(_ context.Context, _ string, _ time.Duration) (*time.Time, error) { return nil, nil },
	}, 7*24*time.Hour, 24*time.Hour)

	refresh, err := tm.IssueRefresh(userID.String(), "alice", userdom.RoleUser, "fam1")
	if err != nil {
		t.Fatalf("IssueRefresh: %v", err)
	}

	pair, err := svc.Refresh(context.Background(), refresh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AnnotationAccessToken == "" || pair.AnnotationRefreshToken == "" {
		t.Fatal("expected a freshly reissued annotation token pair")
	}

	accessClaims, err := tm.Verify(pair.AnnotationAccessToken)
	if err != nil {
		t.Fatalf("verify annotation access token: %v", err)
	}
	if accessClaims.Scope != domainauth.ScopeAnnotation {
		t.Errorf("annotation access token Scope = %q, want %q", accessClaims.Scope, domainauth.ScopeAnnotation)
	}
	if accessClaims.Subject != userID.String() {
		t.Errorf("annotation access token Subject = %q, want %q", accessClaims.Subject, userID.String())
	}

	refreshClaims, err := tm.Verify(pair.AnnotationRefreshToken)
	if err != nil {
		t.Fatalf("verify annotation refresh token: %v", err)
	}
	if refreshClaims.FamilyID != "fam1" {
		t.Errorf("annotation refresh token FamilyID = %q, want %q (must share the main pair's family so Logout revokes both)", refreshClaims.FamilyID, "fam1")
	}
}

// TestRefresh_ReflectsRoleChange guards against a regression where the
// rotated tokens carried the presented refresh token's own (possibly stale)
// Role claim instead of the freshly-reloaded user's current role — see
// rotateRefreshToken's doc comment on why the user row is reloaded on every
// refresh in the first place, and why that must apply to Role, not just
// MustChangePassword: authz.LegacyPermissionsForRole grants a full wildcard
// for a role named "ADMIN"/"SUPER_ADMIN", so a demotion that doesn't take
// effect on refresh is a live privilege-revocation bug, not just staleness.
func TestRefresh_ReflectsRoleChange(t *testing.T) {
	userID := uuid.New()
	u := &userdom.User{ID: userID, Username: "alice", Role: userdom.RoleAdmin}
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	repo := &stubUserRepo{
		// Simulates an admin demoting this user to USER in between the
		// refresh token being issued and it being presented here.
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return u, nil },
	}
	svc := authsvc.New(repo, tm, &stubRefreshStore{
		isFamilyRevoked: func(_ context.Context, _ string) (bool, error) { return false, nil },
		recordFirstUse:  func(_ context.Context, _ string, _ time.Duration) (*time.Time, error) { return nil, nil },
	}, 7*24*time.Hour, 24*time.Hour)

	// Issue the refresh token while the user is still ADMIN...
	refresh, err := tm.IssueRefresh(userID.String(), "alice", userdom.RoleAdmin, "fam1")
	if err != nil {
		t.Fatalf("IssueRefresh: %v", err)
	}
	// ...then demote them before it's ever redeemed.
	u.Role = userdom.RoleUser

	pair, err := svc.Refresh(context.Background(), refresh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	accessClaims, err := tm.Verify(pair.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if accessClaims.Role != userdom.RoleUser {
		t.Errorf("access token Role = %q, want %q (the demotion must take effect immediately, not just after re-login)", accessClaims.Role, userdom.RoleUser)
	}

	refreshClaims, err := tm.Verify(pair.RefreshToken)
	if err != nil {
		t.Fatalf("verify refresh token: %v", err)
	}
	if refreshClaims.Role != userdom.RoleUser {
		t.Errorf("rotated refresh token Role = %q, want %q", refreshClaims.Role, userdom.RoleUser)
	}

	annotationAccessClaims, err := tm.Verify(pair.AnnotationAccessToken)
	if err != nil {
		t.Fatalf("verify annotation access token: %v", err)
	}
	if annotationAccessClaims.Role != userdom.RoleUser {
		t.Errorf("annotation access token Role = %q, want %q", annotationAccessClaims.Role, userdom.RoleUser)
	}
}

// TestRefreshAnnotation_ReflectsRoleChange is TestRefresh_ReflectsRoleChange's
// sibling for the ScopeAnnotation rotation path, which had the identical bug.
func TestRefreshAnnotation_ReflectsRoleChange(t *testing.T) {
	userID := uuid.New()
	u := &userdom.User{ID: userID, Username: "alice", Role: userdom.RoleAdmin}
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	repo := &stubUserRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return u, nil },
	}
	svc := authsvc.New(repo, tm, &stubRefreshStore{
		isFamilyRevoked: func(_ context.Context, _ string) (bool, error) { return false, nil },
		recordFirstUse:  func(_ context.Context, _ string, _ time.Duration) (*time.Time, error) { return nil, nil },
	}, 7*24*time.Hour, 24*time.Hour)

	annotationRefresh, err := tm.IssueAnnotationRefreshWithTTL(userID.String(), "alice", userdom.RoleAdmin, "fam1", true, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("IssueAnnotationRefreshWithTTL: %v", err)
	}
	u.Role = userdom.RoleUser

	pair, err := svc.RefreshAnnotation(context.Background(), annotationRefresh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	accessClaims, err := tm.Verify(pair.AnnotationAccessToken)
	if err != nil {
		t.Fatalf("verify annotation access token: %v", err)
	}
	if accessClaims.Role != userdom.RoleUser {
		t.Errorf("annotation access token Role = %q, want %q", accessClaims.Role, userdom.RoleUser)
	}
}

func TestRefresh_WrongKind(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubRefreshStore{}, 7*24*time.Hour, 24*time.Hour)

	// Pass an access token where a refresh token is expected.
	access, _ := tm.IssueAccess("sub", "alice", userdom.RoleUser, "fam1", false)
	_, err := svc.Refresh(context.Background(), access)
	if !errors.Is(err, domainauth.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

// TestRefresh_RejectsAnnotationScopedToken is the core security property of
// the whole ScopeAnnotation design: a token minted only for the browser
// extension's narrow annotation flow must never be usable to mint a
// full-scope session. Without this check, rotateRefreshToken's wantScope
// argument would be decorative.
func TestRefresh_RejectsAnnotationScopedToken(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubRefreshStore{}, 7*24*time.Hour, 24*time.Hour)

	annotationRefresh, err := tm.IssueAnnotationRefreshWithTTL("sub", "alice", userdom.RoleUser, "fam1", true, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("IssueAnnotationRefreshWithTTL: %v", err)
	}

	_, err = svc.Refresh(context.Background(), annotationRefresh)
	if !errors.Is(err, domainauth.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for an annotation-scoped token, got %v", err)
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
// RefreshAnnotation
// ---------------------------------------------------------------------------

func TestRefreshAnnotation_Success(t *testing.T) {
	userID := uuid.New()
	u := &userdom.User{ID: userID, Username: "alice", Role: userdom.RoleUser}
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	repo := &stubUserRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return u, nil },
	}
	svc := authsvc.New(repo, tm, &stubRefreshStore{
		isFamilyRevoked: func(_ context.Context, _ string) (bool, error) { return false, nil },
		recordFirstUse:  func(_ context.Context, _ string, _ time.Duration) (*time.Time, error) { return nil, nil },
	}, 7*24*time.Hour, 24*time.Hour)

	refresh, err := tm.IssueAnnotationRefreshWithTTL(userID.String(), "alice", userdom.RoleUser, "fam1", true, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("IssueAnnotationRefreshWithTTL: %v", err)
	}

	pair, err := svc.RefreshAnnotation(context.Background(), refresh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pair.AnnotationAccessToken == "" || pair.AnnotationRefreshToken == "" {
		t.Fatal("expected non-empty rotated annotation token pair")
	}
	// Must not touch the main pair's fields at all.
	if pair.AccessToken != "" || pair.RefreshToken != "" {
		t.Errorf("expected empty main token fields, got AccessToken=%q RefreshToken=%q", pair.AccessToken, pair.RefreshToken)
	}

	claims, err := tm.Verify(pair.AnnotationAccessToken)
	if err != nil {
		t.Fatalf("verify rotated annotation access token: %v", err)
	}
	if claims.Scope != domainauth.ScopeAnnotation {
		t.Errorf("rotated access token Scope = %q, want %q", claims.Scope, domainauth.ScopeAnnotation)
	}
}

// TestRefreshAnnotation_RejectsMainScopedToken is Refresh's cross-rejection
// property, mirrored: a full-scope refresh token must not be accepted here
// either. Not a privilege-escalation risk the way the reverse is (a full
// token can already do everything an annotation token can), but accepting
// it would blur the two rotation paths' independence -- e.g. logging out
// only the annotation session should never be possible by design, and
// letting either endpoint accept the other's token is a step toward that.
func TestRefreshAnnotation_RejectsMainScopedToken(t *testing.T) {
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	svc := authsvc.New(&stubUserRepo{}, tm, &stubRefreshStore{}, 7*24*time.Hour, 24*time.Hour)

	mainRefresh, err := tm.IssueRefreshWithTTL("sub", "alice", userdom.RoleUser, "fam1", true, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("IssueRefreshWithTTL: %v", err)
	}

	_, err = svc.RefreshAnnotation(context.Background(), mainRefresh)
	if !errors.Is(err, domainauth.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid for a main-scoped token, got %v", err)
	}
}

func TestRefreshAnnotation_FamilyRevoked(t *testing.T) {
	// The annotation pair shares its family with the main pair (see
	// Service.Login) specifically so that Logout -- which revokes by family
	// -- kills both. This confirms RefreshAnnotation actually honors that
	// shared revocation rather than checking some separate state.
	tm := jwttoken.New("test-secret", 15*time.Minute, 7*24*time.Hour)
	store := &stubRefreshStore{
		isFamilyRevoked: func(_ context.Context, _ string) (bool, error) { return true, nil },
	}
	svc := authsvc.New(&stubUserRepo{}, tm, store, 7*24*time.Hour, 24*time.Hour)

	refresh, _ := tm.IssueAnnotationRefreshWithTTL("sub", "alice", userdom.RoleUser, "fam1", true, 7*24*time.Hour)
	_, err := svc.RefreshAnnotation(context.Background(), refresh)
	if !errors.Is(err, domainauth.ErrSessionInvalidated) {
		t.Fatalf("expected ErrSessionInvalidated, got %v", err)
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

	userID := uuid.New()
	stubUser := &userdom.User{ID: userID, Username: "alice", Role: userdom.RoleUser}
	tm := jwttoken.New("test-secret", 15*time.Minute, refreshTTL)
	svc := authsvc.New(&stubUserRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return stubUser, nil },
	}, tm, &stubRefreshStore{}, refreshTTL, sessionTTL)

	// Issue a persistent-session refresh token.
	origRefresh, err := tm.IssueRefreshWithTTL(userID.String(), "alice", userdom.RoleUser, "fam1", true, refreshTTL)
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

	userID := uuid.New()
	stubUser := &userdom.User{ID: userID, Username: "alice", Role: userdom.RoleUser}
	tm := jwttoken.New("test-secret", 15*time.Minute, refreshTTL)
	svc := authsvc.New(&stubUserRepo{
		findByID: func(_ context.Context, _ uuid.UUID) (*userdom.User, error) { return stubUser, nil },
	}, tm, &stubRefreshStore{}, refreshTTL, sessionTTL)

	// Issue a session-only refresh token.
	origRefresh, err := tm.IssueRefreshWithTTL(userID.String(), "alice", userdom.RoleUser, "fam1", false, sessionTTL)
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
