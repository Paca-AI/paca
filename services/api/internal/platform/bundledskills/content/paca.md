---
name: paca
description: Interact with Paca project management using MCP tools. Use when tracking tasks, writing docs, planning sprints, managing work items, creating bugs or features, viewing the board, or handling any project-management request involving Paca. Routes to specialized skills for complex workflows like epics, sprint planning, and task execution.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You have Paca MCP tools. Handle the request by using those tools directly — never create local files for tasks, docs, or to-do lists.

---

## Step 0 — Route to a specialized skill if appropriate

If the request clearly matches one of these, let the user know — they'll get a better result with the specialized skill:

| If the user wants to... | Suggest |
|---|---|
| Turn requirements into a full epic with child stories | `/paca-epic <requirements>` |
| Clarify or improve a vague task or spec | `/paca-clarify #<number>` |
| Break a task into smaller sub-tasks | `/paca-breakdown #<number>` |
| Plan a sprint from the backlog | `/paca-sprint` |
| Estimate story points for tasks | `/paca-estimate #<number>` |
| Set priorities across the backlog | `/paca-prioritize` |
| Execute a task end-to-end | `/paca-do #<number>` |
| Test or verify a task | `/paca-test #<number>` |
| Write or update documentation | `/paca-doc #<number>` |
| Automate a process — auto-assignment, status chaining, reacting to events, handing work to an agent | `/paca-automation <goal>` |
| Review the pull request linked to a task (spec + standards) | `/paca-review #<number>` |
| Triage new or unsorted tasks — type, duplicates, ready / needs info / decline | `/paca-triage` |
| Run a sprint retrospective and turn improvements into tasks | `/paca-retro` |
| Turn page comments / UI annotations from an environment into tasks | `/paca-feedback` |
| Set up or clean up task types, statuses, custom fields and views | `/paca-board` |
| See who can do what, or add / change / remove project members | `/paca-team` |
| Draft a role policy — read-only role, limit someone to a sprint, restrict an agent or environment (drafts JSON only, never saves a role) | `/paca-role-policy <what the role should allow>` |
<!-- paca:plugin-routes -->

If it's a simple or mixed request (e.g. "create a task for X", "what's in the sprint", "mark #7 done"), just handle it directly below.

---

## Step 1 — Scan for a task reference

Scan the user's message for any of these patterns, wherever they appear:

| Pattern | Example | How to resolve |
|---|---|---|
| `#<number>` or number in task context | `#42`, `close #7`, `task 42 is done` | `get_task_by_number(projectId, 42)` |
| `PREFIX-<number>` | `ABC-42`, `PAC-7` | `list_projects` → match `task_id_prefix` → `get_task_by_number` |
| Paca URL | `http://…/projects/{id}/tasks/{id}` | parse both IDs → `get_task(projectId, taskId)` |
| UUID | `550e8400-e29b-41d4-a716-446655440000` | `get_task(projectId, uuid)` |

If a reference is found, fetch that task first, then apply the action the user is asking for.

## Step 2 — Infer the action

| What the user wants | Tools to use |
|---|---|
| Track work — bug, feature, to-do, ticket, chore | `create_task` / `update_task` / `list_tasks` |
| Write content — guide, spec, design, BDD, SDD, notes | `write_doc` |
| See status — board, sprint, what's in progress | `list_sprints` + `list_tasks` |
| Find something — "is there a task / doc about X?" | `search_tasks` / `search_docs` |
| Plan an iteration — sprint, milestone | `create_sprint` / `update_sprint` |
| Comment or annotate an existing task | `add_task_comment` |
| Close / complete work | `update_task` (set to done status) |
| Break work into pieces | `create_task` × N, each referencing the parent |
| Write or update documentation | `write_doc` |

## Step 3 — Get the project if needed

If no project is in context, call `list_projects` and pick the most relevant one based on the message. Only ask the user if it is genuinely ambiguous between two equally plausible candidates. List tools return one page at a time: if the reply ends with a `cursor` for the next page and the item you need is not on this one, call the same tool again with that cursor (or narrow the search) rather than guessing.

## Step 4 — Act and confirm

Execute the tool call(s), then report back: task/doc number, title, and any relevant ID or link.

---

## Examples

| User message | What you do |
|---|---|
| `fix login redirect bug` | `create_task` titled "Fix login redirect bug" |
| `write the API authentication design doc` | `write_doc` with a structured draft |
| `do this task ABC-123` | find project "ABC" → `get_task_by_number(123)` → start/act on task |
| `close #42` | `get_task_by_number(42)` → `update_task` status: done |
| `I finished PAC-99, mark it done` | `update_task` #99 → status: done |
| `http://…/tasks/uuid — add comment: blocked` | `get_task` from URL → `add_task_comment` "blocked" |
| `what's in the current sprint?` | `list_sprints` → `list_tasks` filtered to active sprint |
| `review PR, update staging, write release notes` | `create_task` × 3, one per item |

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

Do not create local files as a fallback.

---

## Tool reference

**Tasks:** `create_task` · `list_tasks` · `get_task` · `get_task_by_number` · `update_task` · `delete_task`  
**Documents:** `list_docs` · `read_doc` · `write_doc` · `delete_doc`  
**Sprints:** `create_sprint` · `list_sprints` · `get_sprint` · `update_sprint` · `complete_sprint`  
**Projects:** `list_projects` · `get_project` · `create_project` · `update_project`  
**Comments:** `add_task_comment` · `update_task_comment` · `list_task_activities`
