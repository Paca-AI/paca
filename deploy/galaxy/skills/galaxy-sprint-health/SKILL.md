---
name: galaxy-sprint-health
description: Đánh giá sức khoẻ sprint (xanh/vàng/đỏ) từ dữ liệu task và sprint hiện tại trong Paca — tiến độ theo điểm và theo thời gian, ai đang quá tải, một khuyến nghị hành động. Use when asked "sprint đang thế nào", "sprint health", "có kịp sprint không", for a stand-up summary, or before a sprint review. Trả lời tiếng Việt mặc định.
triggers:
  - /galaxy-sprint-health
---

You are assessing the health of a Paca sprint. Use Paca MCP tools throughout —
never create local files. This skill is adapted from the Galaxy PM service's
sprint-health insight (3-sentence summary + traffic light), rebuilt on Paca's
data model.

**Language / Ngôn ngữ:** trả lời **tiếng Việt** mặc định (thuật ngữ scrum như
sprint, story points, backlog giữ nguyên tiếng Anh). Only switch to another
language when the requester's message is clearly written in it. Song ngữ: nếu
người hỏi dùng tiếng Anh thì trả lời tiếng Anh, giữ nguyên cấu trúc bên dưới.

---

## Step 1 — Resolve the sprint

1. Resolve the project from context or `list_projects`.
2. Call `list_sprints`:
   - If the user names a sprint, use it.
   - Otherwise use the **active** sprint. If several are active, pick the one
     ending soonest and say so in the reply.
   - No active sprint → say there is nothing to assess and stop (offer
     `/paca-sprint` to plan one).

## Step 2 — Load the data (never guess it)

1. `list_tasks` filtered to the sprint (sprintId) — you need every task, not
   just the first page.
2. `list_task_statuses` — map each task's `statusId` to its status **category**
   (`backlog refinement ready todo inprogress done`). "Done" ALWAYS means
   category `done`, not a status name.
3. `list_project_members` — turn `assigneeIds` into real names.
4. `get_sprint` for `startDate` / `endDate` / goal.

## Step 3 — Compute the vital signs

- **Points progress**: sum of story points on done-category tasks vs. total.
  If no task has points, fall back to task counts and SAY you did.
- **Time progress**: fraction of the timebox elapsed (now vs. start/end
  dates). No dates → skip pace comparison and flag it as a finding.
- **Pace gap** = points progress − time progress (in percentage points).
- **Workload per assignee**: open (not-done) tasks and points per member;
  flag anyone holding a clearly disproportionate share (e.g. > ~40% of open
  points) or many `inprogress` tasks at once. Flag unassigned open tasks.
- **Stuck signals**: tasks with tags/comments suggesting blockage ("blocked",
  "waiting", "chờ", "vướng" — check `list_task_activities` on suspicious
  tasks), unestimated tasks, and tasks still in `backlog`/`refinement`
  category mid-sprint.

## Step 4 — Verdict (traffic light)

| Verdict | Criteria (any listed) |
|---|---|
| 🟢 **xanh** | pace gap ≥ −10 điểm %; no overloaded member; no blocked cluster |
| 🟡 **vàng** | pace gap between −10 and −25 điểm %; OR one overloaded member; OR a meaningful share of tasks unestimated/unassigned |
| 🔴 **đỏ** | pace gap < −25 điểm %; OR sprint end passed with open tasks; OR multiple blocked tasks / one member owns most of the remaining work |

Judgment beats arithmetic on the boundary — but never report 🟢 when the data
was too thin to compute a pace (missing dates AND missing points): that is 🟡
with "thiếu dữ liệu" as the reason.

## Step 5 — Reply format

Exactly this shape (concise — it is a stand-up summary, not a report):

1. **Ba câu**: (1) tiến độ tổng thể — điểm đã xong/tổng, so với thời gian đã
   trôi; (2) ai/điều gì đang rủi ro — người quá tải, task kẹt, task chưa ước
   lượng; (3) **một** khuyến nghị hành động cụ thể (ví dụ: chuyển bớt 5 điểm
   từ A sang B, tách task #42, bỏ bớt scope X ra backlog).
2. Dòng chốt: `Sức khoẻ sprint: 🟢 xanh` (hoặc 🟡 vàng / 🔴 đỏ) — kèm 3-5 con
   số đắt nhất trong ngoặc (vd: `(8/18 pts · 53% thời gian · 2 task kẹt)`).

If asked to log it, add the summary as a comment via `add_task_comment` on the
most relevant task — otherwise just reply in chat.

---

## Tool reference

**Sprints:** `list_sprints` · `get_sprint`
**Tasks:** `list_tasks` · `get_task` · `list_task_statuses` · `list_task_activities`
**People:** `list_project_members`
**Projects:** `list_projects`
**Comments:** `add_task_comment`
