package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// ScopeResolver resolves a scope-specific project ID for permission checks.
// nil means global-only authorization.
type ScopeResolver func(r *http.Request) (*uuid.UUID, error)

// GlobalScope forces global-only permission checks.
func GlobalScope() ScopeResolver {
	return func(*http.Request) (*uuid.UUID, error) { return nil, nil }
}

// ProjectScopeFromParam resolves a project ID from a chi URL parameter.
func ProjectScopeFromParam(param string) ScopeResolver {
	return func(r *http.Request) (*uuid.UUID, error) {
		v := chi.URLParam(r, param)
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
func RequirePermissions(authorizer *authz.Authorizer, scope ScopeResolver, permissions ...authz.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !EnforcePermissions(w, r, authorizer, scope, permissions...) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// EnforcePermissions checks authorization without advancing the handler chain.
func EnforcePermissions(w http.ResponseWriter, r *http.Request, authorizer *authz.Authorizer, scope ScopeResolver, permissions ...authz.Permission) bool {
	claims := ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return false
	}

	if authorizer == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
		return false
	}

	resolver := scope
	if resolver == nil {
		resolver = GlobalScope()
	}
	projectID, err := resolver(r)
	if err != nil {
		presenter.Error(w, r, err)
		return false
	}

	agentID, hasAgentID := AgentIDFromRequest(r)

	var allowed bool
	if hasAgentID && projectID != nil {
		allowed, err = authorizer.HasPermissionsForAgent(r.Context(), agentID, *projectID, permissions...)
	} else {
		userID, parseErr := uuid.Parse(claims.Subject)
		if parseErr != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
			return false
		}
		allowed, err = authorizer.HasPermissions(r.Context(), userID, projectID, claims.Role, permissions...)
	}

	if err != nil {
		presenter.Error(w, r, err)
		return false
	}
	if !allowed {
		presenter.Error(w, r, apierr.New(apierr.CodeForbidden, "insufficient permissions"))
		return false
	}

	return true
}

// Authz keeps backwards-compatible middleware semantics for global scope.
func Authz(authorizer *authz.Authorizer, permissions ...authz.Permission) func(http.Handler) http.Handler {
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
func RequireAnyPermissions(authorizer *authz.Authorizer, groups ...PermissionGroup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFrom(r)
			if claims == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
				return
			}

			if authorizer == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
				return
			}

			agentID, hasAgentID := AgentIDFromRequest(r)
			var userID uuid.UUID

			if !hasAgentID {
				var parseErr error
				userID, parseErr = uuid.Parse(claims.Subject)
				if parseErr != nil {
					presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
					return
				}
			}

			var firstScopeErr error
			for _, group := range groups {
				resolver := group.Scope
				if resolver == nil {
					resolver = GlobalScope()
				}
				projectID, err := resolver(r)
				if err != nil {
					if firstScopeErr == nil {
						firstScopeErr = err
					}
					continue
				}

				var allowed bool
				if hasAgentID && projectID != nil {
					allowed, err = authorizer.HasPermissionsForAgent(r.Context(), agentID, *projectID, group.Permissions...)
				} else {
					allowed, err = authorizer.HasPermissions(r.Context(), userID, projectID, claims.Role, group.Permissions...)
				}

				if err != nil {
					presenter.Error(w, r, err)
					return
				}
				if allowed {
					next.ServeHTTP(w, r)
					return
				}
			}

			if firstScopeErr != nil {
				presenter.Error(w, r, firstScopeErr)
				return
			}
			presenter.Error(w, r, apierr.New(apierr.CodeForbidden, "insufficient permissions"))
		})
	}
}

// ProjectVisibilityChecker is the minimal interface the public-project
// middleware requires. It is satisfied by *projectsvc.Service.
type ProjectVisibilityChecker interface {
	IsProjectPublic(ctx context.Context, id uuid.UUID) (bool, error)
}

// RequirePublicProjectOrPermissions grants access when at least one of the
// following conditions is true:
//
//   - The request is authenticated and the caller satisfies any of the
//     provided PermissionGroups (same logic as RequireAnyPermissions).
//   - The project identified by the "projectId" route parameter has
//     is_public = true, regardless of authentication status.
//
// Use this instead of RequireAnyPermissions on read-only project-scoped routes
// that should be accessible to anonymous users when the project is public.
func RequirePublicProjectOrPermissions(checker ProjectVisibilityChecker, authorizer *authz.Authorizer, groups ...PermissionGroup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFrom(r)

			agentID, hasAgentID := AgentIDFromRequest(r)
			var userID uuid.UUID

			if !hasAgentID && claims != nil {
				var parseErr error
				userID, parseErr = uuid.Parse(claims.Subject)
				if parseErr != nil {
					presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
					return
				}
			}

			// Authenticated path: run normal permission check.
			if claims != nil {
				var firstScopeErr error
				for _, group := range groups {
					resolver := group.Scope
					if resolver == nil {
						resolver = GlobalScope()
					}
					projectID, err := resolver(r)
					if err != nil {
						if firstScopeErr == nil {
							firstScopeErr = err
						}
						continue
					}

					var allowed bool
					if hasAgentID && projectID != nil {
						allowed, err = authorizer.HasPermissionsForAgent(r.Context(), agentID, *projectID, group.Permissions...)
					} else {
						allowed, err = authorizer.HasPermissions(r.Context(), userID, projectID, claims.Role, group.Permissions...)
					}

					if err != nil {
						presenter.Error(w, r, err)
						return
					}
					if allowed {
						next.ServeHTTP(w, r)
						return
					}
				}
				if firstScopeErr != nil {
					presenter.Error(w, r, firstScopeErr)
					return
				}
				presenter.Error(w, r, apierr.New(apierr.CodeForbidden, "insufficient permissions"))
				return
			}

			// Unauthenticated path: allow only when the project is public.
			projectIDStr := chi.URLParam(r, "projectId")
			if projectIDStr == "" {
				presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
				return
			}
			projectID, err := uuid.Parse(projectIDStr)
			if err != nil {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid project id"))
				return
			}
			isPublic, err := checker.IsProjectPublic(r.Context(), projectID)
			if err != nil {
				if errors.Is(err, projectdom.ErrNotFound) {
					presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
					return
				}
				presenter.Error(w, r, err)
				return
			}
			if !isPublic {
				presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
