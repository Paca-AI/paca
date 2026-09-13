package auth

import (
	"context"
	"time"
)

// TokenPair holds an access token and a companion refresh token.
// RefreshTTL is the effective lifetime of the refresh token so callers (e.g.
// the HTTP handler) can align the cookie MaxAge with the JWT expiry.
//
// AnnotationAccessToken/AnnotationRefreshToken are a second, ScopeAnnotation-
// restricted pair (see that constant's doc comment). Both Login and Refresh
// populate them, from the same identity/family as the main pair, so the
// annotation pair's lifetime piggybacks on ordinary session activity
// instead of requiring the extension to independently keep itself alive —
// see Service.Refresh's own doc comment for why. RefreshAnnotation is the
// complementary, independent path: it validates and rotates whichever
// annotation refresh token the extension itself is holding, for when
// *only* the extension's own activity is keeping the annotation session
// alive (e.g. the main web app tab isn't open at all). Empty on
// RefreshAnnotation's own return — that one never touches the main pair's
// fields.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	RefreshTTL   time.Duration

	AnnotationAccessToken  string
	AnnotationRefreshToken string
}

// Service defines the authentication contract.
type Service interface {
	// Login validates credentials and returns a fresh token pair.
	// rememberMe=false issues a short-lived session (see JWT_REFRESH_SESSION_TTL);
	// rememberMe=true issues a long-lived persistent session (JWT_REFRESH_TTL).
	Login(ctx context.Context, username, password string, rememberMe bool) (*TokenPair, error)
	// Refresh validates a full-scope refresh token and issues a rotated
	// full-scope token pair, plus a freshly reissued (not rotated —
	// see the implementation's own doc comment) ScopeAnnotation pair
	// alongside it. Rejects a ScopeAnnotation refresh token just as
	// RefreshAnnotation rejects a full-scope one — see RefreshAnnotation's
	// doc comment for why the two never accept each other's tokens. Token
	// reuse outside the grace period revokes the entire session family
	// (shared by both scopes, so this also invalidates the annotation pair).
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
	// RefreshAnnotation validates a ScopeAnnotation refresh token and issues
	// a rotated ScopeAnnotation pair (TokenPair's Annotation* fields only —
	// AccessToken/RefreshToken are left empty). Deliberately a separate
	// method rather than a parameter on Refresh: accepting a main refresh
	// token here would let a credential meant only for the browser
	// extension mint a full session, which defeats the point of scoping it
	// down in the first place.
	RefreshAnnotation(ctx context.Context, annotationRefreshToken string) (*TokenPair, error)
	// Logout revokes the entire token family identified by familyID.
	Logout(ctx context.Context, familyID string) error
}
