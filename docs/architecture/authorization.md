# Authorization

How the API decides whether a caller may do something, and how to keep it that way as routes are added.

The rule in one sentence: **a route declares the permissions its operation needs in `router.go`, and what a caller may do is exactly what their role stores. Nothing else grants access, and no handler decides.**

## The model

**Permissions** are strings such as `users.write` or `project.members.read`, defined in `internal/platform/authz/permissions.go`. `*` means everything and `x.*` means everything under `x`. A permission is either *global* (`users.*`, `global_roles.*`, `settings.write`, `plugins.*`, ...) or *project-scoped* (`tasks.*`, `sprints.*`, `docs.*`, ...).

**Roles** store a set of permissions:

| Kind | Stored in | Held by | Scope |
|---|---|---|---|
| Global role | `global_roles` | a user has exactly one (`users.role_id`); a global agent has at most one (`agents.global_role_id`) | global |
| Project role | `project_roles` | a project member has one per project | that project |

What a caller may do is decided **per request from the permissions their role row stores**. The role's *name* never grants anything: the `role` in a JWT is a display value (the UI and plugins read it), not an authorization input. So editing a role's permissions takes effect on the next request, and a stale token cannot keep a privilege the role no longer has.

**Scopes.** A global role reaches into a specific project only through the `*` wildcard (SUPER_ADMIN). A named permission such as `projects.delete` or `agents.write` held globally does not open a project the caller was never added to; only the caller's role in that project can. Any active project membership implies `projects.read` in that project.

**Agents.** A request made with the agent API key that names an agent is judged by that agent's own role (its project role inside a project, its global role otherwise), never by the shared bot user behind the key, which would give every agent full privilege.

**Built-in roles.** `SUPER_ADMIN` (`*`), `ADMIN` and `USER` are defined in `authz.DefaultGlobalRoles()` and written to their rows **on every startup**. Edits to a built-in role therefore last only until the next restart; make a custom role for a lasting change. The definitions are used only for seeding, never when authorizing.

**The default role.** Exactly one global role has `global_roles.is_default` set: the role every new user and every new global agent starts with. It is data, not a name the API hardcodes, so it can be moved to another role with `PUT /admin/global-roles/{roleId}/set-default` (`global_roles.write`, the same shape as a project's default task status and type) and the role that holds it **cannot be deleted** (`409 GLOBAL_ROLE_IS_DEFAULT`; make another role the default first). A partial unique index keeps it to one. New accounts start with `USER` because startup marks `USER` the default when none is set; that also repairs a deployment that once deleted it. Creating a user or global agent when no default exists fails with `409 GLOBAL_ROLE_NO_DEFAULT` instead of guessing a role. Project roles have no default: a project agent's role is part of its create request.

The default is assigned server-side at creation, so it does not need `global_roles.assign`: someone who may create users or agents but not assign roles still creates them with the default role, and cannot give them another.

## Where it is enforced: the router

`internal/transport/http/router/router.go` builds each route with a gate from `guards.go`:

```go
require := newGuards(deps)

r.With(require.Global(authz.PermissionUsersWrite)).Patch("/users/{userId}", h.AdminUpdateUser)
r.With(require.Project(authz.PermissionSprintsWrite)).Post("/", h.CreateSprint)
r.With(require.ProjectOrPublic(authz.PermissionSprintsRead)).Get("/", h.ListSprints)
```

| Gate | Requires |
|---|---|
| `require.Global(p...)` | every `p` held at global scope. The `/admin` surface and routes not tied to one project. |
| `require.Project(p...)` | every `p` in the project named by `{projectId}`, from the caller's role in that project. |
| `require.ProjectOrPublic(p)` | for read-only project routes: `p` in the project, or `projects.read` globally; a caller who is *not authenticated* is admitted when the project is public. |

All three build on `middleware.RequirePermissions` / `RequirePublicProjectOrPermissions`, which turn a request into authorizer calls in one place (`evaluate` in `middleware/authz.go`).

**A gate requires all the permissions it is given.** An operation that crosses two capabilities lists both. Creating a project agent also binds it to a project role, so it requires `agents.write` and `project.members.write`; creating a task from an annotation requires `annotations.write` and `tasks.write`.

**A privileged operation gets its own route.** Do not hide one behind an optional field of a route with a weaker gate: whoever holds the weaker permission could use it. That is why a user's role is changed only by `PUT /admin/users/{userId}/global-roles` (`global_roles.assign`), and a global agent's role only by `PUT /admin/agents/{agentId}/global-role` (`agents.write` + `global_roles.assign`); create and update refuse the field.

Other middleware add conditions on top of a gate, never instead of it: `RequireAgentAccess` / `RequireEnvironmentAccess` (a restricted agent or environment needs an access grant), `RequireFreshPassword`, `RequireJWTAuth` (no API keys).

### What is not decided in the router

- **Which results a caller sees.** `GET /projects` and `GET /projects/workspace-stats` are open to every authenticated user and return every project to a holder of `projects.read`, and only the caller's own projects otherwise. That is a data-scoping query inside the handler, not a gate.
- **Plugin routes.** A plugin declares its own middleware policy in its manifest, applied when the request arrives (`PluginHandler.applyPluginRouteMiddlewares`, which calls `middleware.EnforcePermissions`).
- **Reading a conversation as an agent** (`agentsvc.Service.authorizeConversationsReadForConversation`): an OR of the agent's global and project roles that answers 404 rather than 403.

## How the web app mirrors it

The UI offers only what the API would accept, using the same permissions (`useCanAssignGlobalRole()` is `global_roles.assign` and `global_roles.read`: the first to assign, the second to list the roles to choose from). The server stays the authority; the UI just never offers a refusal.

| Where | What | Needs |
|---|---|---|
| Create user (dialog) | a wizard, **Details → Role → Password**. The first step's button creates the account, which holds the default role; the Role step is a separate request that changes it, and the one-time password comes last. Without the role permissions it is Details → Password. Once the account exists the Role step cannot be closed without reaching the password (closing it moves on), so the password is never lost. | `users.write`; the Role step also `global_roles.assign` + `read` |
| Edit user (dialog) | name and email only, never a role | `users.write` |
| Users table | the role is a button that opens "Change role" | `global_roles.assign` + `read`, independent of `users.write` |
| Create agent (dialog) | a wizard, **Identity → AI configuration → Role**, the same for both scopes. A project agent's role is its required project role and part of the create request, so its last step creates it. A global agent is created by the second step's button and holds the default global role; the Role step then changes it (another role, or none) as a request of its own, and is offered only to someone who may assign roles (without them the wizard is Identity → AI configuration). Closing that step finishes the wizard. | project agent: `agents.write` + `project.members.write`; global agent: `agents.write`, and the Role step also `global_roles.assign` + `read` |
| Global agent, "Global role" tab | shows, changes and removes the role | `agents.write` + `global_roles.assign` + `read` to change it; read-only otherwise |

Choosing a different global role is always a second request made once the account or agent exists (`PUT .../global-roles`, `PUT .../global-role`, or `DELETE .../global-role` for none), and each dialog makes it in a step of its own after the create request has succeeded. If it fails the account or agent is **not** rolled back: it keeps the default role, the step shows the error and offers a retry, and the person can carry on with the role it has.

## Adding or changing a route

1. Give it a gate. Pick the permission for what the operation *does*; if it also grants something of its own (a role, membership, shell access), list that permission too.
2. Do not check permissions in the handler.
3. If it is open to any authenticated user or public by design, add it to `openRouteGroups` in `router/authorization_test.go` with the reason. Otherwise `TestEveryRouteIsGuarded` fails.
4. Add the row to `docs/api/http-design.md`.

## Safety nets

- `router/authorization_test.go` (`TestEveryRouteIsGuarded`) calls every registered route as an authenticated caller with no permissions (must get 403) and as an anonymous caller (must get 401). A route added without a gate fails it.
- `router/guards_test.go` pins what each gate means.
- `TestRoleAssignmentIsSeparatePrivilege` (same file as the first) pins the boundary around roles: profile routes need only their own permission, and changing a role needs `global_roles.assign` (plus `agents.write` for an agent).
- `internal/platform/authz` tests cover the authorizer, including that no role name grants anything and that a global permission does not cross into a project.
- `test/e2e/admin_role_permissions_test.go` runs against a real database: a user whose role has been stripped of a permission is refused everywhere that permission is needed.
- `test/e2e/default_role_test.go` runs against a real database too: one default at a time, it cannot be deleted, new users and global agents get it, and creating a user keeps working after the original `USER` role has been deleted once another role is the default.

## Behaviors worth knowing

- On a public project, an anonymous visitor can read what a logged-in user with no permissions on it cannot: `RequirePublicProjectOrPermissions` consults the public flag only for callers who are not authenticated.
- `GET /projects` decides between "all projects" and "my projects" from the authenticated subject user even for agent-key requests (the shared bot user, seeded SUPER_ADMIN), unlike the gates above, which use the agent's own role.
