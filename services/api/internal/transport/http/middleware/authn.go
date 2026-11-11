// Package middleware provides per-route HTTP middleware for authentication and
// authorization.
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	accountdom "github.com/Paca-AI/api/internal/domain/account"
	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	"github.com/Paca-AI/api/internal/events"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// claimsContextKey is the unexported key used to store JWT claims in the Go request context.
type claimsContextKey struct{}

// principalContextKey is the unexported key used to store the authenticated
// Principal in the Go request context.
type principalContextKey struct{}

// Authentication methods a Principal can carry.
const (
	// MethodSession is a signed access token (cookie or Bearer header) minted
	// by Login for a user.
	MethodSession = "session"
	// MethodAPIKey is a stored API key looked up by its hash.
	MethodAPIKey = "apikey"
)

// Principal is who a request is authenticated as: one account, whatever the
// credential was. Every handler and gate derives the caller from it, so there
// is exactly one place (applyAuthn) that decides who is asking.
type Principal struct {
	// AccountID identifies the caller: the id of its users / agents row, or of
	// the service account.
	AccountID uuid.UUID
	// Kind is the kind of that account.
	Kind accountdom.Kind
	// Method is how the caller authenticated (MethodSession or MethodAPIKey).
	Method string
}

// APIKeyAuthenticator validates a raw API key string and returns the key record.
// It is satisfied by apikeysvc.Service.
type APIKeyAuthenticator interface {
	Authenticate(ctx context.Context, rawKey string) (*apikeydom.APIKey, error)
}

// Authn validates the access JWT and stores the authenticated Principal (and
// the parsed claims) in the request context so service-layer code can access
// them without depending on the HTTP layer.
// It first checks the access_token HttpOnly cookie, then falls back to the
// Authorization: Bearer header for API/CLI clients, and finally accepts
// Authorization: ApiKey or X-API-Key headers (stored API keys: a user's, a
// service account's or an agent's own).
func Authn(tm *jwttoken.Manager, apiKeyAuth ...APIKeyAuthenticator) func(http.Handler) http.Handler {
	var apiKeyAuthenticator APIKeyAuthenticator
	if len(apiKeyAuth) > 0 {
		apiKeyAuthenticator = apiKeyAuth[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, ok := EnforceAuthn(w, r, tm, apiKeyAuthenticator)
			if !ok {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// OptionalAuthn tries to authenticate the request using the same credential
// sources as Authn (cookie → Bearer → API key), but does NOT abort if no
// credentials are present. Downstream handlers must check ClaimsFrom for nil
// to determine whether the caller is authenticated.
func OptionalAuthn(tm *jwttoken.Manager, apiKeyAuth ...APIKeyAuthenticator) func(http.Handler) http.Handler {
	var apiKeyAuthenticator APIKeyAuthenticator
	if len(apiKeyAuth) > 0 {
		apiKeyAuthenticator = apiKeyAuth[0]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r, ok := EnforceOptionalAuthn(w, r, tm, apiKeyAuthenticator)
			if !ok {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// EnforceAuthn validates credentials and sets auth context.
// Returns the updated request (with auth context values) and whether to continue.
func EnforceAuthn(w http.ResponseWriter, r *http.Request, tm *jwttoken.Manager, apiKeyAuth ...APIKeyAuthenticator) (*http.Request, bool) {
	var apiKeyAuthenticator APIKeyAuthenticator
	if len(apiKeyAuth) > 0 {
		apiKeyAuthenticator = apiKeyAuth[0]
	}
	return applyAuthn(w, r, tm, apiKeyAuthenticator, false)
}

// EnforceOptionalAuthn validates optional credentials and sets auth context.
func EnforceOptionalAuthn(w http.ResponseWriter, r *http.Request, tm *jwttoken.Manager, apiKeyAuth ...APIKeyAuthenticator) (*http.Request, bool) {
	var apiKeyAuthenticator APIKeyAuthenticator
	if len(apiKeyAuth) > 0 {
		apiKeyAuthenticator = apiKeyAuth[0]
	}
	return applyAuthn(w, r, tm, apiKeyAuthenticator, true)
}

func applyAuthn(w http.ResponseWriter, r *http.Request, tm *jwttoken.Manager, apiKeyAuthenticator APIKeyAuthenticator, optional bool) (*http.Request, bool) {
	tokenStr := ""
	isAPIKey := false

	if cookie, err := r.Cookie("access_token"); err == nil && cookie.Value != "" {
		tokenStr = cookie.Value
	}
	if tokenStr == "" {
		// Falls back to the browser extension's own, narrower-scoped
		// cookie (see domainauth.ScopeAnnotation) — present whenever
		// access_token itself isn't, e.g. because the extension's
		// forwarded-preview page and this API differ in scheme, so
		// SameSite withholds access_token but not this SameSite=None one.
		// The Scope check below is what stops a token that arrived this
		// way from authenticating anything but
		// AnnotationExtensionPathPattern. Literal name must match
		// handler.annotationAccessCookieName.
		if cookie, err := r.Cookie("annotation_access_token"); err == nil && cookie.Value != "" {
			tokenStr = cookie.Value
		}
	}
	if tokenStr == "" {
		header := r.Header.Get("Authorization")
		if header != "" {
			parts := strings.SplitN(header, " ", 2)
			if len(parts) == 2 {
				switch strings.ToLower(parts[0]) {
				case "bearer":
					tokenStr = parts[1]
				case "apikey":
					tokenStr = parts[1]
					isAPIKey = true
				}
			}
		}
	}
	if tokenStr == "" {
		if v := r.Header.Get("X-API-Key"); v != "" {
			tokenStr = v
			isAPIKey = true
		}
	}

	if tokenStr == "" {
		if optional {
			return r, true
		}
		presenter.Error(w, r, apierr.New(apierr.CodeMissingToken, "missing authentication"))
		return r, false
	}

	if isAPIKey {
		if apiKeyAuthenticator == nil {
			if optional {
				return r, true
			}
			presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "API key authentication not configured"))
			return r, false
		}
		key, err := apiKeyAuthenticator.Authenticate(r.Context(), tokenStr)
		if err != nil {
			// OptionalAuthn's contract is "never abort on bad credentials".
			if optional {
				return r, true
			}
			switch {
			case errors.Is(err, apikeydom.ErrRevoked):
				presenter.Error(w, r, apierr.New(apierr.CodeAPIKeyRevoked, "API key has been revoked"))
			case errors.Is(err, apikeydom.ErrExpired):
				presenter.Error(w, r, apierr.New(apierr.CodeAPIKeyExpired, "API key has expired"))
			default:
				presenter.Error(w, r, apierr.New(apierr.CodeTokenInvalid, "invalid or expired API key"))
			}
			return r, false
		}
		p := Principal{AccountID: key.AccountID, Kind: key.AccountKind, Method: MethodAPIKey}
		return withPrincipal(r, p, nil), true
	}

	claims, err := tm.Verify(tokenStr)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeTokenInvalid, "invalid or expired token"))
		return r, false
	}
	if claims.Kind != "access" {
		presenter.Error(w, r, apierr.New(apierr.CodeTokenInvalid, "expected access token"))
		return r, false
	}
	if !enforceTokenScope(claims, r.URL.Path) {
		// A token whose Scope doesn't permit this path must never
		// authenticate the request, even though it's a real, validly-
		// signed token for a real user — see scope.go for the full policy.
		// Same rejection whether this call came through Authn or
		// OptionalAuthn: an out-of-scope credential is not "no
		// credential", it's a credential this endpoint must actively
		// refuse.
		presenter.Error(w, r, apierr.New(apierr.CodeTokenInvalid, "token is not valid for this endpoint"))
		return r, false
	}
	p, ok := principalFromClaims(claims)
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeTokenInvalid, "invalid subject claim"))
		return r, false
	}
	return withPrincipal(r, p, claims), true
}

// withPrincipal stores p (and the claims the handlers read) in the request
// context. claims is nil for an API-key caller, for which a minimal set is
// synthesised from the principal.
func withPrincipal(r *http.Request, p Principal, claims *domainauth.Claims) *http.Request {
	if claims == nil {
		claims = &domainauth.Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: p.AccountID.String()},
			Kind:             "access",
		}
	}
	ctx := context.WithValue(r.Context(), claimsContextKey{}, claims)
	ctx = context.WithValue(ctx, principalContextKey{}, p)
	origin := events.OriginUser
	if p.Kind == accountdom.KindAgent {
		origin = events.OriginAgent
	}
	ctx = events.WithRequestActor(ctx, p.AccountID, origin)
	// A browser stamps its mutations so it can skip the echo of its own change.
	ctx = events.WithClientOp(ctx, r.Header.Get(events.ClientOpHeader))
	// And names its realtime socket, so a live collaborative editor's own save
	// is not merged back into its room (see services/realtime collab.ts).
	ctx = events.WithCollabSocket(ctx, r.Header.Get(events.CollabSocketHeader))
	return r.WithContext(ctx)
}

// PrincipalFrom returns the authenticated Principal, if any.
func PrincipalFrom(r *http.Request) (Principal, bool) {
	return PrincipalFromContext(r.Context())
}

// principalFromClaims derives the Principal a verified session token stands
// for: the subject is the user's account id.
func principalFromClaims(c *domainauth.Claims) (Principal, bool) {
	id, err := uuid.Parse(c.Subject)
	if err != nil {
		return Principal{}, false
	}
	return Principal{AccountID: id, Kind: accountdom.KindUser, Method: MethodSession}, true
}

// PrincipalFromContext is PrincipalFrom for a bare context. A context that
// carries verified claims but no stored Principal (as handler tests build) is
// read through the same claims derivation authn uses.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if p, ok := ctx.Value(principalContextKey{}).(Principal); ok {
		return p, true
	}
	if c, ok := ctx.Value(claimsContextKey{}).(*domainauth.Claims); ok && c != nil {
		return principalFromClaims(c)
	}
	return Principal{}, false
}

// WithPrincipal returns a context carrying p. Use in tests to simulate an
// authenticated caller.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

// ClaimsFrom retrieves the authenticated claims from the request context.
// It returns nil if no claims are present (e.g. on unauthenticated routes).
func ClaimsFrom(r *http.Request) *domainauth.Claims {
	v, _ := r.Context().Value(claimsContextKey{}).(*domainauth.Claims)
	return v
}

// ClaimsContextKey returns the context key used to store JWT claims.
// Intended for use in tests that need to inject synthetic claims.
func ClaimsContextKey() any { return claimsContextKey{} }

// ActorIDFromContext extracts the authenticated account's UUID from a Go
// context.Context. Returns (uuid.Nil, false) when no actor is set.
func ActorIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	p, ok := PrincipalFromContext(ctx)
	if !ok {
		return uuid.Nil, false
	}
	return p.AccountID, true
}

// AgentIDFromContext returns the caller's id when it is an agent account.
// Returns (uuid.Nil, false) for any other caller.
func AgentIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	p, ok := PrincipalFromContext(ctx)
	if !ok || p.Kind != accountdom.KindAgent {
		return uuid.Nil, false
	}
	return p.AccountID, true
}

// WithActorID returns a new context that carries a user principal for
// actorID. Use in tests to simulate an authenticated caller.
func WithActorID(ctx context.Context, actorID uuid.UUID) context.Context {
	return WithPrincipal(ctx, Principal{AccountID: actorID, Kind: accountdom.KindUser, Method: MethodSession})
}

// WithAgentID returns a new context that carries an agent principal for
// agentID. Use in tests to simulate an agent-authenticated caller.
func WithAgentID(ctx context.Context, agentID uuid.UUID) context.Context {
	return WithPrincipal(ctx, Principal{AccountID: agentID, Kind: accountdom.KindAgent, Method: MethodAPIKey})
}

// IsAPIKeyAuth reports whether the current request was authenticated via an API
// key (a user's, a service account's or an agent's own) rather than a JWT/cookie session.
func IsAPIKeyAuth(r *http.Request) bool {
	p, ok := PrincipalFrom(r)
	return ok && p.Method != MethodSession
}

// AgentIDFromRequest returns the caller's id when it is an agent account.
func AgentIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return AgentIDFromContext(r.Context())
}

// RequireJWTAuth rejects requests that were authenticated via an API key.
// Apply this middleware to sensitive routes (e.g. API key management) that must
// only be reachable through a JWT/cookie session to prevent privilege escalation
// via a leaked API key.
func RequireJWTAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !EnforceJWTAuth(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// EnforceJWTAuth rejects API key-authenticated requests.
func EnforceJWTAuth(w http.ResponseWriter, r *http.Request) bool {
	if IsAPIKeyAuth(r) {
		presenter.Error(w, r, apierr.New(apierr.CodeForbidden, "this endpoint requires session authentication and does not accept API key credentials"))
		return false
	}
	return true
}
