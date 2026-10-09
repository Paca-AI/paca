package iam

import (
	"regexp"
	"strings"
)

// MatchAction reports whether an action pattern ("*", "tasks:*" or an exact
// action) matches action.
func MatchAction(pattern, action string) bool {
	switch {
	case pattern == "*":
		return true
	case strings.HasSuffix(pattern, ":*"):
		return strings.HasPrefix(action, strings.TrimSuffix(pattern, "*"))
	default:
		return pattern == action
	}
}

// MatchResource matches a resource name against a path pattern. A mid-path
// "*" matches exactly one segment; a trailing "*" matches zero or more
// remaining segments, so "project/p1/*" also covers "project/p1" itself.
func MatchResource(pattern, resource string) bool {
	if pattern == "*" {
		return true
	}
	ps := strings.Split(pattern, "/")
	rs := strings.Split(resource, "/")
	for i, p := range ps {
		if p == "*" && i == len(ps)-1 {
			return len(rs) >= i
		}
		if i >= len(rs) {
			return false
		}
		if p != "*" && p != rs[i] {
			return false
		}
	}
	return len(rs) == len(ps)
}

// globMatch implements StringLike: "*" matches any run of characters, "?" one.
func globMatch(pattern, s string) bool {
	re := regexp.QuoteMeta(pattern)
	re = strings.ReplaceAll(re, `\*`, `.*`)
	re = strings.ReplaceAll(re, `\?`, `.`)
	ok, err := regexp.MatchString("(?s)^"+re+"$", s)
	return err == nil && ok
}

// MatchesUnderWildcard reports whether policyResource names a specific child
// that would fall under queryResource when queryResource ends with a wildcard.
// E.g., "project/P/role/R1" matches under "project/P/role/*", and "role/R1"
// matches under "role/*". Used by PossiblyAllowed to detect that an Allow on
// a specific resource ID means the action is possibly allowed on the collection.
func MatchesUnderWildcard(policyResource, queryResource string) bool {
	if !strings.HasSuffix(queryResource, "/*") {
		return false
	}
	prefix := strings.TrimSuffix(queryResource, "*")
	if !strings.HasPrefix(policyResource, prefix) {
		return false
	}
	remainder := strings.TrimPrefix(policyResource, prefix)
	// Must be a specific ID (no wildcards) and no extra path segments
	return remainder != "" && remainder != "*" && !strings.Contains(remainder, "/") && !strings.Contains(remainder, "*")
}
