# Agent Skills Plugin System

## Overview

Paca agents support **plugin-contributed Agent Skills**. Each installed Paca plugin can ship one or more `SKILL.md` files (the same [AgentSkills](https://agentskills.io/specification) format used by Paca's own bundled default skills — see [default-skills.md](../ai-agent/default-skills.md)) as part of its install artifact. When an agent conversation starts, the ai-agent service fetches the skill content for every enabled plugin that declares a `skills` section in its manifest and merges it into that conversation's skill list.

From the model's perspective, plugin skills appear alongside the agent's own configured skills and Paca's bundled defaults — there is no visible distinction beyond the name.

## Architecture

Unlike [MCP tools](mcp-plugin-system.md), which are loaded by a separate Node.js process (`apps/mcp`) that dynamically imports executable code, plugin skills are inert markdown text consumed directly by the Python `ai-agent` service — no code execution, no separate loader process.

```
Plugin install (marketplace or direct)
    │
    │ skills_tar_gz_url artifact
    ▼
services/api Installer
    │
    └── extract to {SkillsDir}/{pluginId}/{skillName}/SKILL.md
            served statically at /plugins-skills/{pluginId}/{skillName}/SKILL.md

Conversation start (services/ai-agent)
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

### Key Components

| Component | Location | Purpose |
|---|---|---|
| `SkillsManifest` | `services/api/internal/domain/plugin/entity.go` | Parses the manifest's `skills` section |
| `Installer` | `services/api/internal/platform/plugin/installer.go` | Extracts the skills tarball to the local skills store |
| `list_enabled_plugin_skills` | `services/ai-agent/src/repositories/agent_repository.py` | Queries enabled plugins and resolves each skill's URL |
| `resolve_plugin_base_url` | `services/ai-agent/src/repositories/agent_repository.py` | SSRF guard, mirrors `apps/mcp/src/plugin-loader.ts`'s `resolveImportUrl` |
| `load_plugin_skills` | `services/ai-agent/src/agent/builder.py` | Fetches each `SKILL.md` and parses it into an SDK `Skill` |

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

## Precedence

When a skill name collides across sources, precedence is: **agent-configured skills > plugin skills > bundled defaults**. A user's own skill for their agent always wins; a plugin skill only fills in where the agent hasn't defined one; Paca's bundled defaults are the fallback. Paca's reserved `paca-trigger-*` scaffolding skills (see [default-skills.md](../ai-agent/default-skills.md)) sit outside this precedence entirely — no skill from any source may use that name prefix, enforced both at plugin-install validation time (Go) and as a runtime guard (Python).

## Loading Behaviour

1. On every conversation start, `ai-agent` queries `SELECT name, manifest FROM plugins WHERE enabled = true` — no caching, so an install/enable/disable takes effect on the next conversation.
2. Plugins without a `skills` section in their manifest are skipped.
3. `baseUrl` is resolved against `GATEWAY_BASE_URL` and checked against the same SSRF allowlist `apps/mcp`'s MCP loader already applies to `remoteEntryUrl`: `https://` is allowed unless the hostname resolves to a private/internal IP; `http://` is allowed only for localhost or the configured gateway host.
4. Each declared skill is fetched individually. A fetch or parse failure for one skill is logged and that skill is skipped — it does not affect any other skill or fail the conversation.

## Security Considerations

- A plugin manifest is admin-installed but still treated as untrusted input for URL resolution purposes — the SSRF guard exists because a malicious or misconfigured `baseUrl` could otherwise make `ai-agent` fetch from an internal service (e.g. a cloud metadata endpoint) and inject the response into every conversation as skill content.
- Skill content itself is **not sandboxed** — it becomes part of the model's context exactly like any other skill. Only install plugins from trusted sources.

## Comparison with the MCP Plugin System

| Aspect | MCP (`mcp.remoteEntryUrl`) | Skills (`skills.baseUrl`) |
|---|---|---|
| Content | Executable ESM module | Static markdown (`SKILL.md`) |
| Loader | Separate Node.js process (`apps/mcp`), per conversation | In-process Python fetch (`ai-agent`), per conversation |
| Manifest shape | Single URL | Base URL + explicit list of names |
| Consumed via | `import(url)` | Plain HTTP `GET` |
| Failure isolation | Whole plugin's tools unavailable | Per-skill — other skills from the same plugin still load |
