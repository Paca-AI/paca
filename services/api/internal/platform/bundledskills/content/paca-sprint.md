---
name: paca-sprint
description: Plan a Paca sprint by selecting tasks from the backlog, detecting carryover from the previous sprint, inferring velocity from past sprints, and assigning tasks to the sprint. Use when starting a new sprint, filling an upcoming sprint, reviewing sprint capacity, or setting the sprint goal.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are planning a sprint in Paca. Use Paca MCP tools throughout — never create local files.

**If no sprint is specified**, default to planning the next upcoming sprint for the most active project.

---

## Step 1 — Load project context

1. Call `list_projects` and identify the relevant project.
2. Call `list_docs` and search for documents with keywords like "roadmap", "goals", "OKR", "retrospective", or "planning". Read the most relevant ones with `read_doc`. What the last sprint achieved and what the project's current goals are will shape which tasks belong in this sprint.
3. Call `list_sprints` to see existing sprints (active, upcoming, completed).
4. Call `list_tasks` (backlog or unassigned filter) to see the candidate task pool.

## Step 2 — Detect carryover and infer velocity

**Carryover:** Check the most recently completed sprint for tasks that were in it but are still open (status is not done). Flag these — carryover tasks typically get priority in the next sprint.

**Velocity:** Look at the last 2–3 completed sprints. Sum the story point estimates of tasks that were marked done. Use the average as the default capacity if the user hasn't stated one.

## Step 3 — Determine sprint parameters

From the user's message and context:
- **Which sprint** — by number, name, or "next sprint"
- **Capacity** — story points available; use inferred velocity as the default, ask to confirm
- **Goal** — what outcome the sprint should deliver; infer from the roadmap or ask
- **Duration** — start/end dates; infer from the cadence of past sprints or ask

## Step 4 — Select tasks

Rank backlog tasks and recommend a set that fits within capacity:
- **First**: carryover tasks from the previous sprint
- **Then**: tasks ranked by explicit priority → unblocking dependencies → business value → estimate size
- **Flag**: tasks missing estimates or acceptance criteria — offer to run `/paca-estimate` or `/paca-clarify` on them before committing

Present the proposed list with a total estimated points and the sprint goal. Ask the user to confirm, swap, or adjust before applying.

## Step 5 — Apply the plan

Once confirmed:
1. If the sprint does not exist, create it with `create_sprint` (name, goal, startDate, endDate)
2. Assign each task to the sprint with `update_task` (set `sprintId`)
3. Set or update the sprint goal with `update_sprint`

Optionally create or update a sprint planning note in Paca Docs (`write_doc`) with the sprint goal, task list, capacity, and velocity reference.

Report back: sprint name, dates, task count, total estimate, capacity used, and sprint goal.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Sprints:** `list_sprints` · `get_sprint` · `create_sprint` · `update_sprint` · `complete_sprint`  
**Tasks:** `list_tasks` · `get_task` · `update_task` · `bulk_move_tasks`  
**Documents:** `list_docs` · `read_doc` · `write_doc`  
**Projects:** `list_projects`
