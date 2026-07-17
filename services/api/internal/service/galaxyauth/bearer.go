package galaxyauth

// Bearer authentication against the trusted Vortex issuer (ADR-038):
// RS256 tokens carry the platform identity; the effective principal is the
// act_as claim (an agent acting on a user's behalf) falling back to the
// token's own sub.  Principals map to local users via users.oidc_sub — never
// auto-created on this path — and act_as_agent is recorded for attribution
// only, granting nothing beyond the principal user's permissions.

import (
	"context"
	"fmt"
	"log/slog"

	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/golang-jwt/jwt/v5"
)

// TokenVerifier validates a raw token against the trusted issuer (signature
// via JWKS, iss, exp, alg=RS256).  Implemented by *oidc.Provider.
type TokenVerifier interface {
	VerifyToken(ctx context.Context, rawToken, expectedAudience string) (jwt.MapClaims, error)
}

// BearerUserStore is the subset of UserStore the bearer path needs.
type BearerUserStore interface {
	FindByOIDCSub(ctx context.Context, sub string) (*userdom.User, error)
}

// BearerAuthenticator resolves trusted-issuer bearer tokens to local users.
// It satisfies the transport middleware's GalaxyBearerAuthenticator contract.
type BearerAuthenticator struct {
	verifier TokenVerifier
	users    BearerUserStore
	log      *slog.Logger
}

// NewBearerAuthenticator returns a configured BearerAuthenticator.
func NewBearerAuthenticator(verifier TokenVerifier, users BearerUserStore, log *slog.Logger) *BearerAuthenticator {
	return &BearerAuthenticator{verifier: verifier, users: users, log: log}
}

// AuthenticateBearer verifies rawToken and returns the effective local user
// plus the attributed agent name (empty when the call is not agent-attributed).
func (a *BearerAuthenticator) AuthenticateBearer(ctx context.Context, rawToken string) (*userdom.User, string, error) {
	claims, err := a.verifier.VerifyToken(ctx, rawToken, "")
	if err != nil {
		return nil, "", err
	}

	tokenSub, _ := claims["sub"].(string)
	principal := effectivePrincipalSub(claims, tokenSub)
	if principal == "" {
		return nil, "", fmt.Errorf("galaxyauth: bearer token has no usable subject")
	}

	u, err := a.users.FindByOIDCSub(ctx, principal)
	if err != nil {
		// Includes userdom.ErrNotFound: no auto-provisioning on the bearer
		// path — unknown principals are rejected outright.
		return nil, "", fmt.Errorf("galaxyauth: resolve bearer principal: %w", err)
	}

	agentName := agentAttribution(claims)
	if agentName != "" {
		a.log.Info("galaxyauth: agent-attributed bearer call",
			"agent", agentName, "principal_user", u.Username, "token_sub", tokenSub)
	}
	return u, agentName, nil
}

// effectivePrincipalSub extracts the acted-for subject from the act_as claim,
// accepting both the object form {"sub": "..."} and a plain string, and
// falling back to the token's own sub.  Malformed act_as values fall back
// rather than fail so a bare platform token still authenticates as itself.
func effectivePrincipalSub(claims jwt.MapClaims, tokenSub string) string {
	actAs, ok := claims["act_as"]
	if !ok || actAs == nil {
		return tokenSub
	}
	switch v := actAs.(type) {
	case string:
		if v != "" {
			return v
		}
	case map[string]any:
		if s, ok := v["sub"].(string); ok && s != "" {
			return s
		}
	}
	return tokenSub
}

// agentAttribution extracts a display name for the acting agent from the
// act_as_agent claim (string, or an object exposing name/handle/sub/id).
func agentAttribution(claims jwt.MapClaims) string {
	raw, ok := claims["act_as_agent"]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"name", "handle", "sub", "id"} {
			if s, ok := v[key].(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}
