# Agent Skills Plugin System

> **How plugin skills reach agents today**: `services/api` reads each enabled
> plugin's skills from the local skills store and serves them from
> `GET /api/v1/skills` (CLI installs) and `GET /api/v1/skills?target=agent`
> (`services/agent-runner`, which re-fetches every 30 seconds and writes them
> to the conversation's sandbox next to the bundled defaults and the agent's
> own skills). The sections below marked *historical* describe the earlier
> `services/ai-agent` design (HTTP fetch of `baseUrl`, per-conversation); the
> end result — install/enable/disable takes effect on a following
> conversation — is preserved. See [default-skills.md](../ai-agent/default-skills.md)
> for the remaining gaps (no per-trigger-type skills, no keyword-triggered
> selection).

## Overview

Paca agents are *meant* to support **plugin-contributed Agent Skills**. Each installed Paca plugin can ship one or more `SKILL.md` files (the same [AgentSkills](https://agentskills.io/specification) format used by Paca's own bundled default skills — see [default-skills.md](../ai-agent/default-skills.md)) as part of its install artifact. When an agent conversation starts, the agent execution service is meant to fetch the skill content for every enabled plugin that declares a `skills` section in its manifest and merge it into that conversation's skill list.

From the model's perspective, plugin skills are meant to appear alongside the agent's own configured skills and Paca's bundled defaults — no visible distinction beyond the name.

## Architecture (historical — as implemented by `services/ai-agent`)

Unlike [MCP tools](mcp-plugin-system.md), which are loaded by a separate Node.js process (`apps/mcp`) that dynamically imports executable code, plugin skills are meant to be inert markdown text, consumed directly by the agent execution service — no code execution, no separate loader process.

```
Plugin install (marketplace or direct)
    │
    │ skills_tar_gz_url artifact
    ▼
services/api Installer
    │
    └── extract to {SkillsDir}/{pluginId}/{skillName}/SKILL.md
            served statically at /plugins-skills/{pluginId}/{skillName}/SKILL.md

Conversation start (services/ai-agent, historically; agent-runner now gets the same result via GET /api/v1/skills?target=agent)
    │
    ├── SELECT name, manifest FROM plugins WHERE enabled = true
    │       │
    │       └── For each plugin with manifest.skills:
    │               resolve baseUrl against GATEWAY_BASE_URL (SSRF-checked)
    │               GET {baseUrl}/{skillName}/SKILL.md  for each declared name
    │               parse frontmatter + body → openhands.sdk.context.Skill
    │
    └── merge_skills_by_name(agent's own skills,
            merge_skills_by_name(plugin skills, bundled defaults))
```

### Key Components (historical — `services/ai-agent`'s Python source, now removed)

| Component | Location | Purpose |
|---|---|---|
| `SkillsManifest` | `services/api/internal/domain/plugin/entity.go` | Parses the manifest's `skills` section — still live, `services/api`-owned |
| `Installer` | `services/api/internal/platform/plugin/installer.go` | Extracts the skills tarball to the local skills store — still live, `services/api`-owned |
| `list_enabled_plugin_skills` | *(removed)* `services/ai-agent/src/repositories/agent_repository.py` | Queried enabled plugins and resolved each skill's URL |
| `resolve_plugin_base_url` | *(removed)* `services/ai-agent/src/repositories/agent_repository.py` | SSRF guard, mirrored `apps/mcp/src/plugin-loader.ts`'s `resolveImportUrl` |
| `load_plugin_skills` | *(removed)* `services/ai-agent/src/agent/builder.py` | Fetched each `SKILL.md` and parsed it into an SDK `Skill` |

## Plugin Manifest

Add a `skills` section to your `plugin.json`:

```json
{
  "id": "com.example.my-plugin",
  "displayName": "My Plugin",
  "version": "1.0.0",
  "skills": {
    "baseUrl": "/plugins-skills/com.example.my-plugin",
    "names": ["paca-pr-review", "paca-changelog"]
  }
}
```

- `baseUrl` is the root URL of your extracted skills bundle. Like `mcp.remoteEntryUrl`, it may be relative (resolved against the gateway) or absolute.
- `names` lists the skill directory names available under `baseUrl` — declared explicitly because the static file server does not support directory listing. Each name must exist as `{baseUrl}/{name}/SKILL.md` in your artifact and follow the AgentSkills naming convention (lowercase alphanumeric segments joined by single hyphens, e.g. `paca-pr-review`).
- **All skill names must start with `paca-`** — this is the naming convention every skill in the Paca ecosystem follows (the bundled defaults are `paca-do`, `paca-doc`, etc.), so a plugin's contributed skills read as clearly Paca-related wherever they surface — in `<available_skills>`, and as slash commands once installed via `install-paca-skills.sh`. This is a separate convention from MCP tool naming (which prefixes with your own plugin name, e.g. `checklist_`, `bdd_` — see [mcp-plugin-system.md](mcp-plugin-system.md#tool-naming)) since tools need to avoid colliding with *other plugins*, while skills all share one `paca-` namespace by design.
- Names may not start with `paca-trigger-` — that prefix is reserved for Paca's own fixed per-conversation scaffolding skills.
- `routes` (optional) tells the core `paca` router skill when to send the user to one of your skills. Each entry has a `skill` (one of your `names`) and a `when` that completes "If the user wants to…":

  ```json
  "skills": {
    "baseUrl": "/plugins-skills/com.example.my-plugin",
    "names": ["paca-pr-review"],
    "routes": [
      { "skill": "paca-pr-review", "when": "Review a pull request against a task's acceptance criteria" }
    ]
  }
  ```

  Each route becomes one row in the router's "Route to a specialized skill" table (`/paca-pr-review` for CLI installs), directly after the bundled rows. Rows appear only while the plugin is installed and enabled and its skill is actually served, so the router never mentions a skill that is not there. `when` must be a single line of at most 200 characters, without `|` or backticks; a route for a skill not in `names`, an empty `when`, or two routes for one skill are rejected at install time. The row is written in each flavor's own routing syntax — a slash command for CLI installs (`GET /api/v1/skills`), `load_skill(name: "…")` for the in-product agent (`?target=agent`). `services/agent-runner` re-fetches that set every 30 seconds rather than once per process, so installing, enabling or disabling a plugin shows up in the router on a following conversation.

## Publishing a skills bundle

Your plugin's install artifacts can include a `skills_tar_gz_url` tarball (alongside `backend_tar_gz_url` / `frontend_tar_gz_url` / `mcp_tar_gz_url`) containing one directory per skill:

```
skills/
  paca-pr-review/
    SKILL.md
  paca-changelog/
    SKILL.md
```

Each `SKILL.md` follows the standard AgentSkills format — YAML frontmatter plus a markdown body:

```markdown
---
name: paca-pr-review
description: Reviews a pull request against the project's coding conventions.
triggers:
  - /paca-pr-review
---

# PR Review Skill

You are reviewing a pull request...
```

`name` in the frontmatter must match the directory name (and the `names` entry in your manifest). `triggers` is optional — omit it for an always-active skill (its content is injected into every conversation's system prompt); declare it for a model-selectable skill invoked on demand (progressive disclosure), the same distinction Paca's own default skills make (see [default-skills.md](../ai-agent/default-skills.md)).

The installer extracts this tarball to `{PLUGINS_SKILLS_DIR}/{pluginId}/` and the gateway serves it at `/plugins-skills/{pluginId}/`.

## Precedence (as designed)

When a skill name collides across sources, precedence is meant to be: **agent-configured skills > plugin skills > bundled defaults**. A user's own skill for their agent always wins; a plugin skill only fills in where the agent hasn't defined one; Paca's bundled defaults are the fallback. Paca's reserved `paca-trigger-*` scaffolding skills (see [default-skills.md](../ai-agent/default-skills.md)) are meant to sit outside this precedence entirely — no skill from any source may use that name prefix. The name-prefix rejection is enforced at plugin-install validation time in `services/api` (Go). On `agent-runner` the bundled and plugin skills arrive as one list ahead of the agent's own skills, so the by-name precedence above is not enforced there beyond the API's own rejection of plugin names that collide with bundled ones.

## Loading Behaviour (historical — `services/ai-agent`, not currently reproduced)

1. On every conversation start, the agent execution service queried `SELECT name, manifest FROM plugins WHERE enabled = true` — no caching, so an install/enable/disable took effect on the next conversation.
2. Plugins without a `skills` section in their manifest were skipped.
3. `baseUrl` was resolved against `GATEWAY_BASE_URL` and checked against the same SSRF allowlist `apps/mcp`'s MCP loader already applies to `remoteEntryUrl`: `https://` allowed unless the hostname resolves to a private/internal IP; `http://` allowed only for localhost or the configured gateway host.
4. Each declared skill was fetched individually. A fetch or parse failure for one skill was logged and that skill skipped — it didn't affect any other skill or fail the conversation.

## Security Considerations

- A plugin manifest is admin-installed but still treated as untrusted input for URL resolution purposes — the SSRF guard existed because a malicious or misconfigured `baseUrl` could otherwise have made the fetching service reach an internal service (e.g. a cloud metadata endpoint) and inject the response into every conversation as skill content. On the current path `services/api` reads skill files from its own local store rather than fetching `baseUrl`, so this guard is not needed there.
- Skill content itself is **not sandboxed** — it becomes part of the model's context exactly like any other skill. Only install plugins from trusted sources.

## Comparison with the MCP Plugin System

| Aspect | MCP (`mcp.remoteEntryUrl`) | Skills (`skills.baseUrl`) |
|---|---|---|
| Content | Executable ESM module | Static markdown (`SKILL.md`) |
| Loader | Separate Node.js process (`apps/mcp`), per conversation | `services/api` serves the files from its local skills store; `agent-runner` re-fetches the set every 30 seconds (historically an in-process Python fetch in `services/ai-agent`, per conversation) |
| Manifest shape | Single URL | Base URL + explicit list of names |
| Consumed via | `import(url)` | Plain HTTP `GET` |
| Failure isolation | Whole plugin's tools unavailable | Per-skill — other skills from the same plugin still load |
