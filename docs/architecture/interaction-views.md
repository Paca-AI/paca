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

For Calendar views (`view_type = "calendar"`), config may also include:

```json
{
  "calendar_mode": "month",
  "calendar_date_field": "start_and_due"
}
```

- `calendar_mode` is the grid: `month` (the default when unset), `week` or `day`.
- `calendar_date_field` picks the dates that place a task: `start_and_due` (the default when unset), `start_date` or `due_date`.

As with the Gantt keys, an unknown value is dropped, and both are per user.

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

A Calendar view (below) sends the same filters, narrowed to the days on screen. A note view (below) skips steps 3 to 6: it loads no tasks, and lists its notes instead.

## Gantt view

The Gantt view (`view_type = "gantt"`) draws each task as a bar from its start date to its due date, both days included. A task with only one of the two dates is drawn as a one-day bar on that date. Rows keep the view's grouping and sorting, and nested subtasks follow the view's `subtask_mode`, just as in the Table view.

- **Task column.** Each row shows the task ID (`PREFIX-n`, or `#n` when the project has no prefix), the title and the assignees. Assignees follow the view's `fields` setting like the Table and Board (shown unless the list leaves out `assignee`), and users who can edit tasks can change them from the row.
- **Zoom.** A toolbar switches the scale between Day, Week, Month, Quarter and Year. The choice is stored per view as `gantt_zoom`. The **Today** button scrolls the chart back to the current date, and the task column stays pinned while the chart scrolls.
- **Dependency arrows.** `blocks` links are drawn as arrows from the end of the blocking task to the start of the blocked one. When the blocked task starts on or before the blocker's due date, the arrow turns red and the blocked bar is outlined, with the conflict named in its tooltip. The links come from `GET /api/v1/projects/:projectId/task-links?link_type=blocks`, which only lists links whose two tasks the caller can read. A link is drawn only when both tasks are on screen with dates. The **Dependency arrows** setting (`gantt_dependencies`) hides the arrows.
- **Drag to reschedule.** People who can edit tasks can drag a bar to move it, or drag either end to change the start or due date. Changes snap to whole days and show the new dates while dragging; Escape cancels. The save goes through the regular task update (`PATCH /tasks/:taskId`, which needs `tasks:write`), so it is recorded in the task's activity, reaches other clients in real time, and can be reverted from the **Undo** action in the confirmation toast. Moving a bar shifts only the dates the task has. Dragging an end of a single-date bar also fills in the missing date, so the task keeps the range that was drawn. Without edit permission the chart is read-only.
- Dragging a row by its task name still reorders it (under manual sort) or moves the task to another group, as before.

`roadmap` is the former name of this layout. Migration `000090` renamed existing views and kept their config. For one release the API still accepts `roadmap` in a create or update request and stores it as `gantt`.

## Calendar view

The Calendar view (`view_type = "calendar"`) lays tasks out on a grid of days, so a team sees what lands when: a busy week, an empty one, what is due today. It can be added in the Sprint, Backlog and Timeline contexts like any other view, and reads the same tasks with the same filters, search, quick assignee filter and personal or shared config.

- **Modes.** A toolbar switches between Month (whole weeks covering the month), Week and Day, with previous and next buttons and **Today**. The mode is stored per view as `calendar_mode`. Rows start on the first day of the week of the user's language (Sunday in English and Japanese, Monday in French or Russian), and month and weekday names come from it too.
- **Placement.** With `calendar_date_field` at `start_and_due` (the default) a task shows on every day from its start date to its due date, both included, drawn as one bar per week row; a task with only one of the two dates shows on that day, as in the Gantt view. With `start_date` or `due_date` a task shows on that one date. Task dates are stored as UTC midnight, so the grid works in whole UTC days and never drifts across time zones. A day with more tasks than its row has room for shows **+N more**, which opens a list of all of them. Clicking a task opens the task detail panel.
- **Loading.** The view asks the task list for the days on screen only, with `scheduled_from` and `scheduled_to` (and `scheduled_by` for a single date field), and loads every page of that window. Undated tasks come from the same list with `unscheduled=true`; the **No date** panel lists them a page at a time.
- **Drag to reschedule.** People who can edit tasks can drag a task to another day: every date it has moves by the same number of days, so it keeps its length whichever date places it. With `start_and_due`, dragging either end of a bar changes only that date. Dropping a task from the **No date** panel onto a day gives it that day's date (both dates with `start_and_due`, a one-day task). Saves go through the regular task update (`PATCH /tasks/:taskId`, `tasks:write`), show the new dates at once, roll back with an error message when they fail, and can be reverted from the **Undo** action in the confirmation toast. Without edit permission the calendar is read-only.
- **Creating.** People who can create tasks can click an empty part of a day, or its **+**, to add a task dated that day; the dates go with the create request.
- **Keyboard.** The grid follows the ARIA grid pattern: one day is in the tab order, the arrow keys move between days (off the edge of the grid turns the page), Home and End go to the ends of the week, and Enter opens the add form of the focused day. Task bars are buttons.
- **Settings.** Grouping, sorting, sums, paging and fields don't apply to a grid of days, so the view settings show only the **Dates** setting (`calendar_date_field`) and the filters.

## Note view

A note view (`view_type = "note"`) holds one free-form document instead of tasks: sprint goals, planning notes, meeting minutes, a release checklist. It can be added in the Sprint, Backlog and Timeline contexts like any other view. Its notes are ordinary documents (BlockNote content, snapshots, activity, comments, file uploads and live collaboration) that belong to the view instead of to a folder of the project's docs (migration `000094`, `documents.view_id`).

- **Where notes live.** A document lives either in the docs tree (`view_id` null) or in one note view (`view_id` set, no folder), never both. Notes do not appear in the Docs sidebar, in tree listings (`GET /docs` without `view_id`), in path lookups (`/docs/by-path`), in search (`/docs/search`, whose hits are located by path) or in the change feed that mirrors the tree. Moving a note into a folder is refused (`DOC_NOTE_IN_FOLDER`).
- **API.** The document endpoints serve notes too. `GET /api/v1/projects/:projectId/docs?view_id=:viewId` lists a view's notes in manual order, and `POST /api/v1/projects/:projectId/docs` with `view_id` in the body creates one (`DOC_NOTE_VIEW_INVALID` when the view is not a note view of the project). Everything else (read, update, rename, reorder by `position`, delete, snapshots, activity, comments) goes through the usual `/docs/:docId` routes.
- **Access.** Notes are documents, so the routes check `docs:read` and `docs:write` as for any document, and the IAM document scope applies. Listing a view's notes and creating one also needs `views:read` on the view itself, so a `Deny` on the view or a role limited to the views of one sprint (`view.sprint_id`) also covers its notes.
- **One document.** A view holds exactly one live document: a second `POST /docs` with the same `view_id` is refused with `409 DOC_NOTE_VIEW_TAKEN`, enforced by the partial unique index `uq_documents_note_view` (migration `000095`), so two clients creating it at once cannot both succeed. Deleting the document frees the view for a new one. Migration `000095` keeps the first live note of a view that already had several (by position, then creation) and soft-deletes the rest.
- **Deleting.** Deleting the view, or the sprint it belongs to, soft-deletes its notes, as deleting a document does. A note view keeps its type: an update cannot turn it into a task view, or a task view into a note view (`VIEW_NOTE_TYPE_CHANGE`), so notes are never left in a view that cannot show them.
- **Web app.** The view is its document: it opens straight in the same editor and comments and activity pane as the project's docs, with no title and no list of notes; its save status and comments and activity toggle sit in the view header, where other views keep their settings. The first time someone with `docs:write` opens an empty view, the document is created; with only `docs:read` an empty view says it has no document yet. The API still lists a view's notes, so a view that had several before migration `000095` shows the first in manual order (the migration soft-deletes the rest). The task search and view settings are hidden, since a note view has no tasks.

## Maintenance guidance

When adding future behavior:

- prefer extending `ViewConfig` instead of adding new route families
- prefer filter-based task queries instead of context-specific list endpoints
- keep the dedicated view task-positions endpoint as the source of truth for manual ordering metadata
- keep timeline behavior driven by task types rather than hard-coded endpoint rules
