# Roles and policies

Paca controls who can do what with **roles**. A role is a JSON *policy*: a list of statements that allow or deny *actions* on *resources*, optionally under *conditions*. The model is the same as AWS IAM. This guide covers the policy format, the web editor and worked examples. For a one-page overview of the whole model see [IAM authorization](iam-authorization.md). For how the API enforces it see [Authorization architecture](../architecture/authorization.md); for the endpoints see [Roles and policies API](../api/roles-and-policies.md).

> Upgrading from an earlier release? Roles and project roles were converted automatically, but **restricted agents and environments were not**: they are open to everyone who can see the project until you recreate the restriction with a `Deny` role (see [Restrict an agent](#restrict-an-agent)). Read the [release notes](../releases/2026-10-iam-authorization.md) first, and **back up your database before upgrading**.

## The basics

- **One kind of role.** There are no separate global and project roles any more. What a role can reach comes from the *resources* in its policy, and from where it is *attached*.
- **Attach many roles.** A user, a global agent, or a project member (human or agent) can hold several roles. Their permissions are combined.
- **Platform-wide or per project.**
  - A **platform-wide** attachment (Administration, on a user or global agent) applies everywhere the role's resources reach.
  - A **project-scoped** attachment (a project's Team page, on a member) applies only inside that project, even if the role names other projects or `project/*`.
  - A role **owned by one project** (created on that project's Roles page) may only name resources inside that project: `project/<projectId>` or `project/<projectId>/...`. Anything else (`*`, `project/*`, another project, `user/*`) is rejected with `ROLE_POLICY_INVALID` and an issue at `statements[i].resources[j]`. It can only be attached inside that project. The project role editor writes `project/<projectId>/*` for you.
  - A **workspace role** (Administration, Roles) may name any resource: `*`, one project (`project/<id>/*`), several projects, or all of them (`project/*`). A workspace role that names specific projects can be attached platform-wide, and then reaches exactly those projects.
  - A **project template** is a workspace role whose resources *all* have a wildcard project segment (`project/*`, `project/*/task/*`). It is reusable: attach it to different members in different projects and it only works in each one. Because it would grant its powers in every project, it can only be attached per project: attaching it platform-wide, or making it the default role, is refused with `ROLE_NOT_ATTACHABLE`, and the Administration role pickers do not offer it. The Administration role dialog edits a template with the project permission list and keeps `project/*`.
- **Default deny, explicit deny wins.** Nothing is allowed unless a statement allows it. If any statement *denies* the request, it is refused, even when other statements allow it.
- **Role names mean nothing.** Only the policy counts. Edits apply on the next request.
- **A project member with no role sees nothing** in the project.

Built-in roles: platform roles `SUPER_ADMIN` (everything), `ADMIN` (runs users, projects, agents, plugins and settings; can read roles but not define or assign them) and `USER` (the default every new account starts with). Every new project gets the project roles `Admin`, `Editor` and `Viewer`. Built-in roles marked *system* can be edited like any other role (by anyone allowed to write roles) but cannot be deleted; Paca creates them if missing and never overwrites your edits on restart or upgrade. A project's Roles page lists the project's own roles plus the workspace roles already attached to someone inside that project.

## Policy reference

```json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "ReadTasks",
      "effect": "Allow",
      "actions": ["tasks:read", "sprints:read"],
      "resources": ["project/0b6f1e4a-1c53-4a6e-9a43-5d0f2c7a9e11/*"],
      "conditions": { "StringEquals": { "task.sprint_id": "..." } }
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `version` | Use `"2026-10-01"`. |
| `statements` | Up to 100 statements. |
| `sid` | Optional label, shown when you simulate a request. |
| `effect` | `"Allow"` or `"Deny"`. |
| `actions` | One or more actions (required). |
| `resources` | One or more resources (required). |
| `conditions` | Optional. All conditions must hold. |

Unknown fields are rejected.

### Actions

`domain:verb`, for example `tasks:write`. `*` is every action and `tasks:*` every action of the `tasks` domain. No other wildcard forms exist. An action Paca does not know is rejected when you save.

| Domain | Actions | Controls |
|---|---|---|
| `users` | `read` `write` `delete` | Users (`write` includes resetting any user's password) |
| `roles` | `read` `write` `assign` | Reading roles, creating/editing them, attaching them |
| `projects` | `read` `write` `create` `delete` | Projects |
| `project.members` | `read` `write` | Project team |
| `project.activities` | `read` | The project activity log |
| `project` | `export` | Exporting a project |
| `tasks` | `read` `write` | Tasks, comments, task links |
| `project.settings.task_types` / `task_statuses` / `custom_fields` | `write` | Editing the project's task schema |
| `sprints` `views` `docs` `workflows` | `read` `write` | Sprints, views, documents, automations |
| `annotations` | `read` `write` `resolve` | Page annotations |
| `agents` | `read` `write` | Agent configuration |
| `conversations` | `read` `write` | Agent chats |
| `environments` | `read` `write` `connect` | Environments; `connect` is an interactive shell/SSH key |
| `settings` | `write` | Workspace settings |
| `settings.sso` | `write` | SSO providers |
| `plugins` | `read` `write` | Plugins |
| _plugin namespace_ | defined by each plugin | See [Plugin actions](#plugin-actions) |

`roles:write`, `roles:assign`, `settings.sso:write` and `users:write` are **root-equivalent**: whoever holds one can give themselves everything. Grant them only to people you would trust as `SUPER_ADMIN`. The exception is `roles:assign` when you scope it with resources to specific roles: see [Let people assign roles](#let-people-assign-roles-rolesassign). The live list is always `GET /roles/actions`, which is also what the JSON editor autocompletes.

### Resources

Paths. `*` in the middle matches exactly one segment; a trailing `*` matches everything below, and the parent itself.

| Resource | Names |
|---|---|
| `*` | Everything |
| `project/<id>` | One project |
| `project/<id>/*` | The project and everything in it (the form a project role uses) |
| `project/*` | Every project (workspace roles only; a template when attached per project) |
| `project/<id>/<kind>/<id>` | One thing in a project. Kinds: `task`, `sprint`, `doc`, `view`, `workflow`, `annotation`, `conversation`, `agent`, `environment`, `role`, `plugin` |
| `project/<id>/agent/*` | Every agent in a project |
| `user/*`, `role/*`, `agent/*`, `plugin/*` | Platform users, roles, global agents, plugins (`user/<id>` for one) |
| `settings`, `sso` | Workspace settings, SSO providers |
| `project` | The project list itself (list and create projects) |

You can copy a project, agent or environment id from its URL in the web app.

Two things worth knowing:

- Project-wide actions are first checked on the project (`project/<id>`) to let a request in, then on the specific thing (`project/<id>/task/<task>`). Give the role a statement on `project/<id>/*` (which covers both), or see the sprint example below for how to limit access.
- Naming an action on a platform resource (for example `projects:write` on `project`) does **not** open any project's contents. Only `*` on `*`, or statements on `project/...`, reach inside projects.

### Conditions

```json
"conditions": { "StringEquals": { "task.sprint_id": "d4a6..." } }
```

Shape: `{ operator: { key: value-or-list } }`.

| Operator | True when |
|---|---|
| `StringEquals` | the attribute equals one of the values |
| `StringNotEquals` | the attribute equals none of the values |
| `StringLike` | the attribute matches a value, where `*` is any run of characters and `?` one character |
| `In` / `NotIn` | the attribute is / is not in the list |
| `Bool` | the attribute equals `true` or `false` |

If an attribute has several values (assignees, ancestor folders), positive operators hold when *any* value matches and negated ones only when *none* does. A missing attribute makes positive operators false and negated ones true.

Available keys (the live list is `GET /roles/attribute-schema`). A key tied to one kind of resource only works in statements whose resources cover that kind:

| Key | Value |
|---|---|
| `principal.id`, `principal.type` | The caller's id; `user` or `agent` |
| `resource.id` | The id of the thing being accessed |
| `task.sprint_id`, `task.status_id`, `task.type_id` | Task's sprint, status and type |
| `task.assignee_id` | Ids of the task's assignees (users or agents) |
| `view.sprint_id` | The sprint a view belongs to (absent for backlog and timeline views) |
| `doc.folder_id` | The document's folder |
| `doc.ancestor_folder_ids` | The document's folder and all its parents |
| `agent.environment_id` | The agent's default environment |
| `environment.type` | The runtime backend: `docker` or `kubernetes` |
| `conversation.environment_id` | The environment a chat runs in |

When something is created or changed, the conditions are checked against what the request sets. A role limited to one sprint cannot create a task with no sprint, and cannot move a task out of (or into) another sprint.

### Plugin actions

An installed plugin can add its own actions, named `<plugin namespace>:<verb>`, for example `time_logging:manage_all`. They appear in the role editor and can be used in policies like any other action, and are checked on `project/<id>/plugin/<plugin id>` inside a project. They disappear from the list when the plugin is uninstalled.

## Simple and Advanced editors

The role dialog (Administration, Global Roles, or a project's Roles page) has two views of the same policy:

- **Simple** shows a checkbox for every permission, grouped by area. This is the default for ordinary roles. A role with every box checked through `*` shows as *Full access*.
- **Advanced (JSON)** shows the policy document. It validates as you type (the server reports problems by path, for example `statements[0].actions[1]: unknown action`), and you can **simulate** a request against it.

Switching from Simple to Advanced writes your checkboxes out as a policy. Switching back only works if the checkboxes can represent the JSON. A role **opens in Advanced, and stays JSON-only**, when it has:

- a `Deny` statement,
- conditions,
- resources other than the whole workspace or the whole project scope, or
- actions the checkbox list does not offer.

The dialog tells you why ("It has a Deny statement.", and so on) and nothing is ever dropped silently. Everything in this guide beyond plain allow lists is therefore edited in Advanced.

## Assigning roles

- **Users:** Administration, Users, change role. Pick one or several platform roles. Needs `roles:assign` (and `roles:read` to list them).
- **Global agents:** the agent's roles tab, with the same rules.
- **Project members and project agents:** the project's Team page. Pick one or several roles (platform roles and the project's own). Changing an existing member's roles needs only `roles:assign`; adding a member needs `project.members:write` **and** `roles:assign`.

`roles:assign` works like AWS IAM `iam:PassRole`. The **resource** of the statement says *which roles* the holder may attach or detach, and nothing else about the role is checked: you do not need to hold the permissions the role grants. Every role a request adds or removes must be allowed; roles that stay as they are need no permission. The project `Admin` role (`*` on `project/<id>/*`) can therefore assign any role inside its project. Because the grant can hand out anything it names, `roles:assign` on `*` is root-equivalent: scope it.

Saving a role (create, edit, or make it the default) needs only `roles:write`; like assigning, it does not check that you hold what the role grants, so `roles:write` is root-equivalent: give it only to people you would trust with full access. A project's own role must name only that project (else `422 ROLE_POLICY_INVALID`). You cannot remove the last platform-wide full-access (`*` on `*`) assignment: the change fails with *This would leave nobody with full access.*

New users and new global agents get the **default role**. Administration, Global Roles, "Set as default role" changes which role it is; the default role cannot be deleted.

## Let people assign roles (`roles:assign`)

Which roles a person may assign is decided by the *resource* of their `roles:assign` statement:

| Resource | Lets them assign |
|---|---|
| `role/<roleId>` | that one workspace role, platform-wide (to a user or global agent) |
| `role/*` | every workspace role, platform-wide |
| `project/<projectId>/role/<roleId>` | that role inside project P (a project role, a workspace role or a template: the attachment lives in P) |
| `project/<projectId>/role/*` | any role inside project P |
| `*` | everything (root-equivalent) |

Changing a project member's roles needs only `roles:assign`. Adding a member or creating a project agent also needs its own gate (`project.members:write` / `agents:write`), and a global agent's roles need `agents:write` on the agent. See the three examples at the end of the worked examples below.

## Worked examples

In the JSON below replace `PROJECT_ID`, `AGENT_ID`, `SPRINT_ID` and so on with the real ids. The first four examples are written so that they work both as a project's own role and as a reusable platform role.

### Full access

Everything, workspace-wide (what `SUPER_ADMIN` has). Attach it platform-wide:

```json
{
  "version": "2026-10-01",
  "statements": [
    { "sid": "FullAccess", "effect": "Allow", "actions": ["*"], "resources": ["*"] }
  ]
}
```

Full access to one project only (the project `Admin` role):

```json
{
  "version": "2026-10-01",
  "statements": [
    { "sid": "FullAccess", "effect": "Allow", "actions": ["*"], "resources": ["project/PROJECT_ID/*"] }
  ]
}
```

### Read-only role

Read everything in a project, change nothing. This is a project role, so it names its own project. (As a workspace role you could write `project/*` instead, which makes it a template: attach it to someone in one project and it only works there.)

```json
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
```

### Developer in one project

Work on tasks, documents, agents and environments of a single project. The member can see nothing in any other project.

```json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "Developer",
      "effect": "Allow",
      "actions": [
        "tasks:read", "tasks:write", "sprints:read", "views:read", "views:write",
        "docs:read", "docs:write", "workflows:read",
        "annotations:read", "annotations:write", "annotations:resolve",
        "agents:read", "agents:write", "conversations:read", "conversations:write",
        "environments:read", "environments:connect", "project.members:read"
      ],
      "resources": ["project/PROJECT_ID/*"]
    },
    { "sid": "SeeProject", "effect": "Allow", "actions": ["projects:read"], "resources": ["project/PROJECT_ID"] }
  ]
}
```

Attach it platform-wide or to the member in that project: both give the same result, because the resources name the project.

### Restrict an agent

Keep one agent away from everybody who holds this role. Create a role with a `Deny` and attach it to the people who should not use the agent. A Deny beats any Allow they have from other roles.

```json
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
```

The agent disappears from their agent list and its chats (and configuration) are refused. This replaces the old *restricted access* setting, which is **not migrated** on upgrade: an agent or environment that was restricted before the upgrade is open until you create a Deny like this one. To make an agent usable by only a few people instead, attach this Deny to the whole team and exempt the allowed users with a condition:

```json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "OnlyThese",
      "effect": "Deny",
      "actions": ["agents:*", "conversations:*"],
      "resources": ["project/PROJECT_ID/agent/AGENT_ID/*"],
      "conditions": { "NotIn": { "principal.id": ["USER_ID_1", "USER_ID_2"] } }
    }
  ]
}
```

Use this form to recreate a formerly restricted agent: list the users and agents that had been granted access in `NotIn`, and attach the role to every member of the project (the old grant lists remain in the `agent_access_grants` and `environment_access_grants` tables until a later migration drops them). Environments work the same way, see [Deny production environments](#deny-production-environments).

### Deny production environments

Environments are denied by id (there is no "production" flag). List the production environments and deny what you want blocked. Here, no shell and no changes:

```json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "NoProduction",
      "effect": "Deny",
      "actions": ["environments:connect", "environments:write"],
      "resources": [
        "project/PROJECT_ID/environment/PROD_ENV_ID_1/*",
        "project/PROJECT_ID/environment/PROD_ENV_ID_2/*"
      ]
    }
  ]
}
```

Use `"actions": ["environments:*"]` to hide them completely. You can also deny by runtime with `environment.type`, for example no shell into any Kubernetes environment in any project:

```json
{
  "version": "2026-10-01",
  "statements": [
    {
      "effect": "Deny",
      "actions": ["environments:connect"],
      "resources": ["project/*/environment/*"],
      "conditions": { "StringEquals": { "environment.type": "kubernetes" } }
    }
  ]
}
```

### Limit tasks, sprint and views to one sprint

Let a contractor read and update only the tasks of one sprint, and open only that sprint's views:

```json
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
```

The first statement is for the project-level checks that let requests in (listing tasks, creating one; listing and creating views); it names the project itself, not what is inside it. The second is what limits access to individual tasks: task lists return only that sprint's tasks (filtered by the database, so pages and counts are right), opening another sprint's task is refused, and a task can neither be created without that sprint nor moved out of it.

Views are limited the same way, with `view.sprint_id` (the sprint a view belongs to): the view list of that sprint returns its views, the lists of other sprints are empty, and opening another sprint's view is refused. Backlog and timeline views are project-level and belong to no sprint, so the attribute is absent for them: a positive condition (`StringEquals`) excludes them, a negated one (`StringNotEquals`) includes them. To let the member create, rename, reorder and delete views of that sprint, add `views:write` to both the OpenProject statement (the project-level check) and the ViewsOfThisSprint statement (the per-view check). A view is created in the sprint named by the `sprint_id` query parameter of `POST /projects/{projectId}/views`, so creating one in another sprint, or a backlog or timeline view, is refused, and reordering is refused if any listed view is not allowed. No list of view ids is needed.

The same pattern limits documents to a folder and its subfolders:

```json
{
  "effect": "Allow",
  "actions": ["docs:read"],
  "resources": ["project/PROJECT_ID/doc/*"],
  "conditions": { "In": { "doc.ancestor_folder_ids": ["FOLDER_ID"] } }
}
```

(plus a `docs:read` on `project/PROJECT_ID`, as in the first statement above).

### A workspace role that gives access to two projects

Create a workspace role (Administration, Roles, or `POST /admin/roles`) that names two projects. It is not a template, so it can be attached platform-wide to a user or global agent, and it reaches exactly those two projects and nothing else:

```json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "TwoProjects",
      "effect": "Allow",
      "actions": ["projects:read", "tasks:read", "tasks:write", "sprints:read", "docs:read"],
      "resources": [
        "project/PROJECT_A_ID/*",
        "project/PROJECT_B_ID/*"
      ]
    }
  ]
}
```

The same policy on a project's own Roles page is refused (`422 ROLE_POLICY_INVALID`, issue at `statements[0].resources[1]`), because a project role may only name its own project.

### Let project leads assign only Editor and Viewer

Attach this role to the leads of project P. They can manage members and attach the Editor and Viewer roles of the project, nothing else (an attempt to assign Admin, or to take Admin away from someone, is refused with `403 FORBIDDEN`). `EDITOR_ID` and `VIEWER_ID` are the ids of those two roles of project P:

```json
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
      "sid": "AssignEditorAndViewer",
      "effect": "Allow",
      "actions": ["roles:assign"],
      "resources": [
        "project/PROJECT_ID/role/EDITOR_ID",
        "project/PROJECT_ID/role/VIEWER_ID"
      ]
    }
  ]
}
```

(As a project's own role, the leads keep their other access through the `Editor` or another role you give them as well; this role only adds the hand-out.)

### Let a support admin assign any workspace role

A workspace role for people who manage accounts and may give any workspace role (to users and global agents), but who cannot edit role definitions. `users:read`, `roles:read` and `agents:*` let them see who and what they assign:

```json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "AssignAnyWorkspaceRole",
      "effect": "Allow",
      "actions": ["roles:assign", "roles:read", "users:read", "agents:read", "agents:write"],
      "resources": ["user", "user/*", "role", "role/*", "agent", "agent/*"]
    }
  ]
}
```

This is close to full control of the workspace, because `role/*` includes the `SUPER_ADMIN` role. To hand out only some roles, list their ids as `role/ROLE_ID` instead of `role/*`.

### Let someone assign any role in one project

`project/PROJECT_ID/role/*` covers every role as seen inside project P. Together with `project.members:write` this is what the project `Admin` role already has through `*`:

```json
{
  "version": "2026-10-01",
  "statements": [
    {
      "sid": "ManageMembers",
      "effect": "Allow",
      "actions": ["project.members:read", "project.members:write"],
      "resources": ["project/PROJECT_ID"]
    },
    {
      "sid": "AssignAnyRoleInThisProject",
      "effect": "Allow",
      "actions": ["roles:assign"],
      "resources": ["project/PROJECT_ID/role/*"]
    }
  ]
}
```

## Testing a policy

In the Advanced editor, **Simulate** asks "would this request be allowed?" for an action, resource and optional attribute values, and shows which statements matched. Over the API use `POST /roles/simulate`; naming a principal evaluates their existing roles together with the policy. The conditions use only the attributes you give, so supply `task.sprint_id` to test a sprint-limited role.

## Troubleshooting

| Symptom | Cause |
|---|---|
| Save fails with `unknown action` | Typo, or the plugin that defines it is not installed. Check `GET /roles/actions`. |
| `condition key ... applies to "task" resources, which this statement's resources do not cover` | Put the condition in a statement whose `resources` include `project/<id>/task/*` (or a broader `project/<id>/*`). |
| `422 ROLE_POLICY_INVALID` at `statements[i].resources[j]` on a project's Roles page | A project role may only name `project/<its own id>` or `project/<its own id>/...`. Create a workspace role to reach other projects. |
| `422 ROLE_NOT_ATTACHABLE` | The role's resources all have a wildcard project segment (`project/*`). Attach it per project, or name specific projects. |
| `403` saving a role | You lack `roles:write` on the role's scope (platform roles, or the project's roles). What the role grants is not checked. |
| `422 ROLE_POLICY_INVALID` saving a project role | The policy names a resource outside the project. |
| `403` assigning (or removing) a role | You need `roles:assign` on that role's resource (`role/<id>`, or `project/<projectId>/role/<id>` inside a project) for every role the request adds or removes. Adding a project member additionally needs `project.members:write`, and creating a project agent `agents:write`. |
| `409 ROLE_LAST_FULL_ACCESS` | The change would remove the last platform-wide full-access assignment. Attach another full-access role first. |
| A member sees an empty project | They have no role attached in that project, or only Deny roles. |
| A member sees the project but a Deny role does not seem to apply | The role must be attached to *that person*, and its resources must name the thing (`.../agent/<id>/*`). |
