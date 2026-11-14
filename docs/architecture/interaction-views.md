# Interaction Views Model

This document explains how sprint, product-backlog, and timeline views work after the consolidation to a single view system.

## Why this exists

Paca used to treat sprint, backlog, and timeline views as separate API families. That made the code harder to evolve and forced the web app to carry route-specific logic in many places.

The current design keeps the UI concepts distinct, but models them through one shared view resource and one shared task-list resource.

## Core model

A saved view always belongs to exactly one project and one of these contexts:

- `sprint`
- `backlog`
- `timeline`

Sprint views also carry a `sprint_id`. Backlog and timeline views are project-level views with no sprint assignment.

## Unified view API

Use one resource for all view CRUD:

```text
GET    /api/v1/projects/:projectId/views?context=sprint&sprint_id=:sprintId
GET    /api/v1/projects/:projectId/views?context=backlog
GET    /api/v1/projects/:projectId/views?context=timeline
POST   /api/v1/projects/:projectId/views?...same query pattern...
PATCH  /api/v1/projects/:projectId/views/:viewId
DELETE /api/v1/projects/:projectId/views/:viewId
PUT    /api/v1/projects/:projectId/views/positions?...same query pattern...
```

Manual task ordering is also shared:

```text
GET /api/v1/projects/:projectId/views/:viewId/task-positions
PUT /api/v1/projects/:projectId/views/:viewId/task-positions/:taskId
PUT /api/v1/projects/:projectId/views/:viewId/task-positions
```

## Unified task-list API

All interaction pages should fetch tasks through the same endpoint:

```text
GET /api/v1/projects/:projectId/tasks
```

Supported query parameters:

- `sprint_id=<uuid>` for one sprint
- `sprint_ids=<uuid,uuid>` for multi-sprint saved views
- `sprint_id=null` when a caller explicitly wants only unscheduled backlog items
- `status_id=<uuid>` or `status_ids=<uuid,uuid>`
- `assignee_id=<uuid>` or `assignee_ids=<uuid,uuid>`
- `task_type_ids=<uuid,uuid>`
- `parent_task_id=<uuid>`

### Context conventions

- **Sprint page**: seeded with the current sprint in `config.filters.sprint_ids`
- **Backlog page**: seeded with all sprints and groups by `column_by = sprint`
- **Timeline page**: seeded with Epic-only `task_type_ids`

Manual ordering metadata is fetched separately from the shared view task-positions endpoint.

## Saved view config

Each view stores both presentation settings and reusable filters.

```json
{
  "fields": ["title", "status", "assignee"],
  "column_by": "status",
  "swimlanes": "assignee",
  "sort_by": "manual",
  "field_sum": "count",
  "slice_by": "none",
  "filters": {
    "sprint_ids": ["..."],
    "status_ids": ["..."],
    "assignee_ids": ["..."],
    "task_type_ids": ["..."]
  }
}
```

For plugin views (`view_type = "plugin"`), config also includes:

```json
{
  "plugin_manifest_id": "com.paca.checklist",
  "plugin_component": "KanbanByPriority"
}
```

`plugin_manifest_id` is the plugin manifest reverse-DNS identifier.

For Gantt views (`view_type = "gantt"`), config may also include:

```json
{
  "gantt_zoom": "week",
  "gantt_dependencies": "shown"
}
```

- `gantt_zoom` is the time scale: `day`, `week` (the default when unset), `month`, `quarter` or `year`.
- `gantt_dependencies` is `shown` (the default when unset) or `hidden`, and controls the dependency arrows.

An unknown value for either key is dropped rather than stored. Like every other config key, both are per user: picking a zoom saves it to the user's personal override of the view.

### Rule of thumb

- **Presentation** belongs in top-level config keys such as `column_by`, `sort_by`, and `slice_by`
- **Query constraints** belong in `config.filters`

That keeps the web app predictable: open a view, read its config, send the same saved filters to the single task-list API.

## Default seeded views

Typical defaults are:

- **Sprint**: Board and Table, both seeded with `column_by = status`, the current sprint selected, and non-system task types
- **Backlog**: Table, seeded with `column_by = sprint`, all sprints, and non-system task types
- **Timeline**: Gantt, seeded with Epic-only task types across all sprints

Additional views can still be created by users per context.

## Web-app flow

For every interaction page:

1. load the views for the current context
2. pick the active view
3. read `activeView.config`
4. call the single task-list API with any saved `config.filters`
   - sprint scope now comes from `filters.sprint_ids` when present
   - page defaults are seeded by the API when projects and sprints are created
5. if the view uses manual sorting, load its task positions from the shared task-positions endpoint
6. render Board, Table, or Gantt using the same task payload

## Gantt view

The Gantt view (`view_type = "gantt"`) draws each task as a bar from its start date to its due date, both days included. A task with only one of the two dates is drawn as a one-day bar on that date. Rows keep the view's grouping and sorting, and nested subtasks follow the view's `subtask_mode`, just as in the Table view.

- **Zoom.** A toolbar switches the scale between Day, Week, Month, Quarter and Year. The choice is stored per view as `gantt_zoom`. The **Today** button scrolls the chart back to the current date, and the task column stays pinned while the chart scrolls.
- **Dependency arrows.** `blocks` links are drawn as arrows from the end of the blocking task to the start of the blocked one. When the blocked task starts on or before the blocker's due date, the arrow turns red and the blocked bar is outlined, with the conflict named in its tooltip. The links come from `GET /api/v1/projects/:projectId/task-links?link_type=blocks`, which only lists links whose two tasks the caller can read. A link is drawn only when both tasks are on screen with dates. The **Dependency arrows** setting (`gantt_dependencies`) hides the arrows.
- **Drag to reschedule.** People who can edit tasks can drag a bar to move it, or drag either end to change the start or due date. Changes snap to whole days and show the new dates while dragging; Escape cancels. The save goes through the regular task update (`PATCH /tasks/:taskId`, which needs `tasks:write`), so it is recorded in the task's activity, reaches other clients in real time, and can be reverted from the **Undo** action in the confirmation toast. Moving a bar shifts only the dates the task has. Dragging an end of a single-date bar also fills in the missing date, so the task keeps the range that was drawn. Without edit permission the chart is read-only.
- Dragging a row by its task name still reorders it (under manual sort) or moves the task to another group, as before.

`roadmap` is the former name of this layout. Migration `000090` renamed existing views and kept their config. For one release the API still accepts `roadmap` in a create or update request and stores it as `gantt`.

## Maintenance guidance

When adding future behavior:

- prefer extending `ViewConfig` instead of adding new route families
- prefer filter-based task queries instead of context-specific list endpoints
- keep the dedicated view task-positions endpoint as the source of truth for manual ordering metadata
- keep timeline behavior driven by task types rather than hard-coded endpoint rules
