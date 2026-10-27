---
name: paca-feedback
description: Turn page comments (annotations pinned to elements of a running app via the Paca browser extension, with captured console errors and failed requests) into well-formed Paca tasks or into resolved items. Use when asked to review page comments or annotations, process UI feedback from an environment, or convert reviewer comments on a preview into tasks.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are processing page comments: feedback pinned to a specific element of a running environment, with the element, the reply thread and any captured console errors and failed network requests attached. Use Paca MCP tools throughout — never create local files.

**If the user names no environment or project**, `list_projects` and ask which to review; page comments are listed per project and can be filtered by environment and status.

---

## Step 1 — Collect

1. `list_annotations` with status `open` (filter by environment if the user named one).
2. For each, `get_annotation` to read the element, the full thread and the captured errors. Do not judge from the first line alone.
3. `search_tasks` for each — a task may already exist for it. Link instead of duplicating.

## Step 2 — Classify

| Kind | Signal | Action |
|---|---|---|
| Defect | console error, failed request, or "broken / wrong" | task of the bug type, with the captured evidence |
| UI change | "move / rename / restyle" | task of the feature/enhancement type |
| Question | "why does this…" | answer in a task comment; no task |
| Already done / duplicate | matches an existing task | link it (`create_task_link`) and note it |

Group annotations that describe the same underlying problem into one task.

## Step 3 — Propose, then create

Show a table: annotation → kind → proposed task title. Wait for confirmation when there are more than a few. Then `create_task` for each with: the element and page, what the commenter said (quote it), the console errors / failed requests (redact tokens and personal data), reproduction steps if inferable, and a testable acceptance criterion. For bugs whose cause is not obvious, suggest `/paca-do` (which diagnoses before fixing).

Report back: annotations reviewed, tasks created (number + title), annotations linked to existing tasks, and ones left open and why.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Annotations:** `list_annotations` · `get_annotation`
**Tasks:** `create_task` · `search_tasks` · `create_task_link` · `list_task_types` · `add_task_comment`
**Projects:** `list_projects`
