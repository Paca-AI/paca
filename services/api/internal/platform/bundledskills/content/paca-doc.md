---
name: paca-doc
description: Write or update documentation for a feature, task, or topic in Paca Docs. Use when asked to document a completed feature, write a guide or runbook, update existing docs, create a spec or architecture document, or produce BDD scenarios. Documentation is saved in Paca — never created as local files.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are writing or updating documentation in Paca Docs. Documentation lives in Paca — never create local files for docs.

**If no task or topic is specified**, call `list_tasks` for recently completed tasks that have no linked document, and ask the user which feature to document.

---

## Step 1 — Load project context

1. Resolve any reference from the user's message:
   - `#42`, `ABC-42` → `get_task_by_number` to read the feature being documented
   - Doc title or path → `list_docs` → `read_doc` to load the existing doc
   - Free-text topic → `list_docs` first to verify no duplicate already exists; if a similar doc is found, offer to update it instead of creating a new one
2. Call `list_docs` and read related docs — architecture, existing guides, BDD scenarios, API references. Matching the existing tone, structure, and terminology matters more than any individual stylistic choice.
3. If documenting a task, call `get_task` + `list_task_activities` to read the implementation details and any design decisions recorded in comments — these are often the most valuable content to capture.
4. If updating an existing doc, call `list_doc_activities` to check for open questions or prior discussion left in its comments.

## Step 2 — Identify doc type and draft outline

Based on context, identify the type:
- **Guide / tutorial** — step-by-step, outcome-oriented, written for someone doing it for the first time
- **Reference** — exhaustive API, config, or CLI reference
- **Architecture / design doc** — decisions, tradeoffs, diagrams (as Markdown)
- **BDD / acceptance spec** — Gherkin-style scenarios
- **Runbook** — operational steps for a known procedure

If the type is obvious from the task or request, proceed directly. Only ask the user to confirm the outline when the type is genuinely unclear or the scope is large.

## Step 3 — Write the documentation

Write complete, clear Markdown:
- Active voice and present tense
- Code examples and command snippets where they aid understanding
- Link to related Paca docs by title or task number
- No "last updated" timestamps, no "Created by Claude" lines — Paca tracks history
- No placeholder sections ("TBD", "coming soon") — write real content or omit the section

## Step 4 — Save to Paca

- **New document**: call `write_doc` with a full path (e.g. `'Architecture/API Design'` — the last segment is the title, preceding segments are folders) and the Markdown content. Missing folders are created automatically.
- **Existing document**: call `write_doc` with the same path. Integrate new content with the existing structure rather than appending everything at the end.
- If the doc is tied to a task, call `add_task_comment` on that task linking to the doc (e.g. "Documentation written: [link]") — that also reliably notifies whoever's watching the task. Otherwise, `add_doc_comment` works too, but `@mentions` there don't send a notification.

Report back: the document's title and the path it was saved at.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Documents:** `write_doc` · `read_doc` · `list_docs` · `move_doc`  
**Tasks:** `get_task` · `get_task_by_number` · `list_task_activities`  
**Comments:** `add_task_comment` · `add_doc_comment` · `list_doc_activities`  
**Projects:** `list_projects`
