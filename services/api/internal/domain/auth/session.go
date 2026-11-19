package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrSessionNotFound means the account has no live session with that id —
// it never existed, already ended, or belongs to someone else. The three are
// indistinguishable on purpose.
var ErrSessionNotFound = errors.New("auth: session not found")

// ClientInfo describes the client a request came from. It is recorded with a
// session so the owner can tell their devices apart.
type ClientInfo struct {
	IP        string
	UserAgent string
}

type clientInfoKey struct{}

// WithClientInfo returns a context carrying the caller's address and
// User-Agent, for IssueSession and Refresh to record.
func WithClientInfo(ctx context.Context, ci ClientInfo) context.Context {
	return context.WithValue(ctx, clientInfoKey{}, ci)
}

// ClientInfoFrom returns the ClientInfo stored by WithClientInfo, or the zero
// value.
func ClientInfoFrom(ctx context.Context) ClientInfo {
	ci, _ := ctx.Value(clientInfoKey{}).(ClientInfo)
	return ci
}

// Session is one signed-in browser or device: a refresh-token family. Its ID
// is the family id carried in the account's tokens.
type Session struct {
	ID         string
	UserID     uuid.UUID
	IP         string
	UserAgent  string
	RememberMe bool
	CreatedAt  time.Time
	// LastActiveAt is when the session last rotated its refresh token, so it
	// lags real activity by up to one access-token lifetime.
	LastActiveAt time.Time
	// Current marks the session the listing request itself belongs to.
	Current bool
}

// SessionStore keeps the registry of live sessions. Revocation itself stays
// with RefreshTokenStore; this only remembers which sessions exist.
type SessionStore interface {
	// Save creates the session, or refreshes IP, User-Agent and LastActiveAt
	// of an existing one (CreatedAt is kept). The entry lives for ttl.
	Save(ctx context.Context, s Session, ttl time.Duration) error
	// Get returns the session, or ErrSessionNotFound.
	Get(ctx context.Context, id string) (*Session, error)
	// ListByUser returns the user's live sessions, most recently active first.
	ListByUser(ctx context.Context, userID uuid.UUID) ([]Session, error)
	// Delete forgets a session. Deleting an unknown id is not an error.
	Delete(ctx context.Context, id string) error
}

// SessionService lets an account see and end its own sessions.
type SessionService interface {
	// ListSessions returns the account's live sessions. currentID marks the
	// one the caller is using.
	ListSessions(ctx context.Context, userID uuid.UUID, currentID string) ([]Session, error)
	// RevokeSession signs one session out everywhere: its refresh and access
	// tokens stop working. ErrSessionNotFound when it is not the account's.
	RevokeSession(ctx context.Context, userID uuid.UUID, sessionID string) error
	// RevokeOtherSessions signs out every session but keepID and reports how
	// many it ended.
	RevokeOtherSessions(ctx context.Context, userID uuid.UUID, keepID string) (int, error)
}

// ParseUserAgent names the browser and operating system a User-Agent claims,
// so a session list can say "Chrome on Linux". It is a best-effort read of a
// client-supplied header, not a fingerprint; either part is empty when not
// recognised.
func ParseUserAgent(userAgent string) (browser, os string) {
	ua := userAgent
	has := func(s string) bool { return strings.Contains(ua, s) }

	switch {
	case ua == "":
		return "", ""
	case has("Edg/") || has("EdgA/") || has("EdgiOS/"):
		browser = "Edge"
	case has("OPR/") || has("Opera"):
		browser = "Opera"
	case has("Firefox/") || has("FxiOS/"):
		browser = "Firefox"
	case has("Chrome/") || has("CriOS/"):
		browser = "Chrome"
	case has("Safari/"):
		browser = "Safari"
	case has("curl/"):
		browser = "curl"
	}

	switch {
	case has("Android"):
		os = "Android"
	case has("iPhone") || has("iPad") || has("iPod"):
		os = "iOS"
	case has("Windows"):
		os = "Windows"
	case has("Mac OS X") || has("Macintosh"):
		os = "macOS"
	case has("CrOS"):
		os = "ChromeOS"
	case has("Linux") || has("X11"):
		os = "Linux"
	}
	return browser, os
}
