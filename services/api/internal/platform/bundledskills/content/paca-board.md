---
name: paca-board
description: Configure how a Paca project models its work — task types, statuses and their order, custom fields, and board/backlog/timeline views — by auditing what exists, proposing a minimal setup that fits the team's process, and applying it after confirmation. Use when starting a new project, when the board does not match how the team works, when asked to add a status, task type, custom field or view, or to clean up an over-grown setup.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are configuring a Paca project's work model. This is the one-time setup that the other skills lean on (task types for epics and bugs, statuses for review and waiting, custom fields for estimates). Use Paca MCP tools throughout — never create local files.

Changing statuses and types affects every task and every automation that references them. **Audit first, propose, get confirmation, then apply.** Deleting a type, status, field or view is destructive — never delete without an explicit yes naming the item.

---

## Step 1 — Audit

1. `get_project`, then `list_task_types`, `list_task_statuses`, `list_custom_fields`, `list_views`.
2. `list_tasks` (counts per status/type are enough) to see what is in use and what is dead.
3. `get_automation` (no id) to see which statuses automations depend on, so you do not break them.
4. Read any process doc (`search_docs` for "workflow", "process", "definition of done").

## Step 2 — Propose the smallest setup that works

Summarise current vs. proposed as a table. Principles:

- **Statuses mirror real hand-offs**, in board order: something like Backlog → To do → In progress → In review → Done, plus a Waiting/Blocked status only if the team uses one. One status per hand-off; do not add a status for every nuance (use a custom field or task link for that).
- **Task types** only where behavior or reporting differs: e.g. Epic, Story, Bug, Task. Exactly one default type (`set_default_task_type`) and one default status (`set_default_task_status`).
- **Custom fields** for data you will filter or report on (e.g. story points, component), not free-form notes. Pick the narrowest field type that fits.
- **Views**: a sprint board, a backlog and — if dates matter — a timeline. Order with `reorder_views`.
- The skills in this set look for an "in progress", a "review" and a "done" status and an "Epic" type; mention which of those the project lacks.

## Step 3 — Apply

After confirmation, apply with `create_task_status` / `update_task_status`, `create_task_type` / `update_task_type`, `create_custom_field` / `update_custom_field`, `create_view` / `update_view`. Prefer renaming (`update_*`) over delete-and-recreate, which orphans tasks.

If automations reference a status you changed, re-check with `get_automation` and fix them via `/paca-automation`.

Report back: what changed (created / renamed / reordered / deleted), and anything the project still lacks.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Types:** `list_task_types` · `create_task_type` · `update_task_type` · `delete_task_type` · `set_default_task_type`
**Statuses:** `list_task_statuses` · `create_task_status` · `update_task_status` · `delete_task_status` · `set_default_task_status`
**Custom fields:** `list_custom_fields` · `get_custom_field` · `create_custom_field` · `update_custom_field` · `delete_custom_field`
**Views:** `list_views` · `get_view` · `create_view` · `update_view` · `reorder_views` · `delete_view`
**Also:** `get_project` · `list_tasks` · `get_automation` · `search_docs`
