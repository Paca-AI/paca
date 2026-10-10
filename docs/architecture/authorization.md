# Authorization

How the API decides whether a caller may do something, and how to keep it that way as routes are added.

The rule in one sentence: **a route declares the action it needs, and on which resource, in `router.go`; what a caller may do is exactly what the policies of their attached roles say. Nothing else grants access, and no handler decides.**

For the user-facing view (writing policies, worked examples, the web editor) see [Roles and policies](../guides/roles-and-policies.md). For the endpoints see [Roles and policies API](../api/roles-and-policies.md). For a single end-to-end overview aimed at users and operators see [IAM authorization](../guides/iam-authorization.md). The design rationale is in [Design notes](#design-notes) at the end of this page.

## The model

Paca uses AWS-IAM-style JSON policies. The engine lives in `internal/platform/authz/iam` (pure: no database, no HTTP).

**Role.** A named policy document (`roles.policy`). One role type serves what used to be global roles and project roles. A role is either a *platform role* (no owner project) or owned by one project (`roles.project_id`), and only that project can attach it.

**Policy.** A document with a `version` and a list of `statements`:

```json
{
  "version": "2026-10-01",
  "statements": [
    { "sid": "ReadTasks", "effect": "Allow",
      "actions": ["tasks:read", "sprints:read"],
      "resources": ["project/7c1e.../*"] },
    { "effect": "Deny", "actions": ["environments:connect"],
      "resources": ["project/*/environment/*"],
      "conditions": { "StringEquals": { "environment.type": "kubernetes" } } }
  ]
}
```

Each statement has an `effect` (`Allow` or `Deny`), `actions`, `resources` and optional `conditions`. `ParsePolicy` is strict: unknown fields, an effect other than `Allow`/`Deny` and trailing data are errors. A stored policy that fails to parse grants nothing.

**Evaluation (`iam.Evaluate`).**

1. Collect the principal's role attachments. A platform-wide attachment applies everywhere; a project-scoped attachment applies only to resources inside its project (`project/<id>` and below), whatever the role's own resources say. A role that names `project/*` or another project therefore never reaches a project other than the attachment's through a project-scoped attachment.
2. A statement matches when one of its actions matches the requested action, one of its resources matches the requested resource and every condition holds.
3. **Default deny.** With no matching `Allow`, the answer is no. **Explicit deny wins:** any matching `Deny` makes the answer no, whatever else allows.
4. **Fail closed.** An unknown condition operator never grants and, in a `Deny`, always applies. A store error, a malformed stored policy, an unknown principal or a missing attribute loader denies.
5. Role names confer nothing. The `role` claim in a JWT is a display value and an inert hint, never an authorization input. Editing a role takes effect on the next request (compiled policies are cached per role id and invalidated when a role or attachment changes).

### Actions

`domain:verb`, for example `tasks:write` or `environments:connect`. The domain may contain dots (`project.members:read`, `project.settings.task_types:write`). Wildcards are `*` (everything) and `domain:*` (every action of the domain); no other wildcard form exists. The registry (`iam.Registry`) is seeded from `iam.BuiltinActions()` in `actions.go`; plugins add their own (below). A policy that names an action not in the registry is rejected when saved (`422 ROLE_POLICY_INVALID`). Use `GET /roles/actions` for the live list.

Built-in actions:

| Domain | Actions |
|---|---|
| `users` | `read`, `write`, `delete` |
| `roles` | `read`, `write`, `assign` |
| `projects` | `read`, `write`, `create`, `delete` |
| `project.members` | `read`, `write` |
| `project.activities` | `read` |
| `project` | `export` |
| `tasks` | `read`, `write` |
| `project.settings.task_types` / `.task_statuses` / `.custom_fields` | `write` |
| `sprints`, `views`, `docs`, `workflows`, `annotations` | `read`, `write` (`annotations` also `resolve`) |
| `agents` | `read`, `write` |
| `conversations` | `read`, `write` |
| `environments` | `read`, `write`, `connect` |
| `settings` | `write` |
| `settings.sso` | `write` |
| `plugins` | `read`, `write` |

### Resources

Path-style names. A mid-path `*` matches exactly one segment; a trailing `*` matches zero or more remaining segments, so `project/p1/*` also covers `project/p1` itself.

```
*                                   everything
project                             the project collection (list, create)
project/<id>                        a project
project/<id>/<kind>/<id>            a project child; kinds: task, sprint, doc, view,
                                    workflow, annotation, conversation, agent,
                                    environment, role, plugin
user/<id>  role/<id>  agent/<id>  plugin/<id>  settings  sso     platform level, outside any project
```

`iam.ParseResource` splits a name into kind, id and project. Anything with an empty segment, or whose root is not one of `project`, `user`, `role`, `plugin`, `agent`, `settings`, `sso`, is rejected on save.

Project-level actions (`tasks:read`, `docs:write`, ...) are checked on `project/<id>` for a collection-level request (list, create) and on `project/<id>/<kind>/<id>` for a request about one entity. Platform actions are checked on their *platform root* (`iam.PlatformRootFor`): `users` on `user/*`, `roles` on `role/*`, `plugins` on `plugin/*`, `agents` on `agent/*`, `settings` on `settings`, `settings.sso` on `sso`, `projects` on `project`. An action with no platform root is checked on `*`, so only a `*` holder passes (fail closed).

A statement on `*` or a platform root applies at platform level. A *named* action granted on a platform root does not open a project (this is the fix for GHSA-hjcj-373w-vq8m): only `*` on `*`, or a grant on `project/...`, reaches inside projects.

### Conditions

Operators: `StringEquals`, `StringNotEquals`, `StringLike` (`*` any run, `?` one character), `In`, `NotIn`, `Bool`. All conditions of a statement must hold (AND).

Keys are *declared attributes*, validated on save against the attribute schema (`iam.AttributeSchema`, `GET /roles/attribute-schema`):

| Key | Applies to | Multi-valued |
|---|---|---|
| `principal.id`, `principal.type` (`user` or `agent`), `resource.id` | any | no |
| `task.sprint_id`, `task.status_id`, `task.type_id` | task | no |
| `task.assignee_id` | task | yes |
| `view.sprint_id` (`sprint_views.sprint_id`; NULL, so absent, for backlog and timeline views) | view | no |
| `doc.folder_id` | doc | no |
| `doc.ancestor_folder_ids` (the folder plus all its parents) | doc | yes |
| `agent.environment_id` | agent | no |
| `environment.type` (the runtime backend: `docker` or `kubernetes`) | environment | no |
| `conversation.environment_id` | conversation | no |

A key bound to a resource kind must fit at least one of the statement's resources, otherwise the policy is rejected. A positive operator matches if any value matches; a negated one (`StringNotEquals`, `NotIn`) only if no value matches; an absent attribute makes positive operators false and negated ones true. Attributes are loaded lazily, only when a statement references them (`iam.AttributeLoader`, registered per kind in `repository/postgres/iam_attribute_loaders.go`).

Creates and moves are judged on the attributes the request carries: `AuthorizeCreate` evaluates the new resource with the request's attributes (a role limited to Sprint 5 cannot create a task with no sprint), and `AuthorizeChange` requires an update to be allowed both before and after the change (moving a task to another sprint, a document to another folder).

### Role attachments

`role_attachments(role_id, principal_type 'user'|'agent', principal_id, project_id NULL)`.

- `project_id NULL`: a **platform-wide** attachment.
- `project_id` set: the role applies only inside that project.
- A principal can hold **several roles** at once; permissions are the union, then explicit Deny subtracts. This holds for users (platform roles), global agents (platform roles) and project members, human or agent (roles scoped to the project).
- `project_members` rows keep membership identity (assignees, chat sessions) but carry no role. A member with no attached role sees nothing.
- A role named in a request must be attachable in that scope: a platform role anywhere, a project-owned role only inside its project (`422 ROLE_NOT_ATTACHABLE` otherwise).
- **Project-owned roles name only their project.** A role with an owning project may only name `project/<ownId>` or `project/<ownId>/...`; create, update and validate (`projectResourceIssues` in the role service) reject anything else with `422 ROLE_POLICY_INVALID` and an issue at `statements[i].resources[j]`. A platform role may name any resource, including specific projects or `project/*`. Migration 000067 rewrote existing project-owned roles to comply.
- A **project-role template** is a role with no owning project whose policy resources *all* have a wildcard project segment (`project/*`, `project/*/task/*`; `PolicyIsProjectTemplate`). It can only be attached per project (as a member or project agent). Attaching it platform-wide (`PUT /admin/users/{id}/roles`, `PUT /admin/agents/{id}/roles`, user creation) or making it the default role is refused with `ROLE_NOT_ATTACHABLE`, because it would grant the template's powers in every project. The workspace role pickers hide templates. A platform role that names specific projects (`project/<id>/*`) is not a template: it can be attached platform-wide and reaches exactly those projects.
- Exactly one platform role is the **default**; it is attached to every new user and new global agent, and it cannot be deleted. The default is data (`roles.is_default`), changed with `PUT /admin/roles/{roleId}/default`.

Attachments are managed as *replace-sets* (`PUT .../roles` with `role_ids`), see the API doc.

### System roles

`internal/bootstrap/defaultroles` embeds the shipped policies. Platform roles `SUPER_ADMIN` (`*` on `*`), `ADMIN` and `USER` are `is_system`. Startup seeding creates any of them that is missing but **never overwrites** an existing role's policy or description, so edits made by an administrator persist across restarts and upgrades. System roles **can be edited** by anyone allowed to write roles but **cannot be deleted** (`409 ROLE_IS_SYSTEM`). Reconciliation is by name, so the roles migration 000064 converted keep their ids and holders. Project templates (`Admin`, `Editor`, `Viewer`) are instantiated into every new project as project-owned roles (`PROJECT_ID` replaced by the real id); the project `Admin` is `is_system`.

`ADMIN` runs the workspace (`users:*`, `projects:*`, `agents:*`, `plugins:*`, `settings:write`) and can read roles (`roles:read`); it does not hold `roles:write` or `roles:assign`.

### Root-equivalent actions

Neither saving a role (create, edit, make it the default) nor attaching one asks the caller to hold what the role grants. *Saving* needs `roles:write` on the role's scope; *attaching* is a `PassRole`-style operation with its own gate (below). Both are therefore powerful actions, listed here.

Some actions let their holder give themselves everything, so holding one is holding `*`:

- `roles:write`: define or edit any role, and choose the default (new accounts start with it);
- `roles:assign`: attach or detach roles, their own account included. It works like AWS IAM `iam:PassRole`: the *resource* of the statement says which roles the holder may hand out, so on `*` (or `role/*`) it is root-equivalent, while scoped to one role id it is not (see [Assigning roles](#assigning-roles-rolesassign));
- `settings.sso:write`: configure an SSO provider that links accounts by email and sign in as anyone (see [SSO](../guides/sso-oidc.md));
- `users:write`: can reset any user's password (`PATCH /admin/users/{userId}/password`), a `SUPER_ADMIN`'s included, and sign in as them.

Only `SUPER_ADMIN` is seeded with them (`ADMIN` holds `users:*`, so it can take over accounts: grant it accordingly). The engine does **not** bound a role author by their own grants: there are no escalation guards on `POST/PUT /admin/roles`, the project role routes or `PUT /admin/roles/{roleId}/default` (the former `GrantablePolicy` / `GrantableRoleInPath` middleware and `iam.CanGrant` / `GrantsCover` were removed). The only content check is that a project's own role names resources inside that project (`422 ROLE_POLICY_INVALID`). Treat `roles:write` and `roles:assign` as root.

- **Last full access.** A change (updating or deleting a role, replacing attachments) that would leave no platform-wide attachment of an unconditional `*` on `*` Allow is refused with `409 ROLE_LAST_FULL_ACCESS`. A workspace that had no such holder to begin with is not blocked.

### Assigning roles (`roles:assign`)

Assignment follows `iam:PassRole`. A caller holding `roles:assign` may assign **any role they are authorized for**; they do not need to hold the role's permissions. The *resource* of the `roles:assign` statement defines which roles may be assigned, so an administrator can scope it to one role, to the roles of one project, or to all roles.

`middleware.RequireAssignRoles` (`guards.AssignUserRoles`, `AssignGlobalAgentRoles`, `AssignMemberRoles`, `AssignNewProjectPrincipalRoles`) authorizes `roles:assign` on the resource of **each affected role**:

| Scope | Routes | Resource per role |
| --- | --- | --- |
| Platform | `PUT /admin/users/{userId}/roles`, `PUT /admin/agents/{agentId}/roles` | `role/<roleId>` |
| Project | `PUT /projects/{projectId}/members/{memberId}/roles`, `POST /projects/{projectId}/members`, `POST /projects/{projectId}/agents` (with `role_ids`) | `project/<projectId>/role/<roleId>` |

- The project form is used for **every** role id, whoever owns it (a project role, a workspace role or a template): the attachment lives in project P, so the role is judged as seen inside P.
- *Affected roles* are the symmetric difference of the roles the target holds now and the request's `role_ids`: added roles and removed roles need the permission; unchanged ones need nothing. Clearing with `role_ids: []` needs it for each role removed. The current set is read through a small lookup (`RoleAttachmentLookup`, backed by the role service's `ListUserRoles`/`ListAgentRoles`/`ListMemberRoles`).
- Ids that name no role are checked like any other and, once allowed, reach the service, which answers `422 ROLE_NOT_ATTACHABLE`. A body that does not decode passes through for the handler's `400`. `POST /members` and `POST /agents` without `role_ids` assign nothing and need no `roles:assign` (only their own gate).
- The gate sits next to the route's other gates, it does not replace them: global agent roles still need `agents:write` on `agent/<id>`, project agent creation still needs `agents:write` and `project.members:write`, and adding a project member still needs `project.members:write`. Changing an existing project member's roles (`PUT .../members/{memberId}/roles`) is only role assignment: `roles:assign` per role, nothing else. Migration 000068 added `roles:assign` next to `project.members:write` in existing Allow statements that reach role resources, so roles that could change member roles before still can. Fail closed: a lookup or authorizer error is `500`, a refusal `403 FORBIDDEN`.
- Shipped roles: the project `Admin` (`*` on `project/<id>/*`) can assign any role inside its project; `Editor` and `Viewer` hold no `roles:assign`; workspace `ADMIN` does not hold it; `SUPER_ADMIN` holds everything.
- A request that changes nothing (every `role_ids` entry unchanged) needs no `roles:assign`, but still passes through the route's other gates.

### Plugin actions

A plugin declares its actions in `customPermissions[].key` of its manifest. Each key is an IAM action `<namespace>:<verb>` where the namespace is the last dot-segment of the plugin id with `-` replaced by `_` (`com.paca.time-logging` gives `time_logging:manage_all`); it cannot redeclare a built-in action or another plugin's. The registry holds a plugin's actions while it is installed (`Registry.SetPluginActions`); role policies may name them and the role editor lists them. A plugin's own action is checked on `project/<projectId>/plugin/<pluginId>` inside a project, so a role can grant it in one project, and on `plugin/<pluginId>` outside any project; the workspace-wide effective actions (`Authorizer.EffectiveActions` with no project) include them too, evaluated on `plugin/<pluginId>` (`Registry.PluginOwner` finds the owner), so a `*` holder sees them. `customPermissions[].scope` is `project`, `global` or `both` (listed in the project and the global role editor). Route middleware is `requireActions`; `requirePermissions` is rejected. See [backend plugin system](../plugins/backend-plugin-system.md#route-middleware-policy).

**Agents.** A request made with the agent API key that names an agent is judged by that agent's own attachments, never by the shared bot user behind the key (seeded `SUPER_ADMIN`), which would give every agent full privilege. The same holds for plugin `permission_check` and `db_query`/`db_exec` (GHSA-g6mx-8g92-w9v5).

## Where it is enforced: the router

`internal/transport/http/router/router.go` builds each route with a gate from `guards.go`. Every gate checks **all** the actions it is given, on the resource it names, through the IAM authorizer:

```go
require := newGuards(deps)

r.With(require.Global(iam.ActionUsersWrite)).Post("/users", h.CreateUser)
r.With(require.Project(iam.ActionSprintsWrite)).Post("/", h.CreateSprint)
r.With(require.ProjectEntity("task", "taskId", resTask, iam.ActionTasksRead)).Get("/{taskId}", h.GetTask)
r.With(require.Environment(iam.ActionEnvironmentsConnect)).Post("/ssh-keys", h.AddSSHKey)
```

| Gate | Checks the actions on |
|---|---|
| `Global(a...)` | each action's platform root (`user/*`, `role/*`, `settings`, ...) |
| `Project(a...)` | `project/{projectId}` (list and create routes, project-wide settings) |
| `ProjectOrPublic(a...)` | as `Project`, or `projects:read` on `project`; an *anonymous* caller is admitted when the project is public |
| `ProjectEntity(kind, idParam, ...)` and `ProjectEntityOrPublic` | `project/{projectId}/<kind>/{id}`: tasks, docs, sprints, views, workflows, annotations, conversations |
| `Environment(a...)` | `project/{projectId}/environment/{environmentId}` |
| `AgentUse(a...)` | `project/{projectId}/agent/{agentId}` (chat sessions of an agent) |
| `ChatEnvironment()`, `SessionEnvironment()` | `environments:read` on the environment a chat will run in |
| `PlatformEntity(root, idParam, ...)` | `user/{userId}`, `role/{roleId}`, `agent/{agentId}` |
| `ProjectRoleCollection`, `ProjectRole` | `project/{projectId}/role/*` and `.../role/{roleId}` |
| `TaskCreate`, `TaskChange`, `DocCreate`, `DocChange` | the attributes a request body sets (sprint, status, type, assignees, folder), old and new |
| `ViewCreate`, `ViewReorderItems` | a new view's sprint (the `sprint_id` query parameter; none for backlog and timeline), and every view id in a reorder body |
| `TaskPositionItems` | `tasks:write` on each task named in the items of a bulk task-position update |
| `AssignUserRoles`, `AssignGlobalAgentRoles` | `roles:assign` on `role/{roleId}` for each role the request adds or removes |
| `AssignMemberRoles`, `AssignNewProjectPrincipalRoles` | `roles:assign` on `project/{projectId}/role/{roleId}` for each role the request adds or removes |

**Entity-level gates.** Gating a by-id route on the entity (not just the project) is what makes a `Deny` on one agent, an `Allow` limited to one sprint's tasks, or a condition on a task's attributes apply to every route that touches it. `Environment` and `AgentUse` cover *every* route on one environment or agent, so restricting an environment is a single `Deny` on `project/<p>/environment/<id>`.

**A gate requires all the actions it is given.** An operation that crosses two capabilities lists both. Creating a project agent also binds roles, so it requires `agents:write` and `project.members:write`; creating a task from an annotation requires `annotations:write` and `tasks:write`.

**A privileged operation gets its own route.** Do not hide one behind an optional field of a route with a weaker gate. A user's roles are changed only by `PUT /admin/users/{userId}/roles` (`roles:assign` on each role concerned), a global agent's only by `PUT /admin/agents/{agentId}/roles` (the same, plus `agents:write` on the agent); user and agent create/update refuse a role field. Making a role the default is `PUT /admin/roles/{roleId}/default` (`roles:write`).

Other middleware add conditions on top of a gate, never instead of it: `RequireFreshPassword`, `RequireJWTAuth` (no API keys).

### List endpoints: SQL-pushdown scoping

A route gate on `project/<p>` cannot hide individual children. List endpoints therefore ask `Authorizer.ListScope(principal, action, projectID, kind)`, which compiles the principal's grants into a predicate tree (`iam.Node`: `True`, `False`, `And`, `Or`, `Not`, attribute conditions) over resources of that kind. Handlers attach it with `scopedContext` and the repository translates it to SQL **inside the list query**, so filtering happens before pagination, counts and sums, never on a fetched page. `Unrestricted()` skips the clause, `DeniesAll()` returns an empty page without querying, and an error fails the request closed. Kinds scoped this way: `agent`, `environment`, `task`, `doc`, `sprint`, `view`, `workflow`, `conversation`, `annotation` (including "assigned to me" task lists). Each kind has a column map in `repository/postgres/iam_scope.go` naming the SQL behind every attribute the schema declares for it; annotations declare none, so only `resource.id` (an Allow or Deny naming one annotation) is expressible and any other key fails the request closed. Callers outside a request (workers, internal services) carry no scope and are unrestricted.

Two more reads follow the task scope because they expose tasks indirectly:

- **Task positions.** `GET /projects/{projectId}/views/{viewId}/task-positions` (and the `view_id` lookup inside the task list) returns only the manual positions of tasks the caller may read (`tasks:read`), filtered in the query. The bulk write `PUT .../task-positions` names its tasks in the body, so the `TaskPositionItems` gate checks `tasks:write` on every named task and refuses the whole request if any is denied.
- **Lists that span projects.** The workspace open-task count (`GET /projects/workspace-stats`) and "my tasks" cannot use one project's scope. `Authorizer.ListScopes` compiles one scope per project from a single fetch of the caller's grants, and the handler attaches them with `iam.WithProjectScopes`. The repository turns them into one predicate per project, OR-ed (projects that are unrestricted share an `IN` list). A project with no scope, or a deny-all one, contributes nothing, so a caller who may read no task in a project does not see that project's tasks counted.

Annotation by-id routes (`/annotations/{id}` and every route nested under a port forward) are gated on `project/{projectId}/annotation/{annotationId}`; the nested ones keep their `Environment` gate as well, so both a `Deny` on the environment and one on the annotation apply. Both annotation list routes (the project-wide search and the per-port-forward list) are scoped to the readable annotations.

### What is not decided in the router

- **Which results a caller sees** beyond the list scoping above. `GET /projects` and `GET /projects/workspace-stats` are open to every authenticated user and return every project to a holder of `projects:read` on `project`, and only the caller's own projects otherwise (a data-scoping query inside the handler). The workspace open-task count is further limited to the tasks the caller may read in each project (see the list scoping above).
- **Plugin routes.** A plugin declares its middleware policy in its manifest, applied when the request arrives (`PluginHandler.applyPluginRouteMiddlewares`, `requireActions`).
- **"My permissions" endpoints** (below) report effective actions for the caller; they gate nothing.

### Effective actions for the UI

`Authorizer.EffectiveActions(principal, projectID)` returns the registered actions the principal may perform on `project/<id>` (or, with no project, the platform actions on their platform roots). In a project an action is included when it is allowed on the project itself *or possibly allowed on some child* (an `Allow` that ignores conditions, honoring only unconditional `Deny`), so a role limited to one sprint's tasks still shows the Tasks UI. It is used to show or hide UI only, never to authorize. Endpoints: `GET /users/me/global-permissions`, `GET /projects/{projectId}/members/me/permissions` and, for an agent key, `GET /agents/me/global-permissions`; all return `{"actions": [...]}`.

## How the web app mirrors it

The UI mirrors the actions the API requires, read from the effective-actions endpoints (`useCanAssignGlobalRole()` is `roles:assign` plus `roles:read`; `useCanAssignProjectRole()` is `roles:assign`: it alone enables editing an existing member's roles; adding a member or project agent also needs `project.members:write` / `agents:write`). There is no endpoint that lists which roles one may assign: the UI offers all roles, and a role outside the caller's `roles:assign` resources is refused with `FORBIDDEN`, which the role dialogs show as an error. The server stays the authority.

The role dialog has a **Simple** view (a checkbox per known action) and an **Advanced (JSON)** view; both edit one policy. A policy the checkboxes cannot express (several statements, a `Deny`, conditions, specific resources, actions the catalogue does not list) opens in Advanced only. See [Roles and policies](../guides/roles-and-policies.md#simple-and-advanced-editors).

Choosing roles for an account or agent is a separate request made once it exists (`PUT .../roles`). If it fails the account or agent is **not** rolled back: it keeps the default role, the step shows the error and offers a retry.

## Adding or changing a route

1. **Pick the action** for what the operation *does* from `iam.BuiltinActions()` (add a new constant and list it in `BuiltinActions()` if the capability is genuinely new; a plugin adds its own via its manifest). If the route also grants something of its own (a role, membership, shell access), list that action too.
2. **Pick the resource** by choosing the gate: `Project` for project-wide and collection routes, `ProjectEntity`/`Environment`/`AgentUse` for a route addressing one entity, `Global`/`PlatformEntity` for platform routes. Prefer the entity gate on every by-id route so a `Deny` or condition on the entity applies. If the request body sets attributes a condition can reference (sprint, folder), add the matching create/change gate.
3. **Lists:** if the route lists children, apply the caller's list scope (`scopedContext`) and make the repository honor `iam.ScopeFrom`, otherwise a `Deny` on one child will not hide it.
4. Do not check permissions in the handler.
5. If it is open to any authenticated user or public by design, add it to `openRouteGroups` in `router/authorization_test.go` with the reason. Otherwise `TestEveryRouteIsGuarded` and the route coverage test fail.
6. Add the row to `docs/api/http-design.md` (and `docs/api/roles-and-policies.md` for role endpoints).
7. If the operation can hand out or raise authority (a role, a membership, a credential), ask what its action lets the holder reach. One that can mint root is root-equivalent: keep it off every built-in role except `SUPER_ADMIN`, and make sure it is only given to trusted roles; a resource-scoped gate such as `roles:assign` can limit which existing roles it reaches.

## Safety nets

- `router/route_coverage_test.go` walks the route table: every route that is not reviewed as open must declare a gate, and reports the resource it authorizes. `router/authorization_test.go` (`TestEveryRouteIsGuarded`) calls every route as an authenticated caller with no roles (must get 403) and as an anonymous caller (must get 401).
- `router/guards_test.go` and `guards_iam_test.go` pin what each gate means.
- `internal/platform/authz/iam` tests cover wildcard matching, Deny precedence, project-scope intersection, each operator, malformed policies failing closed, list-scope compilation versus `Evaluate`, and named regressions (GHSA-hjcj-373w-vq8m, GHSA-g6mx-8g92-w9v5).
- `internal/bootstrap/defaultroles` tests keep every shipped policy valid and pin what each platform role and project template may do.
- `test/integration/iam_migration_test.go` replays the legacy-to-IAM migration against fixtures with every old role shape and compares allow/deny per principal.
- The e2e suites run against a real database: a user whose roles lack an action is refused everywhere that action is needed.

## Behaviors worth knowing

- On a public project, an anonymous visitor can read what a logged-in user with no roles on it cannot: `ProjectOrPublic` consults the public flag only for callers who are not authenticated.
- `GET /projects` decides between "all projects" and "my projects" from the authenticated subject user even for agent-key requests (the shared bot user, seeded `SUPER_ADMIN`), unlike the gates above, which use the agent's own roles.
- Restricting an agent or environment is no special feature: it is an ordinary `Deny` role (see the guide). Migration 000064 does **not** convert legacy restricted agents/environments: they become open to everyone their roles allow, and an administrator recreates each restriction with a `Deny` role (see the [guide](../guides/roles-and-policies.md#restrict-an-agent) and the [release notes](../releases/2026-10-iam-authorization.md#recreate-your-restrictions)).

## Design notes

Why the model looks the way it does. These are the decisions that are easy to undo by accident.

**Native engine, one-shot migration.** The policy engine is plain Go (`internal/platform/authz/iam`), not an external policy service, and there is no runtime translation layer: routes declare IAM actions directly, the registry is seeded from an explicit action list and the roles API accepts only IAM policy JSON. Knowledge of the old flat keys (`tasks.write`) lives in exactly three places: the SQL migrations (000064 converts stored roles, 000065 converts plugin manifests, 000066 relaxes legacy columns) and the web app, which owns the friendly permission catalogue and the conversion between checkboxes and policy JSON. Two test files keep a frozen copy of the old resolver purely to verify migration 000064. Migrations are up-only; rollback means restoring a backup.

**Default deny, explicit deny wins.** The only way to get access is a matching `Allow`; any matching `Deny` beats any number of `Allow`s. That makes "everything except" expressible without `NotAction`/`NotResource`, which were deliberately left out (as were policy variables, time/IP condition keys and permission boundaries) to keep the language small enough to reason about. The engine fails closed: unknown action, malformed stored policy, store or attribute-loader error all deny. Role names and the JWT `ADMIN` claim confer nothing, because the old model's name-based shortcuts were a recurring source of bypasses.

**Actions are `domain:verb`.** A fixed two-part shape lets a policy say "everything about tasks" (`tasks:*`) without a general glob language, lets the registry validate every name on save, and lets plugins namespace their own actions (`time_logging:manage_all`). The mapping from old keys is mechanical (replace the last `.` with `:`). Only `*` and `domain:*` are wildcards; an action absent from the registry is rejected at save and denied at evaluation, so a typo can never silently grant or silently do nothing.

**Resources are paths.** A path (`project/<id>/task/<id>`) makes scope part of the grant: a statement on `*` applies everywhere, one on `project/<id>/...` only inside that project. This replaces the old rule "a global role counts inside a project only if it holds `*`" (GHSA-hjcj-373w-vq8m) with something visible in the policy itself. A mid-path `*` is exactly one segment, a trailing `*` is zero or more, so `project/p1/*` also covers `project/p1`.

**Conditions use declared attributes.** Condition keys are not free-form: each is registered in the attribute schema with a value type, a loader (for single-resource checks) and a SQL mapping (for list endpoints). The schema drives validation, the JSON editor's autocomplete and the "limit to" UI, and adding an attribute needs no evaluator change. Attributes may be multi-valued (`doc.ancestor_folder_ids` makes "in folder ABC" include subfolders); positive operators match if any value matches, negated ones only if none does, and a missing attribute makes negated operators true. That last rule is why a condition on a key the resource kind does not have can never open access on an `Allow` of a negated operator by accident: the schema rejects keys that fit none of the statement's resources. Attributes are loaded lazily, only when a statement references them. Creates are judged on the attributes in the request; updates that change an attribute must be allowed on both the old and the new values, so nobody can move a task out of (or into) a scope they do not control.

**List scoping is pushed into SQL.** Gating a list route on `project/<p>` cannot hide individual children, and filtering a fetched page breaks pagination and counts. So services ask for a predicate (`ListScope`: unrestricted, deny-all, or a tree of attribute conditions) and the repository applies it inside the query. The same attribute schema supplies the column mapping, so what a policy can say and what a list can filter cannot drift apart. Anything that cannot be expressed as SQL fails the request closed.

**Project-scoped attachments intersect.** A role attached to a project applies only to `project/<id>` and below, whatever its statements name. A reusable "Developer" role written on `project/*` can therefore be attached to many projects without ever reaching another project. The corollary rules keep this honest: a project-owned role may only name its own project (so a project admin cannot write a policy about other projects), and a role whose resources are all project wildcards is a template that cannot be attached platform-wide (it would grant its powers in every project).

**One role type, many roles per principal.** Global roles and project roles collapsed into one `roles` table plus `role_attachments`. Permissions are the union of attached roles, minus explicit denies. `project_members` still carries membership identity (assignees, chat sessions) but no role; a member with no role sees nothing.

**Restricted access became ordinary roles.** The old `access_mode` flag and grant tables are not modelled anywhere in the new API: there are no restriction endpoints or special code paths. Restricting an agent or environment is a `Deny` role, optionally with a `NotIn principal.id` condition for "everyone except these". During design a migration that generated "Access to X" and "Restrict X" roles was planned; it was dropped during execution, so **restricted resources are not migrated** and an administrator recreates them. The release notes call this out.

**Guard rails against escalation.** Because roles are data, the dangerous things are editing a role and attaching one. A caller can only save a role, or make it the default, granting what they hold themselves (an unconditional `Allow` with no overlapping `Deny`). Attaching is PassRole-style: `roles:assign`, scoped by resource to the roles the holder may hand out. Some actions are root-equivalent (`roles:write`, `roles:assign`, `settings.sso:write`, `users:write`), so only `SUPER_ADMIN` is seeded with them. The last unconditional full-access attachment cannot be removed. Privileged operations (changing a user's roles, making a role the default) get their own routes rather than an optional field on a weaker route.

**Migration safety.** Migration 000064 runs in one transaction and, before committing, recomputes every user's and agent's access with the legacy rules and with the new policies, at platform scope and in each project they belong to; any difference aborts and rolls everything back. Legacy tables and columns are left in place (not read, not written) so the data can be inspected. Soft-deleted members get no attachment. A project role keeps `projects:read` because membership used to imply it; global-role actions are placed on platform roots only, never `project/*`, matching how they always behaved inside projects.

**Decisions made while building.**

- The backend is IAM-native with no legacy source code at runtime; the front end keeps the checkbox editor and adds an Advanced JSON option, so the two views edit one policy and a policy the checkboxes cannot express opens in Advanced only. Nothing is dropped silently.
- No restricted-access endpoints or code; restriction is expressed only by ordinary roles.
- Built-in roles are editable but not deletable, and seeding never overwrites an administrator's edits.
- A project-owned role may name only its own project (migration 000067 rewrote existing ones).
- `ADMIN` may read roles but not define or assign them; root-equivalent actions stay off every built-in role except `SUPER_ADMIN`.
- Plugin manifests use `requireActions`; a plugin's actions are registered while it is installed and are checked on `project/<id>/plugin/<pluginId>` inside a project.

Not built: `NotAction`, `NotResource`, policy variables, time/IP condition keys, permission boundaries, an external policy engine and a reversible migration.
