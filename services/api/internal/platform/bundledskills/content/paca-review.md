---
name: paca-review
description: Review the pull/merge request linked to a Paca task on two axes — does it satisfy the task's acceptance criteria (spec) and does it follow the project's conventions (standards) — by reading the diff, review threads and CI, then submitting a review with a verdict. Use when asked to review a PR, check a change before merge, or when a task is in review and has an open pull request.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are reviewing a change request (pull/merge request) against a Paca task. Review on **two axes** and keep them separate: **Spec** (does it do what the task asks?) and **Standards** (is it written the way this project writes code?). Use Paca MCP tools for task records and the Git tools for the change — never create local files.

**If no task or PR is specified**, call `list_tasks` for tasks in a review status and ask which to review.

---

## Step 1 — Load the spec and the standards

1. `get_task_by_number` / `get_task` and `list_task_activities`: the acceptance criteria are your Spec axis. **If there are none, say so and stop** — offer `/paca-clarify`; reviewing against nothing produces noise.
2. `list_docs` / `search_docs` → `read_doc` for architecture notes, conventions and BDD scenarios: your Standards axis.
3. Find the change request from the task's linked PRs or the user's message (the repository comes from `list_repositories`).

## Step 2 — Read the change

1. `change_request_get_context` — files, review threads and CI in one call.
2. `change_request_get_diff` — the whole diff, or per file for a large change.
3. `checks_get`; for any failing check `checks_get_logs` for the failing tail. Re-run a flaky check with `checks_rerun` at most once, and say you did.
4. `review_list_threads` — do not repeat comments already made; note which are unresolved.
5. Use `repo_get_file` / `repo_search_code` to read surrounding code or find existing helpers the change should have reused. Prefer this over a clone.

## Step 3 — Review

**Spec axis** — walk each acceptance criterion: met, partly met, or not met, with the file/line that shows it. List behavior in the diff that no criterion asked for (scope creep) and criteria with no test.

**Standards axis** — correctness bugs, missing error and edge-case handling, security (input validation, authorization, secrets), tests that assert behavior rather than implementation, duplication of existing helpers, naming and structure against the docs you read. Only raise what you can point at; skip pure taste.

Severity: **blocker** (wrong or unsafe), **should-fix**, **nit**. Be explicit about what you did **not** check.

## Step 4 — Submit

1. `review_submit` with a verdict and inline comments on specific lines. Verdict: request changes if any blocker or an unmet criterion; approve only if the Spec axis is fully met and CI is green; otherwise comment.
2. `add_task_comment` on the task with the two-axis summary (met / not met per criterion, blockers, CI state, merge risk in one line).
3. Move the task: back to in progress if changes are requested (`update_task`), or leave it for the merger if approved.

**Never merge** with `change_request_merge` unless the user explicitly asked you to merge this PR; it needs the expected head SHA and the `git:merge` permission, and a merge is hard to reverse.

Report back: task number, verdict, blockers, and unmet criteria.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Tasks:** `get_task` · `get_task_by_number` · `list_tasks` · `update_task` · `list_task_activities` · `add_task_comment`
**Documents:** `list_docs` · `search_docs` · `read_doc`
**Git:** `list_repositories` · `change_request_get_context` · `change_request_get_diff` · `checks_get` · `checks_get_logs` · `checks_rerun` · `review_list_threads` · `review_submit` · `review_reply_thread` · `comment_post` · `repo_get_file` · `repo_search_code`
