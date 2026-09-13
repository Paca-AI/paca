package middleware

import (
	"testing"

	domainauth "github.com/Paca-AI/api/internal/domain/auth"
)

func TestEnforceTokenScope_FullScope_AllowsAnyPath(t *testing.T) {
	claims := &domainauth.Claims{}
	paths := []string{
		"/api/v1/projects/proj1",
		"/api/v1/port-forwards/resolve",
		"/api/v1/admin/users",
		"/api/v1/projects/proj1/environments/env1/port-forwards/pf1/annotations",
	}
	for _, path := range paths {
		if !enforceTokenScope(claims, path) {
			t.Errorf("full-scope (empty Scope) claims should be allowed on %q", path)
		}
	}
}

func TestEnforceTokenScope_Annotation_AllowsOnlyMatchingPaths(t *testing.T) {
	claims := &domainauth.Claims{Scope: domainauth.ScopeAnnotation}

	allowed := []string{
		"/api/v1/auth/annotation-refresh",
		"/api/v1/port-forwards/resolve",
		"/api/v1/projects/p1/environments/e1/port-forwards/pf1/annotations",
		"/api/v1/projects/p1/environments/e1/port-forwards/pf1/annotations/ann1/resolve",
	}
	for _, path := range allowed {
		if !enforceTokenScope(claims, path) {
			t.Errorf("ScopeAnnotation claims should be allowed on %q", path)
		}
	}

	denied := []string{
		"/api/v1/projects/p1",
		"/api/v1/projects/p1/tasks",
		"/api/v1/auth/refresh", // the *main* refresh endpoint, not annotation-refresh
		"/api/v1/admin/users",
		"/api/v1/projects/p1/annotations", // project-wide search — a different route than the port-forward-scoped one
	}
	for _, path := range denied {
		if enforceTokenScope(claims, path) {
			t.Errorf("ScopeAnnotation claims should NOT be allowed on %q", path)
		}
	}
}

// TestEnforceTokenScope_UnknownScope_FailsClosed is the property that makes
// this function safe to extend later: a Scope value nothing here recognizes
// — a future scope this build predates, or a tampered token — must be
// rejected everywhere, not just on the one path pattern a narrower case
// happens to check.
func TestEnforceTokenScope_UnknownScope_FailsClosed(t *testing.T) {
	claims := &domainauth.Claims{Scope: "some-future-scope-this-build-does-not-know-about"}
	paths := []string{
		"/api/v1/projects/p1",
		"/api/v1/port-forwards/resolve",
		"/api/v1/projects/p1/environments/e1/port-forwards/pf1/annotations",
	}
	for _, path := range paths {
		if enforceTokenScope(claims, path) {
			t.Errorf("an unrecognized Scope must be rejected on every path, was allowed on %q", path)
		}
	}
}
