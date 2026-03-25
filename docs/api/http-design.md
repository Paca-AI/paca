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
- Role-based checks are enforced in middleware for admin-only operations.

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
| `POST` | `/api/v1/users` | No | Register a new user account. |
| `GET` | `/api/v1/users/me` | Access token | Return the authenticated caller's own profile. |
| `PATCH` | `/api/v1/users/:id` | Access token | Update mutable profile fields for the specified user. Current implementation supports `full_name`. |
| `DELETE` | `/api/v1/users/:id` | Access token + `ADMIN` role | Delete a user account. |

## Current Request and Response Contracts

### `POST /api/v1/auth/login`

Function:

- authenticate a user with username and password;
- set access and refresh tokens as HttpOnly cookies.

Request body:

```json
{
  "username": "alice",
  "password": "secret123"
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

### `POST /api/v1/users`

Function:

- create a new user record;
- hash the password before persistence;
- assign the default `USER` role.

Request body:

```json
{
  "username": "alice",
  "password": "secret123",
  "full_name": "Alice"
}
```

Success response data:

```json
{
  "id": "uuid",
  "username": "alice",
  "full_name": "Alice",
  "role": "USER",
  "created_at": "2026-03-24T00:00:00Z"
}
```

### `GET /api/v1/users/me`

Function:

- read the profile of the authenticated user;
- resolve the user from the JWT subject claim.

### `PATCH /api/v1/users/:id`

Function:

- update mutable user profile fields;
- current implementation supports `full_name` only.

Request body:

```json
{
  "full_name": "Updated Name"
}
```

### `DELETE /api/v1/users/:id`

Function:

- remove or soft-delete a user account;
- restricted to callers with the `ADMIN` role.

## Planned Resource API

The following endpoints are not fully implemented yet, but they are the recommended path design for the next API slices based on the domain model.

## Identity and Administration

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/users` | List users for administration and member selection. |
| `GET` | `/api/v1/users/:id` | Get a user profile by ID. |
| `GET` | `/api/v1/admin/global-roles` | List available global roles and permissions. |
| `POST` | `/api/v1/admin/global-roles` | Create a new global role definition. |
| `PATCH` | `/api/v1/admin/global-roles/:roleId` | Update a global role definition. |
| `DELETE` | `/api/v1/admin/global-roles/:roleId` | Remove a global role definition. |
| `PUT` | `/api/v1/admin/users/:userId/global-roles` | Replace the set of global roles assigned to a user. |

## Projects and Membership

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects` | List projects visible to the caller. |
| `POST` | `/api/v1/projects` | Create a project with initial settings. |
| `GET` | `/api/v1/projects/:projectId` | Get project details, settings, and summary metadata. |
| `PATCH` | `/api/v1/projects/:projectId` | Update project name, description, or settings. |
| `DELETE` | `/api/v1/projects/:projectId` | Archive or delete a project. |
| `GET` | `/api/v1/projects/:projectId/members` | List project members and their project roles. |
| `POST` | `/api/v1/projects/:projectId/members` | Add a user to a project with a project role. |
| `PATCH` | `/api/v1/projects/:projectId/members/:memberId` | Change a member's project role or membership metadata. |
| `DELETE` | `/api/v1/projects/:projectId/members/:memberId` | Remove a member from a project. |
| `GET` | `/api/v1/projects/:projectId/roles` | List custom project roles for a project. |
| `POST` | `/api/v1/projects/:projectId/roles` | Create a project-specific role and permission set. |
| `PATCH` | `/api/v1/projects/:projectId/roles/:roleId` | Update a project role definition. |
| `DELETE` | `/api/v1/projects/:projectId/roles/:roleId` | Delete a project role definition. |

## Task Configuration

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/task-types` | List task type definitions for a project. |
| `POST` | `/api/v1/projects/:projectId/task-types` | Create a task type such as story, bug, or chore. |
| `PATCH` | `/api/v1/projects/:projectId/task-types/:taskTypeId` | Update a task type's name, icon, color, or description. |
| `DELETE` | `/api/v1/projects/:projectId/task-types/:taskTypeId` | Delete a task type definition. |
| `GET` | `/api/v1/projects/:projectId/task-statuses` | List workflow statuses in board order. |
| `POST` | `/api/v1/projects/:projectId/task-statuses` | Create a workflow status. |
| `PATCH` | `/api/v1/projects/:projectId/task-statuses/:statusId` | Update status name, color, position, or category. |
| `DELETE` | `/api/v1/projects/:projectId/task-statuses/:statusId` | Delete a workflow status. |
| `GET` | `/api/v1/projects/:projectId/custom-fields` | List custom field definitions. |
| `POST` | `/api/v1/projects/:projectId/custom-fields` | Create a custom field definition. |
| `PATCH` | `/api/v1/projects/:projectId/custom-fields/:fieldId` | Update a custom field definition. |
| `DELETE` | `/api/v1/projects/:projectId/custom-fields/:fieldId` | Delete a custom field definition. |

## Tasks and Collaboration

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/tasks` | List tasks for a project with filters for status, assignee, sprint, and parent task. |
| `POST` | `/api/v1/projects/:projectId/tasks` | Create a new task in a project. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId` | Get task detail including workflow, assignee, reporter, and custom fields. |
| `PATCH` | `/api/v1/projects/:projectId/tasks/:taskId` | Update task title, description, status, sprint, assignee, or custom field values. |
| `DELETE` | `/api/v1/projects/:projectId/tasks/:taskId` | Delete or archive a task. |
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

## Sprints and Views

| Method | Path | Function |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/sprints` | List sprints for a project. |
| `POST` | `/api/v1/projects/:projectId/sprints` | Create a sprint. |
| `GET` | `/api/v1/projects/:projectId/sprints/:sprintId` | Get sprint details including goal, dates, and status. |
| `PATCH` | `/api/v1/projects/:projectId/sprints/:sprintId` | Update sprint metadata or lifecycle status. |
| `DELETE` | `/api/v1/projects/:projectId/sprints/:sprintId` | Delete or archive a sprint. |
| `GET` | `/api/v1/projects/:projectId/sprints/:sprintId/views` | List saved sprint views such as kanban, list, gantt, and burndown. |
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

1. Normalize the existing auth and user error contracts.
2. Add project and project-member endpoints.
3. Add task configuration endpoints: statuses, types, custom fields.
4. Add task CRUD and task activity endpoints.
5. Add sprint, document, dashboard, BDD scenario, and time-log endpoints.

## Known Model Gaps

The auth/user implementation is now aligned with the database schema:

- Users are identified by `username` (unique, required) and stored with `full_name`.
- Authentication uses `username` + password; there is no email field.
- UUIDs are used for all public resource identifiers.

The schema and HTTP contract are consistent. Before adding the next slice (projects/tasks), update [../architecture/database-schema.md](../architecture/database-schema.md) first so storage model and HTTP contract continue to move together.