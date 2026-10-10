package iam

import (
	"context"
	"fmt"
	"strings"
)

// CanGrant reports whether principal p may put policy into a role (create or
// edit one) or attach a role carrying it: p must hold, itself, everything the
// policy grants. See GrantsCover for the exact rules. A store error is
// returned and the caller must treat it as "no"; an invalid principal or a nil
// policy is "no".
func (a *Authorizer) CanGrant(ctx context.Context, p Principal, policy *Policy) (bool, error) {
	if policy == nil || !p.valid() {
		return false, nil
	}
	grants, err := a.store.ListGrants(ctx, p)
	if err != nil {
		return false, fmt.Errorf("iam: list grants: %w", err)
	}
	return GrantsCover(grants, policy), nil
}

// GrantsCover is the pure core of CanGrant. For every Allow statement of the
// policy and every (action pattern A, resource pattern R) pair in it, the
// grants must:
//
//   - contain an UNCONDITIONAL Allow statement covering the pair, matching the
//     pattern strings themselves (MatchAction(callerAction, A) and
//     MatchResource(callerResource, R)): a caller holding "tasks:*" covers
//     "tasks:read" and "tasks:*"; "project/P/*" covers "project/P/task/*";
//     "project/P/task/t1" does not cover "project/P/task/*". A candidate
//     statement's conditions only narrow it, so they do not change coverage.
//     A project-scoped grant covers only resources inside its project (the
//     evaluator's project intersection);
//   - NOT be hit by any Deny statement, conditional or not, whose actions and
//     resources overlap the pair at all (a Deny of "tasks:write" blocks
//     granting "tasks:*" as well as "tasks:write"). A project-scoped Deny
//     counts for resources inside its project and for patterns that could
//     reach into it.
//
// Deny statements of the candidate policy only ever narrow, so they are always
// grantable. A policy with no Allow statement is grantable by anyone. Grants
// with an unparsable (nil) policy hold nothing.
func GrantsCover(grants []Grant, policy *Policy) bool {
	if policy == nil {
		return false
	}
	for _, st := range policy.Statements {
		if st.Effect != EffectAllow {
			continue
		}
		for _, action := range st.Actions {
			for _, resource := range st.Resources {
				if !pairCovered(grants, action, resource) || pairDenied(grants, action, resource) {
					return false
				}
			}
		}
	}
	return true
}

func pairCovered(grants []Grant, action, resource string) bool {
	for _, g := range grants {
		if g.Policy == nil {
			continue
		}
		if g.ProjectID != "" && !inProject(g.ProjectID, resource) {
			continue
		}
		for _, s := range g.Policy.Statements {
			if s.Effect == EffectAllow && len(s.Conditions) == 0 &&
				anyMatch(s.Actions, action, MatchAction) && anyMatch(s.Resources, resource, patternCovers) {
				return true
			}
		}
	}
	return false
}

// patternCovers reports whether every resource matching candidatePattern also
// matches callerPattern (language inclusion). This is stricter than MatchResource,
// which only checks if the candidate pattern string itself matches.
func patternCovers(callerPattern, candidatePattern string) bool {
	if callerPattern == "*" {
		return true // caller's wildcard covers everything
	}
	if candidatePattern == "*" {
		return callerPattern == "*" // only "*" covers "*"
	}

	callerSegs := strings.Split(callerPattern, "/")
	candidateSegs := strings.Split(candidatePattern, "/")

	// If caller ends with trailing "*", check if candidate is within that scope
	callerTrailing := len(callerSegs) > 0 && callerSegs[len(callerSegs)-1] == "*"
	candidateTrailing := len(candidateSegs) > 0 && candidateSegs[len(candidateSegs)-1] == "*"

	if callerTrailing {
		// Caller "project/P/*" covers:
		// - "project/P" (trailing * matches zero segments)
		// - "project/P/*" (trailing * matches zero or more)
		// - "project/P/task/123" (trailing * matches remaining)
		// But does NOT cover:
		// - "project/Q/*" (different prefix)

		// Check prefix match (all segments before trailing *)
		callerPrefix := callerSegs[:len(callerSegs)-1]

		// Candidate must match the prefix
		for i := 0; i < len(callerPrefix); i++ {
			if i >= len(candidateSegs) {
				return false // candidate shorter than caller prefix
			}

			candidateSeg := candidateSegs[i]
			if candidateSeg == "*" && i == len(candidateSegs)-1 && candidateTrailing {
				// This is the candidate's trailing *, at position before caller's trailing *
				// e.g., caller "project/P/*/*", candidate "project/P/*"
				// The candidate can match "project/P" which caller cannot (requires 2 more segments)
				return false
			}

			switch {
			case callerPrefix[i] == "*":
				// Mid-path wildcard in caller
				if candidateSeg != "*" {
					// Caller has mid-path *, candidate has specific segment - OK
					continue
				}
			case candidateSeg == "*":
				// Candidate has wildcard where caller has specific segment
				// Mid-path * in candidate not covered by specific segment
				return false
			case callerPrefix[i] != candidateSeg:
				return false // different segments
			}
		}

		// If candidate has trailing * and same length as caller, that's OK
		// If candidate is longer (more segments), check if it's all covered
		if candidateTrailing && len(candidateSegs) == len(callerSegs) {
			return true // Same pattern
		}

		// For non-trailing candidate or shorter candidate, prefix match is enough
		return true
	}

	// Caller does not have trailing *, must be exact match
	if len(candidateSegs) != len(callerSegs) {
		return false
	}

	for i := 0; i < len(candidateSegs); i++ {
		cand := candidateSegs[i]
		call := callerSegs[i]

		switch {
		case cand == "*":
			// Candidate has wildcard (mid-path or trailing)
			if call != "*" {
				// Caller has specific segment, candidate wildcard not covered
				return false
			}
		case call == "*":
			// Caller wildcard covers candidate's specific segment
			continue
		case cand != call:
			return false // different specific segments
		}
	}

	return true
}

func pairDenied(grants []Grant, action, resource string) bool {
	for _, g := range grants {
		if g.Policy == nil {
			continue
		}
		if g.ProjectID != "" && !inProject(g.ProjectID, resource) && !mayReachProject(resource) {
			continue
		}
		for _, s := range g.Policy.Statements {
			if s.Effect != EffectDeny {
				continue
			}
			if anyOverlap(s.Actions, action, actionsOverlap) && anyOverlap(s.Resources, resource, resourcesOverlap) {
				return true
			}
		}
	}
	return false
}

func anyOverlap(patterns []string, v string, overlap func(a, b string) bool) bool {
	for _, p := range patterns {
		if overlap(p, v) {
			return true
		}
	}
	return false
}

// mayReachProject reports whether a resource pattern could name resources of
// some project it does not spell out ("*", "project/*", "project/*/...").
func mayReachProject(resource string) bool {
	if resource == "*" {
		return true
	}
	segs := splitPath(resource)
	return len(segs) >= 2 && segs[0] == "project" && segs[1] == "*"
}

// actionsOverlap reports whether two action patterns can match a common
// action. Patterns are "*", "domain:*" or an exact action, so they overlap
// exactly when one matches the other.
func actionsOverlap(a, b string) bool {
	return MatchAction(a, b) || MatchAction(b, a)
}

// resourcesOverlap reports whether two resource patterns can match a common
// resource. A mid-path "*" stands for one segment, a trailing "*" for zero or
// more remaining segments.
func resourcesOverlap(a, b string) bool {
	as, bs := splitPath(a), splitPath(b)
	n := min(len(as), len(bs))
	for i := 0; i < n; i++ {
		if (as[i] == "*" && i == len(as)-1) || (bs[i] == "*" && i == len(bs)-1) {
			return true
		}
		if as[i] != "*" && bs[i] != "*" && as[i] != bs[i] {
			return false
		}
	}
	if len(as) == len(bs) {
		return true
	}
	// One pattern ran out: the longer one overlaps only if what is left of it
	// can match nothing, i.e. it is a single trailing "*".
	rest := as[n:]
	if len(bs) > len(as) {
		rest = bs[n:]
	}
	return len(rest) == 1 && rest[0] == "*"
}

func splitPath(s string) []string { return strings.Split(s, "/") }

// Registry returns the action registry this authorizer validates with, so
// callers (role validation, the action catalogue) share the one instance
// plugins register their actions into.
func (a *Authorizer) Registry() *Registry { return a.reg }

// Schema returns the condition attribute schema this authorizer validates
// with.
func (a *Authorizer) Schema() *AttributeSchema { return a.schema }

// PolicyMatchID is the RoleID reported for statements of the policy under test
// in Simulate results.
const PolicyMatchID = "policy"

// Simulate answers a what-if: would action on resource be allowed if policy
// were in force? With a nil principal only the given policy is evaluated.
// With a principal, that principal's own grants are evaluated together with
// the policy (as if it were one more attached role) and principal.id /
// principal.type are set from it. Condition attributes come only from attrs
// (no resource is loaded: the resource may not exist); a key that is not
// supplied counts as absent. Matched statements of the given policy carry
// RoleID PolicyMatchID. A store error is returned.
func (a *Authorizer) Simulate(ctx context.Context, p *Principal, policy *Policy, action, resource string, attrs map[string][]string) (Result, error) {
	var grants []Grant
	req := Request{Action: action, Resource: resource, Attrs: map[string][]string{}}
	for k, v := range attrs {
		req.Attrs[k] = v
	}
	if p != nil {
		if !p.valid() {
			return Result{}, fmt.Errorf("iam: invalid principal")
		}
		var err error
		if grants, err = a.store.ListGrants(ctx, *p); err != nil {
			return Result{}, fmt.Errorf("iam: list grants: %w", err)
		}
		req.Attrs["principal.id"] = []string{p.ID}
		req.Attrs["principal.type"] = []string{p.Type}
	}
	if _, id, _ := ParseResource(resource); id != "" && id != "*" {
		if _, set := req.Attrs["resource.id"]; !set {
			req.Attrs["resource.id"] = []string{id}
		}
	}
	if policy != nil {
		grants = append(grants, Grant{RoleID: PolicyMatchID, Policy: policy})
	}
	return Evaluate(grants, req), nil
}

// NarrowToProject returns policy as it applies when attached with project
// scope to project projectID: the evaluator intersects a project-scoped
// attachment's resources with project/<projectID>/..., so only that part is
// ever granted (or denied) there. The result keeps just that part:
//
//   - "*" becomes "project/<P>/*";
//   - "project/*" becomes "project/<P>/*", and "project/*/rest" has its project
//     segment replaced: "project/<P>/rest";
//   - patterns already inside P are kept;
//   - patterns that cannot reach inside P (platform roots such as "user/*",
//     the project collection "project", patterns naming another project) are
//     dropped, and a statement left with no resources is dropped, Allow and
//     Deny alike.
//
// Conditions, sids, effects and actions are preserved. policy is not modified;
// a nil policy yields nil. Used to judge whether a caller may assign a role
// inside one project: what matters is what the attachment can do there.
func NarrowToProject(policy *Policy, projectID string) *Policy {
	if policy == nil {
		return nil
	}
	out := &Policy{Version: policy.Version, Statements: []Statement{}}
	for _, st := range policy.Statements {
		var resources []string
		for _, r := range st.Resources {
			if n, ok := narrowResource(r, projectID); ok && !containsString(resources, n) {
				resources = append(resources, n)
			}
		}
		if len(resources) == 0 {
			continue
		}
		st.Resources = resources
		out.Statements = append(out.Statements, st)
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func narrowResource(r, projectID string) (string, bool) {
	if r == "*" {
		return "project/" + projectID + "/*", true
	}
	segs := splitPath(r)
	if segs[0] != "project" || len(segs) < 2 {
		return "", false
	}
	switch {
	case segs[1] == "*" && len(segs) == 2:
		return "project/" + projectID + "/*", true
	case segs[1] == "*":
		segs[1] = projectID
		return strings.Join(segs, "/"), true
	case segs[1] == projectID:
		return r, true
	}
	return "", false
}
