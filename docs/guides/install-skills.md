# Paca Skills

Use Paca directly from Claude Code, Gemini CLI, Cursor, or any AGENTS.md-reading tool, with `/paca` and related slash commands. Once installed, your AI coding tool will use your Paca workspace for tasks, documentation, and sprint management — instead of creating local files.

## Install

The installer fetches all skill content — Paca's bundled set and anything contributed by your installed plugins — from a running Paca instance's API (`GET /api/v1/skills`, `GET /api/v1/plugins`) rather than GitHub or any local copy, so installed content always matches the exact version that instance is running. A reachable Paca instance is therefore required, whether you run it via `curl | bash` or from a local clone:

```bash
PACA_API_URL=http://localhost:8080 \
  curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash
```

> **Security note:** Review the script before running it — `curl | bash` executes remote code directly. You can inspect it at the URL above, then run `bash scripts/install-paca-skills.sh` from a local clone instead.

If `PACA_API_URL` isn't set and you're running the script interactively (a real terminal attached), it prompts for it instead of failing outright. `PACA_API_KEY` is optional — both endpoints are publicly readable — but the prompt offers to collect it too, in case your deployment locks things down further. Both endpoint calls require `jq`.

The installer copies every bundled skill to every supported platform found on this machine:

| Platform | Location | Scope |
|---|---|---|
| Claude Code | `~/.claude/commands/<name>.md` | Global — every session |
| Gemini CLI | `~/.gemini/commands/<name>.toml` | Global — every session |
| Cursor | `<project>/.cursor/commands/<name>.md` | Per-project (Cursor has no global commands directory) |
| Any AGENTS.md-reading tool (Codex, Windsurf, OpenCode, …) | `<project>/AGENTS.md` | Per-project, merged into a marker-delimited section — re-running the installer refreshes only that section and leaves the rest of the file alone |

The per-project targets (Cursor, AGENTS.md) are only written when the installer is run from inside a git working tree — run it from your project root to get those too.

### Choosing which platforms to install to

By default the installer installs to every platform above. To install to only some of them, set `PACA_SKILL_PLATFORMS` to a comma/space-separated list of `claude`, `gemini`, `cursor`, `agents` (or `all`):

```bash
PACA_API_URL=http://localhost:8080 PACA_SKILL_PLATFORMS=claude,gemini \
  curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash
```

Or pass `--platforms=...` as an argument — through a pipe, args go after `bash -s --`:

```bash
curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash -s -- --platforms=claude,gemini
```

If neither is set and a real terminal is attached, the installer prompts for a selection — press Enter there to install to all of them.

### Including plugin-contributed skills

If your Paca instance also has plugins installed that contribute their own skills, no extra step is needed — the installer checks for those automatically alongside the bundled set, using the same `PACA_API_URL`/`PACA_API_KEY` above.

`/paca-setup` (below) already collects both values interactively and can re-run the installer with them for you.

## Configure the MCP server

The skill requires the Paca MCP server to be connected. After installing the skill, run `/paca-setup` inside a Claude Code session for an interactive setup walkthrough, or follow the quick steps below.

### Quick setup — Claude Code CLI

```bash
claude mcp add paca \
  --env PACA_API_KEY=<your-api-key> \
  --env PACA_API_URL=<your-paca-url> \
  -- npx -y @paca-ai/paca-mcp
```

Replace `<your-api-key>` (from Paca → Settings → API Keys) and `<your-paca-url>` (e.g. `http://localhost:8080` or your hosted URL).

### Project-level setup (recommended for teams)

Create `.claude/mcp.json` in your project root:

```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "<your-api-key>",
        "PACA_API_URL": "http://localhost:8080"
      }
    }
  }
}
```

> **Security:** Do not commit API keys. Add `.claude/mcp.json` to `.gitignore` or inject `PACA_API_KEY` from your shell environment.

### Claude Desktop

Add to the config file for your OS:
- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
- **Linux**: `~/.config/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "<your-api-key>",
        "PACA_API_URL": "http://localhost:8080"
      }
    }
  }
}
```

Restart Claude Desktop after saving.

## Commands

All commands read your Paca documentation first — before taking any action, Claude calls `list_documents` and reads the relevant docs so it understands the project context. Documentation updates are always written back to Paca Docs via `update_document` or `create_document`, never as local files.

### `/paca <request>`

General-purpose Paca operations in plain English. Routes to the right tool based on intent.

```
/paca Fix the login redirect bug, assign to sprint 3
/paca What's in the current sprint?
/paca Mark task #42 as done
/paca ABC-17 is blocked — add a comment: needs design review
```

### `/paca-epic <requirements>`

Converts requirements into a structured epic: creates the parent task, breaks it into child stories, and writes a spec document — all in Paca.

```
/paca-epic As a user I want to reset my password via email
/paca-epic #12   ← turn an existing requirement task into a full epic
```

### `/paca-clarify <task-or-doc>`

Reads a task or document, identifies ambiguities (scope gaps, missing edge cases, undefined terms), asks targeted questions, then updates the spec in Paca with the resolved content.

```
/paca-clarify #42
/paca-clarify ABC-17
/paca-clarify "OAuth Integration Spec"
```

### `/paca-breakdown <task>`

Decomposes a task or epic into smaller, independent, estimable sub-tasks and creates them in Paca.

```
/paca-breakdown #42
/paca-breakdown ABC-17
```

### `/paca-sprint`

Plans a sprint: reads the backlog and project roadmap, recommends a task set that fits stated capacity, then assigns tasks to the sprint and sets the sprint goal.

```
/paca-sprint
/paca-sprint next sprint, 30 points capacity
/paca-sprint sprint 4, goal: ship the auth flow
```

### `/paca-estimate <task(s)>`

Estimates story points for one or more tasks using the Fibonacci scale, with reasoning, then writes the estimates back to the tasks.

```
/paca-estimate #42
/paca-estimate #42 #43 #44
/paca-estimate          ← estimates all unestimated tasks in the current sprint
```

### `/paca-prioritize`

Scores tasks by business value, urgency, effort, and dependencies against the project roadmap, then updates their priority fields.

```
/paca-prioritize
/paca-prioritize #42 #43 #44
```

### `/paca-do <task>`

Executes a task end-to-end: marks it in progress, reads all relevant docs, does the work (code, writing, research), then marks it done and updates any affected Paca documentation.

```
/paca-do #42
/paca-do ABC-17
```

### `/paca-test <task>`

Derives test cases from acceptance criteria, runs them, and posts results as a task comment. Advances the task status on pass, reverts to in-progress on fail.

```
/paca-test #42
/paca-test ABC-17
```

### `/paca-doc <task-or-topic>`

Writes or updates a document in Paca Docs. Reads existing docs first to match tone and avoid duplication.

```
/paca-doc #42                          ← document the feature in task #42
/paca-doc "API Authentication Guide"   ← create a new guide
/paca-doc ABC-17 update                ← update an existing doc
```

### `/paca-setup`

Interactive setup wizard. Walks you through connecting Claude Code to your Paca instance and verifying the connection.

```
/paca-setup
```

## Environment variables

| Variable | Required | Default | Description |
|---|---|---|---|
| `PACA_API_KEY` | Yes | — | API key (Paca → Settings → API Keys) |
| `PACA_API_URL` | No | `http://localhost:8080` | Your Paca instance URL |

## Make Paca the default for your project

To make Claude always prefer Paca tools in a project (without needing to type `/paca` every time), add this to your project's `CLAUDE.md`:

```markdown
## Project management

This project uses Paca for all project management. When working in this codebase:

- **Tasks and to-dos** → use `create_task` / `list_tasks` via the Paca MCP tools. Do not create local TODO files or add TODO comments.
- **Documentation** → use `create_document` / `update_document` via Paca MCP. Do not create standalone `.md` docs unless they belong in the repository (e.g. README, CONTRIBUTING).
- **Sprint planning** → use `create_sprint` / `list_sprints` via Paca MCP.

If Paca MCP tools are not available, say so and ask the user to run `/paca-setup`.
```

## Uninstall

```bash
# Claude Code
rm ~/.claude/commands/paca*.md
# Gemini CLI
rm ~/.gemini/commands/paca*.toml
# Cursor (run from the project root)
rm .cursor/commands/paca*.md
```

For AGENTS.md, remove the block between `<!-- BEGIN PACA SKILLS ... -->` and `<!-- END PACA SKILLS -->` — everything else in the file is untouched by the installer and safe to keep.

## Available tools

See [mcp-server-setup.md](mcp-server-setup.md) for the complete tool reference.
