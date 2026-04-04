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
- Permissions may come from legacy role compatibility, assigned global roles, and later project-scoped roles.

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
| `POST` | `/api/v1/admin/users` | Access token (fresh) + `users.write` | Create a new user account. Sets `must_change_password = true`. |
| `GET` | `/api/v1/admin/users/:userId` | Access token (fresh) + `users.read` | Get a user profile by ID. |
| `PATCH` | `/api/v1/admin/users/:userId` | Access token (fresh) + `users.write` | Update a user's `full_name` or `role`. |
| `PATCH` | `/api/v1/admin/users/:userId/password` | Access token (fresh) + `users.write` | Admin password reset. Sets `must_change_password = true`. |
| `DELETE` | `/api/v1/admin/users/:userId` | Access token (fresh) + `users.delete` | Soft-delete a user account. |
| `GET` | `/api/v1/admin/global-roles` | Access token (fresh) + `global_roles.read` | List available global roles and permissions. |
| `POST` | `/api/v1/admin/global-roles` | Access token (fresh) + `global_roles.write` | Create a new global role definition. |
| `PATCH` | `/api/v1/admin/global-roles/:roleId` | Access token (fresh) + `global_roles.write` | Update a global role definition. |
| `DELETE` | `/api/v1/admin/global-roles/:roleId` | Access token (fresh) + `global_roles.write` | Remove a global role definition. Fails with `409` if users are assigned to it. |
| `PUT` | `/api/v1/admin/users/:userId/global-roles` | Access token (fresh) + `global_roles.assign` | Assign or replace the single global role for a user. |
| `GET` | `/api/v1/projects` | Access token (fresh) | List projects visible to the caller. |
| `POST` | `/api/v1/projects` | Access token (fresh) + `projects.create` | Create a new project. |
| `GET` | `/api/v1/projects/:projectId` | Access token (fresh) + `projects.read` | Get project details. |
| `PATCH` | `/api/v1/projects/:projectId` | Access token (fresh) + `projects.write` | Update project name or description. |
| `DELETE` | `/api/v1/projects/:projectId` | Access token (fresh) + `projects.delete` | Delete a project. |
| `GET` | `/api/v1/projects/:projectId/members` | Access token (fresh) + `members.read` | List project members. |
| `POST` | `/api/v1/projects/:projectId/members` | Access token (fresh) + `members.write` | Add a user to a project. |
| `PATCH` | `/api/v1/projects/:projectId/members/:userId` | Access token (fresh) + `members.write` | Change a member's project role. |
| `DELETE` | `/api/v1/projects/:projectId/members/:userId` | Access token (fresh) + `members.write` | Remove a member from a project. |
| `GET` | `/api/v1/projects/:projectId/roles` | Access token (fresh) + `roles.read` | List custom project roles. |
| `POST` | `/api/v1/projects/:projectId/roles` | Access token (fresh) + `roles.write` | Create a project-scoped role. |
| `PATCH` | `/api/v1/projects/:projectId/roles/:roleId` | Access token (fresh) + `roles.write` | Update a project role. |
| `DELETE` | `/api/v1/projects/:projectId/roles/:roleId` | Access token (fresh) + `roles.write` | Delete a project role. |
| `GET` | `/api/v1/projects/:projectId/task-types` | Access token (fresh) + `tasks.read` | List task type definitions. |
| `POST` | `/api/v1/projects/:projectId/task-types` | Access token (fresh) + `tasks.write` | Create a task type (e.g. story, bug, chore). |
| `PATCH` | `/api/v1/projects/:projectId/task-types/:typeId` | Access token (fresh) + `tasks.write` | Update a task type. |
| `DELETE` | `/api/v1/projects/:projectId/task-types/:typeId` | Access token (fresh) + `tasks.write` | Delete a task type. |
| `GET` | `/api/v1/projects/:projectId/task-statuses` | Access token (fresh) + `tasks.read` | List workflow statuses in board order. |
| `POST` | `/api/v1/projects/:projectId/task-statuses` | Access token (fresh) + `tasks.write` | Create a workflow status. |
| `PATCH` | `/api/v1/projects/:projectId/task-statuses/:statusId` | Access token (fresh) + `tasks.write` | Update a workflow status. |
| `DELETE` | `/api/v1/projects/:projectId/task-statuses/:statusId` | Access token (fresh) + `tasks.write` | Delete a workflow status. |
| `GET` | `/api/v1/projects/:projectId/sprints` | Access token (fresh) + `sprints.read` | List sprints for a project ordered by creation date. |
| `POST` | `/api/v1/projects/:projectId/sprints` | Access token (fresh) + `sprints.write` | Create a sprint. |
| `GET` | `/api/v1/projects/:projectId/sprints/:sprintId` | Access token (fresh) + `sprints.read` | Get sprint details (goal, dates, status). |
| `PATCH` | `/api/v1/projects/:projectId/sprints/:sprintId` | Access token (fresh) + `sprints.write` | Update sprint metadata or lifecycle status. |
| `DELETE` | `/api/v1/projects/:projectId/sprints/:sprintId` | Access token (fresh) + `sprints.write` | Delete a sprint. |
| `GET` | `/api/v1/projects/:projectId/sprints/:sprintId/tasks` | Access token (fresh) + `tasks.read` | List tasks assigned to a specific sprint (sprint backlog view). |
| `GET` | `/api/v1/projects/:projectId/product-backlog` | Access token (fresh) + `tasks.read` | List tasks not yet assigned to any sprint (product backlog view). |
| `GET` | `/api/v1/projects/:projectId/tasks` | Access token (fresh) + `tasks.read` | List tasks with optional filters (`sprint_id`, `status_id`, `assignee_id`). |
| `POST` | `/api/v1/projects/:projectId/tasks` | Access token (fresh) + `tasks.write` | Create a task. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId` | Access token (fresh) + `tasks.read` | Get task detail. |
| `PATCH` | `/api/v1/projects/:projectId/tasks/:taskId` | Access token (fresh) + `tasks.write` | Update a task. |
| `DELETE` | `/api/v1/projects/:projectId/tasks/:taskId` | Access token (fresh) + `tasks.write` | Soft-delete a task. |

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

- create a new user account;
- resolve the provided role name against `global_roles`;
- hash password before persistence;
- set `must_change_password = true` so the user is required to change their password on first login.

Request body:

```json
{
  "username": "alice",
  "password": "secret123",
  "full_name": "Alice",
  "role": "USER"
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

- update a user's `full_name` or `role`;
- role changes are validated against `global_roles` — the role name must exist.

Request body:

```json
{
  "full_name": "New Name",
  "role": "ADMIN"
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
- return each role with its assigned permission map.

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
- returns `409 GLOBAL_ROLE_HAS_ASSIGNED_USERS` if any users are currently assigned to the role — reassign users first.

### `PUT /api/v1/admin/users/:userId/global-roles`

Function:

- replace the complete set of global roles assigned to a user.

Request body:

```json
{
  "role_ids": [
    "uuid"
  ]
}
```

## Planned Resource API

The following endpoints are not yet implemented. They are the recommended path design for the next API slices based on the domain model.

## Task Extras

Sub-resources of tasks that are not yet implemented.

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/children` | List child tasks under a parent task. |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/children` | Create a child task under the specified parent task. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/activities` | List audit/activity entries for a task. |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/activities` | Add a task activity entry such as comment, status change note, or system event. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/bdd-scenarios` | List BDD scenarios attached to a task. |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/bdd-scenarios` | Add a BDD scenario to a task. |
| `PATCH` | `/api/v1/projects/:projectId/tasks/:taskId/bdd-scenarios/:scenarioId` | Update a BDD scenario. |
| `DELETE` | `/api/v1/projects/:projectId/tasks/:taskId/bdd-scenarios/:scenarioId` | Delete a BDD scenario. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/time-logs` | List time logs recorded against a task. |
| `POST` | `/api/v1/projects/:projectId/tasks/:taskId/time-logs` | Record time spent on a task. |
| `PATCH` | `/api/v1/projects/:projectId/tasks/:taskId/time-logs/:timeLogId` | Update a time log entry. |
| `DELETE` | `/api/v1/projects/:projectId/tasks/:taskId/time-logs/:timeLogId` | Delete a time log entry. |

## Project Configuration Extras

Custom field definitions are not yet implemented.

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/custom-fields` | List custom field definitions. |
| `POST` | `/api/v1/projects/:projectId/custom-fields` | Create a custom field definition. |
| `PATCH` | `/api/v1/projects/:projectId/custom-fields/:fieldId` | Update a custom field definition. |
| `DELETE` | `/api/v1/projects/:projectId/custom-fields/:fieldId` | Delete a custom field definition. |

## Sprint Extras

Saved sprint views (kanban, list, Gantt, burndown configurations) are not yet implemented.

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/sprints/:sprintId/views` | List saved sprint view configurations. |
| `POST` | `/api/v1/projects/:projectId/sprints/:sprintId/views` | Create a saved sprint view configuration. |
| `PATCH` | `/api/v1/projects/:projectId/sprints/:sprintId/views/:viewId` | Update a sprint view configuration. |
| `DELETE` | `/api/v1/projects/:projectId/sprints/:sprintId/views/:viewId` | Delete a sprint view configuration. |

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
8. Add task sub-resource endpoints: child tasks, activities, BDD scenarios, and time logs.
9. Add custom field definitions.
10. Add sprint saved views (kanban, list, Gantt, burndown).
11. Add knowledge and reporting: documents and dashboards.

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
| `GLOBAL_ROLE_HAS_ASSIGNED_USERS` | 409 | Role cannot be deleted while users are assigned to it. |
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
| `TASK_STATUS_NOT_FOUND` | 404 | Task status with the given ID does not exist. |
| `TASK_STATUS_NAME_INVALID` | 400 | Task status name is empty or invalid. |
| `TASK_STATUS_CATEGORY_INVALID` | 400 | Task status category value is not one of the allowed values. |
| `SPRINT_NOT_FOUND` | 404 | Sprint with the given ID does not exist. |
| `SPRINT_NAME_INVALID` | 400 | Sprint name is empty or invalid. |
| `SPRINT_STATUS_INVALID` | 400 | Sprint status value is not one of the allowed values. |
| `BAD_REQUEST` | 400 | Malformed or invalid request body. |
| `INTERNAL_ERROR` | 500 | Unexpected server error. |