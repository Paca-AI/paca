package iam

import (
	"context"
	"fmt"
	"strings"
)

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

func splitPath(s string) []string { return strings.Split(s, "/") }
