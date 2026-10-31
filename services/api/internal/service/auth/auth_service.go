// Package auth implements the authentication service.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	mfadom "github.com/Paca-AI/api/internal/domain/mfa"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
)

// mfaChallengeTTL is how long a user has, after entering the right password,
// to supply the second factor before starting over.
const mfaChallengeTTL = 5 * time.Minute

// gracePeriod is the window in which a reused refresh token is treated as a
// concurrent/retry request rather than a stolen token.  The family is NOT
// revoked during this window, but the request is still rejected.
const gracePeriod = 5 * time.Second

// RefreshTokenStore is the persistence contract for refresh-token rotation.
type RefreshTokenStore interface {
	// RecordFirstUse marks jti as used on the first call and returns nil.
	// Subsequent calls return the time of the first use.
	RecordFirstUse(ctx context.Context, jti string, ttl time.Duration) (*time.Time, error)
	// RevokeFamily marks the entire token family as revoked.
	RevokeFamily(ctx context.Context, familyID string, ttl time.Duration) error
	// IsFamilyRevoked returns true when the family has been revoked.
	IsFamilyRevoked(ctx context.Context, familyID string) (bool, error)
}

// LoginGate vets a login whose password already checked out: IP allow-lists
// and the second factor. Nil means password alone is enough.
type LoginGate interface {
	CheckLogin(ctx context.Context, u *userdom.User, otp, clientIP string) error
}

// Service is the concrete implementation of domain/auth.Service.
type Service struct {
	gate              LoginGate
	throttle          domainauth.LoginThrottle
	users             userdom.Repository
	tokens            *jwttoken.Manager
	refreshStore      RefreshTokenStore
	refreshTTL        time.Duration
	refreshSessionTTL time.Duration
}

// New returns a configured auth Service.
func New(users userdom.Repository, tokens *jwttoken.Manager, refreshStore RefreshTokenStore, refreshTTL, refreshSessionTTL time.Duration) *Service {
	return &Service{
		users:             users,
		tokens:            tokens,
		refreshStore:      refreshStore,
		refreshTTL:        refreshTTL,
		refreshSessionTTL: refreshSessionTTL,
	}
}

// WithLoginGate installs the IP allow-list / MFA check run after the password.
func (s *Service) WithLoginGate(g LoginGate) *Service {
	s.gate = g
	return s
}

// WithLoginThrottle installs the per-username wrong-password limiter.
func (s *Service) WithLoginThrottle(t domainauth.LoginThrottle) *Service {
	s.throttle = t
	return s
}

// failLogin counts a wrong username/password. When this failure trips the
// lockout the caller is told so, with the wait; otherwise it is the usual
// ErrInvalidCredentials. A throttle outage never blocks sign-in.
func (s *Service) failLogin(ctx context.Context, key string) error {
	if s.throttle == nil {
		return domainauth.ErrInvalidCredentials
	}
	wait, err := s.throttle.Fail(ctx, key)
	if err != nil {
		slog.Warn("login throttle unavailable", "error", err)
		return domainauth.ErrInvalidCredentials
	}
	if wait > 0 {
		return &mfadom.LockedError{RetryAfter: wait}
	}
	return domainauth.ErrInvalidCredentials
}

// Login validates credentials and returns a fresh token pair.
// When RememberMe is true, the refresh token uses the long-lived TTL
// (JWT_REFRESH_TTL); when false, the shorter session TTL is used
// (JWT_REFRESH_SESSION_TTL, default 24 h).
func (s *Service) Login(ctx context.Context, in domainauth.LoginInput) (*domainauth.TokenPair, error) {
	key := domainauth.NormalizeLoginKey(in.Username)
	if s.throttle != nil {
		// Checked before touching the user table or bcrypt, and for names
		// that don't exist too, so a lockout reveals nothing about accounts.
		wait, terr := s.throttle.Check(ctx, key)
		if terr != nil {
			slog.Warn("login throttle unavailable", "error", terr)
		} else if wait > 0 {
			return nil, &mfadom.LockedError{RetryAfter: wait}
		}
	}

	u, err := s.users.FindByUsername(ctx, in.Username)
	if err != nil {
		if errors.Is(err, userdom.ErrNotFound) {
			return nil, s.failLogin(ctx, key)
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)); err != nil {
		return nil, s.failLogin(ctx, key)
	}
	if s.throttle != nil {
		if rerr := s.throttle.Reset(ctx, key); rerr != nil {
			slog.Warn("login throttle reset failed", "error", rerr)
		}
	}

	// Only after the password is right, so these distinct errors can't be
	// used to probe which usernames exist or how they're protected.
	if s.gate != nil {
		if err := s.gate.CheckLogin(ctx, u, in.OTP, in.ClientIP); err != nil {
			if errors.Is(err, mfadom.ErrRequired) {
				// Password is right, a code is owed. Rather than make the
				// client keep the password around for a retry, hand back a
				// short-lived challenge that only CompleteMFALogin accepts.
				challenge, ierr := s.tokens.IssueMFAChallenge(u.ID.String(), u.Username, in.RememberMe, mfaChallengeTTL)
				if ierr != nil {
					return nil, ierr
				}
				return &domainauth.TokenPair{MFAChallenge: challenge}, nil
			}
			return nil, err
		}
	}

	return s.IssueSession(u, in.RememberMe)
}

// CompleteMFALogin finishes a login that stopped at an MFA challenge.
func (s *Service) CompleteMFALogin(ctx context.Context, challenge, code, clientIP string) (*domainauth.TokenPair, error) {
	claims, err := s.tokens.Verify(challenge)
	if err != nil || claims.Kind != jwttoken.MFAChallengeKind {
		return nil, domainauth.ErrTokenInvalid
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, domainauth.ErrTokenInvalid
	}
	u, err := s.users.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, userdom.ErrNotFound) {
			return nil, domainauth.ErrTokenInvalid
		}
		return nil, err
	}
	if s.gate == nil {
		return nil, domainauth.ErrTokenInvalid
	}
	if err := s.gate.CheckLogin(ctx, u, code, clientIP); err != nil {
		return nil, err
	}
	return s.IssueSession(u, claims.RememberMe)
}

// IssueSession starts a new session for an already-authenticated user: a
// fresh token family with the same token pairs Login returns. Login calls it
// after checking the password; SSO sign-in calls it after the identity
// provider has vouched for the user, so both paths yield identical sessions.
func (s *Service) IssueSession(u *userdom.User, rememberMe bool) (*domainauth.TokenPair, error) {
	familyID := uuid.NewString()
	sub := u.ID.String()

	refreshTTL := s.refreshTTL
	if !rememberMe {
		refreshTTL = s.refreshSessionTTL
	}

	access, err := s.tokens.IssueAccess(sub, u.Username, u.RoleClaim(), familyID, u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueRefreshWithTTL(sub, u.Username, u.RoleClaim(), familyID, rememberMe, refreshTTL)
	if err != nil {
		return nil, err
	}

	// Minted from the same identity/family as the pair above, purely so the
	// browser extension has a working credential from the moment of login —
	// see domainauth.ScopeAnnotation's doc comment for why it's a separate
	// pair rather than reusing the one above.
	annotationAccess, err := s.tokens.IssueAnnotationAccess(sub, u.Username, u.RoleClaim(), familyID, u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	annotationRefresh, err := s.tokens.IssueAnnotationRefreshWithTTL(sub, u.Username, u.RoleClaim(), familyID, rememberMe, refreshTTL)
	if err != nil {
		return nil, err
	}

	return &domainauth.TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		RefreshTTL:   refreshTTL,

		AnnotationAccessToken:  annotationAccess,
		AnnotationRefreshToken: annotationRefresh,
	}, nil
}

// rotateRefreshToken validates refreshToken's rotation state — signature,
// Kind, wantScope, family revocation, and reuse detection — common to both
// Refresh and RefreshAnnotation, and looks up the current user so a caller
// can pick up e.g. a password reset that happened mid-session. Each caller
// still does its own issuance: it's the one thing that legitimately differs
// between the two (full-scope tokens vs. domainauth.ScopeAnnotation ones).
func (s *Service) rotateRefreshToken(ctx context.Context, refreshToken, wantScope string) (*domainauth.Claims, *userdom.User, error) {
	claims, err := s.tokens.Verify(refreshToken)
	if err != nil {
		return nil, nil, domainauth.ErrTokenInvalid
	}

	// The Scope check is what keeps the two rotation paths from accepting
	// each other's tokens — see Service.RefreshAnnotation's doc comment for
	// why a main refresh token minting an annotation pair (or vice versa)
	// would be a problem even though "vice versa" looks harmless at first
	// glance.
	if claims.Kind != "refresh" || claims.Scope != wantScope {
		return nil, nil, domainauth.ErrTokenInvalid
	}

	// Fast path: reject immediately if the family was already invalidated.
	revoked, err := s.refreshStore.IsFamilyRevoked(ctx, claims.FamilyID)
	if err != nil {
		return nil, nil, err
	}
	if revoked {
		return nil, nil, domainauth.ErrSessionInvalidated
	}

	// Record use — detect reuse.
	firstUsedAt, err := s.refreshStore.RecordFirstUse(ctx, claims.ID, s.refreshTTL)
	if err != nil {
		return nil, nil, err
	}

	if firstUsedAt != nil {
		// Token was already used once before.
		if time.Since(*firstUsedAt) <= gracePeriod {
			// Within the grace period: likely a network retry — reject without
			// breaking the session so the original response can be retried.
			return nil, nil, domainauth.ErrTokenInvalid
		}
		// Outside the grace period: potential token theft — revoke the family.
		if err := s.refreshStore.RevokeFamily(ctx, claims.FamilyID, s.refreshTTL); err != nil {
			return nil, nil, fmt.Errorf("auth: revoke session family: %w", err)
		}
		return nil, nil, domainauth.ErrSessionInvalidated
	}

	// Look up the user to get the current MustChangePassword flag; this
	// ensures that if an admin resets the password while the user has an
	// active session, the very next Refresh call will carry the updated flag.
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, nil, domainauth.ErrSessionInvalidated
	}
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, nil, domainauth.ErrSessionInvalidated
	}

	return claims, u, nil
}

// Refresh validates a full-scope refresh token and issues a rotated
// full-scope token pair. If the same token is presented twice outside the
// grace period, the entire session family is revoked to mitigate
// token-theft scenarios.
//
// Also reissues a fresh domainauth.ScopeAnnotation pair on every call (the
// returned TokenPair's Annotation* fields) — piggybacking the annotation
// pair's effective lifetime on ordinary web-app usage, the same "ride the
// main session's cadence" reasoning paca_port/paca_scheme already follow.
// Without this, a user who mostly works in the main app and only
// occasionally opens a forwarded preview could find the extension silently
// logged out the moment more than one refresh-TTL passes between two
// extension-triggered RefreshAnnotation calls, even though their main
// session never stopped being valid.
//
// This is a fresh reissue, not a rotation of whatever annotation refresh
// token the browser is currently holding: annotation_refresh_token's
// cookie Path only ever reaches /auth/annotation-refresh (see
// AuthHandler's annotationRefreshCookiePath), so it never arrives on this
// endpoint's request for Refresh to validate/consume — there is nothing
// here to run reuse detection against. The previously-issued annotation
// refresh token is simply superseded (the handler's new Set-Cookie
// overwrites it in the browser) rather than explicitly revoked; it stays
// cryptographically valid until its own TTL expires, same family as
// everything else, so Logout (which revokes by family) still invalidates
// it same as before. RefreshAnnotation remains the only path with real
// rotation/reuse-detection for the annotation refresh token specifically.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (*domainauth.TokenPair, error) {
	claims, u, err := s.rotateRefreshToken(ctx, refreshToken, "")
	if err != nil {
		return nil, err
	}

	// Issue a rotated token pair preserving the same session family and
	// the original remember-me preference so the TTL is consistent across
	// the entire session lifetime.
	refreshTTL := s.refreshTTL
	if !claims.RememberMe {
		refreshTTL = s.refreshSessionTTL
	}

	// u.RoleClaim() (freshly reloaded above in rotateRefreshToken), not
	// claims.Role (the presented token's own, possibly stale claim) —
	// otherwise a role change never shows up in an already-issued refresh
	// token's claims until the user explicitly logs out. The role claim is
	// informational (what the UI and plugins display): what a caller may do
	// is decided per request from the caller's stored role attachments, never
	// from this name.
	// Mirrors why MustChangePassword is read from the freshly-reloaded u just
	// above.
	access, err := s.tokens.IssueAccess(claims.Subject, claims.Username, u.RoleClaim(), claims.FamilyID, u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueRefreshWithTTL(claims.Subject, claims.Username, u.RoleClaim(), claims.FamilyID, claims.RememberMe, refreshTTL)
	if err != nil {
		return nil, err
	}
	annotationAccess, err := s.tokens.IssueAnnotationAccess(claims.Subject, claims.Username, u.RoleClaim(), claims.FamilyID, u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	annotationRefresh, err := s.tokens.IssueAnnotationRefreshWithTTL(claims.Subject, claims.Username, u.RoleClaim(), claims.FamilyID, claims.RememberMe, refreshTTL)
	if err != nil {
		return nil, err
	}

	return &domainauth.TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		RefreshTTL:   refreshTTL,

		AnnotationAccessToken:  annotationAccess,
		AnnotationRefreshToken: annotationRefresh,
	}, nil
}

// RefreshAnnotation validates a domainauth.ScopeAnnotation refresh token and
// issues a rotated ScopeAnnotation pair. Deliberately not folded into
// Refresh: rotateRefreshToken's wantScope check means a main refresh token
// presented here — or an annotation refresh token presented to Refresh
// above — is rejected outright as ErrTokenInvalid, rather than one letting
// the other mint a token pair it has no business minting. That matters more
// than it might look: the annotation token is deliberately weaker (usable
// only against middleware.AnnotationExtensionPathPattern), so silently
// accepting a main refresh token here would just be a no-op narrowing, but
// the reverse — an annotation refresh token, plausibly exposed to a page
// running arbitrary code on a forwarded environment port, being accepted by
// the *main* Refresh — would let it mint a full-scope session.
func (s *Service) RefreshAnnotation(ctx context.Context, annotationRefreshToken string) (*domainauth.TokenPair, error) {
	claims, u, err := s.rotateRefreshToken(ctx, annotationRefreshToken, domainauth.ScopeAnnotation)
	if err != nil {
		return nil, err
	}

	refreshTTL := s.refreshTTL
	if !claims.RememberMe {
		refreshTTL = s.refreshSessionTTL
	}

	// u.RoleClaim(), not claims.Role — see the identical comment in Refresh above.
	access, err := s.tokens.IssueAnnotationAccess(claims.Subject, claims.Username, u.RoleClaim(), claims.FamilyID, u.MustChangePassword)
	if err != nil {
		return nil, err
	}
	refresh, err := s.tokens.IssueAnnotationRefreshWithTTL(claims.Subject, claims.Username, u.RoleClaim(), claims.FamilyID, claims.RememberMe, refreshTTL)
	if err != nil {
		return nil, err
	}

	return &domainauth.TokenPair{
		RefreshTTL:             refreshTTL,
		AnnotationAccessToken:  access,
		AnnotationRefreshToken: refresh,
	}, nil
}

// Logout revokes the entire token family so all in-flight refresh tokens for
// this session are immediately invalidated.
func (s *Service) Logout(ctx context.Context, familyID string) error {
	if familyID == "" {
		return nil
	}
	return s.refreshStore.RevokeFamily(ctx, familyID, s.refreshTTL)
}
