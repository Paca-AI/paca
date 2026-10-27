---
name: paca-team
description: Inspect and manage who has access to a Paca project — list members (humans and AI agents) with their roles and effective permissions, find over-privileged or missing access, and add, change or remove members after confirmation. Use when asked who can do what, to add or remove someone, to give an agent or person access, or to audit project access.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are inspecting or changing project membership in Paca. Access is controlled by IAM-style roles (see `/paca-role-policy` for drafting a new role's JSON policy). Use Paca MCP tools throughout — never create local files.

**Reading is free; writing is not.** Adding, re-roling and removing members changes what people and agents can do. State exactly what you will change and wait for an explicit yes before each write. Never remove a member or change a role on a guess about who someone is.

---

## Step 1 — See the current state

1. `list_project_members` — who is in the project, humans and agents, with their roles.
2. `list_project_roles` — the project's own roles plus attachable workspace roles.
3. `get_my_project_permissions` — the actions *you* hold. You can only do what these allow; if a write is denied, say which permission is missing rather than retrying.

## Step 2 — Answer the question asked

- **"Who can do X?"** map X to an IAM action (e.g. `tasks:write`) and list the roles that include it, then the members holding those roles.
- **Audit:** flag members with broad write/admin roles who look like they need less, agents with more access than the work they are assigned requires, members holding no role, and roles nobody holds.
- **"Give X access to Y":** pick the least-privileged existing role that covers the need. If none fits, offer `/paca-role-policy` to draft one instead of broadening an existing role.

## Step 3 — Write, with confirmation

- Add: `add_project_member` with `roleIds`.
- Change: `update_project_member_role` **replaces the whole set** of `roleIds` — send the full intended set, including roles the member should keep.
- Remove: `remove_project_member` — confirm the member by name and say what they lose; check they are not the only holder of an admin role first.

Re-read with `list_project_members` afterwards and report the new state.

Report back: members changed, before → after roles, and any audit findings left for a human.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Members:** `list_project_members` · `add_project_member` · `update_project_member_role` · `remove_project_member` · `get_my_project_permissions`
**Roles:** `list_project_roles` (create/update/delete exist but belong to `/paca-role-policy` drafting and a human's review)
