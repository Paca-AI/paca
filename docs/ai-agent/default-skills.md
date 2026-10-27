# AI Agent — Default Skill Set

> **Known gap as of `services/agent-runner`**: everything below describes the
> product's intended behavior, which `services/ai-agent` (now fully removed
> from the repository) implemented.
> `services/agent-runner` does **not** currently merge in Paca's default
> skill set or any plugin-contributed skills, and has no equivalent to the
> fixed per-trigger-type skills described below — it only injects whatever
> rows exist in that specific agent's own `agent_skills` table (see
> [agent-runner-service.md](agent-runner-service.md#skills--mcp-server-injection)).
> An `llm`-type agent with no skills explicitly configured on it gets none
> of the guidance this document describes. Porting the default-skill merge
> and trigger-context skills to `services/agent-runner` is open follow-up
> work, not yet scheduled.

Every agent is *meant* to automatically get Paca's default skill set, in addition to whatever skills the user configures for that agent (`agent_skills` table, edited from the agent's Skills tab) and whatever skills are contributed by enabled plugins (see [skills-plugin-system.md](../plugins/skills-plugin-system.md)). Neither `ai-agent` nor `agent-runner` has a local skills directory of its own — all default skill content lives as embedded Markdown in `services/api/internal/platform/bundledskills/content/` and is served over `GET /api/v1/skills?target=agent`. This endpoint, and the merge-at-conversation-start behavior described below, are unchanged by the migration — they're `services/api`'s own responsibility either way; what changed is that nothing on the `agent-runner` side calls this endpoint or performs the merge yet (see the gap notice above).

The same `bundledskills` package also serves a second flavor — `GET /api/v1/skills` (default, `target=cli` or omitted) — consumed by `scripts/install-paca-skills.sh` for Claude Code/Gemini CLI/Cursor/AGENTS.md installs and by ACP-bridge agents. Each skill is authored **once** as `content/<name>.md` (the cli-flavor content); `List()` derives the agent-flavor variant mechanically by condition: a `compatibility: …` frontmatter line becomes a `triggers: […]` one, and a "if Paca MCP is not connected" fallback section (irrelevant to a conversation that's always connected) is dropped. Only two skills have a `content/<name>.agent.md`, hand-authoring a separate agent-flavor body, because their *content* genuinely differs — `paca` (the agent flavor is meant to use an `invoke_skill`-style tool for routing; the cli flavor suggests slash commands, since external CLIs have no equivalent tool) and `paca-do` (the agent flavor adds a clone/push/PR section, since the sandboxed agent has no local git checkout the way an external CLI session already does) — plus `paca-setup`, which is `CLIOnly` (the in-product agent's MCP server is always auto-configured, so there's nothing to set up). See that file's package doc comment for the exact rule.

## Two formats, two behaviors (as designed for `services/ai-agent`; not yet true for `agent-runner`)

- **`paca.md`** — a plain Markdown file (not named `SKILL.md`), Paca's baseline operating procedure. It has no `triggers:` frontmatter, so it's meant to be treated as *legacy format with `trigger=None`*: its full content always injected into every conversation's system prompt, regardless of how the agent was invoked. It routes to the specialized skills below and includes a task-status → skill routing table (e.g. in-progress → `paca-do`, in-review → `paca-test`) so the model picks the right one once it reads the task via the Paca MCP tool.
- **`paca-do/`, `paca-clarify/`, `paca-breakdown/`, `paca-doc/`, `paca-epic/`, `paca-estimate/`, `paca-prioritize/`, `paca-role-policy/`, `paca-sprint/`, `paca-test/`, `paca-automation/`, `paca-review/`, `paca-triage/`, `paca-retro/`, `paca-feedback/`, `paca-board/`, `paca-team/`** — each a `<name>/SKILL.md` directory (the AgentSkills standard). These are meant to be *model-selectable*: listed by name and description in `<available_skills>`, full content read on demand (progressive disclosure), auto-injected on typing e.g. `/paca-do #42` via each one's `triggers: ["/<name>"]`. `services/agent-runner` has no equivalent to trigger-based (keyword-activated) skill selection at all yet — see `agent.Skill`'s doc comment in the Go source — so even an agent with these skills explicitly attached would get every enabled one injected unconditionally on every turn, not selected on demand.

`paca-role-policy` helps a user *draft* the JSON policy of a role (see [Roles and policies](../guides/roles-and-policies.md)): it clarifies who/what/where, then outputs one JSON policy plus a plain-language summary for the user to paste into the role editor's Advanced (JSON) view. It is draft-only by design and its content forbids creating, updating, attaching or deleting a role or claiming it did. It embeds the built-in action list, resource forms, condition keys and scope rules; a bundled-skills test checks every action and JSON example in it against the real IAM registry and validator, so adding an action to `iam.BuiltinActions()` requires listing it in the skill. It has only a cli-flavor file (agent flavor derived) and is routed to from the `paca` skill.

`paca-setup` (used to wire the Paca MCP server into a Claude Code session) is intentionally **not** ported to the agent flavor — the in-product agent always has its MCP server auto-configured, so there's nothing to set up.

## Action-type context, not free-text prompts

The old per-agent `task_trigger_prompt` / `doc_comment_trigger_prompt` / `chat_trigger_prompt` / `description_write_trigger_prompt` columns are gone. `services/ai-agent` replaced them with fixed constants in `trigger_skills.py`, appended as an always-active skill named `paca-trigger-task-assigned` / `paca-trigger-doc-comment` / `paca-trigger-chat` / `paca-trigger-description-write` depending on the trigger type — deterministic scaffolding for the current conversation, not something users, plugins, or the model could edit or discover on its own. `services/agent-runner` has no equivalent: `internal/executor/prompt.go`'s `buildInitialMessage` appends only a plain "Action type: …" line (task assignment vs. comment vs. chat vs. description-write) plus whatever trigger IDs are present, with no per-trigger-type skill content injected.

The reserved-name mechanism this relied on is still live and enforced in `services/api` regardless: the API rejects any user-created skill or plugin-declared skill using one of the four `paca-trigger-*` names (`agentdom.ReservedSkillNames` in `internal/domain/agent`, checked from both `agent_service.go`'s skill validation and the `plugin` domain's manifest validation) — so a future port of this mechanism to `agent-runner` has that name collision already guarded against at the API layer.

## Adding or changing a default skill

Each skill is a Markdown file in `services/api/internal/platform/bundledskills/content/`, embedded into the binary with `go:embed`. Edit `content/<name>.md` (the cli-flavor body); `List()` derives the agent-flavor variant automatically, so most skills need nothing else. To add a skill, write `content/<name>.md` and add one line to `skillEntries` in `catalog.go`. Only add `content/<name>.agent.md` (and set `AgentVariant`, plus `AgentPath` for the one legacy-format exception) when a skill's content genuinely needs to differ by agent type — today `paca` and `paca-do`. A missing file panics at init and is caught by the package tests.

This requires only a `services/api` rebuild/redeploy to take effect for the `target=cli`/`install-paca-skills.sh` and ACP-bridge paths — those fetch this content live over HTTP. It currently has **no effect at all** on `llm`-type agents run through `services/agent-runner`, per the gap notice at the top of this document.

For a plugin author wanting to contribute a skill instead of editing this bundled set, see [skills-plugin-system.md](../plugins/skills-plugin-system.md).

## Engineering practices and Paca-feature skills

The skill set follows [mattpocock/skills](https://github.com/mattpocock/skills/tree/main/skills/engineering): small, single-purpose skills that interview before building, work in thin vertical slices, and leave a written record. Two layers:

**Practices inside existing skills.** `paca-do` builds code test-first in vertical slices, diagnoses non-obvious bugs feedback-loop-first (reproduce, ranked hypotheses, regression test) and opens PRs with a summary / evidence / risk body; `paca-breakdown` favors tracer-bullet slices and records order as real `blocks` task links; `paca-clarify` asks one recommended-answer question at a time, verifies before asking, records glossary terms and decisions in docs and writes agent-ready briefs; `paca-test` records exact reproductions.

**Skills built on Paca's own features.**

| Skill | Paca feature | Engineering analogue |
|---|---|---|
| `paca-review` | linked change requests, review threads, CI checks | code-review, pr |
| `paca-triage` | task types, statuses, links, search | triage |
| `paca-retro` | sprints, activities, docs, estimates | retro |
| `paca-feedback` | page comments (annotations) from the browser extension | triage of reported issues |
| `paca-board` | task types, statuses, custom fields, views | setup |
| `paca-team` | project members, roles, permissions | — |
| `paca-automation` | automations (Trigger → Condition → Action graphs) | — |

`paca-automation` **replaces `paca-workflow`**, which described the removed workflow-node tools (`get_workflow` / `create_workflow`); the MCP server now exposes `get/create/update/delete_automation`. Machines that ran the installer earlier keep a stale `paca-workflow` directory until they remove it (`rm -rf ~/.claude/skills/paca-workflow`).

`TestList_EngineeringPractices` and `TestList_PacaFeatureSkills` guard all of this.
