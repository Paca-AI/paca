package middleware

// Galaxy trusted-issuer bearer authentication (ADR-038).
//
// When GALAXY_TRUSTED_ISSUER is configured, RS256 bearer tokens signed by the
// Vortex identity provider are accepted platform-wide: the effective
// principal (act_as claim, else sub) is resolved to a local user via
// users.oidc_sub and the request proceeds with exactly that user's
// permissions.  Agent attribution (act_as_agent) is recorded for audit but
// never grants anything beyond the principal user.  HS256 bearer tokens (the
// API's own sessions) are untouched and fall through to Authn.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Paca-AI/api/internal/apierr"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
	"github.com/golang-jwt/jwt/v5"
)

// galaxyAgentNameContextKey stores the act_as_agent attribution for logging
// and audit.  Deliberately separate from agentContextKey: agent-role
// permission resolution must never replace the principal user's permissions.
type galaxyAgentNameContextKey struct{}

// authMethodGalaxyBearer marks requests authenticated via a trusted-issuer
// bearer token.
const authMethodGalaxyBearer = "galaxy_bearer"

// GalaxyBearerAuthenticator verifies an RS256 bearer token from the trusted
// issuer and resolves the effective principal to a local user.  Implemented
// by galaxyauth.BearerAuthenticator.  method is the request's HTTP method, used
// for scope enforcement (a read-only token is denied on write methods).
type GalaxyBearerAuthenticator interface {
	AuthenticateBearer(ctx context.Context, rawToken, method string) (user *userdom.User, agentName string, err error)
}

// GalaxyBearer returns a middleware that authenticates RS256 bearer tokens
// from the trusted Vortex issuer.  Requests without a bearer header, or whose
// bearer token is not RS256, pass through untouched.  A presented RS256 token
// that fails verification or maps to no local user is rejected with 401 —
// fail closed, no fallback to other credential sources.
func GalaxyBearer(auth GalaxyBearerAuthenticator, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawToken := bearerTokenFrom(r)
			if auth == nil || rawToken == "" || !hasJWTAlg(rawToken, "RS256") {
				next.ServeHTTP(w, r)
				return
			}

			user, agentName, err := auth.AuthenticateBearer(r.Context(), rawToken, r.Method)
			if err != nil {
				log.Warn("galaxy bearer: token rejected", "error", err, "path", r.URL.Path, "method", r.Method)
				presenter.Error(w, r, apierr.New(apierr.CodeTokenInvalid, "platform bearer token rejected: invalid token, unknown principal, or insufficient scope"))
				return
			}

			// Synthetic access claims for the resolved principal — identity
			// comes from the signed token, never from request headers.
			claims := &domainauth.Claims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: user.ID.String()},
				Username:         user.Username,
				Role:             user.Role,
				Kind:             "access",
			}

			ctx := context.WithValue(r.Context(), claimsContextKey{}, claims)
			ctx = context.WithValue(ctx, actorContextKey{}, user.ID)
			ctx = context.WithValue(ctx, authMethodContextKey{}, authMethodGalaxyBearer)
			if agentName != "" {
				ctx = context.WithValue(ctx, galaxyAgentNameContextKey{}, agentName)
				log.Info("galaxy bearer: agent-attributed request",
					"agent", agentName, "user", user.Username, "method", r.Method, "path", r.URL.Path)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IsGalaxyBearerAuth reports whether the request was authenticated via a
// trusted-issuer bearer token.
func IsGalaxyBearerAuth(r *http.Request) bool {
	v, _ := r.Context().Value(authMethodContextKey{}).(string)
	return v == authMethodGalaxyBearer
}

// GalaxyAgentNameFromRequest returns the act_as_agent attribution recorded
// for this request, if any.
func GalaxyAgentNameFromRequest(r *http.Request) (string, bool) {
	v, ok := r.Context().Value(galaxyAgentNameContextKey{}).(string)
	return v, ok && v != ""
}

// bearerTokenFrom extracts the token from an Authorization: Bearer header.
func bearerTokenFrom(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// hasJWTAlg peeks at the (unverified) JOSE header to check the alg value so
// this middleware only intercepts tokens it is responsible for.  Actual
// algorithm enforcement happens again inside signature verification.
func hasJWTAlg(rawToken, alg string) bool {
	headerSeg, _, ok := strings.Cut(rawToken, ".")
	if !ok {
		return false
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(headerSeg)
	if err != nil {
		return false
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return false
	}
	return header.Alg == alg
}
