---
name: paca-epic
description: Turn a product requirement or feature description into a structured epic in Paca, with child user stories and a spec document. Use when asked to plan a new feature, break down a high-level requirement into stories, create an epic, or go from "we need X" to a fully structured backlog ready for sprint planning.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are turning requirements into a structured epic in Paca. Use Paca MCP tools throughout — never create local files.

**If no requirement is specified**, ask: "What requirement or feature do you want to turn into an epic? Describe it in a sentence or two."

---

## Step 1 — Load project context

1. Call `list_projects` to identify the relevant project (infer from the user's message, or ask if ambiguous).
2. Call `list_docs` and search for documents whose titles or descriptions suggest requirements, roadmap, architecture, or BDD scenarios. Read the most relevant ones with `read_doc`. Understand the domain and existing feature landscape before writing anything.
3. Call `list_task_types` to check whether an "Epic" type exists.
4. Call `list_task_statuses` to know available statuses.
5. Call `list_tasks` to scan for existing epics so you avoid duplicating scope.

## Step 2 — Parse the requirements

Extract from the user's message:
- **Goal** — what user or business outcome this achieves
- **Scope** — what is in / out of scope
- **Stakeholders** — who benefits, who owns
- **Constraints** — tech, time, dependencies

**If requirements are vague**, ask at most 3 targeted questions before proceeding. Focus on what you genuinely cannot infer. Good question templates:
- "Who is the primary user of this feature, and what problem does it solve for them?"
- "Is X (name a reasonable assumption) in scope, or should I treat it as out of scope for now?"
- "Are there existing systems or services this needs to integrate with?"

Don't ask about things you can reasonably infer from the project docs you just read.

## Step 3 — Create the epic task

Call `create_task`:
- **Type**: "Epic" if available from `list_task_types`.
- **Title**: concise, outcome-oriented (e.g. `User Authentication`). The type field already says this is an epic — don't also prefix the title with `Epic:`. Only fall back to a prefix like `Epic: User Authentication` if the project has no Epic type at all, since the title is then the only place that information can live.
- **Description** (Markdown):
  ```
  ## Goal
  <one paragraph>

  ## Scope
  **In:** ...
  **Out:** ...

  ## Acceptance Criteria
  - [ ] ...

  ## Open Questions
  - ...
  ```

## Step 4 — Break into stories

Derive child tasks from the requirements. Aim for 3–8 stories for a typical epic; go higher if the scope is large, but confirm with the user before creating more than 10. For each story:
- Call `create_task` with a `type` of "Story" (or the closest match in `list_task_types`) if one exists, a clear title with no `Story:` prefix — same reasoning as the epic's title, the type field already conveys it — a brief description, and 2–3 acceptance criteria.
- Reference the parent epic in the description: `Part of #<epic-number>`
- Prefer vertical slices (end-to-end thin features) over horizontal layers (all-backend, all-frontend)

## Step 5 — Create a spec document

Call `write_doc`:
- **Title**: `Epic: <name> — Specification`
- **Content**: Goal · Background · User Stories (linked by `#number`) · Acceptance Criteria · Out of Scope · Open Questions

**What's next:** After this, consider running `/paca-estimate #<epic-number>` to add story point estimates to the new tasks, and `/paca-sprint` to plan them into a sprint.

Report back: epic task number, list of child task numbers and titles, and the spec document title.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Tasks:** `create_task` · `update_task` · `list_tasks` · `get_task_by_number` · `list_task_types` · `list_task_statuses`  
**Documents:** `write_doc` · `list_docs` · `read_doc`  
**Projects:** `list_projects` · `get_project`
