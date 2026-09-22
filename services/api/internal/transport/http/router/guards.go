package router

import (
	"net/http"

	"github.com/Paca-AI/api/internal/platform/authz"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// guards builds the permission middleware that routes are declared with. It is
// the one place that says what each kind of gate means, so a route reads as a
// statement of policy:
//
//	r.With(require.Project(authz.PermissionSprintsWrite)).Post("/", h.CreateSprint)
//
// The rules every gate follows:
//
//   - A route declares everything its operation needs, right here in the
//     router; handlers never check permissions themselves.
//   - A gate requires ALL the permissions it is given. An operation that
//     crosses two capabilities lists both — creating a project agent also
//     binds it to a project role, so it requires agents.write and
//     project.members.write.
//   - Only what a caller's role actually stores can satisfy a gate. A role's
//     name (e.g. the "ADMIN" in a token) never grants anything.
type guards struct {
	authorizer *authz.Authorizer
	visibility httpmw.ProjectVisibilityChecker
}

func newGuards(deps Deps) guards {
	return guards{authorizer: deps.Authorizer, visibility: deps.ProjectVisibilitySvc}
}

// Global requires permissions held at global scope: the admin surface
// (/admin/...) and every other route that isn't tied to one project.
func (g guards) Global(permissions ...authz.Permission) func(http.Handler) http.Handler {
	return httpmw.RequirePermissions(g.authorizer, httpmw.GlobalScope(), permissions...)
}

// Project requires permissions in the project named by the {projectId} URL
// parameter, granted by the caller's role in that project. A global role does
// not reach into a project through a named permission (projects.*, agents.*,
// ...); only the "*" wildcard held by SUPER_ADMIN does.
func (g guards) Project(permissions ...authz.Permission) func(http.Handler) http.Handler {
	return httpmw.RequirePermissions(g.authorizer, httpmw.ProjectScopeFromParam("projectId"), permissions...)
}

// ProjectOrPublic gates a read-only project route that can also be served
// without a project role. An authenticated caller is admitted by holding the
// given permissions in the project, or projects.read at global scope. A caller
// who is not authenticated is admitted when the project is public; being
// logged in does not fall back to the public flag.
func (g guards) ProjectOrPublic(permissions ...authz.Permission) func(http.Handler) http.Handler {
	return httpmw.RequirePublicProjectOrPermissions(g.visibility, g.authorizer,
		httpmw.PermissionGroup{Scope: httpmw.GlobalScope(), Permissions: []authz.Permission{authz.PermissionProjectsRead}},
		httpmw.PermissionGroup{Scope: httpmw.ProjectScopeFromParam("projectId"), Permissions: permissions},
	)
}
