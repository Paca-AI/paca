# Task Activity & Comments API

## Overview

Every task has an **activity log** — a unified, time-ordered stream of
system-generated change events and user-authored comments. Both are stored in
the project-wide `activities` table (rows with `entity_type = 'task'`) and
returned through the same list endpoint so the UI can render them in a single
chronological feed.

---

## Database Schema

```sql
CREATE TABLE activities (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    entity_type   TEXT        NOT NULL,          -- 'task' for this API
    entity_id     UUID,                          -- the task id
    actor_id      UUID        REFERENCES project_members(id) ON DELETE SET NULL,
    origin        TEXT        NOT NULL DEFAULT 'system',
    activity_type TEXT        NOT NULL,
    content       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at    TIMESTAMPTZ  -- soft-delete for comments
);
```

> **Note on `actor_id`:** The field references `project_members(id)`, not `users(id)`.
> When a task mutation is recorded, the API publishes the authenticated user's UUID to the
> activity stream along with the task's `project_id`. The `ActivityConsumer` worker resolves
> the user UUID to the corresponding `project_members.id` at consume-time before writing to
> the database. If the member has been removed from the project by the time the message is
> processed, `actor_id` is stored as `NULL`.

Indexes: `(task_id, created_at)` for efficient chronological listing.

---

## Activity Types

| `activity_type`                | Trigger                                   |
|-------------------------------|-------------------------------------------|
| `task.created`                | Task created                              |
| `task.updated`                | Task fields changed                       |
| `task.deleted`                | Task soft-deleted                         |
| `task.attachment.added`       | Attachment linked                         |
| `task.attachment.removed`     | Attachment unlinked                       |
| `task.link.added`             | Task link created                         |
| `task.link.removed`           | Task link deleted                         |
| `comment`                     | User comment (user-created)               |

> **Plugin activities**: Installed plugins may emit additional activity types
> (e.g. `task.bdd_scenario.created`, `task.checklist.created`). Plugin-emitted
> activities should include a `_description` string in their `content`
> payload for human-readable display.

---

## Content Shapes (JSONB)

### `task.created`
```json
{ "title": "Implement login", "task_number": 42 }
```

### `task.updated`
```json
{
  "changes": [
    { "field": "title",     "old": "Old title", "new": "New title" },
    { "field": "status_id", "old": "uuid-a",    "new": "uuid-b" }
  ]
}
```

Tracked fields: `title`, `status_id`, `assignee_id`, `reporter_id`,
`task_type_id`, `sprint_id`, `parent_task_id`, `importance`,
`start_date`, `due_date`.

### `task.deleted`
```json
{ "title": "Old task title", "task_number": 42 }
```

### `task.attachment.added` / `task.attachment.removed`
```json
{ "file_name": "screenshot.png", "file_size": 102400 }
```

### `task.link.added`
```json
{ "target_task_id": "uuid", "link_type": "blocks" }
```

### `task.link.removed`
```json
{ "link_id": "uuid" }
```

### `comment`
```json
{ "text": "Looks good. Ready to review." }
```

---

## API Endpoints

All endpoints are under `/api/v1/projects/:projectId/tasks/:taskId`.
Authentication is required. Project `tasks.read` permission is required for
reading; `tasks.write` is required for posting/editing/deleting comments.

### List Activities

```
GET /api/v1/projects/:projectId/tasks/:taskId/activities
```

Returns all activities and comments for a task, sorted oldest → newest.
Soft-deleted comments are excluded.

**Response:**
```json
{
  "success": true,
  "data": {
    "items": [
      {
        "id": "uuid",
        "task_id": "uuid",
        "actor_id": "uuid",
        "actor_name": "Jane Doe",
        "actor_username": "jane",
        "activity_type": "task.created",
        "content": { "title": "Login screen" },
        "created_at": "2026-04-01T10:00:00Z",
        "updated_at": "2026-04-01T10:00:00Z"
      }
    ]
  }
}
```

### Post a Comment

```
POST /api/v1/projects/:projectId/tasks/:taskId/activities/comments
```

**Request body:**
```json
{ "text": "This looks good to me." }
```

**Response:** `201 Created` — the created activity entry.

### Edit a Comment

```
PATCH /api/v1/projects/:projectId/tasks/:taskId/activities/comments/:commentId
```

Only the original author may edit their comment.

**Request body:**
```json
{ "text": "Updated comment text." }
```

**Response:** `200 OK` — the updated activity entry.

### Delete a Comment

```
DELETE /api/v1/projects/:projectId/tasks/:taskId/activities/comments/:commentId
```

Only the original author may delete their comment. Performs a soft-delete
(sets `deleted_at`); the entry is excluded from normal list responses.

**Response:** `204 No Content`

---

## Valkey Event Stream

System-generated activities (task created, updated, BDD changes, etc.) are
published through `events.Fanout` to the Valkey stream `paca.activities` —
the single stream every activity in a project goes to, whatever the entity.
The `ActivityConsumer` worker reads that stream and writes each entry to the
`activities` table. Fanout also publishes each event to the `paca.events`
Pub/Sub channel for immediate fan-out via `services/realtime`.

> **Note:** Comment operations (add/update/delete) write directly to the
> database, because the request returns the row. They are still appended to
> the stream; the consumer's insert conflicts on the comment's `id` and is
> dropped.

### Stream Entry Format

```
Stream: paca.activities
Fields:
  type           = "task.created" | "task.updated" | ...
  payload        = <JSON-encoded activity payload>
  project_id     = <uuid>
  entity_type    = "task"
  entity_id      = <task uuid>
  origin         = "user" | "agent" | "automation" | "jev" | "system" | ...
  actor_id       = <user uuid>   (optional)
  actor_agent_id = <agent uuid>  (optional)
```

### Stream Payload

The `payload` field contains a JSON-encoded object with the following shape:

```json
{
  "id":            "uuid",
  "task_id":       "uuid",
  "project_id":    "uuid",
  "actor_id":      "uuid",
  "activity_type": "task.updated",
  "content":       "{\"changes\":[...]}",
  "created_at":    "2026-04-01T10:00:00Z",
  "updated_at":    "2026-04-01T10:00:00Z"
}
```

`content` is a JSON-encoded string containing the activity-specific data (see
[Content Shapes](#content-shapes-jsonb) above).  `actor_id` is the
authenticated user's UUID; the consumer resolves it to `project_members.id`
before persisting to the database.

---

## Stream Consumer

A background worker (`internal/worker/activity_consumer.go`) reads from
`paca.activities` using `XREADGROUP`. It:

1. Resolves the stream `actor_id` (user UUID) to `project_members.id`
2. Writes the activity entry to `activities` in PostgreSQL
3. Acknowledges the message

Consumer group name: `api.activity_writer`  
Consumer name: `api.activity_writer.{hostname}`

---

## Actor Resolution

The authenticated user's `user_id` (from JWT `sub` claim) is embedded into the
Go `context.Context` by the `Authn` middleware via `middleware.WithActorID`.
Service methods read it with `middleware.ActorIDFromContext(ctx)`.

---

## Permission Model

| Action              | Required permission      |
|---------------------|--------------------------|
| List activities     | `tasks.read` (project)   |
| Post comment        | `tasks.write` (project)  |
| Edit own comment    | `tasks.write` (project)  |
| Delete own comment  | `tasks.write` (project)  |
