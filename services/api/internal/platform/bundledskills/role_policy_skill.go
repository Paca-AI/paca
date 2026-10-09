package bundledskills

import "strings"

// rolePolicySkillContent is the cli-flavor body of the paca-role-policy skill.
// It is written with "¤" in place of a backtick (Go raw strings cannot contain
// one) and converted here, so the markdown stays readable in source. Every
// action and JSON example in it is checked against the IAM registry by
// TestRolePolicySkill_*.
var rolePolicySkillContent = strings.ReplaceAll(rolePolicySkillRaw, "¤", "`")

const rolePolicySkillRaw = `---
name: paca-role-policy
description: Draft the JSON policy document of a Paca role (IAM-style statements of Allow/Deny actions on resources with conditions) for the user to paste into the role editor's Advanced (JSON) view. Use when asked to write a role policy, make a read-only or limited role, restrict an agent or environment, limit someone to one sprint or folder, or give access to specific projects. Drafts only - never creates, updates, attaches or deletes a role.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You help the user DRAFT the JSON policy document of a Paca role. You only produce JSON text. You NEVER create, update, attach, assign or delete a role, and you never claim you did. The user pastes the result into the web role editor (Administration > Roles, or a project's Roles page) in the Advanced (JSON) view, or sends it to the API themselves. Paca MCP is optional for this skill; you do not need it to draft.

---

## Procedure

1. **Clarify** with at most a few short questions, only for what you cannot infer: who or what the role is for; which scope (one project, several projects, the whole workspace); read-only or read-write; what must be excluded (an agent, an environment, a sprint, a folder). If the role is for a single project, ask for the project id (copyable from the project's URL in the web app); use a clear placeholder like ¤PROJECT_ID¤ if the user does not have it yet and tell them to replace it. Do the same for agent, environment, sprint, folder and user ids.
2. **Draft** the policy using only the actions, resources, conditions and rules below.
3. **Self-check** it against the checklist at the end of this skill. If a Paca MCP tool that validates a role policy exists, you may call it ONLY to validate the draft (read-only); never call any tool that saves, creates or updates a role.
4. **Present** exactly ONE ¤json¤ code block with the full policy, then a plain-language summary (what it allows, what it denies, where it applies), then caveats (placeholders to replace, where it can be attached, anything the user must hold themselves). Offer to refine.
5. If the request needs an action that is not in the list below, say so plainly and propose the closest real actions. Do not invent one.

If a Paca MCP tool that lists the available role actions exists, prefer it over the embedded list below (plugins add their own actions at runtime; the live list is also ¤GET /roles/actions¤ and what the JSON editor autocompletes). Otherwise use the list below, and tell the user plugin actions are not covered.

---

## Policy shape

¤¤¤jsonc
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "OptionalLabel",
      "effect": "Allow",            // or "Deny"
      "actions": ["tasks:read"],     // required, at least one
      "resources": ["project/PROJECT_ID/*"],   // required, at least one
      "conditions": { "StringEquals": { "task.sprint_id": "SPRINT_ID" } }  // optional
    }
  ]
}
¤¤¤

- ¤version¤ must be ¤"2026-10-01"¤. At most 100 statements. Unknown fields are rejected. ¤sid¤ is optional.
- ¤conditions¤ shape: ¤{ operator: { key: value-or-list } }¤. All conditions must hold. The final answer must be plain JSON: no comments, no trailing commas (the ¤jsonc¤ block above only annotates the shape).

## Actions

Form ¤domain:verb¤. Wildcards: ¤*¤ (every action) and ¤domain:*¤ (every action of one domain, e.g. ¤tasks:*¤). No other wildcard forms (never ¤task*¤ or ¤*:read¤). An unknown action is rejected on save.

- users: ¤users:read¤ ¤users:write¤ ¤users:delete¤
- roles: ¤roles:read¤ ¤roles:write¤ ¤roles:assign¤
- projects: ¤projects:read¤ ¤projects:write¤ ¤projects:create¤ ¤projects:delete¤
- project team and log: ¤project.members:read¤ ¤project.members:write¤ ¤project.activities:read¤ ¤project:export¤
- tasks: ¤tasks:read¤ ¤tasks:write¤ (tasks, comments, task links)
- project task schema: ¤project.settings.task_types:write¤ ¤project.settings.task_statuses:write¤ ¤project.settings.custom_fields:write¤
- planning and content: ¤sprints:read¤ ¤sprints:write¤ ¤views:read¤ ¤views:write¤ ¤docs:read¤ ¤docs:write¤ ¤workflows:read¤ ¤workflows:write¤
- annotations: ¤annotations:read¤ ¤annotations:write¤ ¤annotations:resolve¤
- agents: ¤agents:read¤ ¤agents:write¤ ¤conversations:read¤ ¤conversations:write¤
- environments: ¤environments:read¤ ¤environments:write¤ ¤environments:connect¤ (connect = interactive shell / SSH key, separate from configuring)
- workspace: ¤settings:write¤ ¤settings.sso:write¤ ¤plugins:read¤ ¤plugins:write¤

Plugins can add actions named ¤<plugin namespace>:<verb>¤ (checked on ¤project/<id>/plugin/<pluginId>¤ inside a project). Only use one if the user names it or a live list shows it.

**Root-equivalent actions** (whoever holds one can give themselves everything): ¤roles:write¤, ¤roles:assign¤, ¤settings.sso:write¤, ¤users:write¤ (includes resetting any password). Never add them unasked; if requested, warn that they equal full admin. ¤roles:assign¤ works like AWS IAM ¤iam:PassRole¤: its RESOURCES say WHICH roles the holder may attach to people or agents (¤role/<id>¤ workspace role platform-wide, ¤project/<projectId>/role/<id>¤ for an attachment inside one project, ¤role/*¤ / ¤project/<id>/role/*¤ for all of them). Granted on ¤*¤ it is root-equivalent, so always scope it to the roles it should hand out; the assigner does NOT need to hold the permissions of those roles.

## Resources

Slash-separated paths, no empty segments. A middle ¤*¤ matches exactly one segment; a trailing ¤*¤ matches everything below AND the parent itself (so ¤project/p1/*¤ also covers ¤project/p1¤).

| Resource | Names |
|---|---|
| ¤*¤ | Everything |
| ¤project/<id>¤ | The project itself only |
| ¤project/<id>/*¤ | The project and everything in it |
| ¤project/*¤ | Every project (workspace roles only) |
| ¤project/<id>/task/<taskId>¤, ¤project/<id>/task/*¤ | Tasks. Same shape for kinds ¤sprint¤ ¤doc¤ ¤view¤ ¤workflow¤ ¤annotation¤ ¤conversation¤ ¤agent¤ ¤environment¤ ¤role¤ ¤plugin¤ |
| ¤project/<id>/agent/<agentId>/*¤ | One agent in a project and anything under it |
| ¤project/<id>/environment/<envId>/*¤ | One environment in a project |
| ¤project/*/environment/*¤ | Environments in every project |
| ¤user/*¤ ¤role/*¤ ¤agent/*¤ ¤plugin/*¤ | Platform users, roles, global agents, plugins (¤user/<id>¤ for one) |
| ¤settings¤, ¤sso¤ | Workspace settings, SSO providers |
| ¤project¤ | The project list itself (listing and creating projects) |

Valid roots are only: ¤project¤ ¤user¤ ¤role¤ ¤plugin¤ ¤agent¤ ¤settings¤ ¤sso¤ (or the bare ¤*¤). Anything else is rejected.

Gate rule: project-wide actions are first checked on the project (¤project/<id>¤) to let a request in, then on the specific thing (¤project/<id>/task/<taskId>¤). ¤project/<id>/*¤ covers both. When you limit access to part of a project (a sprint, a folder, one agent), ALSO add a statement giving the needed actions on exactly ¤project/<id>¤ (usually ¤projects:read¤, plus the task/doc read actions the list screens need), as in the sprint example. Naming an action on a platform resource (e.g. ¤projects:write¤ on ¤project¤) does not open any project's contents; only ¤*¤ on ¤*¤ or statements on ¤project/...¤ reach inside projects.

## Conditions

| Operator | True when the attribute... |
|---|---|
| ¤StringEquals¤ | equals one of the values |
| ¤StringNotEquals¤ | equals none of the values |
| ¤StringLike¤ | matches a value; ¤*¤ = any run of characters, ¤?¤ = one character |
| ¤In¤ / ¤NotIn¤ | is / is not in the list |
| ¤Bool¤ | equals ¤"true"¤ or ¤"false"¤ (exactly one value) |

Values are strings (a single string or a list). An operand must not be empty. Multi-valued attributes: positive operators hold when ANY value matches, negated ones only when NONE does. **A missing attribute makes positive operators false and negated operators (StringNotEquals, NotIn) true.**

Attribute keys. A key tied to one resource kind only works in a statement whose ¤resources¤ cover that kind (e.g. ¤task.*¤ needs ¤project/<id>/task/*¤ or a broader ¤project/<id>/*¤); otherwise the save fails.

| Key | Kind | Value |
|---|---|---|
| ¤principal.id¤, ¤principal.type¤ | any | caller's id; ¤user¤ or ¤agent¤ |
| ¤resource.id¤ | any | id of the thing accessed |
| ¤task.sprint_id¤, ¤task.status_id¤, ¤task.type_id¤ | task | the task's sprint, status, type |
| ¤task.assignee_id¤ | task | assignee ids (multi-valued) |
| ¤view.sprint_id¤ | view | the sprint a view belongs to (absent for backlog and timeline views) |
| ¤doc.folder_id¤ | doc | the document's folder |
| ¤doc.ancestor_folder_ids¤ | doc | folder and all parents (multi-valued) |
| ¤agent.environment_id¤ | agent | the agent's default environment |
| ¤environment.type¤ | environment | ¤docker¤ or ¤kubernetes¤ |
| ¤conversation.environment_id¤ | conversation | environment a chat runs in |

On create/update the conditions are checked against what the request sets: a role limited to one sprint cannot create a task without that sprint nor move a task out of it. Use only these keys; do not invent others.

## Evaluation rules

- Default deny: nothing is allowed unless a statement allows it.
- An explicit Deny that matches always wins over any Allow, including Allows from other roles the person holds.
- A statement applies only when an action pattern AND a resource pattern match AND all its conditions hold.
- Careful with Deny + negated conditions: a Deny conditioned on ¤NotIn principal.id¤ also applies when the attribute is missing. That is what makes "only these users" work, but double-check it is intended.
- A project member with no role sees nothing in the project.
- Role names mean nothing; only the policy counts.

## Scopes: where the role lives

- **Project-owned role** (created on a project's Roles page): every resource MUST be ¤project/<projectId>¤ or ¤project/<projectId>/...¤ of ITS OWN project. ¤*¤, ¤project/*¤, another project, ¤user/*¤ are rejected (¤ROLE_POLICY_INVALID¤ at ¤statements[i].resources[j]¤). It can only be attached inside that project. Ask for or reuse the project id.
- **Workspace role** (Administration > Roles): may name anything: ¤*¤, ¤project/*¤, specific projects (¤project/<id>/*¤), platform roots. One that names specific projects can be attached platform-wide and then reaches exactly those projects. A project-scoped attachment of any role applies only inside that project.
- **Project template**: a workspace role whose resources ALL start with a wildcard project segment (¤project/*¤, ¤project/*/task/*¤). Reusable per project (attach to a member in project A, it works only in A), but it cannot be attached platform-wide or made the default role (¤ROLE_NOT_ATTACHABLE¤). If the user wants a platform-wide role, include at least one non-¤project/*¤ resource or name specific projects.

Role-editor note: policies with more than one statement, a Deny, conditions, or narrowed resources open only in the Advanced (JSON) view; that is where to paste.

## Guard rails to tell the user about

- Whoever SAVES (creates/edits) a role, or makes it the default role, must already hold every permission it grants (no privilege escalation), and none of their own Deny statements may overlap it; otherwise they get a 403. ATTACHING an existing role to someone is different: it needs ¤roles:assign¤ on that role's resource (see example 7) and nothing else about the role is checked; changing someone's roles needs it for every role added or removed, and nothing else (changing a project member's roles does not need ¤project.members:write¤). Adding a project member or creating a project agent still needs ¤project.members:write¤ / ¤agents:write¤ on top, because that is member management.
- The last platform-wide full-access (¤*¤ on ¤*¤) assignment cannot be removed (¤ROLE_LAST_FULL_ACCESS¤).
- Edits apply on the next request. Restrictions on agents/environments are expressed as Deny roles that must be attached to each person they should affect.
- They can try the draft with the editor's Simulate button before saving.

---

## Worked examples

Replace the ALL_CAPS ids with real ones.

### 1. Read-only role for one project (project-owned or workspace)

¤¤¤json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "ReadOnly",
      "effect": "Allow",
      "actions": [
        "projects:read", "project.members:read", "tasks:read", "sprints:read", "views:read",
        "docs:read", "workflows:read", "annotations:read", "agents:read",
        "conversations:read", "environments:read"
      ],
      "resources": ["project/PROJECT_ID/*"]
    }
  ]
}
¤¤¤

As a workspace role, ¤project/*¤ makes it a per-project template instead.

### 2. Developer limited to one sprint (its tasks and views)

The first statement lets requests into the project (listing, creating); the second limits individual tasks; the third limits views to the sprint's own views (¤view.sprint_id¤).

¤¤¤json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "OpenProject",
      "effect": "Allow",
      "actions": ["projects:read", "tasks:read", "tasks:write", "sprints:read", "views:read", "views:write"],
      "resources": ["project/PROJECT_ID"]
    },
    {
      "sid": "OnlyThisSprint",
      "effect": "Allow",
      "actions": ["tasks:read", "tasks:write"],
      "resources": ["project/PROJECT_ID/task/*"],
      "conditions": { "StringEquals": { "task.sprint_id": "SPRINT_ID" } }
    },
    {
      "sid": "ViewsOfThisSprint",
      "effect": "Allow",
      "actions": ["views:read", "views:write"],
      "resources": ["project/PROJECT_ID/view/*"],
      "conditions": { "StringEquals": { "view.sprint_id": "SPRINT_ID" } }
    }
  ]
}
¤¤¤

Task lists return only that sprint's tasks; other sprints' tasks are refused; a task cannot be created without the sprint or moved out of it. View lists return only that sprint's views: backlog and timeline views belong to no sprint, so a positive ¤view.sprint_id¤ condition excludes them. To let the member create, rename, reorder and delete views of that sprint, add ¤views:write¤ to both the OpenProject statement (the project-level check) and the ViewsOfThisSprint statement (the per-view check). Creating a view needs the sprint in the request (¤sprint_id¤ query parameter) to be this sprint.

### 3. Restrict an agent (Deny)

Attach to the people who must not use the agent; the Deny beats their other Allows.

¤¤¤json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "NoSalesBot",
      "effect": "Deny",
      "actions": ["agents:*", "conversations:*"],
      "resources": ["project/PROJECT_ID/agent/AGENT_ID/*"]
    }
  ]
}
¤¤¤

To make the agent usable by only a few people instead, attach this to the whole team and add ¤"conditions": { "NotIn": { "principal.id": ["USER_ID_1", "USER_ID_2"] } }¤.

### 4. Deny one environment

¤¤¤json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "NoProduction",
      "effect": "Deny",
      "actions": ["environments:connect", "environments:write"],
      "resources": ["project/PROJECT_ID/environment/PROD_ENV_ID/*"]
    }
  ]
}
¤¤¤

Use ¤"environments:*"¤ to hide it completely. Environments are denied by id; there is no "production" flag. To block a runtime everywhere (workspace role): resources ¤["project/*/environment/*"]¤ with ¤"conditions": { "StringEquals": { "environment.type": "kubernetes" } }¤.

### 5. Workspace role for two projects

Workspace role only (a project's own Roles page would refuse the second project). Attachable platform-wide; reaches exactly these two projects.

¤¤¤json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "TwoProjects",
      "effect": "Allow",
      "actions": ["projects:read", "tasks:read", "tasks:write", "sprints:read", "docs:read"],
      "resources": ["project/PROJECT_A_ID/*", "project/PROJECT_B_ID/*"]
    }
  ]
}
¤¤¤

### 6. Platform-wide read-only support role

Reads users, roles and every project. Because the first statement uses non-project resources, the role is not a template and can be attached platform-wide.

¤¤¤json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "ReadDirectory",
      "effect": "Allow",
      "actions": ["users:read", "roles:read", "projects:read"],
      "resources": ["user", "user/*", "role", "role/*", "project"]
    },
    {
      "sid": "ReadAllProjects",
      "effect": "Allow",
      "actions": [
        "projects:read", "project.members:read", "project.activities:read", "tasks:read",
        "sprints:read", "views:read", "docs:read", "workflows:read", "annotations:read"
      ],
      "resources": ["project/*"]
    }
  ]
}
¤¤¤

### 7. Let project leads assign only Editor and Viewer

Roles of the project say which roles may be attached, not what they contain. Attach this to the leads, together with ¤project.members:write¤ if they should also add and remove members (changing the roles of existing members needs only ¤roles:assign¤). Replace the ids with the ids of the project's Editor and Viewer roles (shown in the Roles page URL or the roles API). Any other role id is refused with 403; removing someone's Admin role is refused too unless Admin is listed.

¤¤¤json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "ManageMembers",
      "effect": "Allow",
      "actions": ["project.members:write"],
      "resources": ["project/PROJECT_ID"]
    },
    {
      "sid": "AssignOnlyEditorAndViewer",
      "effect": "Allow",
      "actions": ["roles:assign"],
      "resources": [
        "project/PROJECT_ID/role/EDITOR_ROLE_ID",
        "project/PROJECT_ID/role/VIEWER_ROLE_ID"
      ]
    }
  ]
}
¤¤¤

Platform-wide, the same idea is ¤"resources": ["role/ROLE_ID"]¤ (one workspace role) or ¤["role/*"]¤ (any workspace role, for example a support admin). Use ¤project/PROJECT_ID/role/*¤ to let someone assign any role inside one project.

---

## Self-check before presenting

- Valid JSON; ¤version¤ is ¤"2026-10-01"¤; every statement has ¤effect¤ (Allow/Deny), non-empty ¤actions¤ and ¤resources¤.
- Every action is in the list above (or a live list), or is ¤*¤ / ¤domain:*¤ of a real domain.
- Resource roots are valid; no empty segments; scope rules respected (project-owned: only its own project).
- Every condition key exists, its operator is one of the six, its kind is covered by the statement's resources, and Bool uses ¤"true"¤/¤"false"¤.
- Partial-project access has the extra ¤project/<id>¤ statement.
- No root-equivalent action unless explicitly requested and warned about; ¤roles:assign¤ always with role resources, never on ¤*¤.
- Read-only means no ¤:write¤, ¤:create¤, ¤:delete¤, ¤:assign¤, ¤:connect¤, ¤:resolve¤.

## Do not

- Do NOT create, update, attach, assign or delete a role, or call any Paca tool or endpoint that does (for example MCP role create/update/delete tools). You only output JSON text for the user to apply.
- Do NOT say or imply the role was created, saved or applied. Say the user has to paste it into the Advanced (JSON) view (or send it to the API) and save.
- Do NOT invent actions, condition keys, operators or resource roots. If something the user wants is not expressible, say so and offer the nearest alternative.
- Do NOT put several JSON blocks in the answer; one final policy only.
- Do NOT guess ids; use placeholders and say which to replace.
`
