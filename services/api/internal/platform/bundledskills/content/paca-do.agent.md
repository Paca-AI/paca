---
name: paca-do
description: Execute a Paca task end-to-end — reading context and acceptance criteria, doing the work (code, writing, research, review), updating task status, and commenting results. Use when asked to start, implement, complete, or work on a specific Paca task. Reads project docs first to understand the codebase and tech stack before acting.
triggers:
  - /paca-do
---

You are executing a task from Paca — reading it, understanding context, doing the work, and updating the record. Use Paca MCP tools throughout — never create local files for task records or documentation.

**If no task is specified**, call `list_tasks` filtered to in-progress tasks in the current sprint. Show them and ask which to work on.

---

## Step 1 — Load task and project context

1. Resolve the task reference from the user's message using `get_task_by_number` or `get_task`.
2. **If the task has no acceptance criteria**, stop and ask the user to clarify before starting — or offer to run `/paca-clarify` first. Starting work without a clear "done" condition wastes effort.
3. Call `list_docs` and search for documents relevant to this task — architecture, design specs, BDD scenarios, API references, integration guides. Read before writing any code or content; what's already decided shapes every implementation choice.
4. Call `list_task_activities` to read prior comments and implementation notes — someone may have already investigated this.
5. Note the acceptance criteria from the task description. These are your exit criteria.

## Step 2 — Mark in progress

1. Call `list_task_statuses` to find the "in progress" status.
2. Call `update_task` to set the status. (No confirmation needed — this is a lightweight, reversible status change.)
3. Call `add_task_comment`: "Starting work on this task."

## Step 3 — Do the work

Execute based on the task type:

- **Code task**: find the relevant source files, read existing tests to understand the expected behavior, then build it test-first in thin vertical slices: for each acceptance criterion write ONE failing test through the public interface (red), write the minimum code to pass it (green), then refactor with the suite green. Never write all tests up front, and test behavior, not implementation details. Run the full suite before finishing. If you need to understand what "in scope" looks like, the BDD scenarios you read in Step 1 are authoritative.
- **Bug task whose cause is not obvious**: do not guess at fixes. (1) Build the fastest deterministic pass/fail check that fails on THIS bug (failing test, script, replayed request, bisect) — if you cannot, say what is missing via `add_task_comment` and ask. (2) Reproduce and minimise it. (3) Post 3–5 ranked, falsifiable hypotheses as a task comment, then test them one variable at a time, tagging any temporary debug output with a unique prefix. (4) Fix the confirmed cause and add a regression test at a seam that reproduces the real pattern. (5) Remove all debug output and record the root cause in the completion comment.
- **Writing task**: draft the content in the response, or create/update a Paca document via `write_doc`. Never write to a local file.
- **Research / investigation task**: investigate, write findings as a comment via `add_task_comment` or as a Paca doc, then update the task description with the conclusions.
- **Review task**: analyse the artifact (PR, document, design), post a structured review as `add_task_comment`.

If you discover a blocker or a genuine sub-task that wasn't anticipated, create it in Paca with `create_task` (reference the parent in the description and add a `create_task_link` `blocks` link). Don't silently skip or work around it.

### Code tasks with a linked repository — push and open a PR (MANDATORY)

If this task involves code changes AND the project has a linked repository, completing the work means publishing it — changes left only in the sandbox are NOT done. Follow ALL steps:

1. **Prepare the checkout first** (before editing):
   - `list_repositories` → note the `repoId`.
   - `workspace_inspect` — see what is already checked out; reuse it rather than cloning again.
   - `workspace_prepare` (strategy "reuse" if a checkout exists, otherwise "clone" into an empty directory) on the default branch, then create a feature branch: `git checkout -b feat/<task-number>-<short-description>`.

2. **Make your changes** in the checkout, test-first for code (see above). Run the project's tests and linters and fix what you broke.

3. **Commit** with `git_commit` (stage specific `paths` rather than everything when unrelated files changed). Use a short conventional message: `<type>: <concise message>`. Changes to CI/CD configuration are refused by the tool — if the task needs one, say so in the task comment for a human.

4. **Push** the branch with `git_push` (never the default branch, never forced). Without a push the remote has no branch for the PR.

5. **Open the pull request** with `change_request_open`. A pushed branch without a PR is unfinished work. Write the body in this shape:
   - **Summary** — what changed and why, referencing the task number
   - **Evidence** — before/after behavior, test results, screenshots or output
   - **Risk** — what could break, what you did not verify, and a one-line merge-risk call (low / medium / high)
   - **Reviewer notes** — where to look first

6. **Report the PR URL** in the task comment (Step 4). Do not merge it yourself.

If any step fails (push rejected, token error, etc.), report it in a task `add_task_comment` rather than silently moving on. When you need a human decision you cannot make yourself (which approach, a risky step), use `ask_user_question` instead of guessing.

## Step 4 — Update and close

1. Call `add_task_comment` with a completion summary: what was done, what changed, any known caveats or follow-up needed.
2. If any project documentation was affected (README, architecture doc, API reference), update the relevant Paca document with `write_doc`. Never write new docs as local files.
3. Call `update_task` to set the status to done (or the next stage — e.g. "review" — if your workflow has one).

**What's next:** Consider running `/paca-test #<number>` to verify the implementation against acceptance criteria.

Report back: task number, title, summary of what was done, and any new tasks or docs created. If you pushed code and opened a PR, include the PR URL.

---

## Tool reference

**Tasks:** `get_task` · `get_task_by_number` · `update_task` · `create_task` · `list_task_statuses`
**Comments:** `add_task_comment` · `list_task_activities`
**Documents:** `list_docs` · `read_doc` · `write_doc`
**Projects:** `list_projects`
**Git (built-in tools):** `list_repositories` · `workspace_inspect` · `workspace_prepare` · `git_commit` · `git_push` · `change_request_open` · `repo_search_code`
**Links:** `create_task_link` · `list_task_links`
**Questions:** `ask_user_question`
