package router

import (
	"net/http"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
	httpmw "github.com/Paca-AI/api/internal/transport/http/middleware"
)

// guards builds the authorization middleware that routes are declared with.
// It is the one place that says what each kind of gate means, so a route reads
// as a statement of policy:
//
//	r.With(require.Project(iam.ActionSprintsWrite)).Post("/", h.CreateSprint)
//
// The rules every gate follows:
//
//   - A route declares everything its operation needs, right here in the
//     router; handlers never check permissions themselves.
//   - A gate requires ALL the actions it is given. An operation that
//     crosses two capabilities lists both — creating a project agent also
//     binds it to a project role, so it requires agents:write and
//     project.members:write. (Changing an existing member's roles is only
//     role assignment: roles:assign, checked per role, nothing else.)
//   - Every gate is decided by the IAM authorizer (package iam), each action
//     checked on the resource the gate names (see each gate). Role names
//     confer nothing.
//
// Every gate is wrapped in a gateHandler marker so route_coverage_test can
// tell, from the route table alone, that a route is gated and on which
// resource.
type guards struct {
	iam        *iam.Authorizer
	visibility httpmw.ProjectVisibilityChecker
	agentEnvs  httpmw.AgentEnvironmentLookup
	sessionEnv httpmw.SessionEnvironmentLookup
	// rolePolicies backs the escalation guard of making a role the default.
	rolePolicies httpmw.RolePolicyLookup
	// roleAttachments backs the assignment gates (which roles a request adds
	// and removes).
	roleAttachments httpmw.RoleAttachmentLookup
	// members resolves the assignees a task request names to principal ids.
	members httpmw.MemberPrincipalLookup
	// taskNumbers resolves GET /tasks/by-number/{n} to the task it names.
	taskNumbers httpmw.TaskNumberLookup
}

func newGuards(deps Deps) guards {
	return guards{
		iam: deps.IAM, visibility: deps.ProjectVisibilitySvc, agentEnvs: deps.AgentEnvironments,
		sessionEnv: deps.SessionEnvironments, rolePolicies: deps.RolePolicies, roleAttachments: deps.RoleAttachments, members: deps.MemberPrincipals, taskNumbers: deps.TaskNumbers,
	}
}

// gateHandler marks a handler produced by a guards gate. It has no runtime
// effect (ServeHTTP is the wrapped gate's); it exists so the route coverage
// test can find every route's gate by applying each of the route's
// middlewares to a stub and looking for this type.
type gateHandler struct {
	http.Handler
	// resource is the resource template the gate authorizes against.
	resource string
}

func markGate(resource string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return gateHandler{Handler: mw(next), resource: resource}
	}
}

// Resource templates the gates report through gateHandler.
const (
	resProject      = "project/{projectId}"
	resAgent        = "project/{projectId}/agent/{agentId}"
	resTask         = "project/{projectId}/task/{taskId}"
	resDoc          = "project/{projectId}/doc/{docId}"
	resSprint       = "project/{projectId}/sprint/{sprintId}"
	resView         = "project/{projectId}/view/{viewId}"
	resWorkflow     = "project/{projectId}/workflow/{automationId}"
	resConversation = "project/{projectId}/conversation/{conversationId}"
	resAnnotation   = "project/{projectId}/annotation/{annotationId}"

	// Request-attribute gates (see TaskCreate/TaskChange/DocCreate/DocChange):
	// they authorize the attribute values a request body sets.
	resTaskAttrs = "request attributes of a task create/update"
	resDocAttrs  = "request attributes of a document create/update"
	resViewAttrs = "request attributes of a view create (sprint from the query)"
	// resViewReorderItems: a view reorder names its views in the body, and
	// each one is authorized individually.
	resViewReorderItems = "views named in view_ids of a view reorder"
	// resTaskPositionItems: a bulk task-position write names its tasks in the
	// body, and each one is authorized individually.
	resTaskPositionItems = "tasks named in the items of a bulk task-position update"
	resEnvironment       = "project/{projectId}/environment/{environmentId}"
	resPlatform          = "platform-root"
	// resChatEnvironment / resSessionEnvironment name the environment a chat
	// runs in, resolved from the request (see ChatEnvironment/SessionEnvironment).
	resChatEnvironment    = "project/{projectId}/environment/{environmentId from body, else the agent's default}"
	resSessionEnvironment = "project/{projectId}/environment/{environmentId of the session}"

	// Roles and attachments (see Roles*/Assign* gates below).
	resRoleCollection        = "role/*"
	resRole                  = "role/{roleId}"
	resProjectRoleCollection = "project/{projectId}/role/*"
	resProjectRole           = "project/{projectId}/role/{roleId}"
	resUser                  = "user/{userId}"
	resGlobalAgent           = "agent/{agentId}"

	// resGrantablePolicy / resGrantableRole name the escalation guards of role
	// create/update and set-default (they authorize against the caller's own
	// grants, not a resource).
	resGrantablePolicy = "escalation guard: the policy in the body must be grantable by the caller"
	resGrantableRole   = "escalation guard: the role in the URL must be grantable by the caller"

	// resAssignRoles / resAssignProjectRoles name the assignment gates: roles:assign
	// is authorized on the resource of each role the request adds or removes.
	resAssignRoles        = "roles:assign on role/{roleId} for each role the request adds or removes"
	resAssignProjectRoles = "roles:assign on project/{projectId}/role/{roleId} for each role the request adds or removes"
)

// Global requires permissions held at platform level: the admin surface
// (/admin/...) and every other route that isn't tied to one project. Each
// action is checked on its platform root (iam.PlatformRootFor: users ->
// user/*, roles -> role/*, settings -> settings, projects -> project, ...);
// an action with no platform root is checked on "*", so only "*" holders
// pass.
func (g guards) Global(actions ...iam.Action) func(http.Handler) http.Handler {
	checks := make([]httpmw.ActionCheck, len(actions))
	for i, a := range actions {
		checks[i] = httpmw.ActionCheck{Action: a, Resource: httpmw.StaticResource(iam.PlatformResource(a))}
	}
	return markGate(resPlatform, httpmw.RequireActions(g.iam, checks...))
}

// Project requires the actions on project/{projectId}. A platform role's named
// permissions only cover the platform roots, so they never reach into a
// project (GHSA-hjcj-373w-vq8m); "*" on "*" does.
func (g guards) Project(actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resProject, g.actions(httpmw.ProjectResource("projectId"), actions))
}

// ProjectOrPublic gates a read-only project route that can also be served
// without a project role. An authenticated caller is admitted by being
// allowed the actions on project/{projectId}, or projects:read on the project
// collection "project" (the platform-wide projects:read). A caller who is not
// authenticated is admitted when the project is public; being logged in does
// not fall back to the public flag.
func (g guards) ProjectOrPublic(actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resProject, httpmw.RequirePublicProjectOrActions(g.visibility, g.iam,
		[]httpmw.ActionCheck{{Action: iam.ActionProjectsRead, Resource: httpmw.StaticResource("project")}},
		actionChecks(httpmw.ProjectResource("projectId"), actions),
	))
}

// ProjectEntity requires the actions on one entity inside the project:
// project/{projectId}/<kind>/{idParam}. A Deny on the entity, a role limited
// to some ids, and a condition on its attributes (a task's sprint, a
// document's folder) all apply, which a gate on the project alone cannot
// express. resource is the template reported to the coverage test.
func (g guards) ProjectEntity(kind, idParam, resource string, actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resource, g.actions(httpmw.ProjectChildResource("projectId", kind, idParam), actions))
}

// ProjectEntityOrPublic is ProjectEntity for a read-only route that can also
// be served without a project role (see ProjectOrPublic): the platform-wide
// projects:read or the actions on the entity admit an authenticated caller,
// and anyone is admitted to a public project.
func (g guards) ProjectEntityOrPublic(kind, idParam, resource string, actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resource, httpmw.RequirePublicProjectOrActions(g.visibility, g.iam,
		[]httpmw.ActionCheck{{Action: iam.ActionProjectsRead, Resource: httpmw.StaticResource("project")}},
		actionChecks(httpmw.ProjectChildResource("projectId", kind, idParam), actions),
	))
}

// TaskCreate authorizes creating a task with the sprint, status, type and
// assignees its body names. Declare it after the route's action gate.
func (g guards) TaskCreate() func(http.Handler) http.Handler {
	return markGate(resTaskAttrs, httpmw.RequireRequestAttrs(g.iam, iam.ActionTasksWrite,
		httpmw.ProjectChildCollection("projectId", "task"), httpmw.TaskAttrs(g.members), true))
}

// TaskChange authorizes an update of one task: when the body changes its
// sprint, status, type or assignees, the task must be allowed both before and
// after. Declare it after the route's action gate.
func (g guards) TaskChange() func(http.Handler) http.Handler {
	return markGate(resTaskAttrs, httpmw.RequireRequestAttrs(g.iam, iam.ActionTasksWrite,
		httpmw.ProjectChildResource("projectId", "task", "taskId"), httpmw.TaskAttrs(g.members), false))
}

// TaskPositionItems authorizes tasks:write on every task named in the items of
// a bulk task-position update. Declare it after the route's action gate.
func (g guards) TaskPositionItems() func(http.Handler) http.Handler {
	return markGate(resTaskPositionItems, httpmw.RequireItemTasks(g.iam, iam.ActionTasksWrite, "projectId"))
}

// ViewCreate authorizes creating a view in the sprint its query names
// (context=sprint&sprint_id=...), or with no sprint for the project-level
// backlog and timeline contexts. Declare it after the route's action gate.
func (g guards) ViewCreate() func(http.Handler) http.Handler {
	return markGate(resViewAttrs, httpmw.RequireRequestAttrsFromRequest(g.iam, iam.ActionViewsWrite,
		httpmw.ProjectChildCollection("projectId", "view"), httpmw.ViewAttrs, true))
}

// ViewReorderItems authorizes views:write on every view named in view_ids of
// a view reorder. Declare it after the route's action gate.
func (g guards) ViewReorderItems() func(http.Handler) http.Handler {
	return markGate(resViewReorderItems, httpmw.RequireViewIDs(g.iam, iam.ActionViewsWrite, "projectId"))
}

// DocCreate authorizes creating a document in the folder its body names.
func (g guards) DocCreate() func(http.Handler) http.Handler {
	return markGate(resDocAttrs, httpmw.RequireRequestAttrs(g.iam, iam.ActionDocsWrite,
		httpmw.ProjectChildCollection("projectId", "doc"), httpmw.DocAttrs, true))
}

// DocChange authorizes an update of one document: moving it to another folder
// needs the document allowed both where it is and where it goes.
func (g guards) DocChange() func(http.Handler) http.Handler {
	return markGate(resDocAttrs, httpmw.RequireRequestAttrs(g.iam, iam.ActionDocsWrite,
		httpmw.ProjectChildResource("projectId", "doc", "docId"), httpmw.DocAttrs, false))
}

// TaskByNumberOrPublic is ProjectEntityOrPublic for the route that addresses a
// task by its project-scoped number: the number is resolved to the task, which
// the actions are then authorized on.
func (g guards) TaskByNumberOrPublic(actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resTask, httpmw.RequirePublicProjectOrActions(g.visibility, g.iam,
		[]httpmw.ActionCheck{{Action: iam.ActionProjectsRead, Resource: httpmw.StaticResource("project")}},
		actionChecks(httpmw.TaskByNumberResource(g.taskNumbers, "projectId", "taskNumber"), actions),
	))
}

// Environment requires the actions on one environment:
// project/{projectId}/environment/{environmentId}. Every route on one
// environment uses it, so Deny statements on the environment and
// environment-scoped roles apply to all of them.
func (g guards) Environment(actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resEnvironment, g.actions(httpmw.ProjectChildResource("projectId", "environment", "environmentId"), actions))
}

// AgentUse requires the actions on one project agent:
// project/{projectId}/agent/{agentId} (its chat sessions), so Deny statements
// on the agent and agent-scoped roles apply.
func (g guards) AgentUse(actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resAgent, g.actions(httpmw.ProjectChildResource("projectId", "agent", "agentId"), actions))
}

// ChatEnvironment requires environments:read on the environment a new chat
// will run in: the body's environment_id, else the agent's default (nothing
// to check when there is none). Declare it after AgentUse on the same route
// so the environment is only looked up for a caller already allowed to use
// the agent.
func (g guards) ChatEnvironment() func(http.Handler) http.Handler {
	return markGate(resChatEnvironment, httpmw.RequireAction(g.iam, iam.ActionEnvironmentsRead,
		httpmw.ChatEnvironmentResource(g.agentEnvs, "projectId", "agentId")))
}

// SessionEnvironment requires environments:read on the environment of the
// chat session in the URL (nothing to check when it has none). Declare it
// after AgentUse on the same route.
func (g guards) SessionEnvironment() func(http.Handler) http.Handler {
	return markGate(resSessionEnvironment, httpmw.RequireAction(g.iam, iam.ActionEnvironmentsRead,
		httpmw.SessionEnvironmentResource(g.sessionEnv, "projectId", "agentId", "sessionId")))
}

func (g guards) actions(res httpmw.ResourceResolver, actions []iam.Action) func(http.Handler) http.Handler {
	return httpmw.RequireActions(g.iam, actionChecks(res, actions)...)
}

// actionChecks checks every action on res.
func actionChecks(res httpmw.ResourceResolver, actions []iam.Action) []httpmw.ActionCheck {
	checks := make([]httpmw.ActionCheck, len(actions))
	for i, a := range actions {
		checks[i] = httpmw.ActionCheck{Action: a, Resource: res}
	}
	return checks
}

// PlatformEntity requires the actions on one platform-level entity:
// "<root>/{idParam}" ("user/{userId}", "role/{roleId}", "agent/{agentId}").
// resource is the template reported to the coverage test.
func (g guards) PlatformEntity(root, idParam, resource string, actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resource, g.actions(httpmw.PlatformChildResource(root, idParam), actions))
}

// ProjectRoleCollection requires the actions on every role owned by the
// project: "project/{projectId}/role/*" (list and create).
func (g guards) ProjectRoleCollection(actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resProjectRoleCollection, g.actions(httpmw.ProjectChildCollection("projectId", "role"), actions))
}

// ProjectRole requires the actions on one project role:
// "project/{projectId}/role/{roleId}".
func (g guards) ProjectRole(actions ...iam.Action) func(http.Handler) http.Handler {
	return markGate(resProjectRole, g.actions(httpmw.ProjectChildResource("projectId", "role", "roleId"), actions))
}

// GlobalAgentRoles requires every action on one global agent
// ("agent/{agentId}"). Reading an agent's roles needs roles:read and
// agents:read on it; changing them needs agents:write on it plus the
// AssignGlobalAgentRoles gate.
func (g guards) GlobalAgentRoles(actions ...iam.Action) func(http.Handler) http.Handler {
	return g.PlatformEntity("agent", "agentId", resGlobalAgent, actions...)
}

// GrantablePolicy is the escalation guard of role create/update: the policy
// in the body must be one the caller could grant themselves. Declare it after
// the route's action gate.
func (g guards) GrantablePolicy() func(http.Handler) http.Handler {
	return markGate(resGrantablePolicy, httpmw.RequireGrantablePolicy(g.iam))
}

// AssignUserRoles is the assignment gate of PUT /admin/users/{userId}/roles:
// roles:assign on "role/{roleId}" for each role added to or removed from the
// user (see httpmw.RequireAssignRoles). Declare it after the route's other gates.
func (g guards) AssignUserRoles() func(http.Handler) http.Handler {
	return markGate(resAssignRoles, httpmw.RequireAssignRoles(g.iam, httpmw.UserRolesTarget(g.roleAttachments, "userId")))
}

// AssignGlobalAgentRoles is AssignUserRoles for PUT /admin/agents/{agentId}/roles.
func (g guards) AssignGlobalAgentRoles() func(http.Handler) http.Handler {
	return markGate(resAssignRoles, httpmw.RequireAssignRoles(g.iam, httpmw.AgentRolesTarget(g.roleAttachments, "agentId")))
}

// AssignMemberRoles is the assignment gate of PUT /projects/{projectId}/members/{memberId}/roles:
// roles:assign on "project/{projectId}/role/{roleId}" for each role added to or
// removed from the member, whoever owns the role.
func (g guards) AssignMemberRoles() func(http.Handler) http.Handler {
	return markGate(resAssignProjectRoles, httpmw.RequireAssignRoles(g.iam, httpmw.MemberRolesTarget(g.roleAttachments, "projectId", "memberId")))
}

// AssignNewProjectPrincipalRoles is the assignment gate of the routes that
// create a project member or agent with role_ids (POST /members, POST /agents):
// roles:assign on "project/{projectId}/role/{roleId}" for each role in
// role_ids. A request without role_ids assigns nothing and needs nothing.
func (g guards) AssignNewProjectPrincipalRoles() func(http.Handler) http.Handler {
	return markGate(resAssignProjectRoles, httpmw.RequireAssignRoles(g.iam, httpmw.NewProjectPrincipalTarget("projectId")))
}

// GrantableRoleInPath is the escalation guard of making the role in the URL
// the default (it is handed to every future account).
func (g guards) GrantableRoleInPath() func(http.Handler) http.Handler {
	return markGate(resGrantableRole, httpmw.RequireGrantableRoleInPath(g.rolePolicies, g.iam, "roleId"))
}

// SimulateWithPrincipal admits any authenticated caller to a what-if
// simulation of a policy alone, but requires roles:read on the role
// collection ("role/*") when the request names a principal (that exposes the
// principal's grants). It is deliberately not a marked gate: the route is open
// to authenticated callers and the coverage test lists it as such.
func (g guards) SimulateWithPrincipal() func(http.Handler) http.Handler {
	return httpmw.RequireActionsForSimulatedPrincipal(g.iam,
		httpmw.ActionCheck{Action: iam.ActionRolesRead, Resource: httpmw.StaticResource(iam.PlatformResource(iam.ActionRolesRead))})
}
