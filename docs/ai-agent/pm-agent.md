# AI Agent — PM / Business Analyst Agent (design)

> **Status:** implemented — the Analyst role, `task.status_category` and the
> Refinement status ship in the IAM change; the `paca-analyst` bundled skill
> ships with the agent change (see [Delivery](#delivery-two-prs)).

## Summary

A project-scoped agent that acts as the team's product manager / business
analyst. It discusses requirements with users in plain or technical language,
reads the project's linked repositories (read-only), checks requests against
what already exists on the board and in the code, and writes the outcome as
**draft** tasks in a **Refinement** status. Drafts are persisted, visible to the
team and resumable by anyone. Only a human moves a draft out of Refinement
(approval); from then on the task follows Paca's existing assignment paths and
developer agents can pick it up.

It reuses existing building blocks rather than adding a subsystem:

| Need | Existing mechanism used |
|---|---|
| Chat without a task | `AgentChatSession` / chat-message conversations |
| Persona preset | `ba` skill template (`service/agent/skill_templates.go`) |
| Workflow | new bundled skill `paca-analyst` (`platform/bundledskills/content/`) |
| Task / doc writes | Paca MCP (`create_task`, `update_task`, task links, `write_doc`, `ask_user_question`) |
| Code reading | MCP `workspace_inspect` / `workspace_prepare` (clone uses a **read** credential) |
| Read-only + "only humans approve" | IAM: new seeded **Analyst** project role, conditioned on a new `task.status_category` attribute |
| Draft state | existing `refinement` status category (already in `domain/task/entity.go`, not seeded today) |

## Problem

Paca's agents implement tasks well, but turning a vague idea or bug report into
a well-specified backlog item is still manual. The pieces exist — BA and PO
skill templates, `paca-epic`, `paca-clarify`, `ask_user_question` — but:

- the BA/PO templates only produce text; `paca-epic` creates tasks directly with
  no human gate;
- no agent can be made read-only for code: a `provider_cli` agent runs with
  permission prompts skipped, so the only boundary is IAM, and no seeded role
  gives "edit tasks, read code, never push";
- nothing stops an agent from moving a half-baked task into the developer flow.

## Goals and non-goals

**Goals**

- Requirements discovery in chat for technical and non-technical users.
- Drafts persisted on the board, resumable by humans or other analyst agents.
- Guaranteed read-only code access and guaranteed "cannot approve its own
  drafts" for `provider_cli` and `llm` agents holding the Analyst role.
- Duplicate / overlap detection against the existing board.
- Bug intake with evidence and a code-level plausibility check.

**Non-goals (v1)**

- External messaging (Slack, Discord, Teams). Separate future project; it would
  relay into the same chat sessions.
- Global (no-project) analyst agents. Drafts need a project, so v1 analysts are
  project-scoped.
- Auto-assigning approved tasks. Approved tasks land unassigned in Backlog;
  existing manual assignment, automations or Jev auto-assign take over.
- Preventing *developer* agents (Editor role) from moving drafts. Documented
  opt-in Deny recipe instead (see [Caveats](#caveats)).
- Detailed manual test runs (existing `manual-tester` template) and code-level
  tests (developer agents).

## Enforcement model (IAM)

### New condition attribute: `task.status_category`

The category (`backlog`, `refinement`, …) of the task's status. Status ids are
per project, so a seeded role cannot name "the Refinement status"; a category
can.

- **Registry:** add `task.status_category` (string, single-valued) to the
  built-in attributes in `platform/authz/iam/attributes.go` (label key
  `roles.attributes.task.status_category`, derived automatically; the web shows
  the raw key, there is no locale file).
- **Loader:** the task loader in `repository/postgres/iam_attribute_loaders.go`
  loads it via `task_statuses.category` for the task's `status_id`, and is
  wrapped as `taskLoader{scalarLoader}` to gain an `Expand` (the
  `iam.AttributeExpander` interface, precedent: `docLoader`). When a request
  carries `task.status_id`, `Expand` sets `task.status_category` to that
  status's category (status not in the project → `ErrResourceNotInProject`;
  null status → category absent). This is what makes the condition work on
  **create** (`AuthorizeCreate`) and on the **new state** of an update
  (`AuthorizeChange`); without it the old category would be reused for the new
  state. A create without `status_id` has no category and is denied (the
  default status is applied later, in the task service).
- **Scope SQL:** add the matching entry to `taskScopeColumns` in
  `repository/postgres/iam_scope.go`
  (`(SELECT category FROM task_statuses WHERE id = tasks.status_id)`), keeping
  loader and scope in step as that file requires.

### New seeded project role: Analyst

Added to `bootstrap/defaultroles/manifest.json` with a policy file
`policies/project_analyst.json`. Description: "Create and refine draft tasks in
Refinement; read code."

Allow:

- `projects:read`, `tasks:read`, `sprints:read`, `views:read`, `docs:read`,
  `workflows:read`, `project.members:read`, `annotations:read`
- `git:read` (read credential, repository listing, reviews/issues reads)
- `docs:write`
- `conversations:read`, `conversations:write`, `agents:read`,
  `environments:read`
- `tasks:write` in two statements, following the sprint-restricted recipe in
  `docs/guides/roles-and-policies.md`:
  - unconditioned on `project/PROJECT_ID` — the task-create collection gate
    (`require.Project(iam.ActionTasksWrite)`) is project-level, where no
    `task.*` attribute exists;
  - conditioned on `project/PROJECT_ID/task/*` with
    `{"In": {"task.status_category": ["refinement"]}}` — every per-task check,
    including the create body check (`require.TaskCreate`).

  The project-level grant also passes the other project-level `tasks:write`
  gates (bulk positions, which re-checks each task; annotations).

Deny, so a draft never reaches developer agents before a human approves it:

- `tasks:write` on `project/PROJECT_ID/task/*` when the task has an assignee
  (`{"StringLike": {"task.assignee_id": ["*"]}}`) or a sprint
  (`{"StringLike": {"task.sprint_id": ["*"]}}`). The Analyst cannot create a
  draft with either, set either on a draft, or edit a draft a human has
  assigned or planned (that draft is now the human's).

Auto-assign (`worker/task_auto_assign_consumer.go`) also skips tasks in a
refinement-category status. A draft left in assignment mode "auto" is assigned
by the update that moves it out of Refinement.

Not granted: `git:write`, `git:merge`, `git:approve`, any `project.settings.*`,
`project.members:write`, `sprints:write`.

Because task updates use `AuthorizeChange` (old **and** new state must pass) and
creates use `AuthorizeCreate` (requested state must pass), the single
conditioned `tasks:write` statement yields:

| Analyst action | Result |
|---|---|
| Create a task in Refinement | allowed |
| Edit / comment / link / attach on a task in Refinement | allowed |
| Create a task in Backlog (or with no status → default Backlog) | 403 |
| Move a task out of Refinement (approve) | 403 |
| Create or edit a draft with an assignee or sprint | 403 |
| Edit / comment on a task outside Refinement | 403 |
| Clone / fetch a linked repo | allowed (read credential) |
| Push | 403 (no `git:write`) |

Task comments and links are gated by `tasks:write` on the task in the route
(`transport/http/router/router.go`), so they inherit the same condition. A link
is authorized on the task it starts from, so a draft may link to any task.

### Refinement status

- **New projects:** `seedDefaultTaskStatuses`
  (`service/project/project_service.go`) adds a "Refinement" status, category
  `refinement`, at position 1 before Backlog (others shift to 2–5; positions
  stay positive). Backlog stays the default status.
- **Existing projects:** migration `000082_add_refinement_status_and_analyst_role.sql`
  (up-only, `BEGIN; … COMMIT;`), for every project with no
  `refinement`-category status, shifts that project's status positions up by
  one and inserts Refinement at the old lowest position (named "Drafts" when
  the project already has a status named "Refinement"); and inserts the
  Analyst role into `roles` for every project that lacks one
  (`WHERE NOT EXISTS`, unique on `(project_id, name)`). Idempotent; no task
  changes status and no existing role is changed.

### Role-policy skill and docs

- `paca-role-policy` content lists the new condition key in its `## Conditions`
  table (no test enforces condition keys; its JSON examples are validated).
- `docs/guides/roles-and-policies.md` documents the Analyst role, the new
  attribute, and the optional Deny recipe below.

## Agent and skill

### Preset

Bundled skills reach every agent, so an agent becomes a PM / Business Analyst
by **holding the Analyst role** (picked in the existing role selector of the
create-agent dialog, which already requires `role_ids`) plus a description that
says so (Jev Auto mode routes by description). No template-to-role coupling is
added: skill templates are only served by `GET /agents/skill-templates`, which
nothing in the web app consumes, and no code links a template to a role. The
create-agent dialog's **Business Analyst** preset (`AGENT_PRESETS` in
`apps/web/src/lib/agent-api.ts`, whose prompt is what new agents actually get)
and the `ba` template both defer to `paca-analyst`, so the persona and the skill
do not drift: drafts go in the refinement-category status, and an assigned task
routes to `paca-analyst`. `po-assistant` is left as is.

Supported agent types:

- `provider_cli` and `llm`: read-only and no-approve are **guaranteed** by IAM.
- `acp`: works, but runs on the user's machine with their own git/`gh`
  credentials, so read-only is **trust-based** (the skill forbids commit/push).
  Stated in the docs.

### Bundled skill `paca-analyst`

`content/paca-analyst.md` (plus `paca-analyst.agent.md` only if the agent flavor
must differ), registered in `catalog.go` and added to the `paca` / `paca.agent`
router table. Its content defines the behavior below.

#### Conversation style

- Start in plain language; switch to technical detail (paths, APIs, data model)
  when the user writes technically or asks. Honor "explain simply" / "go
  technical" at any time.
- One question at a time; play back understanding before drafting. Use
  `ask_user_question` when an answer is required.
- Read code only when it changes the answer: `workspace_inspect`, then
  `workspace_prepare` with `strategy: "clone"`, then read files.

#### Feature intake

1. Understand the problem and who it is for.
2. **References:** ask whether the user has something similar to point to — an
   existing Paca feature, another product, a mockup, screenshot or link.
   Optional; captured under References when given.
3. **Fit check:** compare the request with the board (`list_tasks`), the code,
   `ROADMAP.md` and project docs, and say which applies:
   - *Fits* — extends feature X;
   - *Overlaps* — similar to existing #N (see [Matching](#matching-existing-tasks));
   - *Conflicts* — contradicts how Y works or a stated principle; ask whether to
     proceed and record the decision in the draft.
4. **Examples:** propose 2–3 concrete examples ("so if a user does X they see
   Y — right?") and have the user confirm or correct them. Confirmed examples
   become the acceptance scenarios.

#### Bug intake

1. Collect: steps to reproduce, expected vs actual, environment/version,
   frequency, who is affected, and evidence (screenshots, logs, error text —
   chat context items; the analyst may attach them to its draft).
2. **Code check:** look for the likely cause in the repo and record one of:
   - *Confirmed in code* — `path:line` and reasoning;
   - *Plausible, not confirmed* — what was found;
   - *Not supported by code* — e.g. behavior is intentional; show evidence and
     ask whether this is really a feature request.
   The analyst never claims to have reproduced a bug (it does not run the app).
3. Use the "Bug" task type when the project has one; suggest a severity.

#### Matching existing tasks

Before drafting, search the board for similar work and tell the user (number,
title, status), proposing to extend rather than duplicate:

- Match **in Refinement** → update that draft directly.
- Match **past Refinement** → create an *addendum draft* in Refinement titled
  `Addendum to #N: …` with only the additional requirements, linked
  `relates_to` #N. The approving human merges it into #N or approves it as a
  follow-up task.
- Exact duplicate → link the draft `duplicates` #N and say so.

#### Draft shape (adaptive)

- **Small** request → one task in Refinement.
- **Large** request → an epic plus 3–8 stories, all in Refinement, plus a spec
  doc (`paca-epic` shape; stories reference `Part of #<epic>`).
- The analyst states which shape it chose and why; the user may override.
- It summarizes what it will create, then creates drafts without a separate
  confirmation (approval is the gate), and reports task numbers plus how to
  approve.

#### Draft content

Feature draft description sections:

- **Problem / why** — plain language.
- **User story** — "As a … I want … so that …".
- **Use cases and acceptance scenarios** — Gherkin `Given / When / Then`,
  including edge and error cases, built from the confirmed examples.
- **Technical notes** — affected modules/files with paths (skippable for
  non-technical readers; marked "not verified against code" when no repo is
  available).
- **References** — from the references step.
- **Fit** — fits / overlaps / conflicts, plus any recorded decision.
- **Open questions** — what is unresolved; this is what makes a draft resumable.
- **Source** — link to the originating chat.

Bug drafts replace User story / Use cases with the bug intake fields and the code
check result.

#### Test responsibility split

- **Analyst:** use cases and behavior-level acceptance scenarios (the acceptance
  tests), confirmed by the user and approved with the draft.
- **Manual Tester template (`manual-tester`):** turns approved scenarios into
  step-by-step test runs.
- **Developer agents:** unit and integration tests in code.

#### Approval and resuming

- Approval is per task: a human moves each task from Refinement to Backlog.
  Partial approval is allowed; approving an epic does not cascade to its stories.
- Asked to approve, commit or push, the analyst refuses, explains it is
  read-only, and tells the user how to do it themselves.
- "Continue #42" makes an analyst read the draft, its open questions and source
  chat, and carry on refining. Humans can also edit drafts directly.

## Failure handling

| Situation | Behavior |
|---|---|
| Project has no Refinement status | Tell the user to add one in project settings; create nothing. Never fall back to Backlog. |
| No linked repo / no git provider plugin | Say code can't be read; continue with requirements only; mark technical notes "not verified against code". |
| 403 writing a task | Switch to the addendum path; never retry in a way that tries to get around the denial. |
| Asked to commit, push or approve | Refuse and explain. |
| Agent configured without the Analyst role | Skill still follows the rules; docs state the guarantees depend on the role. |

## Caveats

- **Developer agents can still approve.** Agents holding Editor have
  unconditioned `tasks:write`. Projects that want to block this attach a Deny to
  their agents, exempting analyst agents:

  ```json
  {
    "version": "2026-10-01",
    "statements": [
      {
        "sid": "AgentsCannotLeaveRefinement",
        "effect": "Deny",
        "actions": ["tasks:write"],
        "resources": ["project/PROJECT_ID/task/*"],
        "conditions": {
          "In": { "principal.type": ["agent"], "task.status_category": ["refinement"] },
          "NotIn": { "principal.id": ["ANALYST_AGENT_ID"] }
        }
      }
    ]
  }
  ```

  All conditions in a statement must hold (`conditionsHold` in
  `platform/authz/iam/eval.go`), and `principal.type` is `agent` for agent
  accounts (`domain/account/entity.go`). Because updates check the old state
  too, this also stops those agents editing drafts, which is the intended
  trade-off.
- **`docs:write` is unconditioned** — the analyst can edit any project doc.
- **Push guarantee needs a clean environment** — it holds only while no other git
  credential (e.g. `gh auth login`) exists inside the agent's static
  environment.
- **`acp` analysts are trust-based** (see [Preset](#preset)).

## Delivery: two PRs

**PR 1 — `feat(iam)`: Refinement drafts that agents cannot approve**

- `task.status_category` attribute: registry, loader + `Expand`, scope SQL.
- Analyst role + policy in the default-roles manifest.
- Refinement in `seedDefaultTaskStatuses`.
- Migration `000082_…`.
- `paca-role-policy` skill and `docs/guides/roles-and-policies.md` updates.

**PR 2 — `feat(agent)`: PM / Business Analyst agent**

- `paca-analyst` bundled skill, catalog entry, router row.
- `ba` template content update.
- This document (status → implemented), `default-skills.md`, `docs/README.md`
  index, `ROADMAP.md`.

## Testing

**PR 1**

- Attribute: loader returns `task.status_category`; `Expand` fills it on create
  and on change; scope SQL filters lists correctly (alongside existing attribute
  and scope tests).
- Authorization matrix from the [Analyst table](#new-seeded-project-role-analyst):
  each row as a test, plus read credential allowed / write credential denied.
- Migration: adds Refinement only where missing, safe to re-run, Analyst role
  present in every project.
- Existing `paca-role-policy` coverage test and default-roles manifest tests pass.

**PR 2**

- Bundled-skill catalog tests (`TestList_PacaFeatureSkills` etc.) cover
  `paca-analyst`.

Loader, scope and migration tests need Postgres: they read `PACA_TEST_PG_DSN`
and skip when it is unset (CI's unit job does not set it; the `PACA_E2E=1`
suite applies real migrations).
- Optional Playwright e2e, if existing agent e2e patterns allow: chat with an
  analyst, a draft appears in Refinement, its approve attempt is refused.
