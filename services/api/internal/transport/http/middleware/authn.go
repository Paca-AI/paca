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
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
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

// AgentIdentityVerifier confirms an agent-API-key request's claimed
// X-Agent-ID / X-Actor-User-ID header values against the database before
// either is trusted. The agent API key (apikeysvc.WithAgentKey) is a single
// static secret shared by every agent process — presenting it proves only
// "this caller knows the shared secret," not "this caller actually is the
// specific agent, or is acting for the specific human, it claims to be" via
// these two headers. Without this check, anyone holding that one key (a
// leak, or a compromised/malicious agent sandbox reading its own env) could
// set X-Agent-ID to any agent UUID to read that agent's own permissions and
// invited-projects list (GetMyGlobalPermissions, GetMyInvitedProjects), or
// set X-Actor-User-ID to attribute actions like CreateProject to an
// arbitrary human. Implemented directly by *postgres.AgentRepository (it
// already has both FindAgentByID and HasActiveGlobalChatSession).
type AgentIdentityVerifier interface {
	// FindAgentByID must return agentdom.ErrAgentNotFound (or any error) for
	// an unknown or soft-deleted agent — applyAuthn treats any error here as
	// "reject the claimed identity."
	FindAgentByID(ctx context.Context, agentID uuid.UUID) (*agentdom.Agent, error)
	// HasActiveGlobalChatSession reports whether agentID has an active
	// global chat session with actorUserID — the actual evidence binding
	// the two together, not just their both being well-formed UUIDs.
	HasActiveGlobalChatSession(ctx context.Context, agentID, actorUserID uuid.UUID) (bool, error)
}

// errAgentIdentityUnverified is returned by verifyAgentIdentity for any
// failed check — deliberately generic (never "no such agent" vs. "wrong
// actor") so the HTTP response doesn't help an attacker distinguish a
// nonexistent agent ID from a real one paired with the wrong human.
var errAgentIdentityUnverified = errors.New("authn: unable to verify claimed agent identity")

// verifyAgentIdentity checks a claimed (agentID, actorUserID) pair from
// X-Agent-ID / X-Actor-User-ID against the database via verifier. Returns
// the same pair back only if every applicable check passes; otherwise
// returns errAgentIdentityUnverified. agentID == uuid.Nil (no X-Agent-ID
// header) is a no-op — nothing to verify. A verifier of nil fails closed
// rather than silently trusting an unverifiable claim.
func verifyAgentIdentity(ctx context.Context, verifier AgentIdentityVerifier, agentID, actorUserID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if agentID == uuid.Nil {
		return uuid.Nil, uuid.Nil, nil
	}
	if verifier == nil {
		return uuid.Nil, uuid.Nil, errAgentIdentityUnverified
	}
	agent, err := verifier.FindAgentByID(ctx, agentID)
	if err != nil {
		return uuid.Nil, uuid.Nil, errAgentIdentityUnverified
	}
	if actorUserID == uuid.Nil {
		return agentID, uuid.Nil, nil
	}
	// Only global agents legitimately chat with a human directly — a
	// project-scoped agent's actions are attributed via its
	// project_members.id, never a raw actor_user_id.
	if agent.AgentScope != agentdom.AgentScopeGlobal {
		return uuid.Nil, uuid.Nil, errAgentIdentityUnverified
	}
	hasSession, err := verifier.HasActiveGlobalChatSession(ctx, agentID, actorUserID)
	if err != nil || !hasSession {
		return uuid.Nil, uuid.Nil, errAgentIdentityUnverified
	}
	return agentID, actorUserID, nil
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
					// Unverifiable agent/actor claims are dropped (zero
					// value), not hard-failed — OptionalAuthn's contract is
					// "never abort on bad credentials," so a spoofed claim
					// degrades to "no agent identity" rather than a 401,
					// same as it already does for an Authenticate error a
					// few lines up.
					agentID, actorUserID, _ := resolveAgentClaims(r, apiKeyAuthenticator, tokenStr)
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

		agentID, actorUserID, claimErr := resolveAgentClaims(r, apiKeyAuthenticator, tokenStr)
		if claimErr != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unable to verify claimed agent identity"))
			return r, false
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
// could impersonate another simply by setting it. Returns (uuid.Nil, nil)
// when the header is absent — nothing claimed. A non-nil error means the
// header was present but malformed; resolveAgentClaims treats that as a
// verification failure rather than silently falling back to "no claim."
func parseActorUserIDHeader(r *http.Request) (uuid.UUID, error) {
	header := r.Header.Get("X-Actor-User-ID")
	if header == "" {
		return uuid.Nil, nil
	}
	return uuid.Parse(header)
}

// resolveAgentClaims parses X-Agent-ID / X-Actor-User-ID off r and verifies
// them against the database via apiKeyAuthenticator's AgentIdentityVerifier
// (when it implements one) — see that type's doc comment for why an
// unverified claim can't be trusted. Only ever does anything when tokenStr
// is the shared static agent key (see IsAgentKey); otherwise returns zero
// values and a nil error, since there is no agent claim to resolve for a
// personal API key. Returns a non-nil error whenever a claim was made
// (a header was present) but failed verification — callers decide whether
// that's fatal (required auth: reject the request) or just dropped
// (OptionalAuthn: proceed with no agent identity, same as any other bad
// credential in that path).
func resolveAgentClaims(r *http.Request, apiKeyAuthenticator APIKeyAuthenticator, tokenStr string) (agentID, actorUserID uuid.UUID, err error) {
	agentKeyAuth, ok := apiKeyAuthenticator.(AgentAPIKeyAuthenticator)
	if !ok || !agentKeyAuth.IsAgentKey(r.Context(), tokenStr) {
		return uuid.Nil, uuid.Nil, nil
	}

	var claimedAgentID uuid.UUID
	if agentIDHeader := r.Header.Get("X-Agent-ID"); agentIDHeader != "" {
		parsed, parseErr := uuid.Parse(agentIDHeader)
		if parseErr != nil {
			return uuid.Nil, uuid.Nil, errAgentIdentityUnverified
		}
		claimedAgentID = parsed
	}
	claimedActorUserID, parseErr := parseActorUserIDHeader(r)
	if parseErr != nil {
		return uuid.Nil, uuid.Nil, errAgentIdentityUnverified
	}

	verifier, _ := apiKeyAuthenticator.(AgentIdentityVerifier)
	return verifyAgentIdentity(r.Context(), verifier, claimedAgentID, claimedActorUserID)
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
