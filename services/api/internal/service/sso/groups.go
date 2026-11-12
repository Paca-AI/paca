package sso

import (
	"slices"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	ssodom "github.com/Paca-AI/api/internal/domain/sso"
)

// checkGroupPolicy enforces p's group allow-list against the verified ID
// token. It is a no-op while the policy is disabled. Otherwise it fails
// closed: the identity is let through only when the group claim is present,
// well-formed and holds at least one allowed value. The reason for a denial
// is logged; the caller only ever sees ssodom.ErrGroupNotAllowed, so claim
// contents never reach the end user.
func (s *Service) checkGroupPolicy(p *ssodom.Provider, idTok *oidc.IDToken) error {
	if !p.GroupPolicyEnabled {
		return nil
	}
	var claims map[string]any
	if err := idTok.Claims(&claims); err != nil {
		s.log.Warn("sso: group policy: cannot decode id token claims", "provider", p.Slug, "error", err)
		return ssodom.ErrGroupNotAllowed
	}
	if reason := groupDenialReason(claims, p.GroupClaim, p.AllowedGroups); reason != "" {
		s.log.Warn("sso: sign-in denied by group policy", "provider", p.Slug, "subject", idTok.Subject, "reason", reason)
		return ssodom.ErrGroupNotAllowed
	}
	return nil
}

// groupDenialReason returns why claims fail the allow-list, or "" when they
// satisfy it.
func groupDenialReason(claims map[string]any, claimPath string, allowed []string) string {
	raw, found := lookupClaim(claims, claimPath)
	if !found {
		// Entra ID replaces an oversized "groups" claim with a pointer to
		// Microsoft Graph (_claim_names / _claim_sources). Paca doesn't
		// resolve it, so it is denied — but say so in the log.
		if names, ok := claims["_claim_names"].(map[string]any); ok {
			if _, overage := names[claimPath]; overage {
				return "group overage: claim " + claimPath + " was replaced by a distributed-claim reference"
			}
		}
		return "claim " + claimPath + " is absent"
	}
	var values []string
	switch v := raw.(type) {
	case string:
		values = []string{v}
	case []any:
		for _, item := range v {
			str, ok := item.(string)
			if !ok {
				return "claim " + claimPath + " contains a non-string value"
			}
			values = append(values, str)
		}
	default:
		return "claim " + claimPath + " is neither a string nor a list of strings"
	}
	if !slices.ContainsFunc(values, func(v string) bool { return slices.Contains(allowed, v) }) {
		return "claim " + claimPath + " holds none of the allowed groups"
	}
	return ""
}

// lookupClaim resolves path in claims. A claim literally named path wins
// (claim names such as "https://example.com/groups" contain dots); otherwise
// path is walked as nested objects, e.g. "realm_access.roles".
func lookupClaim(claims map[string]any, path string) (any, bool) {
	if v, ok := claims[path]; ok {
		return v, true
	}
	cur := any(claims)
	for _, part := range strings.Split(path, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = obj[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

// validClaimPath reports whether path is a usable group claim: non-empty,
// without whitespace, and without empty dotted segments.
func validClaimPath(path string) bool {
	if path == "" || strings.ContainsAny(path, " \t\r\n") {
		return false
	}
	return !slices.Contains(strings.Split(path, "."), "")
}
