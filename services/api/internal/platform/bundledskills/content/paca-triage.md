---
name: paca-triage
description: Triage new or unsorted Paca tasks — categorise each by task type, check for duplicates and whether the work already exists, reproduce reported bugs, then move it to a clear next state (ready for an agent, needs info, ready for a human, or declined) with a written reason. Use when the backlog has untriaged items, when asked to triage or sort incoming requests, or to decide what an agent can pick up.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are triaging tasks in Paca: each gets a task type, a next state and a written reason. Use Paca MCP tools throughout — never create local files.

Start every comment you post with `> Triage note (AI-generated)` so it is distinguishable from a human decision. Humans can override any call you make.

**If no task is specified**, show the **attention view**: `list_tasks` in the project's first/default status plus anything without a task type, oldest first, and any task you marked needs-info that has had new comments since (`list_task_activities`). Ask which to triage — do not triage everything unprompted.

---

## Step 1 — Learn this project's vocabulary

`list_task_types` and `list_task_statuses`. Triage uses the project's **own** statuses; map the four outcomes below onto them (e.g. "To do" = ready, a "Blocked"/"Waiting" status = needs info). If no suitable status exists, do not invent one silently: use a comment and ask whether to add one (`/paca-board`).

## Step 2 — Per task

1. `get_task_by_number` and `list_task_activities`. Resume from earlier triage notes; never re-ask a question that was already answered.
2. **Duplicates and prior work:** `search_tasks` and `search_docs` for the same request, a declined one, or an existing feature. If found, link it (`create_task_link` with `duplicates` or `relates_to`) instead of working it twice.
3. **Verify, don't trust.** Bug: try to reproduce it. Enhancement: check the docs and code for whether it exists.
4. Pick exactly one **outcome**:

| Outcome | Use when | Do |
|---|---|---|
| Ready for agent | clear, verified, scoped, testable | rewrite the description as an agent brief (below); set the ready status |
| Ready for human | needs a judgment, access or decision only a person has | note the open decision; assign / @mention the right member (`list_project_members`) |
| Needs info | blocked on the reporter | numbered, specific questions with your recommended answer; @mention the reporter; set the waiting status |
| Decline | out of scope, duplicate, already done | reason + link; close using the project's convention |

5. Set the task type with `update_task` and post the triage note with `add_task_comment`.

### Agent brief

Rewrite the description so it stands alone: what and why from the user's point of view; current vs. desired behavior (bugs: exact reproduction steps); acceptance criteria as a testable checklist; what is out of scope. Name modules and behaviors, not file paths or pasted code, which go stale.

### Triage note

**Established:** what is confirmed. **Still needed:** specific, answerable questions. **Decision:** the outcome and why.

## Quick override

If someone says "mark #42 ready" or "decline #9 as a duplicate of #3", confirm the change in one line and do it — no investigation.

Report back, per task: number, task type, outcome, and the new status.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Tasks:** `get_task` · `get_task_by_number` · `list_tasks` · `search_tasks` · `update_task` · `list_task_types` · `list_task_statuses` · `create_task_link`
**Comments:** `add_task_comment` · `list_task_activities`
**Documents:** `search_docs` · `read_doc`
**People:** `list_project_members`
