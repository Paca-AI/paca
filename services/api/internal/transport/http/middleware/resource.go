package middleware

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
)

// ResourceResolver builds the IAM resource name a request acts on
// ("project/<P>", "project/<P>/environment/<E>", "user/<U>", "settings", ...)
// from the request's URL. An error is returned as-is to the caller (a
// malformed id is a 400 apierr, like ProjectScopeFromParam's).
//
// Cross-project contract: a resolver only ever reads ids from the URL path —
// the project from the {projectId} segment and the entity from its own
// segment — never from the body, the query string or a client-supplied
// header, so a caller cannot name a project of their choosing. Note that the
// authorizer checks that the entity really belongs to that project ONLY when
// a conditional grant needs the entity's attributes (the attribute loader
// enforces entity ∈ project). Without such a condition, a grant on
// "project/<P>/environment/*" is satisfied by any environment id put in the
// URL. Handlers MUST therefore scope their own entity fetch by the URL
// project (e.g. WHERE id = $env AND project_id = $project), exactly as they
// do today, and must not trust an entity id that resolves to another project.
//
// A resolver may return ErrNoResource when, for this request, there is
// nothing to authorize (e.g. a chat started with no environment at all); the
// gate skips that check. The lookup-based resolvers in resource_lookup.go are
// the one place a resource id comes from outside the URL path (the request
// body's environment_id, or an entity loaded by URL ids); the entity is
// still confined to the project in the URL by the service that acts on it.
type ResourceResolver func(r *http.Request) (string, error)

// ErrNoResource is returned by a ResourceResolver to say there is nothing to
// authorize on this request; the gate skips that check.
var ErrNoResource = errors.New("middleware: no resource to authorize")

// StaticResource always names resource (a platform root such as "settings",
// "user/*" or the project collection "project").
func StaticResource(resource string) ResourceResolver {
	return func(*http.Request) (string, error) { return resource, nil }
}

// ProjectResource names the project in the param URL parameter:
// "project/<id>".
func ProjectResource(param string) ResourceResolver {
	return func(r *http.Request) (string, error) {
		projectID, err := urlUUID(r, param, "project")
		if err != nil {
			return "", err
		}
		return "project/" + projectID, nil
	}
}

// ProjectChildResource names an entity inside a project, both taken from URL
// parameters: "project/<projectParam>/<kind>/<idParam>".
func ProjectChildResource(projectParam, kind, idParam string) ResourceResolver {
	return func(r *http.Request) (string, error) {
		projectID, err := urlUUID(r, projectParam, "project")
		if err != nil {
			return "", err
		}
		id, err := urlUUID(r, idParam, kind)
		if err != nil {
			return "", err
		}
		return "project/" + projectID + "/" + kind + "/" + id, nil
	}
}

// ProjectChildCollection names every entity of a kind inside a project, for
// list and create routes: "project/<projectParam>/<kind>/*".
func ProjectChildCollection(projectParam, kind string) ResourceResolver {
	return func(r *http.Request) (string, error) {
		projectID, err := urlUUID(r, projectParam, "project")
		if err != nil {
			return "", err
		}
		return "project/" + projectID + "/" + kind + "/*", nil
	}
}

// PlatformChildResource names one platform-level entity: "<root>/<id>" with id
// from the idParam URL parameter ("user/<U>", "role/<R>", "agent/<A>").
func PlatformChildResource(root, idParam string) ResourceResolver {
	return func(r *http.Request) (string, error) {
		id, err := urlUUID(r, idParam, root)
		if err != nil {
			return "", err
		}
		return root + "/" + id, nil
	}
}

// urlUUID reads a UUID URL parameter and returns it in canonical (lowercase)
// form, so resource names always compare equal to the ids stored in policies.
func urlUUID(r *http.Request, param, what string) (string, error) {
	v := chi.URLParam(r, param)
	if v == "" {
		return "", apierr.New(apierr.CodeBadRequest, "missing "+what+" id")
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return "", apierr.New(apierr.CodeBadRequest, "invalid "+what+" id")
	}
	return id.String(), nil
}
