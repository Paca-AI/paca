# HTTP API Design

This document defines the REST API design for `services/api`: the path layout, the function of each endpoint, and the conventions new endpoints should follow.

## Scope

This design combines two sources of truth:

- the currently implemented HTTP slice in `services/api`;
- the product/domain model described in [../architecture/database-schema.md](../architecture/database-schema.md).

Where the current implementation and the schema diverge, this document calls that out explicitly rather than hiding the mismatch.

## Design Goals

- Use resource-oriented paths under a versioned API prefix.
- Keep state-changing business logic in `services/api`.
- Separate public transport contracts from internal database structure.
- Make auth, project, and task workflows discoverable from predictable paths.
- Keep room for future realtime and AI integrations without overloading the core API.

## Base Conventions

### Base URL

- Health check: `/api/healthz`
- Versioned product API: `/api/v1`

### Resource naming

- Use plural nouns for top-level collections: `/users`, `/projects`, `/tasks`.
- Use nested resources only when the child has clear ownership: `/projects/:projectId/members`.
- Use path parameters for identifiers and query parameters for filtering, sorting, and pagination.

### Authentication

- Protected endpoints require `Authorization: Bearer <access-token>`.
- Access and refresh token lifecycle is handled under `/api/v1/auth`.
- Authorization is permission-based and enforced in middleware for protected operations.
- Permissions come only from what the caller's assigned roles store (a global role, and a role in each project they belong to) — never from a role's name. See [authorization](../architecture/authorization.md).

### Identifier strategy

- Current implementation uses UUIDs for user identifiers.
- New HTTP APIs should also use UUIDs for public identifiers unless there is a strong reason to expose another format.

### Response shape

Target response envelope:

```json
{
  "success": true,
  "data": {},
  "request_id": "9a1d7c2b-..."
}
```

Target error envelope:

```json
{
  "success": false,
  "error": "descriptive message",
  "request_id": "9a1d7c2b-..."
}
```

Current-state note:

- `/api/v1` success responses already use the envelope above.
- The `/api/healthz` endpoint intentionally returns a minimal `{ "status": "ok" }` body without this envelope.
- New `/api/v1` endpoints should use the standard envelope consistently, and existing endpoints should be normalized over time.

### Timestamps

- Return timestamps in RFC 3339 / ISO 8601 format.
- Prefer `created_at`, `updated_at`, and `deleted_at` field names.

### Pagination and filtering

For list endpoints, use query parameters in this shape:

- `page`: 1-based page number.
- `page_size`: number of items per page.
- `sort`: field name, optionally prefixed with `-` for descending order.
- resource-specific filters such as `status`, `assignee_id`, `sprint_id`, `project_id`.

Recommended paginated response shape:

```json
{
  "success": true,
  "data": {
    "items": [],
    "page": 1,
    "page_size": 20,
    "total": 0
  },
  "request_id": "9a1d7c2b-..."
}
```

## Current Implemented Endpoints

These routes already exist in the Go API service.

In the **Auth** column, permissions joined by `+` are all required, and the permission a route lists is the only thing that grants access: it is read from the caller's role as stored, never inferred from the role's name. See [authorization](../architecture/authorization.md).

| Method | Path | Auth | Function |
|---|---|---|---|
| `GET` | `/api/healthz` | No | Liveness/health probe for infrastructure, containers, and local development. |
| `POST` | `/api/v1/auth/login` | No | Validate user credentials and set access/refresh tokens as HttpOnly cookies. |
| `POST` | `/api/v1/auth/refresh` | No | Exchange refresh token cookie for rotated access/refresh token cookies. |
| `POST` | `/api/v1/auth/logout` | Access token | Revoke the current authenticated token/session and clear cookies. |
| `PATCH` | `/api/v1/users/me/password` | Access token | Change the authenticated user's own password. Allowed even when `must_change_password` is true. Revokes the current session after a successful change. |
| `GET` | `/api/v1/users/me` | Access token (fresh) | Return the authenticated caller's own profile. |
| `PATCH` | `/api/v1/users/me` | Access token (fresh) | Update mutable profile fields (`full_name`) for the caller. |
| `GET` | `/api/v1/users/me/global-permissions` | Access token (fresh) | Return the authenticated caller's effective global permissions. |
| `GET` | `/api/v1/admin/users` | Access token (fresh) + `users.read` | List all users with pagination. |
| `POST` | `/api/v1/admin/users` | Access token (fresh) + `users.write` | Create a new user account with the default global role (the one marked `is_default`, `USER` unless changed). Sets `must_change_password = true`. A `role` in the body is rejected with `400`. Fails with `409 GLOBAL_ROLE_NO_DEFAULT` if no role is the default. |
| `GET` | `/api/v1/admin/users/:userId` | Access token (fresh) + `users.read` | Get a user profile by ID. |
| `PATCH` | `/api/v1/admin/users/:userId` | Access token (fresh) + `users.write` | Update a user's `full_name` or `email`. A `role` in the body is rejected with `400`; change roles with `PUT .../global-roles`. |
| `PATCH` | `/api/v1/admin/users/:userId/password` | Access token (fresh) + `users.write` | Admin password reset. Sets `must_change_password = true`. |
| `DELETE` | `/api/v1/admin/users/:userId` | Access token (fresh) + `users.delete` | Soft-delete a user account. |
| `GET` | `/api/v1/admin/global-roles` | Access token (fresh) + `global_roles.read` | List available global roles and permissions. Exactly one carries `is_default = true`. |
| `POST` | `/api/v1/admin/global-roles` | Access token (fresh) + `global_roles.write` | Create a new global role definition. Root-equivalent: see [authorization](../architecture/authorization.md). |
| `PATCH` | `/api/v1/admin/global-roles/:roleId` | Access token (fresh) + `global_roles.write` | Update a global role definition. Root-equivalent: see [authorization](../architecture/authorization.md). |
| `DELETE` | `/api/v1/admin/global-roles/:roleId` | Access token (fresh) + `global_roles.write` | Remove a global role definition. Fails with `409 GLOBAL_ROLE_IS_DEFAULT` for the default role, and with `409 GLOBAL_ROLE_HAS_ASSIGNED_USERS` if users or global agents are assigned to it. |
| `PUT` | `/api/v1/admin/global-roles/:roleId/set-default` | Access token (fresh) + `global_roles.write` | Make a role the default that new users and new global agents start with, clearing the flag on the previous default. Returns the role. Root-equivalent: see [authorization](../architecture/authorization.md). |
| `PUT` | `/api/v1/admin/users/:userId/global-roles` | Access token (fresh) + `global_roles.assign` | Assign or replace the single global role for a user. The only route that changes a user's role. Root-equivalent: see [authorization](../architecture/authorization.md). |
| `PUT` | `/api/v1/admin/agents/:agentId/global-role` | Access token (fresh) + `agents.write` + `global_roles.assign` | Bind a global agent to the global role that decides what it may do. The only route that sets one. Root-equivalent: see [authorization](../architecture/authorization.md). |
| `DELETE` | `/api/v1/admin/agents/:agentId/global-role` | Access token (fresh) + `agents.write` + `global_roles.assign` | Unbind a global agent from its global role. |
| `GET` | `/api/v1/projects` | Access token (fresh) | List projects visible to the caller. |
| `POST` | `/api/v1/projects` | Access token (fresh) + `projects.create` | Create a new project. |
| `GET` | `/api/v1/projects/:projectId` | Access token (fresh) + `projects.read` | Get project details. |
| `PATCH` | `/api/v1/projects/:projectId` | Access token (fresh) + `projects.write` | Update project name or description. |
| `DELETE` | `/api/v1/projects/:projectId` | Access token (fresh) + `projects.delete` | Delete a project. |
| `GET` | `/api/v1/projects/:projectId/members` | Access token (fresh) + `project.members.read` (or global `projects.read`); anonymous on a public project | List project members. |
| `POST` | `/api/v1/projects/:projectId/members` | Access token (fresh) + `project.members.write` | Add a user to a project. |
| `PATCH` | `/api/v1/projects/:projectId/members/:userId` | Access token (fresh) + `project.members.write` | Change a member's project role. |
| `DELETE` | `/api/v1/projects/:projectId/members/:userId` | Access token (fresh) + `project.members.write` | Remove a member from a project. |
| `GET` | `/api/v1/projects/:projectId/roles` | Access token (fresh) + `project.roles.read` (or global `projects.read`); anonymous on a public project | List custom project roles. |
| `POST` | `/api/v1/projects/:projectId/roles` | Access token (fresh) + `project.roles.write` | Create a project-scoped role. |
| `PATCH` | `/api/v1/projects/:projectId/roles/:roleId` | Access token (fresh) + `project.roles.write` | Update a project role. |
| `DELETE` | `/api/v1/projects/:projectId/roles/:roleId` | Access token (fresh) + `project.roles.write` | Delete a project role. |
| `GET` | `/api/v1/projects/:projectId/task-types` | Access token (fresh) + `tasks.read` (or global `projects.read`); anonymous on a public project | List task type definitions. System types (`is_system = true`) are included in the response but are marked as non-editable. |
| `POST` | `/api/v1/projects/:projectId/task-types` | Access token (fresh) + `project.settings.task_types.write` | Create a task type (e.g. story, bug, chore). Cannot be used to create system types (Epic, Subtask) — returns `400 TASK_TYPE_SYSTEM_TYPE_NOT_ALLOWED`. |
| `PATCH` | `/api/v1/projects/:projectId/task-types/:typeId` | Access token (fresh) + `project.settings.task_types.write` | Update a task type. Returns `409 TASK_TYPE_IS_SYSTEM` if the target type is a system type. |
| `DELETE` | `/api/v1/projects/:projectId/task-types/:typeId` | Access token (fresh) + `project.settings.task_types.write` | Delete a task type. Returns `409 TASK_TYPE_IS_SYSTEM` if the target type is a system type, and `409 TASK_TYPE_IS_DEFAULT` if it is the project's default type. |
| `PUT` | `/api/v1/projects/:projectId/task-types/:typeId/set-default` | Access token (fresh) + `project.settings.task_types.write` | Make a task type the default that new tasks get, clearing the flag on the previous default. |
| `GET` | `/api/v1/projects/:projectId/task-statuses` | Access token (fresh) + `tasks.read` (or global `projects.read`); anonymous on a public project | List workflow statuses in board order. |
| `POST` | `/api/v1/projects/:projectId/task-statuses` | Access token (fresh) + `project.settings.task_statuses.write` | Create a workflow status. |
| `PATCH` | `/api/v1/projects/:projectId/task-statuses/:statusId` | Access token (fresh) + `project.settings.task_statuses.write` | Update a workflow status. |
| `DELETE` | `/api/v1/projects/:projectId/task-statuses/:statusId` | Access token (fresh) + `project.settings.task_statuses.write` | Delete a workflow status. Returns `409 TASK_STATUS_IS_DEFAULT` if it is the project's default status. |
| `PUT` | `/api/v1/projects/:projectId/task-statuses/:statusId/set-default` | Access token (fresh) + `project.settings.task_statuses.write` | Make a status the default that new tasks start in, clearing the flag on the previous default. |
| `GET` | `/api/v1/projects/:projectId/sprints` | Access token (fresh) + `sprints.read` (or global `projects.read`); anonymous on a public project | List sprints for a project ordered by creation date. |
| `POST` | `/api/v1/projects/:projectId/sprints` | Access token (fresh) + `sprints.write` | Quick-create a sprint with a system-generated default name ("Sprint N"). No request body required. The sprint is created with `status = planned`. |
| `GET` | `/api/v1/projects/:projectId/sprints/:sprintId` | Access token (fresh) + `sprints.read` (or global `projects.read`); anonymous on a public project | Get sprint details (goal, dates, status). |
| `PATCH` | `/api/v1/projects/:projectId/sprints/:sprintId` | Access token (fresh) + `sprints.write` | Update sprint metadata (name, goal, start_date, end_date). Cannot be used to change `status`; use the dedicated lifecycle actions instead. |
| `DELETE` | `/api/v1/projects/:projectId/sprints/:sprintId` | Access token (fresh) + `sprints.write` | Delete a sprint. Fails with `409 SPRINT_IS_ACTIVE` if the sprint is currently active. |
| `POST` | `/api/v1/projects/:projectId/sprints/:sprintId/start` | Access token (fresh) + `sprints.write` | Start a planned sprint: set name, goal, start date, and due date, then transition `status` to `active`. Multiple sprints may be active simultaneously. Fails with `409 SPRINT_NOT_PLANNED` if the sprint is not in `planned` state. |
| `POST` | `/api/v1/projects/:projectId/sprints/:sprintId/complete` | Access token (fresh) + `sprints.write` | Complete an active sprint: transition `status` to `completed` and move all incomplete tasks to the specified sprint (or back to no-sprint if `move_to_sprint_id` is `null`). Fails with `409 SPRINT_NOT_ACTIVE` if the sprint is not in `active` state. |
| `GET` | `/api/v1/projects/:projectId/views?context=sprint&sprint_id=:sprintId` | Access token (fresh) + `sprints.read` | List saved view configurations. `context` must be `sprint`, `backlog`, or `timeline`; the `sprint` context requires `sprint_id`. |
| `POST` | `/api/v1/projects/:projectId/views?context=sprint&sprint_id=:sprintId` | Access token (fresh) + `sprints.write` | Create a saved view configuration. `context` must be `sprint`, `backlog`, or `timeline`; the `sprint` context requires `sprint_id`. Sprint creation seeds one Board and one Table view with `column_by = status`, the current sprint selected, and non-system task types. Project creation seeds one backlog Table view with `column_by = sprint` plus one timeline Roadmap view filtered to Epics. |
| `GET` | `/api/v1/projects/:projectId/views/:viewId` | Access token (fresh) + `views.read` (or global `projects.read`); anonymous on a public project | Get a single view configuration. |
| `PATCH` | `/api/v1/projects/:projectId/views/:viewId` | Access token (fresh) + `views.write` | Update a view's name or config. |
| `DELETE` | `/api/v1/projects/:projectId/views/:viewId` | Access token (fresh) + `views.write` | Delete a view. Fails with `409 VIEW_IS_LAST_VIEW` if it is the only remaining view. |
| `PUT` | `/api/v1/projects/:projectId/views/positions?context=sprint&sprint_id=:sprintId` | Access token (fresh) + `sprints.write` | Reorder all views for the given context. `context` must be `sprint` (with `sprint_id`), `backlog`, or `timeline`. Body: `{ "view_ids": ["<uuid>", ...] }` — must include every view ID in the desired tab order. Returns `400 VIEW_REORDER_INVALID` if the list is missing or contains unknown IDs. |
| `GET` | `/api/v1/projects/:projectId/views/:viewId/task-positions` | Access token (fresh) + `tasks.read` (or global `projects.read`); anonymous on a public project | List manual task ordering positions within a view. |
| `PUT` | `/api/v1/projects/:projectId/views/:viewId/task-positions/:taskId` | Access token (fresh) + `tasks.write` | Set or update the manual position of a task within a view. |
| `PUT` | `/api/v1/projects/:projectId/views/:viewId/task-positions` | Access token (fresh) + `tasks.write` | Bulk-upsert manual positions of multiple tasks within a view. |
| `GET` | `/api/v1/projects/:projectId/tasks` | Access token (fresh) + `tasks.read` (or global `projects.read`); anonymous on a public project | List tasks through one shared endpoint. Supported filters include `sprint_id`, `sprint_ids`, `status_id`, `status_ids`, `assignee_id`, `assignee_ids`, `task_type_ids`, and `parent_task_id`. `sprint_id=null` is still supported for unscheduled-only backlog queries. Timeline pages should use `task_type_ids` to request Epic tasks, and manual ordering should be read from `/views/:viewId/task-positions`. |
| `POST` | `/api/v1/projects/:projectId/tasks` | Access token (fresh) + `tasks.write` | Create a task. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId` | Access token (fresh) + `tasks.read` (or global `projects.read`); anonymous on a public project | Get task detail. |
| `PATCH` | `/api/v1/projects/:projectId/tasks/:taskId` | Access token (fresh) + `tasks.write` | Update a task. |
| `DELETE` | `/api/v1/projects/:projectId/tasks/:taskId` | Access token (fresh) + `tasks.write` | Soft-delete a task. |
| `GET` | `/api/v1/projects/:projectId/custom-fields` | Access token (fresh) + `tasks.read` (or global `projects.read`); anonymous on a public project | List custom field definitions for a project. |
| `POST` | `/api/v1/projects/:projectId/custom-fields` | Access token (fresh) + `project.settings.custom_fields.write` | Create a custom field definition. |
| `GET` | `/api/v1/projects/:projectId/custom-fields/:fieldId` | Access token (fresh) + `tasks.read` (or global `projects.read`); anonymous on a public project | Get a custom field definition by ID. |
| `PATCH` | `/api/v1/projects/:projectId/custom-fields/:fieldId` | Access token (fresh) + `project.settings.custom_fields.write` | Update a custom field definition. |
| `DELETE` | `/api/v1/projects/:projectId/custom-fields/:fieldId` | Access token (fresh) + `project.settings.custom_fields.write` | Delete a custom field definition. |
| `GET` | `/api/v1/projects/:projectId/github` | Access token (fresh) + `projects.write` | Get the GitHub integration for a project (token presence only — the PAT value is never returned). Returns `404 GITHUB_INTEGRATION_NOT_FOUND` when no integration is configured. |
| `PUT` | `/api/v1/projects/:projectId/github/token` | Access token (fresh) + `projects.write` | Validate and store (or replace) a GitHub personal access token. The token is validated against the GitHub API before being encrypted at rest. Returns `422 GITHUB_INVALID_TOKEN` if the token is rejected. |
| `DELETE` | `/api/v1/projects/:projectId/github/token` | Access token (fresh) + `projects.write` | Remove the stored GitHub integration and delete all linked repositories and their webhooks. |
| `GET` | `/api/v1/projects/:projectId/github/repositories` | Access token (fresh) + `projects.write` | List all repositories accessible with the project's GitHub PAT. Proxies the GitHub API. |
| `GET` | `/api/v1/projects/:projectId/github/linked-repositories` | Access token (fresh) + `projects.write` | List the repositories currently linked to the project. |
| `POST` | `/api/v1/projects/:projectId/github/linked-repositories` | Access token (fresh) + `projects.write` | Link a repository to the project. Automatically registers a webhook on the GitHub repository using the `PUBLIC_URL` base. |
| `DELETE` | `/api/v1/projects/:projectId/github/linked-repositories/:repoId` | Access token (fresh) + `projects.write` | Unlink a specific linked repository from the project and delete its webhook. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/github/pull-requests` | Access token (fresh) + `tasks.read` | List pull requests linked to a task. |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/github/pull-requests` | Access token (fresh) + `tasks.write` | Link a pull request to a task by PR number. Fetches and caches the PR metadata from GitHub. |
| `DELETE` | `/api/v1/projects/:projectId/tasks/:taskId/github/pull-requests/:prId` | Access token (fresh) + `tasks.write` | Unlink a pull request from a task. |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/github/branches` | Access token (fresh) + `tasks.write` | Create a new git branch in the linked repository from an optional source branch (defaults to `default_branch`). |
| `POST` | `/api/v1/github/webhook` | No (HMAC signature verified) | Receive GitHub webhook events (push, pull_request, check_run, etc.). Signature is verified with the per-repo HMAC-SHA256 secret. Always responds `204`. |

> **"fresh" access token**: an access token whose `must_change_password` claim is `false`. If the claim is `true`, the request is rejected with `403 AUTH_PASSWORD_CHANGE_REQUIRED` and the user must call `PATCH /api/v1/users/me/password` first.

## Current Request and Response Contracts

### `POST /api/v1/auth/login`

Function:

- authenticate a user with username and password;
- set access and refresh tokens as HttpOnly cookies;
- the access token embeds `must_change_password` in the JWT payload so clients can prompt immediately on login.

Request body:

```json
{
  "username": "alice",
  "password": "secret123",
  "remember_me": false
}
```

Success response:

```json
{
  "success": true,
  "data": {
    "message": "logged in"
  },
  "request_id": "..."
}
```

### `POST /api/v1/auth/refresh`

Function:

- read refresh token from HttpOnly cookie;
- issue a rotated access and refresh token pair as HttpOnly cookies.

Request body: (empty - token read from cookie)

Success response:

```json
{
  "success": true,
  "data": {
    "message": "token refreshed"
  },
  "request_id": "..."
}
```

### `POST /api/v1/auth/logout`

Function:

- revoke the authenticated token identified by the current JWT claims;
- terminate the current logical session.

Headers:

```text
Authorization: Bearer <access-token>
```

Success response:

```json
{
  "success": true,
  "data": {
    "message": "logged out"
  },
  "request_id": "..."
}
```

### `GET /api/v1/users/me`

Function:

- read the profile of the authenticated user;
- resolve the user from the JWT subject claim.

Success response data:

```json
{
  "id": "uuid",
  "username": "alice",
  "full_name": "Alice",
  "role": "USER",
  "must_change_password": false,
  "created_at": "2026-03-24T00:00:00Z"
}
```

### `PATCH /api/v1/users/me`

Function:

- update mutable user profile fields;
- current implementation supports `full_name` only.

Request body:

```json
{
  "full_name": "Updated Name"
}
```

### `PATCH /api/v1/users/me/password`

Function:

- change the authenticated user's own password;
- verify `current_password` against the stored bcrypt hash;
- set the new password and clear `must_change_password`;
- revoke the current session (the user must re-authenticate with the new password).

This endpoint is explicitly **not** gated by the `RequireFreshPassword` middleware, so users with `must_change_password = true` can reach it to fulfil the force-change requirement.

Request body:

```json
{
  "current_password": "old-password",
  "new_password": "new-password-min-8"
}
```

Success response: `204 No Content`

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `USER_INVALID_CURRENT_PASSWORD` | 422 | Supplied `current_password` does not match the stored hash. |

### `GET /api/v1/users/me/global-permissions`

Function:

- return the authenticated caller's effective global permissions;
- merge legacy compatibility permissions from the user's stored role with permissions granted by assigned global roles;
- return a deduplicated permission list.

Success response:

```json
{
  "success": true,
  "data": {
    "permissions": [
      "users.read",
      "global_roles.read"
    ]
  },
  "request_id": "..."
}
```

## Implemented Administration API

### `GET /api/v1/admin/users`

Function:

- list all non-deleted users ordered by creation date;
- paginated via `page` and `page_size` query parameters.

Query parameters:

| Parameter | Default | Description |
|---|---|---|
| `page` | `1` | 1-based page number |
| `page_size` | `20` | Items per page (max 100) |

Success response data:

```json
{
  "items": [
    {
      "id": "uuid",
      "username": "alice",
      "full_name": "Alice",
      "role": "USER",
      "must_change_password": true,
      "created_at": "2026-03-24T00:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

### `POST /api/v1/admin/users`

Function:

- create a new user account with the default global role (see `PUT /api/v1/admin/global-roles/:roleId/set-default`; `USER` until another role is made the default);
- return `409 GLOBAL_ROLE_NO_DEFAULT` if no role is the default, rather than guessing one;
- hash password before persistence;
- set `must_change_password = true` so the user is required to change their password on first login.

Assigning a role is a separate privilege (`global_roles.assign`) with its own route, so this body has no `role`: sending one is rejected with `400`. The account always starts with the default role, whatever the caller may assign. To give it another role, create the account and then call `PUT /api/v1/admin/users/:userId/global-roles`.

Request body:

```json
{
  "username": "alice",
  "password": "secret123",
  "full_name": "Alice"
}
```

Success response: `201 Created` with user response data (see `GET /api/v1/users/me` shape).

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `USER_USERNAME_TAKEN` | 409 | Username already in use. |

### `GET /api/v1/admin/users/:userId`

Function:

- get a user profile by UUID;
- returns full user data including `must_change_password` flag.

### `PATCH /api/v1/admin/users/:userId`

Function:

- update a user's profile: `full_name` and `email`;
- a user's role is not part of the profile — a `role` in the body is rejected with `400`; change it with `PUT /api/v1/admin/users/:userId/global-roles`, which requires `global_roles.assign`.

Request body:

```json
{
  "full_name": "New Name"
}
```

### `PATCH /api/v1/admin/users/:userId/password`

Function:

- reset a user's password without knowing the current password (admin privilege);
- set `must_change_password = true` so the user is forced to set a personal password on next login.

Request body:

```json
{
  "new_password": "new-password-min-8"
}
```

Success response: `204 No Content`

### `DELETE /api/v1/admin/users/:userId`

Function:

- soft-delete a user account (sets `deleted_at`);
- restricted to callers with the `users.delete` permission.

Success response: `204 No Content`

### `GET /api/v1/admin/global-roles`

Function:

- list global role definitions;
- return each role with its assigned permission map and whether it is the default (`is_default`).

### `POST /api/v1/admin/global-roles`

Function:

- create a global role definition;
- persist a role name and permission map.

Request body:

```json
{
  "name": "SECURITY_ADMIN",
  "permissions": {
    "global_roles.read": true,
    "users.delete": true
  }
}
```

### `PATCH /api/v1/admin/global-roles/:roleId`

Function:

- update the target global role's name and permission map.

### `DELETE /api/v1/admin/global-roles/:roleId`

Function:

- remove a global role definition;
- returns `409 GLOBAL_ROLE_IS_DEFAULT` if the role is the default — new users and global agents start with it, so make another role the default first;
- returns `409 GLOBAL_ROLE_HAS_ASSIGNED_USERS` if any users or global agents are currently assigned to the role — reassign them first.

### `PUT /api/v1/admin/global-roles/:roleId/set-default`

Function:

- make the role the default: the one every new user and every new global agent starts with;
- clear the flag on the previous default in the same transaction, so exactly one role is the default at any moment (a partial unique index on `global_roles.is_default` enforces it);
- existing users and agents keep the role they have;
- same shape as `PUT /api/v1/projects/:projectId/task-statuses/:statusId/set-default` and its task type sibling.

No request body. Success response: `200 OK` with the role. Returns `404 GLOBAL_ROLE_NOT_FOUND` if the role does not exist.

### `PUT /api/v1/admin/users/:userId/global-roles`

Function:

- set the global role assigned to a user (a user holds exactly one, so this replaces it);
- requires `global_roles.assign`, a privilege separate from `users.write`: no other route changes a user's role.

Request body:

```json
{
  "role_ids": [
    "uuid"
  ]
}
```

### `PUT /api/v1/admin/agents/:agentId/global-role`

Function:

- bind a global agent to the global role that decides what it may do at global scope;
- requires both `agents.write` and `global_roles.assign`;
- the only route that sets an agent's global role: `POST /api/v1/admin/agents` and `PATCH /api/v1/admin/agents/:agentId` reject a `global_role_id` with `400`;
- a new global agent already holds the default global role, or no role when none is the default (creating an agent never fails for lack of one), so this route is for choosing a different role or the first one.

Request body:

```json
{
  "global_role_id": "uuid"
}
```

Success response: `200 OK` with the agent. Returns `404` if the agent is not a global agent or the role does not exist.

### `DELETE /api/v1/admin/agents/:agentId/global-role`

Function:

- unbind a global agent from its global role, leaving it with no global permissions;
- same permissions as binding one: removing a role is the same privileged action.

## Sprint Lifecycle Contracts

### `POST /api/v1/projects/:projectId/sprints` (quick create)

Function:

- create a new sprint with a system-generated default name ("Sprint N" where N is the next sprint number for the project);
- set `status = planned`;
- no request body required.

Request body: (empty)

Success response: `201 Created`

```json
{
  "success": true,
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "name": "Sprint 1",
    "goal": null,
    "start_date": null,
    "end_date": null,
    "status": "planned",
    "created_at": "2026-04-13T00:00:00Z"
  },
  "request_id": "..."
}
```

### `POST /api/v1/projects/:projectId/sprints/:sprintId/start`

Function:

- transition a `planned` sprint to `active`;
- accept name, goal, start date, and due date to set or confirm before starting;
- multiple sprints may be active simultaneously within the same project.

Request body:

```json
{
  "name": "Sprint 1",
  "goal": "Ship the login flow",
  "start_date": "2026-04-14",
  "end_date": "2026-04-27"
}
```

All fields are optional. Fields omitted from the body retain their current values.

Success response: `200 OK`

```json
{
  "success": true,
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "name": "Sprint 1",
    "goal": "Ship the login flow",
    "start_date": "2026-04-14",
    "end_date": "2026-04-27",
    "status": "active",
    "created_at": "2026-04-13T00:00:00Z"
  },
  "request_id": "..."
}
```

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `SPRINT_NOT_PLANNED` | 409 | The sprint is not in `planned` state and cannot be started. |

### `POST /api/v1/projects/:projectId/sprints/:sprintId/complete`

Function:

- transition an `active` sprint to `completed`;
- move all tasks in this sprint whose status category is not `done` to the sprint identified by `move_to_sprint_id`, or to no sprint if `move_to_sprint_id` is `null`;
- tasks whose status category is `done` remain on the completed sprint for record-keeping.

Request body:

```json
{
  "move_to_sprint_id": "uuid-or-null"
}
```

`move_to_sprint_id` is required. Pass `null` to return incomplete tasks to the product backlog (no sprint assigned).

Success response: `200 OK`

```json
{
  "success": true,
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "name": "Sprint 1",
    "status": "completed",
    "moved_task_count": 3,
    "move_to_sprint_id": "uuid-or-null"
  },
  "request_id": "..."
}
```

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `SPRINT_NOT_ACTIVE` | 409 | The sprint is not in `active` state and cannot be completed. |
| `SPRINT_MOVE_TARGET_NOT_FOUND` | 404 | The `move_to_sprint_id` does not refer to a valid sprint in this project. |
| `SPRINT_MOVE_TARGET_COMPLETED` | 409 | The target sprint is already `completed` and cannot receive new tasks. |

---

## Sprint View Contracts

Plugin view config identifier note:

- In view `config`, `plugin_manifest_id` is the canonical plugin identifier for
  `view_type = "plugin"`; it uses the manifest reverse-DNS format (for example
  `com.paca.checklist`).
- `plugin_component` identifies the frontend component export to render.
- This is distinct from plugin-extension-settings APIs where `plugin_id` means
  the plugin UUID.

### `GET /api/v1/projects/:projectId/sprints/:sprintId/views`

Function:

- list all saved view configurations for the sprint ordered by `position`;
- sprint creation automatically seeds a Board view (position 0) and a Table view (position 1).

Success response data:

```json
{
  "items": [
    {
      "id": "uuid",
      "sprint_id": "uuid",
      "name": "Board",
      "view_type": "board",
      "position": 0,
      "config": {},
      "created_at": "2026-03-24T00:00:00Z",
      "updated_at": "2026-03-24T00:00:00Z"
    }
  ]
}
```

### `POST /api/v1/projects/:projectId/sprints/:sprintId/views`

Function:

- create a new sprint view;
- `view_type` must be one of `board`, `table`, or `roadmap`;
- `position` defaults to the next available slot.

Request body:

```json
{
  "name": "My Kanban",
  "view_type": "board",
  "position": 2,
  "config": {
    "column_by": "status",
    "sort_by": "manual"
  }
}
```

Success response: `201 Created` with view data.

### `GET /api/v1/projects/:projectId/sprints/:sprintId/views/:viewId`

Function:

- get a sprint view by ID including its full `config` map.

### `PATCH /api/v1/projects/:projectId/sprints/:sprintId/views/:viewId`

Function:

- update a sprint view's `name`, `position`, or `config`;
- only supplied fields are changed.

Request body:

```json
{
  "name": "Renamed Board",
  "config": {
    "sort_by": "manual"
  }
}
```

Success response: `200 OK` with updated view data.

### `DELETE /api/v1/projects/:projectId/sprints/:sprintId/views/:viewId`

Function:

- delete a sprint view;
- returns `409 VIEW_IS_LAST_VIEW` if this is the only remaining view on the sprint.

Success response: `204 No Content`

### `GET /api/v1/projects/:projectId/sprints/:sprintId/views/:viewId/task-positions`

Function:

- return the manual task ordering positions stored for this view;
- positions are scoped per `group_key` (e.g. a status column ID) and ordered by `position`.

Success response data:

```json
{
  "items": [
    {
      "view_id": "uuid",
      "task_id": "uuid",
      "group_key": "status-uuid",
      "position": 0
    }
  ]
}
```

### `PUT /api/v1/projects/:projectId/sprints/:sprintId/views/:viewId/task-positions/:taskId`

Function:

- upsert the manual position of a task within a view;
- used when `sort_by` is `"manual"` and the user reorders tasks via drag-and-drop.

Request body:

```json
{
  "group_key": "status-uuid",
  "position": 2
}
```

Success response: `204 No Content`

---

## Product Backlog View Contracts

Product-backlog views are identical in structure to sprint views, but they are scoped to a project rather than a sprint. The `sprint_id` field is omitted (or `null`) in all responses; `project_id` is always present.

### `GET /api/v1/projects/:projectId/product-backlog/views`

Function:

- list all saved view configurations for the product backlog ordered by `position`;
- project creation automatically seeds a Table view (position 0, `config.column_by = "sprint"`) as the default, and a Board view (position 1).

Success response data:

```json
{
  "items": [
    {
      "id": "uuid",
      "project_id": "uuid",
      "name": "Board",
      "view_type": "board",
      "position": 0,
      "config": {},
      "created_at": "2026-03-24T00:00:00Z",
      "updated_at": "2026-03-24T00:00:00Z"
    }
  ]
}
```

### `POST /api/v1/projects/:projectId/product-backlog/views`

Function:

- create a new product-backlog view;
- `view_type` must be one of `board`, `table`, or `roadmap`;
- `position` defaults to the next available slot.

Request body:

```json
{
  "name": "My Table",
  "view_type": "table",
  "position": 2,
  "config": {
    "sort_by": "manual"
  }
}
```

Success response: `201 Created` with view data.

### `GET /api/v1/projects/:projectId/product-backlog/views/:viewId`

Function:

- get a product-backlog view by ID including its full `config` map.

### `PATCH /api/v1/projects/:projectId/product-backlog/views/:viewId`

Function:

- update a product-backlog view's `name`, `position`, or `config`;
- only supplied fields are changed.

Request body:

```json
{
  "name": "Renamed Board",
  "config": {
    "sort_by": "manual"
  }
}
```

Success response: `200 OK` with updated view data.

### `DELETE /api/v1/projects/:projectId/product-backlog/views/:viewId`

Function:

- delete a product-backlog view;
- returns `409 VIEW_IS_LAST_VIEW` if this is the only remaining view on the project's backlog.

Success response: `204 No Content`

### `GET /api/v1/projects/:projectId/product-backlog/views/:viewId/task-positions`

Function:

- return the manual task ordering positions stored for this view;
- positions are scoped per `group_key` (e.g. a status column ID) and ordered by `position`.

Success response data:

```json
{
  "items": [
    {
      "view_id": "uuid",
      "task_id": "uuid",
      "group_key": "status-uuid",
      "position": 0
    }
  ]
}
```

### `PUT /api/v1/projects/:projectId/product-backlog/views/:viewId/task-positions/:taskId`

Function:

- upsert the manual position of a task within a product-backlog view;
- used when `sort_by` is `"manual"` and the user reorders tasks via drag-and-drop.

Request body:

```json
{
  "group_key": "status-uuid",
  "position": 2
}
```

Success response: `204 No Content`

---

## Timeline View Contracts

The Timeline interaction surfaces **Epics only** — tasks whose type has `is_system = true AND name = 'Epic'`. Timeline views are stored in `sprint_views` with `view_context = 'timeline'` and `sprint_id = NULL`. The API surface mirrors the Product Backlog contracts.

### `GET /api/v1/projects/:projectId/timeline`

Function:

- list Epics for a project (tasks whose type has `is_system = true AND name = 'Epic'`);
- supports `status_id`, `assignee_id`, and `view_id` query parameters;
- when `view_id` is supplied, each task includes its manual `view_position` and `view_group_key`.

Success response data: same envelope as the product-backlog task list.

### `GET /api/v1/projects/:projectId/timeline/views`

Function:

- list all saved view configurations for the timeline ordered by `position`;
- project creation automatically seeds a single Roadmap view (position 0) as the default.

Success response data: same envelope as the product-backlog views list.

### `POST /api/v1/projects/:projectId/timeline/views`

Function:

- create a new timeline view;
- `view_type` must be one of `board`, `table`, or `roadmap`;
- `position` defaults to the next available slot.

Success response: `201 Created` with view data.

### `GET /api/v1/projects/:projectId/timeline/views/:viewId`

Function:

- get a timeline view by ID.

### `PATCH /api/v1/projects/:projectId/timeline/views/:viewId`

Function:

- update a timeline view's `name`, `position`, or `config`;
- only supplied fields are changed.

Success response: `200 OK` with updated view data.

### `DELETE /api/v1/projects/:projectId/timeline/views/:viewId`

Function:

- delete a timeline view;
- returns `409 VIEW_IS_LAST_VIEW` if this is the only remaining view.

Success response: `204 No Content`

### `PUT /api/v1/projects/:projectId/timeline/views/positions`

Function:

- reorder all timeline views for a project;
- body must contain every timeline view ID for the project in the desired tab order.

Request body:

```json
{ "view_ids": ["<uuid>", "..."] }
```

Success response: `204 No Content`

### `GET /api/v1/projects/:projectId/timeline/views/:viewId/task-positions`

Function:

- return the manual task (Epic) ordering positions stored for this view.

Success response data: same envelope as the product-backlog task-positions list.

### `PUT /api/v1/projects/:projectId/timeline/views/:viewId/task-positions/:taskId`

Function:

- upsert the manual position of an Epic within a timeline view;
- used when `sort_by` is `"manual"`.

Request body:

```json
{
  "group_key": "status-uuid",
  "position": 2
}
```

Success response: `204 No Content`

### `PUT /api/v1/projects/:projectId/timeline/views/:viewId/task-positions`

Function:

- bulk-upsert the manual positions of multiple Epics within a timeline view in a single request.

Request body:

```json
{
  "items": [
    { "task_id": "<uuid>", "position": 65536, "group_key": "status-uuid" }
  ]
}
```

Success response: `204 No Content`

---

## Task Management API

### `POST /api/v1/projects/:projectId/tasks`

Creates a new task in a project. The `description` field is a **JSON array of BlockNote block objects** — not a stringified JSON string.

**Request body:**

```json
{
  "title": "Implement login",
  "description": [
    {
      "id": "1",
      "type": "paragraph",
      "props": { "textColor": "default", "backgroundColor": "default", "textAlignment": "left" },
      "content": [{ "type": "text", "text": "Implement OAuth2 flow", "styles": {} }],
      "children": []
    }
  ],
  "importance": 3,
  "sprint_id": "uuid-or-null",
  "status_id": "uuid-or-null",
  "task_type_id": "uuid-or-null",
  "assignee_ids": ["uuid"],
  "tags": ["backend", "auth"]
}
```

Success response: `201 Created` — task object (see task response shape below).

---

### `GET /api/v1/projects/:projectId/tasks/:taskId`

Returns a single task by its UUID.

Success response: `200 OK`

```json
{
  "data": {
    "id": "uuid",
    "project_id": "uuid",
    "task_number": 1,
    "title": "Implement login",
    "description": [
      {
        "id": "1",
        "type": "paragraph",
        "content": [{ "type": "text", "text": "Implement OAuth2 flow", "styles": {} }],
        "children": []
      }
    ],
    "importance": 3,
    "sprint_id": null,
    "status_id": null,
    "task_type_id": null,
    "assignee_ids": [],
    "reporter_id": null,
    "tags": [],
    "custom_fields": {},
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z"
  }
}
```

---

### `PATCH /api/v1/projects/:projectId/tasks/:taskId`

Partially updates a task. Only fields present in the request body are updated. Sending `null` for a nullable field explicitly clears it. The `description` field follows the same JSON array format as the create endpoint.

**Three-state patch semantics:**

| Request body         | Effect                             |
|----------------------|------------------------------------|
| Field absent         | Field is not changed               |
| `"description": null`| Description is cleared             |
| `"description": [...]`| Description is set to new value   |

**Example — update title only (description is preserved):**

```json
{ "title": "Updated title" }
```

**Example — update description:**

```json
{
  "description": [
    {
      "type": "paragraph",
      "content": [{ "type": "text", "text": "New content", "styles": {} }],
      "children": []
    }
  ]
}
```

**Example — clear description:**

```json
{ "description": null }
```

Success response: `200 OK` — updated task object.

---

### `DELETE /api/v1/projects/:projectId/tasks/:taskId`

Soft-deletes a task.

Success response: `204 No Content`

---

## Task List API

### `GET /api/v1/projects/:projectId/tasks`

Function:

- list all non-deleted tasks for a project, with optional filtering and pagination;
- when `view_id` is supplied, each task in the response includes its manual `view_position` and `view_group_key` from that view's ordering (these fields are omitted when the task has no recorded position in the requested view);
- tasks with system task types (`is_system = true`, i.e. Epic and Subtask) are **included** in this general list endpoint and are distinguished by the `is_system_type` flag in the response; use `exclude_system_types=true` to omit them (default behaviour of the product-backlog and sprint-specific task list endpoints).

Query parameters:

| Parameter | Default | Description |
|---|---|---|
| `page` | `1` | 1-based page number |
| `page_size` | `20` | Items per page (max 100) |
| `sprint_id` | – | Filter to tasks assigned to a specific sprint |
| `status_id` | – | Filter to tasks with a specific status |
| `assignee_id` | – | Filter to tasks assigned to a specific user |
| `view_id` | – | UUID of a view; enriches each task with its manual position in that view |
| `exclude_system_types` | `false` | When `true`, omit tasks whose type has `is_system = true` (Epic, Subtask) |

Success response data (without `view_id`):

```json
{
  "items": [
    {
      "id": "uuid",
      "project_id": "uuid",
      "title": "Implement feature X",
      "importance": 3,
      "custom_fields": {},
      "created_at": "2026-04-01T00:00:00Z",
      "updated_at": "2026-04-01T00:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

Success response data (with `view_id`):

```json
{
  "items": [
    {
      "id": "uuid",
      "project_id": "uuid",
      "title": "Implement feature X",
      "importance": 3,
      "custom_fields": {},
      "view_position": 2,
      "view_group_key": "status-uuid",
      "created_at": "2026-04-01T00:00:00Z",
      "updated_at": "2026-04-01T00:00:00Z"
    }
  ],
  "total": 1,
  "page": 1,
  "page_size": 20
}
```

Notes:

- `view_position` and `view_group_key` are omitted (`omitempty`) when the task has no recorded position in the requested view, or when `view_id` is not provided.
- If `view_id` refers to a view that has no stored positions, tasks are returned without position fields (200 OK, no error).
- An invalid UUID supplied as `view_id` returns `400 BAD_REQUEST`.

---

## Custom Field Definition Contracts

Custom field definitions describe extra metadata fields that can be attached to tasks within a project. Each definition is scoped to a project and has an immutable `field_key` that uniquely identifies it within the project.

**Supported `field_type` values:** `text`, `number`, `date`, `select`, `multi_select`, `boolean`, `url`

### `GET /api/v1/projects/:projectId/custom-fields`

Function:

- list all custom field definitions for the project in creation order.

Success response data:

```json
{
  "items": [
    {
      "id": "uuid",
      "project_id": "uuid",
      "field_key": "priority_level",
      "display_name": "Priority Level",
      "field_type": "text",
      "options": [],
      "is_required": false,
      "created_at": "2026-04-01T00:00:00Z",
      "updated_at": "2026-04-01T00:00:00Z"
    }
  ]
}
```

### `POST /api/v1/projects/:projectId/custom-fields`

Function:

- create a new custom field definition;
- `field_key` must be unique within the project and is immutable after creation;
- `options` is required (and used) only for `select` and `multi_select` types.

Request body:

```json
{
  "field_key": "priority_level",
  "display_name": "Priority Level",
  "field_type": "select",
  "options": ["low", "medium", "high"],
  "is_required": false
}
```

Success response: `201 Created` with the created custom field definition.

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `CUSTOM_FIELD_KEY_INVALID` | 400 | `field_key` is empty or invalid. |
| `CUSTOM_FIELD_NAME_INVALID` | 400 | `display_name` is empty or invalid. |
| `CUSTOM_FIELD_TYPE_INVALID` | 400 | `field_type` is not one of the allowed values. |
| `CUSTOM_FIELD_KEY_TAKEN` | 409 | A field with that `field_key` already exists in this project. |

### `GET /api/v1/projects/:projectId/custom-fields/:fieldId`

Function:

- return a single custom field definition by ID.

Success response data: same shape as a single item in `GET /custom-fields`.

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `CUSTOM_FIELD_NOT_FOUND` | 404 | No custom field with the given ID exists. |

### `PATCH /api/v1/projects/:projectId/custom-fields/:fieldId`

Function:

- update mutable fields: `display_name`, `field_type`, `options`, `is_required`;
- `field_key` is **immutable** and cannot be changed after creation;
- only supplied fields are updated (partial update).

Request body:

```json
{
  "display_name": "Priority Level (Updated)",
  "options": ["low", "medium", "high", "critical"]
}
```

Success response: `200 OK` with the updated custom field definition.

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `CUSTOM_FIELD_NOT_FOUND` | 404 | No custom field with the given ID exists. |
| `CUSTOM_FIELD_NAME_INVALID` | 400 | `display_name` is empty or invalid. |
| `CUSTOM_FIELD_TYPE_INVALID` | 400 | `field_type` is not one of the allowed values. |

### `DELETE /api/v1/projects/:projectId/custom-fields/:fieldId`

Function:

- permanently delete a custom field definition;
- existing task data referencing this field key in `tasks.custom_fields` is not automatically cleaned up.

Success response: `200 OK`

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `CUSTOM_FIELD_NOT_FOUND` | 404 | No custom field with the given ID exists. |

---

## GitHub Integration Contracts

### `PUT /api/v1/projects/:projectId/github/token`

Function:

- validate the supplied GitHub personal access token against the GitHub API;
- encrypt the token with AES-256-GCM and store it for the project;
- replace any previously stored integration.

Request body:

```json
{
  "token": "ghp_xxxxxxxxxxxxxxxxxxxx"
}
```

Success response (`200 OK`):

```json
{
  "success": true,
  "data": {
    "id": "<uuid>",
    "project_id": "<uuid>",
    "created_at": "2026-04-22T10:00:00Z",
    "updated_at": "2026-04-22T10:00:00Z"
  },
  "request_id": "..."
}
```

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `GITHUB_INVALID_TOKEN` | 422 | Token was rejected by the GitHub API (401/403). |

### `GET /api/v1/projects/:projectId/github`

Function: return confirmation that a GitHub integration exists (PAT is never returned).

Success response (`200 OK`): same shape as `PUT /github/token`.

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `GITHUB_INTEGRATION_NOT_FOUND` | 404 | No integration has been configured for this project. |

### `DELETE /api/v1/projects/:projectId/github/token`

Function: remove the stored PAT. Also removes the linked repository record and attempts to delete the webhook from GitHub. Success response: `204 No Content`.

### `GET /api/v1/projects/:projectId/github/repositories`

Function: proxy the GitHub API and return all repositories accessible with the project's PAT.

Success response (`200 OK`):

```json
{
  "success": true,
  "data": [
    {
      "full_name": "owner/repo",
      "owner": "owner",
      "repo_name": "repo",
      "default_branch": "main",
      "private": false,
      "description": "A repository"
    }
  ],
  "request_id": "..."
}
```

### `PUT /api/v1/projects/:projectId/github/repository`

Function:

- link the specified repository to the project;
- fetch repository metadata from GitHub;
- generate an HMAC secret and register a webhook on the GitHub repository using `PUBLIC_URL` as the base;
- store encrypted webhook secret.

Request body:

```json
{
  "owner": "my-org",
  "repo_name": "my-repo"
}
```

Success response (`200 OK`):

```json
{
  "success": true,
  "data": {
    "id": "<uuid>",
    "project_id": "<uuid>",
    "integration_id": "<uuid>",
    "owner": "my-org",
    "repo_name": "my-repo",
    "full_name": "my-org/my-repo",
    "default_branch": "main",
    "webhook_id": 12345678,
    "created_at": "2026-04-22T10:00:00Z",
    "updated_at": "2026-04-22T10:00:00Z"
  },
  "request_id": "..."
}
```

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `GITHUB_INTEGRATION_NOT_FOUND` | 404 | No PAT has been configured; call `PUT /github/token` first. |
| `GITHUB_WEBHOOK_URL_REQUIRED` | 500 | `PUBLIC_URL` env var is not set; automatic webhook creation is unavailable. |

### `DELETE /api/v1/projects/:projectId/github/repository`

Function: unlink the repository and attempt to delete the GitHub webhook. Success response: `204 No Content`.

### `GET /api/v1/projects/:projectId/tasks/:taskId/github/pull-requests`

Function: return pull requests linked to the task.

Success response (`200 OK`):

```json
{
  "success": true,
  "data": [
    {
      "id": "<uuid>",
      "project_id": "<uuid>",
      "repo_id": "<uuid>",
      "pr_number": 42,
      "github_pr_id": 999999,
      "title": "feat: add dark mode",
      "state": "open",
      "html_url": "https://github.com/owner/repo/pull/42",
      "head_branch": "feature/dark-mode",
      "base_branch": "main",
      "author": "alice",
      "merged_at": null,
      "created_at": "2026-04-22T10:00:00Z",
      "updated_at": "2026-04-22T10:00:00Z"
    }
  ],
  "request_id": "..."
}
```

### `POST /api/v1/projects/:projectId/tasks/:taskId/github/pull-requests`

Function:

- fetch the specified PR from the GitHub API and cache it;
- create a link between the task and the PR.

Request body:

```json
{
  "pr_number": 42
}
```

Success response: `201 Created` with the same PR object shape.

Error codes:

| Code | HTTP | Meaning |
|---|---|---|
| `GITHUB_INTEGRATION_NOT_FOUND` | 404 | No PAT configured for the project. |
| `GITHUB_REPOSITORY_NOT_FOUND` | 404 | No repository linked to the project. |
| `GITHUB_PR_NOT_FOUND` | 404 | PR with the given number does not exist in the linked repository. |
| `GITHUB_PR_ALREADY_LINKED` | 409 | PR is already linked to this task. |

### `DELETE /api/v1/projects/:projectId/tasks/:taskId/github/pull-requests/:prId`

Function: remove the link between the task and the pull request (does not affect the PR on GitHub). Success response: `204 No Content`.

### `POST /api/v1/projects/:projectId/tasks/:taskId/github/branches`

Function: create a new branch in the linked GitHub repository.

Request body:

```json
{
  "branch_name": "feature/PACA-42-dark-mode",
  "source_branch": "main"
}
```

`source_branch` is optional; defaults to the repository's `default_branch` when omitted.

Success response (`201 Created`):

```json
{
  "success": true,
  "data": {
    "branch_name": "feature/PACA-42-dark-mode"
  },
  "request_id": "..."
}
```

### `POST /api/v1/github/webhook`

Function:

- receive a GitHub webhook event (push, pull_request, check_run, etc.);
- look up the repository by `repository.full_name` in the payload;
- verify the `X-Hub-Signature-256` HMAC-SHA256 signature against the stored per-repo secret;
- handle `pull_request` events by upserting cached PR metadata.

This endpoint is **public** (no bearer token required). Signature verification is mandatory; mismatched signatures are silently ignored.

Always responds `204 No Content` regardless of outcome so GitHub does not retry on application errors.

---

## Planned Resource API

The following endpoints are not yet implemented. They are the recommended path design for the next API slices based on the domain model.

## Task Extras

Sub-resources of tasks that are not yet implemented.

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/children` | List child tasks under a parent task. |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/children` | Create a child task under the specified parent task. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/activities` | Access token (fresh) + `tasks.read` (or global `projects.read`); anonymous on a public project |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/activities` | Add a task activity entry such as comment, status change note, or system event. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/time-logs` | List time logs recorded against a task. |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/time-logs` | Record time spent on a task. |
| `PATCH` | `/api/v1/projects/:projectId/tasks/:taskId/time-logs/:timeLogId` | Update a time log entry. |
| `DELETE` | `/api/v1/projects/:projectId/tasks/:taskId/time-logs/:timeLogId` | Delete a time log entry. |

## Project Configuration Extras

Custom field definition endpoints are implemented. See [Custom Field Definition Contracts](#custom-field-definition-contracts) below.

## Sprint Extras

No additional sprint sub-resources are planned at this time.

## Knowledge and Reporting

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/documents` | List project documents. |
| `POST` | `/api/v1/projects/:projectId/documents` | Create a project document. |
| `GET` | `/api/v1/projects/:projectId/documents/:documentId` | Get document content and metadata. |
| `PATCH` | `/api/v1/projects/:projectId/documents/:documentId` | Update a document title or content. |
| `DELETE` | `/api/v1/projects/:projectId/documents/:documentId` | Delete a document. |
| `GET` | `/api/v1/projects/:projectId/dashboards` | List saved dashboards. |
| `POST` | `/api/v1/projects/:projectId/dashboards` | Create a dashboard layout. |
| `GET` | `/api/v1/projects/:projectId/dashboards/:dashboardId` | Get a dashboard definition. |
| `PATCH` | `/api/v1/projects/:projectId/dashboards/:dashboardId` | Update dashboard name or layout. |
| `DELETE` | `/api/v1/projects/:projectId/dashboards/:dashboardId` | Delete a dashboard. |

## Recommended Delivery Order

To keep the API coherent and aligned with the current codebase, implement the next slices in this order:

1. ~~Normalize the existing auth and user error contracts.~~ ✅ Done
2. ~~Add complete admin user management: list, create, update, delete, reset-password.~~ ✅ Done
3. ~~Force password change after admin create/reset.~~ ✅ Done
4. ~~Add project and project-member endpoints.~~ ✅ Done
5. ~~Add task configuration endpoints: statuses and types.~~ ✅ Done
6. ~~Add task CRUD endpoints.~~ ✅ Done
7. ~~Add sprint CRUD, sprint backlog view (`GET /sprints/:id/tasks`), and product-backlog view (`GET /product-backlog`).~~ ✅ Done
8. ~~Add sprint saved views: board, table, roadmap with manual task ordering.~~ ✅ Done
9. Add task sub-resource endpoints: child tasks, activities, and time logs.
10. ~~Add custom field definitions.~~ ✅ Done
11. ~~Add documents (CRUD, folders, snapshots, activities).~~ ✅ Done
12. Add dashboards (planned).

## Known Model Gaps

The auth/user implementation is aligned with the database schema:

- Users are identified by `username` (unique, required) and stored with `full_name`.
- Authentication uses `username` + password; there is no email field.
- UUIDs are used for all public resource identifiers.
- The `users` table stores a `role_id` FK pointing to `global_roles`; the role name is resolved via a JOIN on every read.
- `must_change_password` is persisted in `users` and embedded in access tokens so middleware can enforce the force-change requirement without a DB round-trip.

The schema and HTTP contract are consistent. Before adding the next slice (projects/tasks), update [../architecture/database-schema.md](../architecture/database-schema.md) first so the storage model and HTTP contract continue to move together.

## Error Codes Reference

| Code | HTTP | Meaning |
|---|---|---|
| `AUTH_INVALID_CREDENTIALS` | 401 | Username or password incorrect. |
| `AUTH_MISSING_TOKEN` | 401 | Request has no access token. |
| `AUTH_TOKEN_INVALID` | 401 | Token is malformed, expired, or of the wrong kind. |
| `AUTH_UNAUTHENTICATED` | 401 | Generic unauthenticated access. |
| `AUTH_PASSWORD_CHANGE_REQUIRED` | 403 | User must call `PATCH /users/me/password` before accessing this resource. |
| `USER_NOT_FOUND` | 404 | User with the given ID does not exist. |
| `USER_USERNAME_TAKEN` | 409 | Username already in use. |
| `USER_INVALID_CURRENT_PASSWORD` | 422 | Supplied `current_password` does not match the stored hash. |
| `FORBIDDEN` | 403 | Caller lacks the required permission. |
| `GLOBAL_ROLE_NOT_FOUND` | 404 | Global role with the given ID does not exist. |
| `GLOBAL_ROLE_NAME_TAKEN` | 409 | A global role with that name already exists. |
| `GLOBAL_ROLE_NAME_INVALID` | 400 | Role name does not meet naming requirements. |
| `GLOBAL_ROLE_HAS_ASSIGNED_USERS` | 409 | Role cannot be deleted while users or global agents are assigned to it. |
| `GLOBAL_ROLE_IS_DEFAULT` | 409 | The default role cannot be deleted; make another role the default first. |
| `GLOBAL_ROLE_NO_DEFAULT` | 409 | No global role is the default, so a new user cannot be given one. Creating a global agent does not fail: it is created without a role. |
| `PROJECT_NOT_FOUND` | 404 | Project with the given ID does not exist. |
| `PROJECT_NAME_TAKEN` | 409 | A project with that name already exists. |
| `PROJECT_NAME_INVALID` | 400 | Project name is empty or does not meet naming requirements. |
| `PROJECT_ROLE_NOT_FOUND` | 404 | Project role with the given ID does not exist. |
| `PROJECT_ROLE_NAME_TAKEN` | 409 | A role with that name already exists within the project. |
| `PROJECT_ROLE_NAME_INVALID` | 400 | Project role name is empty or invalid. |
| `PROJECT_ROLE_HAS_MEMBERS` | 409 | Project role cannot be deleted while members are assigned to it. |
| `PROJECT_MEMBER_NOT_FOUND` | 404 | Membership record for the given user in this project does not exist. |
| `PROJECT_MEMBER_ALREADY_ADDED` | 409 | User is already a member of the project. |
| `TASK_NOT_FOUND` | 404 | Task with the given ID does not exist. |
| `TASK_TITLE_INVALID` | 400 | Task title is empty or invalid. |
| `TASK_TYPE_NOT_FOUND` | 404 | Task type with the given ID does not exist. |
| `TASK_TYPE_NAME_INVALID` | 400 | Task type name is empty or invalid. |
| `TASK_TYPE_IS_DEFAULT` | 409 | The project's default task type cannot be deleted; make another type the default first. |
| `TASK_STATUS_NOT_FOUND` | 404 | Task status with the given ID does not exist. |
| `TASK_STATUS_NAME_INVALID` | 400 | Task status name is empty or invalid. |
| `TASK_STATUS_IS_DEFAULT` | 409 | The project's default status cannot be deleted; make another status the default first. |
| `TASK_STATUS_CATEGORY_INVALID` | 400 | Task status category value is not one of the allowed values. |
| `SPRINT_NOT_FOUND` | 404 | Sprint with the given ID does not exist. |
| `SPRINT_NAME_INVALID` | 400 | Sprint name is empty or invalid. |
| `SPRINT_STATUS_INVALID` | 400 | Sprint status value is not one of the allowed values. |
| `VIEW_NOT_FOUND` | 404 | Sprint view with the given ID does not exist. |
| `VIEW_NAME_INVALID` | 400 | View name is empty or invalid. |
| `VIEW_TYPE_INVALID` | 400 | View type is not one of `board`, `table`, or `roadmap`. |
| `VIEW_IS_LAST_VIEW` | 409 | View cannot be deleted because it is the only remaining view on the sprint. |
| `CUSTOM_FIELD_NOT_FOUND` | 404 | Custom field definition with the given ID does not exist. |
| `CUSTOM_FIELD_KEY_INVALID` | 400 | `field_key` is empty or contains invalid characters. |
| `CUSTOM_FIELD_KEY_TAKEN` | 409 | A field with that `field_key` already exists within the project. |
| `CUSTOM_FIELD_TYPE_INVALID` | 400 | `field_type` is not one of the allowed values. |
| `CUSTOM_FIELD_NAME_INVALID` | 400 | `display_name` is empty or invalid. |
| `NOTIFICATION_NOT_FOUND` | 404 | Notification with the given ID does not exist or belongs to another user. |
| `GITHUB_INTEGRATION_NOT_FOUND` | 404 | No GitHub integration configured for the project. |
| `GITHUB_REPOSITORY_NOT_FOUND` | 404 | No GitHub repository linked to the project. |
| `GITHUB_PR_NOT_FOUND` | 404 | Pull request with the given ID does not exist. |
| `GITHUB_PR_LINK_NOT_FOUND` | 404 | Task-PR link with the given ID does not exist. |
| `GITHUB_PR_ALREADY_LINKED` | 409 | This pull request is already linked to the task. |
| `GITHUB_INVALID_TOKEN` | 422 | The GitHub personal access token was rejected by the GitHub API. |
| `GITHUB_WEBHOOK_URL_REQUIRED` | 500 | `PUBLIC_URL` is not configured; automatic webhook creation is unavailable. |
| `BAD_REQUEST` | 400 | Malformed or invalid request body. |
| `INTERNAL_ERROR` | 500 | Unexpected server error. |