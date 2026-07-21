---
name: paca-setup
description: Configure the Paca MCP server for use with Claude Code or Claude Desktop. Use when setting up Paca for the first time, adding or editing the MCP server config, troubleshooting connectivity, or installing the Paca skills globally. Walks the user through prerequisites, config file generation, verification, and optional global skill install.
compatibility: Requires Node.js 18+ and a running Paca instance.
---

You are helping the user configure the Paca MCP server for their Claude Code environment.

Walk the user through the setup interactively, step by step. Do not dump all instructions at once — confirm each step before proceeding.

---

## Setup flow

### Step 1 — Check prerequisites

Ask the user to confirm:
- [ ] Paca is running (local: `http://localhost:8080`, or their hosted URL)
- [ ] Node.js 18+ is installed (`node --version`)
- [ ] They have a Paca API key (Settings → API Keys inside Paca UI)

If any prerequisite is missing, guide the user to resolve it before continuing.

### Step 2 — Identify the Claude Code config file

Ask which environment they want to configure:

**A. Claude Code (CLI) — project-level** (recommended for team projects)
→ Create or edit `.claude/mcp.json` in the project root.

**B. Claude Code (CLI) — global**
→ Run: `claude mcp add paca --env PACA_API_KEY=<key> --env PACA_API_URL=<url> -- npx -y @paca-ai/paca-mcp`

**C. Claude Desktop**
→ Edit the config file for your OS:
- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`
- **Linux**: `~/.config/Claude/claude_desktop_config.json`

### Step 3 — Generate the config snippet

Based on their choice, generate the exact config block. Example for option A (`.claude/mcp.json`):

```json
{
  "mcpServers": {
    "paca": {
      "command": "npx",
      "args": ["-y", "@paca-ai/paca-mcp"],
      "env": {
        "PACA_API_KEY": "<their-api-key>",
        "PACA_API_URL": "<their-paca-url>"
      }
    }
  }
}
```

Substitute the actual values the user provides. Remind them:
- **Never commit API keys to git.** Add `.claude/mcp.json` to `.gitignore` if it contains a key, or use environment variable substitution.

### Step 4 — Apply the config

For option A, write the `.claude/mcp.json` file directly (ask permission first).  
For options B and C, show the command/snippet and ask the user to apply it.

### Step 5 — Verify

Once configured, ask the user to restart Claude Code / Claude Desktop, then test with:

> "List my Paca projects"

If Paca tools appear and return results, setup is complete. If not, check:
1. JSON syntax is valid
2. API key is correct (no extra spaces)
3. Paca API URL is reachable (`curl <PACA_API_URL>/api/v1/health`)
4. Node.js / npx is in PATH

### Step 6 — Install the Paca skills globally (optional)

Offer to run the skill installer so `/paca` and related skills are always available — not just in Claude Code, but in Gemini CLI, Cursor, and any AGENTS.md-reading tool too:

```bash
curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash
```

Review the script before running it — `curl | bash` executes remote code directly.

If the user is installing plugins on their Paca instance (or already has some enabled), offer to also pass through the `PACA_API_URL` / `PACA_API_KEY` collected in Step 1 so the installer pulls in each enabled plugin's contributed skills too, not just Paca's own bundled set:

```bash
PACA_API_URL=<their-paca-url> PACA_API_KEY=<their-api-key> \
  curl -fsSL https://raw.githubusercontent.com/Paca-AI/paca/master/scripts/install-paca-skills.sh | bash
```

This step additionally requires `jq` to be installed — mention that if it's missing, the installer still succeeds but skips plugin-contributed skills.

---

## Environment variables reference

| Variable | Required | Description |
|---|---|---|
| `PACA_API_KEY` | Yes | API key from Paca → Settings → API Keys |
| `PACA_API_URL` | No (default: `http://localhost:8080`) | Your Paca instance URL |
