---
name: galaxy-triage
description: Phân loại (triage) task mới hoặc chưa được phân loại trong Paca — gợi ý epic/parent phù hợp, mức importance, và tags; đề xuất trước, xác nhận rồi mới ghi. Use when new tasks pile up unclassified, after a bug-report burst, when asked to "dọn backlog", "phân loại task", or to assign epics/priorities/tags in bulk. Trả lời tiếng Việt mặc định.
triggers:
  - /galaxy-triage
---

You are triaging tasks in Paca. Use Paca MCP tools throughout — never create
local files. This skill is adapted from the Galaxy PM service's triage insight
(suggest epic + priority per task, with a reason), rebuilt on Paca's data
model: epic = `parentTaskId` to an Epic-type task, priority = numeric
`importance`, plus Paca `tags`.

**Language / Ngôn ngữ:** trả lời **tiếng Việt** mặc định; switch only when the
requester clearly writes in another language. Song ngữ: giữ nguyên cấu trúc
đề xuất/bảng bên dưới ở cả hai thứ tiếng.

---

## Step 1 — Collect the triage set and the vocabulary

1. Resolve the project (`list_projects` if not in context).
2. **Tasks to triage**: the ones the user referenced (`#42`, `ABC-42` →
   `get_task_by_number`), or — when asked to sweep — `list_tasks` and select
   tasks that are unclassified: no `parentTaskId`, importance 0/unset, no
   tags, typically recently created and in backlog-ish categories. Cap a
   sweep at ~20 tasks per round; say so if more remain.
3. **Epic candidates**: `list_task_types` to find the Epic type (`is_system`,
   name "Epic"), then `list_tasks` filtered to that type. Read enough of each
   epic (title + description via `get_task`) to know its scope.
4. **Tag vocabulary**: collect existing `tags` across `list_tasks` — reuse
   this vocabulary; do not invent near-duplicates ("auth" vs "authentication").
5. `list_docs` for a roadmap/goals doc if present — alignment beats guessing.

## Step 2 — Propose (do NOT write yet)

For each task, using its title + description (`get_task`):

- **Epic / parent**: the Epic-type task whose scope contains it. None fits →
  leave empty and (if a theme repeats) suggest creating an epic via
  `/paca-epic`. Never force a fit and never invent epics yourself. If the
  task is clearly a subtask of a non-epic task the user mentioned, `parentTaskId`
  may point there instead — say so.
- **Importance**: Paca stores a NUMBER; the UI buckets it. Propose the label
  and write the bucket midpoint:

  | Label | Bucket | Write value |
  |---|---|---|
  | None | 0 | 0 |
  | Low | 1–19 | 10 |
  | Medium | 20–49 | 35 |
  | High | 50–99 | 75 |
  | Critical | ≥100 | 150 |

  Judge by user impact, urgency/risk, and whether it blocks other work —
  production bugs and release blockers trend High/Critical; cleanups trend Low.
- **Tags**: ≤3, from the existing vocabulary (component, area, kind — e.g.
  `bug`, `mobile`, `auth`). Only mint a new tag when a repeated theme has no
  existing tag.

Present ONE table: `task · epic đề xuất · importance · tags · lý do (1 dòng)`
— then ask the user to confirm or adjust. Batch-confirm is fine ("apply all").

## Step 3 — Apply after confirmation

For each confirmed row call `update_task` with only the agreed fields:
`parentTaskId`, `importance` (the numeric bucket value), `tags` (the FULL
resulting array — the field is replaced, not merged; include any tags the
task already had that should survive).

Report back the same table with ✅ per applied row, plus anything skipped and
why. If some tasks stayed epic-less, list them under "cần epic mới?".

---

## Tool reference

**Tasks:** `list_tasks` · `get_task` · `get_task_by_number` · `update_task` · `list_task_types` · `list_task_statuses`
**Documents:** `list_docs` · `read_doc`
**Projects:** `list_projects`
**People:** `list_project_members`
