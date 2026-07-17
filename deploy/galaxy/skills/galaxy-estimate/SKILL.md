---
name: galaxy-estimate
description: Ước lượng story points Fibonacci (1,2,3,5,8,13,21) cho task Paca kèm lý do một câu và độ tin cậy, hiệu chuẩn theo các task done tương tự của chính dự án; xác nhận rồi mới ghi. Use when a task needs sizing fast, before sprint planning, when asked "task này mấy điểm", or to fill missing estimates. Trả lời tiếng Việt mặc định.
triggers:
  - /galaxy-estimate
---

You are estimating story points in Paca. Use Paca MCP tools throughout — never
create local files. This skill is adapted from the Galaxy PM service's
estimate insight (Fibonacci suggestion + confidence + one-sentence reasoning,
calibrated on similar tasks), rebuilt on Paca's data model. It complements the
default `paca-estimate` skill: this one is the quick, structured estimator —
for a deep sizing workshop across a whole backlog, `paca-estimate` remains the
richer flow.

**Language / Ngôn ngữ:** trả lời **tiếng Việt** mặc định (giữ "story points",
"sprint" nguyên bản). Switch only when the requester clearly writes in another
language — keep the same output structure either way.

---

## Step 1 — Resolve the task(s)

- `#42`, `ABC-42` → `get_task_by_number`; URL/UUID → `get_task`.
- No reference → `list_tasks` and pick tasks with **no story points** in the
  current sprint first, then ready/refined backlog. Cap a sweep at ~10 per
  round.
- Read each task fully (`get_task`): title, description, acceptance criteria,
  tags, subtasks (`list_tasks` with parentTaskId when relevant).

## Step 2 — Calibrate against the project's own history

1. `list_tasks` filtered to done-category tasks that HAVE story points
   (`list_task_statuses` to know which statuses are category done).
2. Pick 3–5 reference tasks most similar in kind to the one being estimated
   ("Task X was 3 pts and this is about twice that" beats absolute judgment).
3. No usable references → say the estimate is uncalibrated and lower the
   confidence one notch.

## Step 3 — Estimate

Score four dimensions, then place on the scale:

- **Complexity** — layers touched, algorithmic difficulty
- **Uncertainty** — how well-understood; unknown → higher
- **Test surface** — unit/integration/edge cases required
- **Integration points** — external APIs, DBs, other services

**Scale (Fibonacci): 1 · 2 · 3 · 5 · 8 · 13 · 21**
- 1–2: simple, well-understood
- 3–5: moderate complexity or some unknowns
- 8: complex or high uncertainty — suggest considering `/paca-breakdown`
- 13/21: too large to estimate reliably — recommend splitting BEFORE
  committing; still record the number if the user insists

Per task produce exactly:

```
#42 「title」 → đề xuất: 5 điểm · độ tin cậy: vừa
lý do: <một câu — dựa trên độ phức tạp/độ mù mờ, so với task tham chiếu nào>
tham chiếu: #31 (3đ), #28 (5đ)   ← bỏ dòng này nếu không có
```

Confidence: **cao** (clear scope + good references) / **vừa** (some unknowns)
/ **thấp** (vague description or no calibration — say what's missing and, if
the gap is real, suggest `/paca-clarify`).

## Step 4 — Confirm, then write

Ask the user to confirm or adjust. For each confirmed estimate call
`update_task` with `storyPoints` (integer). If asked to keep the rationale,
add it via `add_task_comment`. Report back: task number · điểm đã ghi ·
confidence.

Never silently overwrite an existing estimate — if the task already has
points, show old → new and require explicit confirmation.

---

## Tool reference

**Tasks:** `get_task` · `get_task_by_number` · `list_tasks` · `update_task` · `list_task_statuses` · `list_custom_fields`
**Sprints:** `list_sprints`
**Projects:** `list_projects`
**Comments:** `add_task_comment`
