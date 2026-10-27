---
name: paca-retro
description: Run a sprint retrospective in Paca — gather what was planned vs. delivered, what slipped and why, how estimates held up, and how agents and humans worked together, then record the retro as a doc, turn improvements into tasks, and optionally complete the sprint. Use at the end of a sprint, when asked for a retro, a sprint review, or "what went well / what to change".
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are running the Check and Adapt steps of Paca's Plan-Act-Check-Adapt cycle for a sprint. Evidence first, opinions second. Use Paca MCP tools throughout — never create local files.

**If no sprint is specified**, `list_sprints` and take the active one (or the most recently ended); confirm with the user if it is ambiguous.

---

## Step 1 — Gather evidence

1. `get_sprint` and `list_tasks` for the sprint. Bucket every task: done, carried over (not done), added mid-sprint (created after the sprint started), dropped.
2. For carried-over and long-running tasks, `list_task_activities` — look for blockers, repeated status bounces (in review → in progress), long waits and unanswered questions.
3. Compare estimates to outcomes: points planned vs. delivered, and the largest misses (see `/paca-estimate`'s scale).
4. Note who did the work — humans and AI agents — and where an agent handed work back to a person or got stuck waiting for information.
5. `list_docs` / `read_doc`: the previous retro ("Retros/…") and its action items — were they done?

## Step 2 — Draft the retro

Present a draft before saving:

- **Outcome** — planned vs. delivered (tasks and points), sprint goal met or not
- **What went well** — with the evidence
- **What did not** — each with its cause as far as the record shows it, not a guess; mark guesses as hypotheses
- **Estimate accuracy** — biggest over/under-estimates and the pattern
- **Human–agent collaboration** — what agents did well, where they were blocked, which task shapes suit them
- **Last retro's action items** — done / not done

Then ask the team (via `ask_user_question` if available) which 1–3 changes to commit to. Fewer, specific changes beat a long list.

## Step 3 — Record and adapt

1. `write_doc` at `Retros/<sprint name>` with the final retro.
2. `create_task` for each committed change — an owner, a testable done condition, and a link back to the retro doc. Put them in the next sprint or the top of the backlog.
3. Process fixes that belong in tooling (a missing status, an unclear task template, a manual hand-off) → suggest `/paca-board` or `/paca-automation` as the follow-up.
4. If the sprint is over and still open, ask before `complete_sprint` — it moves unfinished work and is not casually reversible. Decide with the user where carried-over tasks go.

Report back: the retro doc path, tasks created, and whether the sprint was completed.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Sprints:** `list_sprints` · `get_sprint` · `update_sprint` · `complete_sprint`
**Tasks:** `list_tasks` · `get_task` · `create_task` · `list_task_activities`
**Documents:** `list_docs` · `read_doc` · `write_doc`
