package auth

import (
	"context"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
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

	// MFAChallenge, when set, means the password was right but a second
	// factor is still owed: no session exists and every other field is empty.
	// Hand it to CompleteMFALogin with a code to get the real pair.
	MFAChallenge string
}

// LoginThrottle limits wrong-password attempts. Keys are normalised by
// NormalizeLoginKey.
//
// A lockout must not be a lever for locking someone else out, so it is scoped
// two ways. Failures from one client address lock that (key, ip) pair only,
// leaving the real user's own address untouched. Failures from many addresses
// add up per key, which stops a distributed guesser; that second lock applies
// only to addresses that have never completed a sign-in for the key, so the
// account owner's usual locations keep working while it is in force.
type LoginThrottle interface {
	// Check returns how long key is locked for the client at ip, or 0.
	Check(ctx context.Context, key, ip string) (time.Duration, error)
	// Fail records a wrong password; a non-zero result is the lockout this
	// failure started for ip.
	Fail(ctx context.Context, key, ip string) (time.Duration, error)
	// Reset clears the pair's failure count after a correct password.
	Reset(ctx context.Context, key, ip string) error
	// Trust remembers ip as a place key has fully signed in from.
	Trust(ctx context.Context, key, ip string) error
}

// NormalizeLoginKey is the throttle key for a typed username.
func NormalizeLoginKey(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// dummyHash lets a lookup miss cost the same bcrypt work as a wrong password,
// so response time doesn't tell which usernames exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("paca-dummy-password"), bcrypt.DefaultCost)

// BurnPasswordCheck spends one bcrypt comparison against a throwaway hash.
func BurnPasswordCheck(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
}

// LoginInput is everything a password login carries.
type LoginInput struct {
	Username string
	Password string
	// OTP is the second factor: a TOTP code or a recovery code. Empty on the
	// first attempt; required once the account has MFA enabled.
	OTP        string
	RememberMe bool
	// ClientIP is the caller's address, checked against IP allow-lists.
	ClientIP string
}

// Service defines the authentication contract.
type Service interface {
	// Login validates credentials — password, then IP allow-list and second
	// factor where they apply — and returns a fresh token pair.
	// RememberMe=false issues a short-lived session (see JWT_REFRESH_SESSION_TTL);
	// RememberMe=true issues a long-lived persistent session (JWT_REFRESH_TTL).
	// mfadom.ErrRequired asks the caller to retry with LoginInput.OTP set.
	Login(ctx context.Context, in LoginInput) (*TokenPair, error)
	// CompleteMFALogin is the second step of a login that returned a
	// TokenPair with MFAChallenge set: it checks the challenge and the TOTP
	// or recovery code (and the IP allow-lists again) and, if they hold,
	// starts the session Login would have.
	CompleteMFALogin(ctx context.Context, challenge, code, clientIP string) (*TokenPair, error)
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
