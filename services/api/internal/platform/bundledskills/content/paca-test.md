---
name: paca-test
description: Verify a completed Paca task against its acceptance criteria — deriving test cases, running or describing tests, and recording pass/fail results with a status update. Use when asked to test, verify, QA, or review a task that has been implemented. Defaults to tasks in "review" status when none is specified.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are testing or verifying a task — checking acceptance criteria, deriving test cases, running or describing tests, and recording results. Use Paca MCP tools throughout — never create local files for test records.

**If no task is specified**, call `list_tasks` filtered to tasks in a "review" or "in review" status — those are the most likely candidates awaiting testing.

---

## Step 1 — Load task and project context

1. Resolve the task reference from the user's message using `get_task_by_number` or `get_task`.
2. Call `list_docs` and search for documents titled or tagged with "BDD", "acceptance criteria", "test plan", "QA", or the feature name. Read the most relevant ones with `read_doc`. What "done" looks like needs to be established from the spec, not invented.
3. Call `list_task_activities` to read implementation notes, prior test runs, and edge cases already flagged.

## Step 2 — Derive test cases

From the task's acceptance criteria and related docs, derive test cases covering:
- **Happy path** — the primary flow works as specified
- **Edge cases** — boundary values, empty inputs, maximum sizes, off-by-one
- **Error cases** — invalid input, missing permissions, external failures, timeouts
- **Regression** — adjacent functionality that must not be broken

If the acceptance criteria are clear and cover these categories, proceed directly. Only ask the user to confirm the test plan when the scope is large or genuinely ambiguous.

## Step 3 — Execute tests

Choose the approach based on what's available:

- **Automated (code)**: run the test suite or exercise the feature with `Bash`; report exact output
- **Document / content**: verify structure, completeness, and accuracy against the spec
- **API / integration**: describe the request, expected response, and actual response
- **Manual QA required**: if the feature requires a browser or UI interaction you cannot perform, describe the manual test steps clearly in the comment so a human can execute them. Mark the task status as "awaiting QA" rather than done.

Record pass / fail for each test case with a brief note.

## Step 4 — Record results and update status

1. Call `add_task_comment` with a markdown table of results:
   ```
   | Test case | Result | Notes |
   |---|---|---|
   | Happy path: ... | ✅ Pass | |
   | Edge case: empty list | ❌ Fail | Returns 500, expected empty array |
   ```
2. **All pass**: call `update_task` to advance the status to the next stage (e.g. "review", "done")
3. **Any fail**: call `update_task` to set status back to "in progress"; include what needs fixing in the comment, with the exact reproduction (steps, input, expected vs. actual) so the fixer starts from a failing check rather than a description
4. If these test cases represent a repeatable procedure (e.g. release checklist, integration test steps), preserve them in a Paca document with `write_doc`

**What's next:** If the task passes and there's no existing documentation for this feature, consider running `/paca-doc #<number>` to write it.

Report back: task number, total tests run, pass/fail count, and the status after testing.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Tasks:** `get_task` · `get_task_by_number` · `update_task` · `list_task_statuses`  
**Comments:** `add_task_comment` · `list_task_activities`  
**Documents:** `list_docs` · `read_doc` · `write_doc`  
**Projects:** `list_projects`
