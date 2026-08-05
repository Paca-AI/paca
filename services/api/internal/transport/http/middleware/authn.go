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
	apikeydom "github.com/Paca-AI/api/internal/domain/apikey"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	jwttoken "github.com/Paca-AI/api/internal/platform/token"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// claimsContextKey is the unexported key used to store JWT claims in the Go request context.
type claimsContextKey struct{}

// authMethodContextKey stores the authentication method (e.g. "apikey").
type authMethodContextKey struct{}

// actorContextKey is the unexported key used to store the authenticated user's
// UUID in the Go request context.
type actorContextKey struct{}

// agentContextKey is the unexported key used to store the agent ID when
// authenticating with an agent API key.
type agentContextKey struct{}

// agentActorUserContextKey is the unexported key used to store the human
// actor's user ID for an agent-authenticated request that names one via
// X-Actor-User-ID (e.g. a global agent's MCP tool call made on behalf of the
// human chatting with it — see setAPIKeyAuthContext). Only ever set
// alongside a resolved agentContextKey; a plain human JWT session already
// carries its own identity via actorContextKey and never needs this.
type agentActorUserContextKey struct{}

// APIKeyAuthenticator validates a raw API key string and returns the key record.
// It is satisfied by apikeysvc.Service.
type APIKeyAuthenticator interface {
	Authenticate(ctx context.Context, rawKey string) (*apikeydom.APIKey, error)
}

// AgentAPIKeyAuthenticator extends APIKeyAuthenticator with the ability to check
// if a key is the static agent API key.
type AgentAPIKeyAuthenticator interface {
	APIKeyAuthenticator
	IsAgentKey(ctx context.Context, rawKey string) bool
}

// Authn validates the access JWT and stores the parsed claims in the request context
// as well as the caller's user UUID so service-layer code can access it without
// depending on the HTTP layer.
// It first checks the access_token HttpOnly cookie, then falls back to the
// Authorization: Bearer header for API/CLI clients, and finally accepts
// Authorization: ApiKey or X-API-Key headers for API key authentication.
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
		if optional {
			if apiKeyAuthenticator != nil {
				key, err := apiKeyAuthenticator.Authenticate(r.Context(), tokenStr)
				if err == nil {
					var agentID, actorUserID uuid.UUID
					if agentKeyAuth, ok := apiKeyAuthenticator.(AgentAPIKeyAuthenticator); ok && agentKeyAuth.IsAgentKey(r.Context(), tokenStr) {
						agentIDHeader := r.Header.Get("X-Agent-ID")
						if agentIDHeader != "" {
							if parsedID, parseErr := uuid.Parse(agentIDHeader); parseErr == nil {
								agentID = parsedID
							}
						}
						actorUserID = parseActorUserIDHeader(r)
					}
					r = setAPIKeyAuthContext(r, key.UserID, agentID, actorUserID)
				}
			}
			return r, true
		}
		if apiKeyAuthenticator == nil {
			presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "API key authentication not configured"))
			return r, false
		}
		key, err := apiKeyAuthenticator.Authenticate(r.Context(), tokenStr)
		if err != nil {
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

		var agentID, actorUserID uuid.UUID
		if agentKeyAuth, ok := apiKeyAuthenticator.(AgentAPIKeyAuthenticator); ok && agentKeyAuth.IsAgentKey(r.Context(), tokenStr) {
			agentIDHeader := r.Header.Get("X-Agent-ID")
			if agentIDHeader != "" {
				if parsedID, parseErr := uuid.Parse(agentIDHeader); parseErr == nil {
					agentID = parsedID
				}
			}
			actorUserID = parseActorUserIDHeader(r)
		}

		r = setAPIKeyAuthContext(r, key.UserID, agentID, actorUserID)
		return r, true
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

	ctx := context.WithValue(r.Context(), claimsContextKey{}, claims)
	if actorID, parseErr := uuid.Parse(claims.Subject); parseErr == nil {
		ctx = context.WithValue(ctx, actorContextKey{}, actorID)
	}
	r = r.WithContext(ctx)
	return r, true
}

// parseActorUserIDHeader parses X-Actor-User-ID, the human actor a global
// agent's MCP tool call is acting on behalf of (e.g. "create a project for
// me" from the home-page chat). Only ever consulted from the agent-API-key
// branch above — a human JWT session already carries its own identity via
// actorContextKey and must never have this header honored, or one human
// could impersonate another simply by setting it.
func parseActorUserIDHeader(r *http.Request) uuid.UUID {
	header := r.Header.Get("X-Actor-User-ID")
	if header == "" {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(header)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

func setAPIKeyAuthContext(r *http.Request, userID, agentID, actorUserID uuid.UUID) *http.Request {
	syntheticClaims := &domainauth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: userID.String(),
		},
		Kind: "access",
	}
	if agentID != uuid.Nil {
		agentIDStr := agentID.String()
		syntheticClaims.AgentID = &agentIDStr
	}
	ctx := context.WithValue(r.Context(), claimsContextKey{}, syntheticClaims)
	ctx = context.WithValue(ctx, authMethodContextKey{}, "apikey")
	ctx = context.WithValue(ctx, actorContextKey{}, userID)
	if agentID != uuid.Nil {
		ctx = context.WithValue(ctx, agentContextKey{}, agentID)
		// Only ever paired with a resolved agent ID — see
		// parseActorUserIDHeader's doc comment.
		if actorUserID != uuid.Nil {
			ctx = context.WithValue(ctx, agentActorUserContextKey{}, actorUserID)
		}
	}
	return r.WithContext(ctx)
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

// ActorIDFromContext extracts the authenticated user's UUID from a Go
// context.Context. Returns (uuid.Nil, false) when no actor is set.
func ActorIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v := ctx.Value(actorContextKey{})
	if v == nil {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}

// AgentIDFromContext extracts the agent ID from a Go context.Context when
// authenticated via an agent API key with X-Agent-ID header.
// Returns (uuid.Nil, false) when no agent ID is set.
func AgentIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v := ctx.Value(agentContextKey{})
	if v == nil {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}

// WithActorID returns a new context that carries actorID.
// Use in tests to simulate an authenticated caller.
func WithActorID(ctx context.Context, actorID uuid.UUID) context.Context {
	return context.WithValue(ctx, actorContextKey{}, actorID)
}

// WithAgentID returns a new context that carries agentID.
// Use in tests to simulate an agent-authenticated caller.
func WithAgentID(ctx context.Context, agentID uuid.UUID) context.Context {
	return context.WithValue(ctx, agentContextKey{}, agentID)
}

// IsAPIKeyAuth reports whether the current request was authenticated via an API
// key rather than a JWT/cookie session.
func IsAPIKeyAuth(r *http.Request) bool {
	v, _ := r.Context().Value(authMethodContextKey{}).(string)
	return v == "apikey"
}

// AgentIDFromRequest extracts the agent ID from the request context when
// authenticated via an agent API key with X-Agent-ID header.
// Returns (uuid.Nil, false) when no agent ID is set.
func AgentIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return AgentIDFromContext(r.Context())
}

// AgentActorUserIDFromContext extracts the human actor's user ID for an
// agent-authenticated request that named one via X-Actor-User-ID (see
// parseActorUserIDHeader). Returns (uuid.Nil, false) when absent — which is
// the common case: only a global agent's MCP tool calls, made on behalf of
// the specific human chatting with it, set this.
func AgentActorUserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v := ctx.Value(agentActorUserContextKey{})
	if v == nil {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}

// AgentActorUserIDFromRequest extracts the human actor's user ID from the
// request context. See AgentActorUserIDFromContext.
func AgentActorUserIDFromRequest(r *http.Request) (uuid.UUID, bool) {
	return AgentActorUserIDFromContext(r.Context())
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
