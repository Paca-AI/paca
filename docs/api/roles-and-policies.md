# Roles and policies API

Endpoints for IAM-style roles: role definitions, role assignment to users, agents and project members, role-editor helpers (validate, simulate, action and attribute catalogues) and the "my permissions" endpoints. Start with the [IAM authorization overview](../guides/iam-authorization.md); concepts are in [Roles and policies](../guides/roles-and-policies.md) and [Authorization architecture](../architecture/authorization.md).

All paths are under `/api/v1`. Every route needs an access token with a fresh password (`Authorization: Bearer ...`). Responses use the usual envelope (`{"success": true, "data": ..., "request_id": "..."}`; errors `{"success": false, "error_code": "...", "error": "..."}`). Permissions below are IAM actions; see [authorization](../architecture/authorization.md) for the resource each is checked on.

## Role object

```json
{
  "id": "uuid",
  "name": "Sprint contractor",
  "description": "",
  "policy": {
    "version": "2026-10-01",
    "statements": [
      {
        "sid": "ReadTasks",
        "effect": "Allow",
        "actions": ["tasks:read"],
        "resources": ["project/<projectId>/task/*"],
        "conditions": { "StringEquals": { "task.sprint_id": "<sprintId>" } }
      }
    ]
  },
  "project_id": null,
  "is_system": false,
  "is_default": false,
  "attachment_count": 3,
  "created_at": "2026-10-06T10:00:00Z",
  "updated_at": "2026-10-06T10:00:00Z"
}
```

- `policy` is the policy document itself (never a permission map). `statements[].effect` is `Allow` or `Deny`; `actions` are `domain:verb` strings (`*` and `domain:*` allowed); `resources` are path patterns; `conditions` is `{ operator: { key: value | [values] } }` with operators `StringEquals`, `StringNotEquals`, `StringLike`, `In`, `NotIn`, `Bool`. Unknown fields are rejected.
- `project_id` is the owning project, or `null` for a platform role.
- `is_system` roles (`SUPER_ADMIN`, `ADMIN`, `USER`, each project's `Admin`) can be edited but not deleted (`409 ROLE_IS_SYSTEM` on delete). Startup seeding creates missing system roles but never overwrites an existing role's policy or description.
- A role with a `project_id` (created on `/projects/:projectId/roles`) may only name resources inside its own project: `project/<projectId>` or `project/<projectId>/...`. `POST /projects/:projectId/roles`, `PUT /projects/:projectId/roles/:roleId` and `POST /projects/:projectId/roles/validate` reject anything else (`*`, `project/*`, another project, `user/*`) with `422 ROLE_POLICY_INVALID` and an issue at `statements[i].resources[j]`. A platform role (`/admin/roles`) may name any resource, including specific projects (`project/<id>/*`) or all of them (`project/*`).
- A **project-role template** (no `project_id`, and *every* policy resource has a wildcard project segment, such as `project/*` or `project/*/task/*`) can only be attached per project. Attaching it platform-wide or making it the default is refused with `ROLE_NOT_ATTACHABLE`. A platform role that names specific projects is not a template and can be attached platform-wide; it reaches exactly those projects.
- `is_default` marks the one platform role every new user and new global agent starts with.
- `attachment_count` is the number of attachments of the role (all scopes in platform listings; inside the project in project listings).

Request body for create and update (`name` is trimmed, 1 to 100 characters; `description` optional):

```json
{ "name": "Sprint contractor", "description": "", "policy": { "version": "2026-10-01", "statements": [] } }
```

## Platform roles

| Method | Path | Auth | Function |
|---|---|---|---|
| `GET` | `/admin/roles` | `roles:read` | List platform roles (`project_id` null). |
| `POST` | `/admin/roles` | `roles:write` | Create a platform role. `201` with the role. |
| `GET` | `/admin/roles/:roleId` | `roles:read` on the role | Get a role. |
| `PUT` | `/admin/roles/:roleId` | `roles:write` on the role | Replace name, description and policy. |
| `DELETE` | `/admin/roles/:roleId` | `roles:write` on the role | Delete a role; its attachments are removed with it. `204`. |
| `PUT` | `/admin/roles/:roleId/default` | `roles:write` on the role | Make the role the default for new accounts. Returns the role. |

`roles:write` is root-equivalent. Saving a role (create, update, make it the default) needs **only** `roles:write` on the role's scope: what the policy grants is not compared with what the caller holds, so there is no escalation guard on these routes. Updating or deleting a role cannot remove the last platform-wide full-access (`*` on `*`) assignment.

## Project roles

Roles owned by a project, attachable only inside it.

| Method | Path | Auth | Function |
|---|---|---|---|
| `GET` | `/projects/:projectId/roles` | `roles:read` on `project/:projectId/role/*` | List the project's own roles **and** the platform roles already attached to someone inside that project (not every platform role). |
| `POST` | `/projects/:projectId/roles` | `roles:write` on `project/:projectId/role/*` | Create a role owned by the project. `201`. |
| `GET` | `/projects/:projectId/roles/:roleId` | `roles:read` on the role | Get a project role. |
| `PUT` | `/projects/:projectId/roles/:roleId` | `roles:write` on the role | Replace a project role. Every resource of the policy must lie inside the project, else `422 ROLE_POLICY_INVALID`. |
| `DELETE` | `/projects/:projectId/roles/:roleId` | `roles:write` on the role | Delete a project role. `204`. |

Request and response bodies are the same as for platform roles. A role id that belongs to another project answers `404 ROLE_NOT_FOUND`.

## Role-editor helpers

Pure functions over the action registry and attribute schema; they read no workspace data.

| Method | Path | Auth | Function |
|---|---|---|---|
| `GET` | `/roles/actions` | any authenticated caller | The catalogue of known actions (built-in and from installed plugins), sorted. |
| `GET` | `/roles/attribute-schema` | any authenticated caller | The condition attributes a policy may use. |
| `POST` | `/roles/validate` | any authenticated caller | Validate a policy. |
| `POST` | `/roles/simulate` | any authenticated caller; naming a `principal` also needs `roles:read` | Evaluate a request against a policy. |

The same four exist per project at `/projects/:projectId/roles/actions`, `/attribute-schema`, `/validate` and `/simulate`, needing `roles:read` on the project (`simulate` with a `principal` additionally needs `roles:read` on `role/*`).

`GET /roles/actions` returns an array of strings:

```json
{ "success": true, "data": ["agents:read", "agents:write", "conversations:read", "..."] }
```

`GET /roles/attribute-schema` returns:

```json
{
  "success": true,
  "data": [
    { "key": "task.sprint_id", "resource_kind": "task", "type": "string", "multi_valued": false, "label_key": "roles.attributes.task.sprint_id" },
    { "key": "doc.ancestor_folder_ids", "resource_kind": "doc", "type": "string", "multi_valued": true, "label_key": "roles.attributes.doc.ancestor_folder_ids" }
  ]
}
```

`resource_kind` is empty for the generic keys `principal.id`, `principal.type` and `resource.id`. Built-in keys: `task.sprint_id`, `task.status_id`, `task.type_id`, `task.assignee_id`, `view.sprint_id`, `doc.folder_id`, `doc.ancestor_folder_ids`, `agent.environment_id`, `environment.type` (`docker` or `kubernetes`), `conversation.environment_id`.

`POST /roles/validate` always answers `200`; problems with the policy are the result, not a request failure:

```json
// request
{ "policy": { "version": "2026-10-01", "statements": [ { "effect": "Allow", "actions": ["tasks:reed"], "resources": ["project/*"] } ] } }
// response data
{ "valid": false, "issues": [ { "path": "statements[0].actions[0]", "message": "unknown action \"tasks:reed\"" } ] }
```

`path` addresses the offending element (`statements[i].effect`, `.actions[j]`, `.resources[j]`, `.conditions.<operator>.<key>`); a document that does not parse as a policy (unknown field, wrong shape) reports one issue at `policy`.

`POST /roles/simulate` asks whether `action` on `resource` would be allowed:

```json
// request
{
  "policy": { "version": "2026-10-01", "statements": [ ... ] },
  "principal": { "type": "user", "id": "<userId>" },
  "action": "tasks:read",
  "resource": "project/<projectId>/task/<taskId>",
  "attributes": { "task.sprint_id": "<sprintId>" }
}
// response data
{ "allowed": true, "matched": [ { "role_id": "policy", "sid": "ReadTasks", "effect": "Allow", "index": 0 } ] }
```

- `policy`, `action` and `resource` are required (`422 ROLE_POLICY_INVALID` for a bad policy, `400` if action or resource is empty or the principal is malformed).
- `principal` is optional: without it only `policy` is evaluated; with it the principal's own attached roles are evaluated together with `policy` as if it were one more role.
- `attributes` supplies condition values (a string, a bool or a list of strings per key). No resource is loaded, so an attribute you do not supply counts as absent.
- In `matched`, `role_id` is `"policy"` for statements of the policy under test, otherwise the id of the principal's role; `index` is the statement's position.

## Assigning roles

Attachments are managed as **replace-sets**: `PUT` sends the complete list of roles the principal should hold in that scope; roles not listed are detached. The response is the resulting list of role objects.

```json
{ "role_ids": ["<roleId>", "<roleId>"] }
```

| Method | Path | Auth | Function |
|---|---|---|---|
| `GET` | `/admin/users/:userId/roles` | `roles:read` on `user/:userId` | The user's platform-wide roles. |
| `PUT` | `/admin/users/:userId/roles` | `roles:assign` on `role/:roleId` for each role added or removed | Replace the user's platform-wide roles. |
| `GET` | `/admin/agents/:agentId/roles` | `roles:read` + `agents:read` | A global agent's platform-wide roles. |
| `PUT` | `/admin/agents/:agentId/roles` | `agents:write` on the agent + `roles:assign` on `role/:roleId` for each role added or removed | Replace a global agent's roles. |
| `GET` | `/projects/:projectId/members/:memberId/roles` | `project.members:read` | The roles a member (human or agent) holds in the project. |
| `PUT` | `/projects/:projectId/members/:memberId/roles` | `roles:assign` on `project/:projectId/role/:roleId` for each role added or removed (`project.members:write` is not needed) | Replace the member's roles in the project. |

Rules:

- The ids must name existing roles that can be attached in that scope: a platform role anywhere (except a project-role template, whose resources all have a wildcard project segment and which is attachable only on the project routes), a project-owned role only inside its project. Otherwise `422 ROLE_NOT_ATTACHABLE`. This also applies to `PUT /admin/roles/:roleId/default` and `POST /admin/users`. Duplicates are ignored.
- Assignment works like AWS IAM `iam:PassRole`. `roles:assign` is authorized on the resource of **each role the request adds or removes** (the symmetric difference of the roles held now and `role_ids`; unchanged roles need nothing, and an empty `role_ids` needs it for each role removed): `role/:roleId` on the platform routes, `project/:projectId/role/:roleId` on project routes, for every role whoever owns it. `403 FORBIDDEN` when one is not allowed. The caller does **not** need to hold the permissions of the roles, and `roles:assign` on `*` is root-equivalent: scope it with resources (for example `project/:projectId/role/:editorId`). Unknown ids are checked the same way and then answered with `422`. There is no endpoint that lists the roles a caller may assign.
- A replace that would leave no platform-wide full-access attachment answers `409 ROLE_LAST_FULL_ACCESS`.
- An empty `role_ids` is accepted on these routes and detaches everything in that scope; a member with no roles sees nothing in the project.
- Other routes set roles at creation: `POST /projects/:projectId/members` and `POST /projects/:projectId/agents` require `role_ids` (at least one, else `400 ROLE_REQUIRED`) and need `roles:assign` on `project/:projectId/role/:roleId` for each role in `role_ids` (next to their own gate); `POST /admin/users` and `POST /admin/agents` take no role and attach the default role (`409 ROLE_NO_DEFAULT` for a user when none is set). `PATCH /admin/users/:userId` and the agent create/update bodies do not accept roles.
- Responses that describe a principal carry the attached roles as `roles: [{ "id": "...", "name": "..." }]` (users: platform-wide roles; project members: roles in that project; global agents: platform-wide roles).

## My permissions

These report the caller's effective IAM actions so the UI can show or hide features. They gate nothing; the server decides on every request.

| Method | Path | Auth | Returns |
|---|---|---|---|
| `GET` | `/users/me/global-permissions` | any authenticated user | The caller's platform-level actions. |
| `GET` | `/projects/:projectId/members/me/permissions` | any authenticated caller | The caller's effective actions in the project. |
| `GET` | `/agents/me/global-permissions` | agent API key (`X-Agent-ID`) | The calling agent's own platform-level actions. |

```json
{ "success": true, "data": { "actions": ["projects:read", "tasks:read", "tasks:write"] }, "request_id": "..." }
```

Inside a project an action is listed when it is allowed on the project, or *possibly* allowed on something inside it (a role limited to one sprint's tasks still lists `tasks:read`). Treat the list as a UI hint, not a guarantee for a specific resource.

## Error codes

| Code | HTTP | Meaning |
|---|---|---|
| `ROLE_NOT_FOUND` | 404 | No such role in the addressed scope. |
| `ROLE_NAME_TAKEN` | 409 | Another role in the same scope already has the name. |
| `ROLE_NAME_INVALID` | 400 | Name empty or longer than 100 characters. |
| `ROLE_POLICY_INVALID` | 422 | The policy is not valid. The response carries `issues: [{ "path", "message" }]`. On a project role this includes a resource outside the project (issue at `statements[i].resources[j]`). |
| `ROLE_IS_SYSTEM` | 409 | System roles cannot be deleted (they can be edited). |
| `ROLE_IS_DEFAULT` | 409 | The default role cannot be deleted; make another role the default first. |
| `ROLE_LAST_FULL_ACCESS` | 409 | The change would leave no platform-wide `*` on `*` assignment. |
| `ROLE_NOT_ATTACHABLE` | 422 | A role id is unknown or cannot be attached in this scope, including a project-role template (all resources have a wildcard project segment) attached platform-wide or made the default. |
| `ROLE_NO_DEFAULT` | 409 | No role is the default, so a new account has none to start with. |
| `ROLE_REQUIRED` | 400 | Adding a project member or agent needs at least one `role_ids` entry. |
| `FORBIDDEN` | 403 | Missing action, or `roles:assign` is not allowed on the resource of a role the request adds or removes. |
| `USER_NOT_FOUND`, `AGENT_NOT_FOUND`, `PROJECT_NOT_FOUND`, `PROJECT_MEMBER_NOT_FOUND` | 404 | The addressed principal or project does not exist. |
