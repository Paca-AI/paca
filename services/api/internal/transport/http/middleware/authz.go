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

// PermissionGroup pairs a scope resolver with the permissions required in that
// scope. A caller satisfies a group by holding ALL of its permissions in the
// scope its resolver names. Used with RequirePublicProjectOrPermissions to
// express OR-style policies: satisfying any one group is enough.
type PermissionGroup struct {
	Scope       ScopeResolver
	Permissions []authz.Permission
}

// RequirePermissions enforces permission-based authorization and supports
// global and project-scoped checks: the caller must hold ALL of permissions in
// the scope the resolver names.
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

// EnforcePermissions is RequirePermissions without the handler chain: it
// writes the rejection itself and reports whether the request may proceed. For
// callers whose gate is only known at request time, such as plugin routes
// that declare their own requirePermissions in a manifest.
func EnforcePermissions(w http.ResponseWriter, r *http.Request, authorizer *authz.Authorizer, scope ScopeResolver, permissions ...authz.Permission) bool {
	allowed, err := evaluate(r, authorizer, []PermissionGroup{{Scope: scope, Permissions: permissions}})
	return proceedIfAllowed(w, r, allowed, err)
}

// evaluate is the single place a request's caller is turned into authorizer
// calls, so every gate treats callers identically. It reports whether the
// caller satisfies at least one of groups, evaluated in order (the first
// satisfied group short-circuits).
//
// The caller is either an agent — an agent-API-key request naming one, judged
// by its own role: in the project for a project scope, its own global role
// otherwise — or a human, judged by the permissions their assigned roles
// store. Deliberately never the shared bot user behind the agent API key
// (seeded SUPER_ADMIN), which would let any agent act with full privilege.
//
// A group whose scope cannot be resolved (e.g. a malformed project id) is
// skipped rather than fatal, so another group can still admit the caller; if
// none does, the first such error is returned in place of a plain denial.
//
// The result is (true, nil) when allowed, (false, nil) when merely not
// permitted (the caller answers 403), and (false, err) when the request
// could not be judged: unauthenticated, an invalid subject or scope, or an
// authorizer failure.
func evaluate(r *http.Request, authorizer *authz.Authorizer, groups []PermissionGroup) (bool, error) {
	claims := ClaimsFrom(r)
	if claims == nil {
		return false, apierr.New(apierr.CodeUnauthenticated, "unauthenticated")
	}
	if authorizer == nil {
		return false, apierr.New(apierr.CodeInternalError, "authorization not configured")
	}

	agentID, isAgent := AgentIDFromRequest(r)
	var userID uuid.UUID
	if !isAgent {
		id, err := uuid.Parse(claims.Subject)
		if err != nil {
			return false, apierr.New(apierr.CodeBadRequest, "invalid subject claim")
		}
		userID = id
	}

	var firstScopeErr error
	for _, group := range groups {
		resolve := group.Scope
		if resolve == nil {
			resolve = GlobalScope()
		}
		projectID, err := resolve(r)
		if err != nil {
			if firstScopeErr == nil {
				firstScopeErr = err
			}
			continue
		}

		var allowed bool
		switch {
		case isAgent && projectID != nil:
			allowed, err = authorizer.HasPermissionsForAgent(r.Context(), agentID, *projectID, group.Permissions...)
		case isAgent:
			allowed, err = authorizer.HasGlobalPermissionsForAgent(r.Context(), agentID, group.Permissions...)
		default:
			allowed, err = authorizer.HasPermissions(r.Context(), userID, projectID, group.Permissions...)
		}
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}
	return false, firstScopeErr
}

// proceedIfAllowed turns evaluate's result into a response: nothing (and true)
// when the request may proceed, otherwise the matching error (and false).
func proceedIfAllowed(w http.ResponseWriter, r *http.Request, allowed bool, err error) bool {
	switch {
	case err != nil:
		presenter.Error(w, r, err)
		return false
	case !allowed:
		presenter.Error(w, r, apierr.New(apierr.CodeForbidden, "insufficient permissions"))
		return false
	}
	return true
}

// ProjectVisibilityChecker is the minimal interface the public-project
// middleware requires. It is satisfied by *projectsvc.Service.
type ProjectVisibilityChecker interface {
	IsProjectPublic(ctx context.Context, id uuid.UUID) (bool, error)
}

// RequirePublicProjectOrPermissions serves a read-only project route that may
// also be reached without a project role:
//
//   - An authenticated caller must satisfy at least one of groups (the same
//     logic every other gate uses). Being logged in does not fall back to the
//     public flag below.
//   - A caller who is not authenticated is admitted when the project named by
//     the "projectId" route parameter has is_public = true, and receives 401
//     otherwise.
//
// Use it instead of RequirePermissions on read-only project-scoped routes that
// should be open to anonymous visitors of a public project.
func RequirePublicProjectOrPermissions(checker ProjectVisibilityChecker, authorizer *authz.Authorizer, groups ...PermissionGroup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Authenticated path: run the normal permission check.
			if ClaimsFrom(r) != nil {
				allowed, err := evaluate(r, authorizer, groups)
				if proceedIfAllowed(w, r, allowed, err) {
					next.ServeHTTP(w, r)
				}
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

// AgentAccessChecker resolves whether a specific member has usage access to
// a specific agent — see agentdom.AgentAccessGrantService.HasAgentUsageAccess.
// Satisfied directly by *agentsvc.Service.
type AgentAccessChecker interface {
	HasAgentUsageAccess(ctx context.Context, projectID, agentID, memberID uuid.UUID) (bool, error)
}

// EnvironmentAccessChecker is AgentAccessChecker's environment sibling —
// satisfied directly by *environmentsvc.Service.
type EnvironmentAccessChecker interface {
	HasEnvironmentUsageAccess(ctx context.Context, projectID, environmentID, memberID uuid.UUID) (bool, error)
}

// resolveActorMemberID resolves the caller of r (human or agent, same dual
// path EnforcePermissions already uses) to their project_members.id in
// projectID, via the canonical projectdom.MemberRepository.FindMemberByActor
// lookup — reused as-is rather than a new one, per the same convention task
// assignees and agent_chat_sessions.member_id already follow.
func resolveActorMemberID(r *http.Request, memberRepo projectdom.MemberRepository, projectID uuid.UUID) (uuid.UUID, error) {
	ctx := r.Context()
	if agentID, ok := AgentIDFromRequest(r); ok {
		m, err := memberRepo.FindMemberByActor(ctx, projectID, uuid.Nil, &agentID)
		if err != nil {
			return uuid.Nil, err
		}
		return m.ID, nil
	}
	claims := ClaimsFrom(r)
	if claims == nil {
		return uuid.Nil, apierr.New(apierr.CodeUnauthenticated, "unauthenticated")
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid subject claim")
	}
	m, err := memberRepo.FindMemberByActor(ctx, projectID, userID, nil)
	if err != nil {
		return uuid.Nil, err
	}
	return m.ID, nil
}

// RequireAgentAccess additionally gates a route on the caller holding an
// access grant for a restricted agent (identified by the "agentId" URL
// param) — see agentdom.AgentAccessGrantService's doc comment. Must run
// AFTER RequirePermissions in the chain, not instead of it: the plain
// permission check still governs whether the caller may use agents at all;
// this only narrows that further to the specific agent in the URL when it's
// restricted.
func RequireAgentAccess(checker AgentAccessChecker, memberRepo projectdom.MemberRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			projectID, err := ProjectScopeFromParam("projectId")(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			agentID, err := uuid.Parse(chi.URLParam(r, "agentId"))
			if err != nil {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid agent id"))
				return
			}
			memberID, err := resolveActorMemberID(r, memberRepo, *projectID)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			ok, err := checker.HasAgentUsageAccess(r.Context(), *projectID, agentID, memberID)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			if !ok {
				presenter.Error(w, r, apierr.New(apierr.CodeAgentAccessRestricted, "this agent is restricted — you don't have access to use it"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireEnvironmentAccess is RequireAgentAccess's environment sibling,
// keyed off the "environmentId" URL param.
func RequireEnvironmentAccess(checker EnvironmentAccessChecker, memberRepo projectdom.MemberRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			projectID, err := ProjectScopeFromParam("projectId")(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			environmentID, err := uuid.Parse(chi.URLParam(r, "environmentId"))
			if err != nil {
				presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid environment id"))
				return
			}
			memberID, err := resolveActorMemberID(r, memberRepo, *projectID)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			ok, err := checker.HasEnvironmentUsageAccess(r.Context(), *projectID, environmentID, memberID)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			if !ok {
				presenter.Error(w, r, apierr.New(apierr.CodeEnvironmentAccessRestricted, "this environment is restricted — you don't have access to use it"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
