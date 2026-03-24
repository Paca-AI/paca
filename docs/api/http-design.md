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

- Health check: `/healthz`
- Versioned product API: `/v1`

### Resource naming

- Use plural nouns for top-level collections: `/users`, `/projects`, `/tasks`.
- Use nested resources only when the child has clear ownership: `/projects/:projectId/members`.
- Use path parameters for identifiers and query parameters for filtering, sorting, and pagination.

### Authentication

- Protected endpoints require `Authorization: Bearer <access-token>`.
- Access and refresh token lifecycle is handled under `/v1/auth`.
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

- `/v1` success responses already use the envelope above.
- The `/healthz` endpoint intentionally returns a minimal `{ "status": "ok" }` body without this envelope.
- New `/v1` endpoints should use the standard envelope consistently, and existing endpoints should be normalized over time.

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
| `GET` | `/healthz` | No | Liveness/health probe for infrastructure, containers, and local development. |
| `POST` | `/v1/auth/login` | No | Validate user credentials and issue an access token plus refresh token. |
| `POST` | `/v1/auth/refresh` | No | Exchange a valid refresh token for a new access token. |
| `POST` | `/v1/auth/logout` | Access token | Revoke the current authenticated token/session. |
| `POST` | `/v1/users` | No | Register a new user account. |
| `GET` | `/v1/users/me` | Access token | Return the authenticated caller's own profile. |
| `PATCH` | `/v1/users/:id` | Access token | Update mutable profile fields for the specified user. Current implementation supports `name`. |
| `DELETE` | `/v1/users/:id` | Access token + `ADMIN` role | Delete a user account. |

## Current Request and Response Contracts

### `POST /v1/auth/login`

Function:

- authenticate a user with email and password;
- return a fresh access token and refresh token pair.

Request body:

```json
{
  "email": "user@example.com",
  "password": "secret123"
}
```

Success response:

```json
{
  "success": true,
  "data": {
    "access_token": "...",
    "refresh_token": "..."
  },
  "request_id": "..."
}
```

### `POST /v1/auth/refresh`

Function:

- accept a refresh token;
- issue a new access token without requiring credentials again.

Request body:

```json
{
  "refresh_token": "..."
}
```

### `POST /v1/auth/logout`

Function:

- revoke the authenticated token identified by the current JWT claims;
- terminate the current logical session.

Headers:

```text
Authorization: Bearer <access-token>
```

### `POST /v1/users`

Function:

- create a new user record;
- hash the password before persistence;
- assign the default `USER` role.

Request body:

```json
{
  "email": "new@example.com",
  "password": "secret123",
  "name": "Alice"
}
```

Success response data:

```json
{
  "id": "uuid",
  "email": "new@example.com",
  "name": "Alice",
  "role": "USER",
  "created_at": "2026-03-24T00:00:00Z"
}
```

### `GET /v1/users/me`

Function:

- read the profile of the authenticated user;
- resolve the user from the JWT subject claim.

### `PATCH /v1/users/:id`

Function:

- update mutable user profile fields;
- current implementation supports `name` only.

Request body:

```json
{
  "name": "Updated Name"
}
```

### `DELETE /v1/users/:id`

Function:

- remove or soft-delete a user account;
- restricted to callers with the `ADMIN` role.

## Planned Resource API

The following endpoints are not fully implemented yet, but they are the recommended path design for the next API slices based on the domain model.

## Identity and Administration

| Method | Path | Function |
|---|---|---|
| `GET` | `/v1/users` | List users for administration and member selection. |
| `GET` | `/v1/users/:id` | Get a user profile by ID. |
| `GET` | `/v1/admin/global-roles` | List available global roles and permissions. |
| `POST` | `/v1/admin/global-roles` | Create a new global role definition. |
| `PATCH` | `/v1/admin/global-roles/:roleId` | Update a global role definition. |
| `DELETE` | `/v1/admin/global-roles/:roleId` | Remove a global role definition. |
| `PUT` | `/v1/admin/users/:userId/global-roles` | Replace the set of global roles assigned to a user. |

## Projects and Membership

| Method | Path | Function |
|---|---|---|
| `GET` | `/v1/projects` | List projects visible to the caller. |
| `POST` | `/v1/projects` | Create a project with initial settings. |
| `GET` | `/v1/projects/:projectId` | Get project details, settings, and summary metadata. |
| `PATCH` | `/v1/projects/:projectId` | Update project name, description, or settings. |
| `DELETE` | `/v1/projects/:projectId` | Archive or delete a project. |
| `GET` | `/v1/projects/:projectId/members` | List project members and their project roles. |
| `POST` | `/v1/projects/:projectId/members` | Add a user to a project with a project role. |
| `PATCH` | `/v1/projects/:projectId/members/:memberId` | Change a member's project role or membership metadata. |
| `DELETE` | `/v1/projects/:projectId/members/:memberId` | Remove a member from a project. |
| `GET` | `/v1/projects/:projectId/roles` | List custom project roles for a project. |
| `POST` | `/v1/projects/:projectId/roles` | Create a project-specific role and permission set. |
| `PATCH` | `/v1/projects/:projectId/roles/:roleId` | Update a project role definition. |
| `DELETE` | `/v1/projects/:projectId/roles/:roleId` | Delete a project role definition. |

## Task Configuration

| Method | Path | Function |
|---|---|---|
| `GET` | `/v1/projects/:projectId/task-types` | List task type definitions for a project. |
| `POST` | `/v1/projects/:projectId/task-types` | Create a task type such as story, bug, or chore. |
| `PATCH` | `/v1/projects/:projectId/task-types/:taskTypeId` | Update a task type's name, icon, color, or description. |
| `DELETE` | `/v1/projects/:projectId/task-types/:taskTypeId` | Delete a task type definition. |
| `GET` | `/v1/projects/:projectId/task-statuses` | List workflow statuses in board order. |
| `POST` | `/v1/projects/:projectId/task-statuses` | Create a workflow status. |
| `PATCH` | `/v1/projects/:projectId/task-statuses/:statusId` | Update status name, color, position, or category. |
| `DELETE` | `/v1/projects/:projectId/task-statuses/:statusId` | Delete a workflow status. |
| `GET` | `/v1/projects/:projectId/custom-fields` | List custom field definitions. |
| `POST` | `/v1/projects/:projectId/custom-fields` | Create a custom field definition. |
| `PATCH` | `/v1/projects/:projectId/custom-fields/:fieldId` | Update a custom field definition. |
| `DELETE` | `/v1/projects/:projectId/custom-fields/:fieldId` | Delete a custom field definition. |

## Tasks and Collaboration

| Method | Path | Function |
|---|---|---|
| `GET` | `/v1/projects/:projectId/tasks` | List tasks for a project with filters for status, assignee, sprint, and parent task. |
| `POST` | `/v1/projects/:projectId/tasks` | Create a new task in a project. |
| `GET` | `/v1/projects/:projectId/tasks/:taskId` | Get task detail including workflow, assignee, reporter, and custom fields. |
| `PATCH` | `/v1/projects/:projectId/tasks/:taskId` | Update task title, description, status, sprint, assignee, or custom field values. |
| `DELETE` | `/v1/projects/:projectId/tasks/:taskId` | Delete or archive a task. |
| `GET` | `/v1/projects/:projectId/tasks/:taskId/children` | List child tasks under a parent task. |
| `POST` | `/v1/projects/:projectId/tasks/:taskId/children` | Create a child task under the specified parent task. |
| `GET` | `/v1/projects/:projectId/tasks/:taskId/activities` | List audit/activity entries for a task. |
| `POST` | `/v1/projects/:projectId/tasks/:taskId/activities` | Add a task activity entry such as comment, status change note, or system event. |
| `GET` | `/v1/projects/:projectId/tasks/:taskId/bdd-scenarios` | List BDD scenarios attached to a task. |
| `POST` | `/v1/projects/:projectId/tasks/:taskId/bdd-scenarios` | Add a BDD scenario to a task. |
| `PATCH` | `/v1/projects/:projectId/tasks/:taskId/bdd-scenarios/:scenarioId` | Update a BDD scenario. |
| `DELETE` | `/v1/projects/:projectId/tasks/:taskId/bdd-scenarios/:scenarioId` | Delete a BDD scenario. |
| `GET` | `/v1/projects/:projectId/tasks/:taskId/time-logs` | List time logs recorded against a task. |
| `POST` | `/v1/projects/:projectId/tasks/:taskId/time-logs` | Record time spent on a task. |
| `PATCH` | `/v1/projects/:projectId/tasks/:taskId/time-logs/:timeLogId` | Update a time log entry. |
| `DELETE` | `/v1/projects/:projectId/tasks/:taskId/time-logs/:timeLogId` | Delete a time log entry. |

## Sprints and Views

| Method | Path | Function |
|---|---|---|
| `GET` | `/v1/projects/:projectId/sprints` | List sprints for a project. |
| `POST` | `/v1/projects/:projectId/sprints` | Create a sprint. |
| `GET` | `/v1/projects/:projectId/sprints/:sprintId` | Get sprint details including goal, dates, and status. |
| `PATCH` | `/v1/projects/:projectId/sprints/:sprintId` | Update sprint metadata or lifecycle status. |
| `DELETE` | `/v1/projects/:projectId/sprints/:sprintId` | Delete or archive a sprint. |
| `GET` | `/v1/projects/:projectId/sprints/:sprintId/views` | List saved sprint views such as kanban, list, gantt, and burndown. |
| `POST` | `/v1/projects/:projectId/sprints/:sprintId/views` | Create a saved sprint view configuration. |
| `PATCH` | `/v1/projects/:projectId/sprints/:sprintId/views/:viewId` | Update a sprint view configuration. |
| `DELETE` | `/v1/projects/:projectId/sprints/:sprintId/views/:viewId` | Delete a sprint view configuration. |

## Knowledge and Reporting

| Method | Path | Function |
|---|---|---|
| `GET` | `/v1/projects/:projectId/documents` | List project documents. |
| `POST` | `/v1/projects/:projectId/documents` | Create a project document. |
| `GET` | `/v1/projects/:projectId/documents/:documentId` | Get document content and metadata. |
| `PATCH` | `/v1/projects/:projectId/documents/:documentId` | Update a document title or content. |
| `DELETE` | `/v1/projects/:projectId/documents/:documentId` | Delete a document. |
| `GET` | `/v1/projects/:projectId/dashboards` | List saved dashboards. |
| `POST` | `/v1/projects/:projectId/dashboards` | Create a dashboard layout. |
| `GET` | `/v1/projects/:projectId/dashboards/:dashboardId` | Get a dashboard definition. |
| `PATCH` | `/v1/projects/:projectId/dashboards/:dashboardId` | Update dashboard name or layout. |
| `DELETE` | `/v1/projects/:projectId/dashboards/:dashboardId` | Delete a dashboard. |

## Recommended Delivery Order

To keep the API coherent and aligned with the current codebase, implement the next slices in this order:

1. Normalize the existing auth and user error contracts.
2. Add project and project-member endpoints.
3. Add task configuration endpoints: statuses, types, custom fields.
4. Add task CRUD and task activity endpoints.
5. Add sprint, document, dashboard, BDD scenario, and time-log endpoints.

## Known Model Gaps

There is currently a mismatch between the documentation and the first implementation slice:

- [../architecture/database-schema.md](../architecture/database-schema.md) models integer IDs, `username`, and `full_name`.
- The implemented API and first SQL migration use UUID IDs plus `email`, `name`, and `role` fields.

Recommended rule:

- use the implemented auth/user contract as the near-term API baseline;
- update the database schema document before adding project/task endpoints so the storage model and HTTP contract move together.