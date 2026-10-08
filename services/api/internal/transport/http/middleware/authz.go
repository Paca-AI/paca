package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// EnforceActions writes the rejection itself and reports whether the caller
// passes every check (see RequireActions).
func EnforceActions(w http.ResponseWriter, r *http.Request, a *iam.Authorizer, checks ...ActionCheck) bool {
	allowed, err := authorizeAll(r, a, checks)
	return proceedIfAllowed(w, r, allowed, err)
}

// proceedIfAllowed turns a check's result into a response: nothing (and true)
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

// servePublicProject serves next to an unauthenticated caller only when the
// project named by the "projectId" route parameter is public, and answers 401
// otherwise (400 for a malformed id).
func servePublicProject(w http.ResponseWriter, r *http.Request, checker ProjectVisibilityChecker, next http.Handler) {
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
	if checker == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
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
}
