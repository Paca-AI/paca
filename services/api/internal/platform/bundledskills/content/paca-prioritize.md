---
name: paca-prioritize
description: Set or adjust priorities across the Paca backlog, aligned to roadmap goals and business value. Use when the backlog needs sorting, before sprint planning, when tasks need explicit Critical/High/Medium/Low priority labels, or when asked to rank work by importance or urgency.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are setting priorities for tasks in Paca. Use Paca MCP tools throughout — never create local files.

**If no task is specified**, operate on the full backlog of the most active project.

---

## Step 1 — Load project context

1. Resolve task references from the user's message if given (`#42`, `ABC-42` → `get_task_by_number`). If no references, operate on the full backlog.
2. Call `list_docs` and search for documents titled "roadmap", "goals", "OKR", "strategy", or "release plan". Read the most relevant ones with `read_doc`. Priority without a goal to align to is just opinion — the docs anchor your reasoning.
   - **If no strategy docs exist**, ask the user: "What are the top 3 outcomes you're trying to achieve this quarter?" Use their answer as the alignment anchor.
3. Call `list_tasks` to load the full task set.
4. Call `list_sprints` to know which tasks are already committed vs. unscheduled.

## Step 2 — Score and rank

For each task, assess four dimensions:

| Dimension | What to look for |
|---|---|
| **Business value** | How directly it advances the roadmap or improves user outcomes |
| **Urgency / risk** | Customer-facing bugs, blockers, compliance deadlines |
| **Effort** | Estimated size — high-value + low-effort tasks rank up |
| **Dependencies** | Tasks that unblock others rank higher |

Assign one of these labels:
- **Critical** — blocks a release, breaks production, or is a contractual obligation
- **High** — directly advances the top roadmap goal; would be missed by users if delayed
- **Medium** — valuable but not time-sensitive; no clear near-term cost to waiting
- **Low** — nice-to-have, cleanup, or exploratory; can be deprioritized indefinitely without user impact

Present the ranked list with one-line reasoning per task. Ask the user to confirm or adjust before writing.

## Step 3 — Apply priorities

For each confirmed priority, call `update_task`:
- Set the `priority` field to the agreed value
- Optionally append a one-line rationale to the description so the reasoning is preserved and reviewable

Report back: full updated priority list as a table (task number · title · priority).

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Tasks:** `get_task` · `get_task_by_number` · `list_tasks` · `update_task`  
**Documents:** `list_docs` · `read_doc`  
**Sprints:** `list_sprints`  
**Projects:** `list_projects`
