---
name: paca-estimate
description: Estimate story points for Paca tasks using the Fibonacci scale, calibrated against recently completed reference tasks and project tech stack. Use when tasks are missing estimates, before sprint planning, when the team needs sizing for prioritization, or when asked to size a backlog.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are estimating effort for tasks in Paca. Use Paca MCP tools throughout — never create local files.

**If no task is specified**, call `list_tasks` and focus on tasks in the current sprint or backlog that have no estimate yet — those are the most urgent to address.

---

## Step 1 — Load project context

1. Resolve task reference(s) from the user's message:
   - `#42`, `ABC-42` → `get_task_by_number`
   - No reference → `list_tasks`; focus on unestimated tasks
2. Call `list_docs` and search for documents titled or tagged with "estimation", "velocity", "definition of ready", "tech stack", or "architecture". Read the most relevant ones with `read_doc`. Knowing the tech stack and team conventions is the difference between a calibrated and a random estimate.
3. Call `list_tasks` filtered to recently completed (done) tasks to find reference points — "Task X was 3 pts, this feels similar" is more reliable than estimating in a vacuum.

## Step 2 — Estimate each task

For each task, call `get_task` and reason through four dimensions:
- **Implementation complexity** — number of layers touched, algorithmic difficulty
- **Uncertainty** — how well-understood the work is; unknown → estimate higher
- **Test surface** — unit tests, integration tests, edge cases to cover
- **Integration points** — external APIs, databases, other services, third-party SDKs

**Scale (Fibonacci):** 1 · 2 · 3 · 5 · 8 · 13
- **1–2 pts**: simple, well-understood, < 1 day
- **3–5 pts**: moderate complexity or some unknowns, 1–3 days
- **8 pts**: complex or high uncertainty — consider whether `/paca-breakdown` would reduce risk
- **13 pts**: too large to estimate reliably — strongly recommend breaking down first

Calibrate against the reference tasks you found: if a reference task was 3 pts and this one feels about twice as hard, say 5–8 pts.

Show estimates with one-line justifications and ask the user to confirm or adjust.

## Step 3 — Write estimates back

For each confirmed estimate, call `update_task`:
- Prepend `**Estimate:** N pts` as the first line of the description (or update the custom field if the project has one — check `list_custom_fields` to see)
- Include a one-line rationale so the reasoning is preserved

Report back: table of task number, title, and estimate for each task updated.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Tasks:** `get_task` · `get_task_by_number` · `list_tasks` · `update_task` · `list_task_statuses` · `list_custom_fields`  
**Documents:** `list_docs` · `read_doc`  
**Projects:** `list_projects`
