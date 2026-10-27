---
name: paca-automation
description: Design, build, review and fix an automation in Paca — an event-driven graph of Trigger, Condition and Action nodes (status changed, task created, due date reached, cron, API call; assign, set status, set priority, tag, set custom field, trigger an AI agent, call an API). Use when asked to automate a process, auto-assign work, chain statuses, hand work to an agent when something happens, wire task dependencies, or to edit, pause, activate or archive an existing automation.
compatibility: Requires Paca MCP server. Run /paca-setup if Paca tools are not available.
---

You are designing an automation in Paca. An automation is a directed acyclic graph: **Trigger** nodes start it, optional **Condition** nodes branch it (an N-way switch on task fields, with an `else` fallback), and **Action** nodes change things. Use Paca MCP tools throughout — never create local files.

Automations act on real tasks and can notify or call external systems. **Design on paper, build as a draft, verify, and only then activate** — and confirm with the user before activating anything that calls an API or triggers an agent.

**If no goal is specified**, call `get_automation` without `automationId` to list the project's automations and ask what should happen automatically.

---

## Step 1 — Write the rule in one sentence

"When <event>, if <condition>, then <action>." If you cannot say it in one sentence, split it into several automations. If the goal is vague, ask the one question that fixes it (via `ask_user_question` when choices are a short list), recommending an answer.

## Step 2 — Gather ids before touching a tool

- `list_task_statuses` (status ids), `list_project_members` (member ids — humans and agents), `list_custom_fields`, `list_task_types`.
- `get_automation` for existing automations: **prefer editing over duplicating**, and check that the new one cannot fight an existing one (two automations setting the same task's status can loop or flip-flop).

## Step 3 — Pick node types

| Need | Node |
|---|---|
| Task enters a status / is created / changes assignee or priority / gets a tag | Trigger `status_changed`, `task_created`, `assignee_changed`, `priority_changed`, `tag_added` |
| Deadline | Trigger `due_date_reached` (offset in minutes) |
| Hand-off: start B when A is done | Trigger `predecessor_done` with `watched_task_ids` (fires once all are done) and `target_task_id` |
| Recurring | Trigger `cron` (5-field, **UTC**) |
| From outside Paca | Trigger `api_trigger` (the webhook secret can only be generated in the web UI — tell the user) |
| Branch on status / type / priority / assignee / tags / custom field | Condition (`branches` with `handle`, an `else` fallback is shared) |
| Judgment a field comparison cannot make | Condition `jev_condition` — only if Jev is configured on the project |
| Change the task | Action `assign`, `set_status`, `set_priority`, `add_tag`, `set_custom_field` |
| Give work to an AI agent | Action `trigger_ai_agent` with the agent's `member_id` (and a `message` if there is no target task) |
| Call a system | Action `call_api` (http(s) only) |

Actions can retarget with `target` (`parent`, `children`, `blocks`, `is_blocked_by`, … or `other` with an id) — e.g. when the last child is done, `set_status` on the `parent`. A trigger with no `target_task_id` may only connect to `call_api` / `trigger_ai_agent`.

## Step 4 — Build as a draft

`create_automation` with the whole graph in one call: give every node a `ref` so edges in the same call can wire them (`{source:'t1', target:'a1'}`; for a condition source add the branch `sourceHandle`). Lay nodes out left-to-right with distinct `posX`/`posY` (≥200 apart). It starts as `draft` and the engine ignores it.

For an existing automation use `update_automation` with every touched node in **one** `nodes.set` array (omit `id` to create, pass `id` to update). Each item is applied independently: read the response for `failed` items (a cycle, a bad handle) and fix them.

## Step 5 — Verify, then activate

1. `get_automation` with the id and walk the graph against the sentence from Step 1: every trigger reaches an action, every condition branch (including `else`) goes somewhere or deliberately nowhere.
2. Dry-run mentally with one real task: which node fires, what changes.
3. Ask before activating, then `update_automation` with `status: "active"` (needs ≥1 Trigger, ≥1 Action, acyclic).

## Pausing and retiring

**Pause** = `status: "draft"` (keeps it editable). **Archive** = `status: "archived"` is permanent and locks the graph — confirm the user does not just mean pause. `delete_automation` is permanent: confirm by name.

Report back: automation name and id, the rule in one sentence, its status, and who or what it will touch.

---

## If Paca MCP is not connected

> Paca MCP tools are not available. Run `/paca-setup` to configure the connection.

---

## Tool reference

**Automations:** `get_automation` (lists when `automationId` is omitted) · `create_automation` · `update_automation` · `delete_automation`
**Lookups:** `list_task_statuses` · `list_project_members` · `list_custom_fields` · `list_task_types` · `list_tasks` · `list_projects`
