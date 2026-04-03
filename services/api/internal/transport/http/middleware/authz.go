package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/paca/api/internal/apierr"
	"github.com/paca/api/internal/platform/authz"
	"github.com/paca/api/internal/transport/http/presenter"
)

// ScopeResolver resolves a scope-specific project ID for permission checks.
// nil means global-only authorization.
type ScopeResolver func(c *gin.Context) (*uuid.UUID, error)

// GlobalScope forces global-only permission checks.
func GlobalScope() ScopeResolver {
	return func(*gin.Context) (*uuid.UUID, error) { return nil, nil }
}

// ProjectScopeFromParam resolves a project ID from a route parameter.
func ProjectScopeFromParam(param string) ScopeResolver {
	return func(c *gin.Context) (*uuid.UUID, error) {
		v := c.Param(param)
		if v == "" {
			return nil, apierr.New(apierr.CodeBadRequest, "missing project id")
		}
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, apierr.New(apierr.CodeBadRequest, "invalid project id")
		}
		return &id, nil
	}
}

// RequirePermissions enforces permission-based authorization and supports
// global and project-scoped checks.
func RequirePermissions(authorizer *authz.Authorizer, scope ScopeResolver, permissions ...authz.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims == nil {
			presenter.Error(c, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
			return
		}

		if authorizer == nil {
			presenter.Error(c, apierr.New(apierr.CodeInternalError, "authorization not configured"))
			return
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
			return
		}

		resolver := scope
		if resolver == nil {
			resolver = GlobalScope()
		}
		projectID, err := resolver(c)
		if err != nil {
			presenter.Error(c, err)
			return
		}

		allowed, err := authorizer.HasPermissions(c.Request.Context(), userID, projectID, claims.Role, permissions...)
		if err != nil {
			presenter.Error(c, err)
			return
		}
		if !allowed {
			presenter.Error(c, apierr.New(apierr.CodeForbidden, "insufficient permissions"))
			return
		}

		c.Next()
	}
}

// Authz keeps backwards-compatible middleware semantics for global scope.
func Authz(authorizer *authz.Authorizer, permissions ...authz.Permission) gin.HandlerFunc {
	return RequirePermissions(authorizer, GlobalScope(), permissions...)
}

// PermissionGroup pairs a scope resolver with the permissions required in that scope.
// Used with RequireAnyPermissions to express OR-style authorization policies.
type PermissionGroup struct {
	Scope       ScopeResolver
	Permissions []authz.Permission
}

// RequireAnyPermissions grants access if the user satisfies at least one of the
// provided PermissionGroups. Groups are evaluated in order; the first satisfied
// group short-circuits the check. If no group is satisfied, 403 is returned.
//
// Typical use: allow access when the caller holds a broad global permission
// (e.g. projects.read) OR a narrower project-scoped one.
func RequireAnyPermissions(authorizer *authz.Authorizer, groups ...PermissionGroup) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims := ClaimsFrom(c)
		if claims == nil {
			presenter.Error(c, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
			return
		}

		if authorizer == nil {
			presenter.Error(c, apierr.New(apierr.CodeInternalError, "authorization not configured"))
			return
		}

		userID, err := uuid.Parse(claims.Subject)
		if err != nil {
			presenter.Error(c, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
			return
		}

		var firstScopeErr error
		for _, group := range groups {
			resolver := group.Scope
			if resolver == nil {
				resolver = GlobalScope()
			}
			projectID, err := resolver(c)
			if err != nil {
				// Capture the first scope-resolution error; still try remaining groups
				// (a later global-scope group may succeed without needing projectId).
				if firstScopeErr == nil {
					firstScopeErr = err
				}
				continue
			}
			allowed, err := authorizer.HasPermissions(c.Request.Context(), userID, projectID, claims.Role, group.Permissions...)
			if err != nil {
				presenter.Error(c, err)
				return
			}
			if allowed {
				c.Next()
				return
			}
		}

		// Surface the scope-resolution error (e.g. 400 for an invalid projectId)
		// rather than hiding it behind a generic 403 Forbidden.
		if firstScopeErr != nil {
			presenter.Error(c, firstScopeErr)
			return
		}
		presenter.Error(c, apierr.New(apierr.CodeForbidden, "insufficient permissions"))
	}
}
