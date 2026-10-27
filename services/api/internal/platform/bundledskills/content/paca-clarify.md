---
name: paca-clarify
description: Clarify a vague or incomplete Paca task or specification by identifying ambiguities, asking targeted questions, and rewriting the description with explicit acceptance criteria. Use when a task is unclear, missing edge cases, lacks a testable done condition, or when someone asks to improve or flesh out a spec.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are clarifying a task or specification in Paca. Use Paca MCP tools throughout — never create local files.

**If no task is specified**, call `list_tasks` and surface tasks that have no acceptance criteria or have a very short description — those are the best candidates for clarification. Present them and ask which to work on.

---

## Step 1 — Load project context

1. Resolve the target from the user's message:
   - `#42` or `ABC-42` → `get_task_by_number`
   - Paca task URL → parse the task ID → `get_task`
   - Doc title / keyword / path → `list_docs` → `read_doc`
   - **If the task or document is not found**, tell the user clearly ("Task #99 was not found in project X") and ask them to verify the reference.
2. Call `list_docs` and read documents that provide context for this task — requirements, architecture, BDD scenarios, prior decisions. Reading broadly here means you won't ask questions the docs already answer.
3. If it's a task, call `list_task_activities` to read prior comments, decisions, and any clarifications already given.

## Step 2 — Identify ambiguities

Read the task description or document carefully and find:
- **Scope gaps** — what is in vs. out is not stated
- **Missing edge cases** — error states, empty states, permission boundaries, concurrency
- **Undefined terms** — domain words that could mean different things in this codebase
- **Unstated assumptions** — things the author assumed but did not write down
- **Acceptance criteria gaps** — no measurable, testable "done" condition

Only surface real ambiguities. Skip things that are clearly inferable from context or docs you just read.

## Step 3 — Ask clarifying questions

Present a numbered list of at most 6 questions, grouped by theme (scope / edge cases / definitions). Err on the side of fewer, better questions over many shallow ones. For each question, include your **recommended answer** and why, so the requester can reply "yes" instead of writing an essay.

Before asking, verify instead of trusting: if the task reports a bug, try to reproduce it; if it requests a feature, check the code and docs for whether it already exists, and look through `list_tasks` for duplicates or previously declined requests — link them rather than asking. If an earlier comment already answered a question, do not ask it again. Probe the boundaries of every stated rule with a concrete scenario (empty state, concurrency, permissions, failure halfway through, existing data).

**Post these where the requester will actually see them, not just in this conversation** — for a task, that's `add_task_comment` with an `@username` mention, which sends a real notification. For a document with no linked task, `add_doc_comment` exists but `@mentions` there do NOT notify anyone — the person only sees it if they reopen the doc; if the doc is linked to a task, ask there instead. Wait for the user's answers — typically a reply that mentions you back — before writing anything.

**Example format:**
```
**Scope**
1. Should this cover X, or is X a separate initiative?
2. Does this apply to guest users, or only authenticated users?

**Edge cases**
3. What should happen when Y is empty?

**Acceptance criteria**
4. What does "success" look like — a UI state, an API response, something else?
```

## Step 4 — Update the spec in Paca

Once the user answers:
- **Task**: call `update_task` with an improved description including explicit acceptance criteria. Don't just append — rewrite the description so it stands alone without this conversation.
- **Document**: call `write_doc` with the resolved content and any new decisions recorded as a "Decisions" section.

Do not create a new document — update the existing one.

**Record vocabulary and decisions where they will be found.** If answers settle a domain term (or expose a term used two ways), add it to the project glossary doc with `write_doc` (term, one-sentence definition, what it is not). If a decision is hard to reverse, surprising without context, and the result of a real trade-off, record it in a short decisions doc (context, decision, alternatives, consequences). Skip trivial decisions.

For a task that is now ready for an agent, write the description as a standalone brief: what and why from the user's point of view, current vs. desired behavior (bugs: exact reproduction steps), testable acceptance criteria as a checklist, and what is out of scope. Prefer naming modules and behaviors over file paths and pasted code, which go stale.

Report back: what was clarified and the task/doc number and title that was updated.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Tasks:** `get_task` · `get_task_by_number` · `update_task` · `list_tasks`  
**Comments:** `add_task_comment` · `list_task_activities` · `add_doc_comment` · `list_doc_activities`  
**Documents:** `read_doc` · `write_doc` · `list_docs`  
**Projects:** `list_projects`
