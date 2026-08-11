# AI Agent — Repository Plugin Adapter

This document describes how AI agents securely access source code and create pull requests through the Paca repository plugin system.

## Design Goals

1. **Agents never store credentials** — all VCS tokens are ephemeral and fetched on demand.
2. **Credentials are never visible in agent output** — the tool that fetches and uses a token scrubs it from any command output it returns, so it never lands in a log or the model's context.
3. **Plugin plugins remain the single source of VCS auth** — the GitHub plugin, GitLab plugin, etc., own token generation.
4. **The agent cannot read the raw token value** — the agent calls a tool (`clone_repository`, `push_branch`, ...) with a `pluginId`/`repoId`; the token itself is fetched and consumed entirely inside that tool's implementation, never returned to the agent as text.

---

## Protocol

Fetching and using a repository token is entirely a **tool call the agent makes inside its own sandbox**, not something the orchestrating service (`services/agent-runner`) does on the agent's behalf — a deliberate difference from `services/ai-agent`'s design, where the orchestrator fetched the token via an internal, service-to-service endpoint before the agent's first turn even began.

```
Paca MCP server (apps/mcp, running                services/api
inside the agent's own sandbox)                    (repository plugin adapter)
        │                                              │
        │  GET /api/v1/plugins/:pluginId/projects/     │
        │      :projectId/repositories/:repoId/        │
        │      clone-info                              │
        │  Headers: X-API-Key: <this agent's           │
        │           PACA_API_KEY>                      │
        │ ─────────────────────────────────────────────►│
        │                                              │── invoke plugin's token provider
        │                                              │── GitHub: create installation token
        │                                              │── GitLab: create project access token
        │  200 OK                                      │
        │  { "token": "ghs_...", "clone_url": "..." }  │
        │ ◄─────────────────────────────────────────────│
        │                                              │
        │  git clone/push as a subprocess, token        │
        │  embedded in the URL; token scrubbed from     │
        │  any output before it's returned as a tool    │
        │  result (apps/mcp/src/tools/repo-tools.ts)    │
```

The Paca MCP server authenticates as the specific agent running the conversation — `PACA_API_KEY` is injected into its environment by `services/agent-runner` at sandbox start (see [agent-runner-service.md](agent-runner-service.md#skills--mcp-server-injection)), scoping every repository-adapter call to that one agent rather than to a shared internal service credential.

### Endpoint

`GET /api/v1/plugins/:pluginId/projects/:projectId/repositories/:repoId/clone-info`

Returns a fresh clone URL and token for the repository, resolved by the named plugin.

---

## Implementation per Plugin

### GitHub Plugin

Uses the GitHub App installation token API:

```
POST https://api.github.com/app/installations/:installation_id/access_tokens
Body: { "repositories": ["repo-name"], "permissions": { "contents": "write", "pull_requests": "write" } }
```

Tokens expire after 60 minutes. Since `apps/mcp`'s `clone_repository`/`push_branch` tools each fetch a fresh token on every call rather than caching one for the whole conversation, there is no separate renewal step to get wrong.

### GitLab Plugin

Uses GitLab project access tokens with `read_repository` + `write_repository` scopes, configured with a 1-hour expiry:

```
POST https://gitlab.com/api/v4/projects/:id/access_tokens
Body: { "name": "paca-agent-<conversation_id>", "scopes": ["read_repository", "write_repository"], "expires_at": "..." }
```

The token is revoked by the plugin adapter when the conversation finishes.

---

## Git Operations Inside the Container

Unlike `services/ai-agent`, the agent never runs a raw `git clone`/`git push` itself and no `GIT_TOKEN` environment variable exists. The agent calls the Paca MCP server's `clone_repository`/`push_branch` tools with a `pluginId`/`repoId`; the tool implementation (`apps/mcp/src/tools/repo-tools.ts`) fetches the token, builds an authenticated HTTPS URL, and runs `git` as a subprocess itself. Any command failure is returned to the agent as tool output with the token scrubbed out first (`scrubToken` — three passes: the raw token, its percent-encoded form, and the general `x-access-token:...@` credential pattern git itself may echo in an error message).

---

## PR Creation Flow

When the agent signals completion:

1. The agent calls a repository plugin's own PR-creation tool (e.g. `github_create_pull_request` — a plugin-contributed MCP tool, not part of the built-in Paca MCP server's `repo-tools.ts`) with the branch name and description it generated.
2. That tool calls the plugin adapter's PR creation endpoint:

```
POST /internal/plugins/:pluginId/pull-requests
Body:
{
  "project_id": "<uuid>",
  "head_branch": "agent/implement-oauth-login",
  "base_branch": "main",
  "title": "feat: implement OAuth login flow (PACA-42)",
  "body": "Agent-generated PR.\n\n## Changes\n...",
  "task_id": "<uuid>"
}
```

3. The plugin creates the PR and returns the URL.
4. `services/api` links the PR to the task and posts a comment with the PR URL.

### GitHub Plugin PR endpoint

```
POST https://api.github.com/repos/:owner/:repo/pulls
```

### GitLab Plugin PR endpoint

```
POST https://gitlab.com/api/v4/projects/:id/merge_requests
```

---

## Security Considerations

| Concern | Mitigation |
|---|---|
| Token leakage in logs | `repo-tools.ts`'s `scrubToken` removes all occurrences of the token value (raw, percent-encoded, and the general `x-access-token:...@` pattern) from any tool output before it's returned to the agent or logged |
| Token used beyond conversation scope | Tokens have a maximum TTL (60 min for GitHub, configurable for GitLab) and are revoked on conversation end |
| Agent pushing to protected branches | PR creation enforces a separate branch; direct pushes to `main` are not permitted by the plugin adapter |
| SSRF via clone URL | Clone URL is fetched from the plugin (trusted), not from user input. The URL is validated to match a configured repository. |
| Container network access to internal services | Agent containers run on an isolated Docker network with no route to Paca services; the token is the only channel out |
