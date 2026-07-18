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
	"net/http"
	"strings"

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

	// audience, when non-empty, is enforced as the token's aud claim (PACA-C1):
	// a token minted for a different resource is rejected, closing the
	// confused-deputy hole where a bearer token issued for another audience
	// could be replayed against Paca.
	audience string

	// resourceScopePrefix, when non-empty, enables scope enforcement (PACA-C1).
	// It is Paca's own resource-scope prefix (e.g. "mcp:paca:"). A token that
	// carries any resource-family scope (the segment up to the first colon,
	// e.g. "mcp:") but none for Paca is rejected as a foreign-resource token,
	// and a token whose Paca scopes are read-only is denied write operations.
	resourceScopePrefix string
}

// NewBearerAuthenticator returns a configured BearerAuthenticator.
func NewBearerAuthenticator(verifier TokenVerifier, users BearerUserStore, log *slog.Logger) *BearerAuthenticator {
	return &BearerAuthenticator{verifier: verifier, users: users, log: log}
}

// WithResourceAudience configures the aud value enforced on every bearer token
// (PACA-C1). Empty leaves audience enforcement off. Returns the receiver for
// fluent wiring.
func (a *BearerAuthenticator) WithResourceAudience(aud string) *BearerAuthenticator {
	a.audience = strings.TrimSpace(aud)
	return a
}

// WithResourceScopePrefix configures Paca's own resource-scope prefix (e.g.
// "mcp:paca:") used for scope enforcement (PACA-C1). Empty disables scope
// checks. Returns the receiver for fluent wiring.
func (a *BearerAuthenticator) WithResourceScopePrefix(prefix string) *BearerAuthenticator {
	a.resourceScopePrefix = strings.TrimSpace(prefix)
	return a
}

// AuthenticateBearer verifies rawToken and returns the effective local user
// plus the attributed agent name (empty when the call is not agent-attributed).
//
// method is the HTTP method of the request being authorized; it is used only
// for scope enforcement (write methods require a non-read-only Paca scope when
// the token carries scopes at all). Pass an empty string for method-agnostic
// callers, which are then treated as reads.
func (a *BearerAuthenticator) AuthenticateBearer(ctx context.Context, rawToken, method string) (*userdom.User, string, error) {
	claims, err := a.verifier.VerifyToken(ctx, rawToken, a.audience)
	if err != nil {
		return nil, "", err
	}

	if err := a.enforceScope(claims, method); err != nil {
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

// enforceScope applies OAuth-scope enforcement to a verified token (PACA-C1).
// It is a no-op when scope enforcement is disabled (no resource-scope prefix
// configured) or when the token carries no scope claim at all — the audience
// check remains the primary defense in those cases. When the token DOES carry
// resource-family scopes, it must include at least one Paca scope (else it is a
// foreign-resource token) and, for write methods, at least one non-read-only
// Paca scope.
func (a *BearerAuthenticator) enforceScope(claims jwt.MapClaims, method string) error {
	if a.resourceScopePrefix == "" {
		return nil
	}
	scopes := scopeList(claims)
	if len(scopes) == 0 {
		// No scope claim present: nothing to gate on. (Service/act_as tokens
		// minted by identity carry no scope.)
		return nil
	}

	family := scopeFamily(a.resourceScopePrefix)
	var resourceScopes, pacaScopes []string
	for _, s := range scopes {
		if family != "" && strings.HasPrefix(s, family) {
			resourceScopes = append(resourceScopes, s)
		}
		if strings.HasPrefix(s, a.resourceScopePrefix) {
			pacaScopes = append(pacaScopes, s)
		}
	}

	// The token declares no resource-family scopes at all (only generic scopes
	// like "openid profile"): don't gate on resource scope.
	if len(resourceScopes) == 0 {
		return nil
	}

	// Resource-scoped token that targets other resources but not Paca — a
	// confused-deputy / foreign-resource token.
	if len(pacaScopes) == 0 {
		return fmt.Errorf("galaxyauth: bearer token scope does not grant access to this resource")
	}

	if isWriteMethod(method) && isReadOnlyScopes(pacaScopes) {
		return fmt.Errorf("galaxyauth: bearer token is read-only; %s requires a write scope", method)
	}
	return nil
}

// scopeList extracts the OAuth scopes from a token's scope claim, accepting the
// space-delimited string form ("scope"/"scp") and the JSON array forms.
func scopeList(claims jwt.MapClaims) []string {
	for _, key := range []string{"scope", "scp"} {
		raw, ok := claims[key]
		if !ok || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case string:
			return strings.Fields(v)
		case []string:
			return v
		case []any:
			out := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, s)
				}
			}
			return out
		}
	}
	return nil
}

// scopeFamily returns the resource family of a scope prefix: the segment up to
// and including the first colon (e.g. "mcp:paca:" -> "mcp:"). A prefix with no
// colon is its own family.
func scopeFamily(prefix string) string {
	if i := strings.Index(prefix, ":"); i >= 0 {
		return prefix[:i+1]
	}
	return prefix
}

// isReadOnlyScopes reports whether every Paca scope grants only read access —
// i.e. its action (the segment after the final colon) is exactly "read".
func isReadOnlyScopes(pacaScopes []string) bool {
	for _, s := range pacaScopes {
		if scopeAction(s) != "read" {
			return false
		}
	}
	return true
}

// scopeAction returns the action segment of a scope (the part after the final
// colon), e.g. "mcp:paca:write" -> "write".
func scopeAction(scope string) string {
	if i := strings.LastIndex(scope, ":"); i >= 0 {
		return scope[i+1:]
	}
	return scope
}

// isWriteMethod reports whether an HTTP method mutates state.
func isWriteMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
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
