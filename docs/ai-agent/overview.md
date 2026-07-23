# AI Agent Feature — Overview

Paca AI Agents are first-class project members, triggered by task assignment, comment @mentions, or direct chat. Agents participate in the project exactly like human members — they appear in member lists, can be assigned tasks, and exchange messages in comments and chats. Depending on `agent_type`, an agent's conversations either run in an isolated Docker container managed by Paca (`llm`, powered by the [OpenHands Software Agent SDK](https://docs.openhands.dev/sdk)) or on a coding CLI the user runs locally themselves (`acp`) — see [Execution Models](#execution-models).

## Table of Contents

- [Concepts](#concepts)
- [Architecture](#architecture)
- [Execution Models](#execution-models)
- [Trigger Model](#trigger-model)
- [Conversation Lifecycle](#conversation-lifecycle)
- [Repository Access & PR Creation](#repository-access--pr-creation)
- [Skill Templates](#skill-templates)
- [Customization](#customization)
- [Related Documents](#related-documents)

---

## Concepts

| Term | Meaning |
|---|---|
| **Agent** | A project-scoped AI entity with a role, execution config, skills, MCP servers, and a system prompt. |
| **Agent Member** | A `project_members` row with `member_type = 'agent'` and a reference to the `agents` table. Agents are treated identically to human members in all product surfaces. |
| **Agent Type** | `llm` (default) or `acp` — determines *where and how* an agent's conversations execute. See [Execution Models](#execution-models) below. Not to be confused with the pre-refactor "Agent Type" template concept (PO Assistant, Business Analyst, etc.); those are now [Skill Templates](#skill-templates). |
| **Skill Template** | A built-in, reusable skill (`developer`, `ba`, `manual-tester`, `po-assistant`) that any agent — `llm` or `acp` — can attach, instead of a full agent preset. See [Skill Templates](#skill-templates). |
| **Agent Conversation** | A single execution session for one trigger event. For `llm` agents, an OpenHands SDK `Conversation` spawned in a dedicated Docker container; for `acp` agents, a turn dispatched to the user's own locally-run bridge. |
| **Conversation Event** | An atomic action/observation within a conversation (LLM message, bash command, file edit, etc.). Persisted to the database for history and real-time monitoring. |
| **Trigger** | An event that creates an agent conversation: task assignment, comment @mention, or direct chat message. Applies identically to both agent types — only how the resulting conversation executes differs. |

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│  apps/web                                                                   │
│  • Agent management UI (project settings)                                   │
│  • Real-time conversation monitoring (stop / continue / history)            │
│  • @mention autocomplete for agents in comments                             │
│  • Direct chat panel with agents                                            │
└─────────────────┬───────────────────────────────────────────────────────────┘
                  │ HTTP / Socket.IO
                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  services/api  (Go + Gin)                                                   │
│  • Agent CRUD (domain: agent)                                               │
│  • Publishing agent-trigger events → Valkey Stream "paca:agent:triggers"   │
│  • REST endpoints for conversation history & control                        │
│  • Writing conversation summaries / replies back to tasks/comments          │
└──────────┬───────────────────────────────┬──────────────────────────────────┘
           │  Valkey Stream (triggers)      │  Valkey Stream (events back)
           ▼                                ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  services/ai-agent  (Python + FastAPI + OpenHands SDK)                      │
│  • Stream consumer: reads "paca:agent:triggers"                             │
│  • Spawns one DockerWorkspace per conversation                              │
│  • Runs OpenHands Conversation inside the container                         │
│  • Publishes conversation events → Valkey Stream "paca:agent:events"        │
│  • REST endpoints: pause, resume, stop, history                             │
└──────────────────────────────────────────────────────────────────────────────┘
                  │
                  │  Docker socket (spawn / manage containers)
                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│  Agent Docker Containers  (ghcr.io/paca-ai/paca-agent-server:latest)        │
│  • One container per active conversation                                    │
│  • Completely isolated from other containers                                │
│  • Workspace cloned from repo plugin (credentials injected as secrets)     │
│  • Destroyed when conversation finishes / is stopped                        │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Service Responsibilities

| Service | Responsibility |
|---|---|
| `services/api` | Owns agent configuration, triggers agent invocations, stores conversation summaries and replies, exposes control APIs. |
| `services/ai-agent` | Executes agent conversations via OpenHands SDK, manages Docker container lifecycle, streams events back. |
| `services/realtime` | Delivers real-time conversation events to the web client via Socket.IO (same existing Valkey→Socket.IO fan-out). |
| Docker host | Provides container isolation. Agent containers cannot reach other Paca service containers on the internal network by default. |

---

## Execution Models

Every agent has an `agent_type` of `llm` (default) or `acp`, fixed at creation. The trigger model, comment/chat surfaces, and conversation history all work identically for both — only *where the conversation actually runs* differs:

| | `llm` | `acp` |
|---|---|---|
| Runs in | A Paca-managed Docker container (the architecture diagram above) | A coding CLI running as a real local process on the user's own machine |
| LLM credential | `llm_api_key`, stored encrypted, managed by Paca | The user's own local CLI auth (e.g. `claude setup-token`, `OPENAI_API_KEY`) — Paca never sees, stores, or requests this |
| MCP servers / skills / env vars | Configured on the agent in Paca, injected into the container at conversation start | Entirely the user's own local CLI configuration — Paca injects nothing |
| Git / VCS access | Short-lived scoped token from a repository plugin (see [Repository Access & PR Creation](#repository-access--pr-creation)) | Whatever `git`/`gh` credentials are already configured on the user's machine |
| Connection to Paca | N/A — the container is spawned and controlled by `services/ai-agent` directly | An authenticated WebSocket from the [`paca-acp-bridge`](../../apps/acp-bridge/README.md) daemon to `services/ai-agent`, using a per-agent bridge token generated in the Agents UI (`POST .../agents/:agentId/acp-bridge-token`) |

`acp` exists for users who already have a coding CLI configured the way they want (auth, MCP servers, skills, git access) and would rather point Paca at that setup than duplicate it in a sandboxed container. See [api-design.md](api-design.md) for the full field-level split between the two types, and the bridge's own README for its local setup and auth model.

---

## Trigger Model

### 1. Task Assignment

A task can have multiple assignees (stored in the `task_assignees` join table). When one of a task's assignees points to a `project_members` row with `member_type = 'agent'`, the API publishes an `agent.task.assigned` event to the Valkey Stream — one event per newly-added agent assignee — containing:

```json
{
  "trigger_type": "task_assigned",
  "agent_id": "<uuid>",
  "project_id": "<uuid>",
  "task_id": "<uuid>",
  "task_title": "...",
  "task_description": "...",
  "actor_member_id": "<uuid>"
}
```

The agent service picks this up, spins up a conversation, and instructs the agent to work on the task. When complete, the agent posts a summary comment on the task and optionally creates a PR.

### 2. Comment @mention

When a comment body contains `@<agent-handle>`, the API publishes an `agent.comment.mention` event:

```json
{
  "trigger_type": "comment_mention",
  "agent_id": "<uuid>",
  "project_id": "<uuid>",
  "task_id": "<uuid>",
  "comment_id": "<uuid>",
  "comment_body": "...",
  "actor_member_id": "<uuid>"
}
```

The agent responds directly in the comment thread.

### 3. Direct Chat

A dedicated chat API allows users to send messages to an agent member. Internally this publishes an `agent.chat.message` event and opens (or resumes) a persistent conversation per agent per user.

```json
{
  "trigger_type": "chat_message",
  "agent_id": "<uuid>",
  "project_id": "<uuid>",
  "chat_session_id": "<uuid>",
  "message": "...",
  "actor_member_id": "<uuid>"
}
```

---

## Conversation Lifecycle

This describes the `llm` execution path (see [Execution Models](#execution-models)). `acp` conversations skip container spawning entirely — the trigger is dispatched as a `start_turn` message to the user's already-running bridge, which runs the turn locally and streams events back over the bridge's WebSocket instead.

```
Trigger event published
        │
        ▼
ai-agent service dequeues event
        │
        ▼
Resolve agent config (LLM, skills, MCP servers, system prompt)
        │
        ▼
Clone repository (if coding task) via repository plugin adapter
  - fetch clone URL + temporary token from plugin
  - inject credentials as OpenHands SecretSource (never logged)
        │
        ▼
Spawn DockerWorkspace (OpenHands agent-server image)
        │
        ▼
Create OpenHands Conversation with:
  - LLM from agent config
  - Skills from agent config
  - MCP servers from agent config
  - System prompt from agent config
  - Conversation ID stored in DB
  - Persistence dir mounted into container
  - Event callback → publish to Valkey "paca:agent:events"
        │
        ├─── User sends "pause" → conversation.pause()
        ├─── User sends "resume" → conversation.run()
        ├─── User sends "stop" → conversation.close(), container destroyed
        │
        ▼
Conversation finishes (agent sends finish action)
        │
        ▼
Persist summary + outputs
  - Post reply comment / chat message via API
  - Create PR if coding task (via repo plugin)
        │
        ▼
Container destroyed, conversation state archived
```

---

## Repository Access & PR Creation

This describes the `llm` execution path. `acp` agents don't go through a repository plugin at all — they use whatever `git`/`gh` credentials are already configured on the user's own machine, exactly as if the user were driving the CLI themselves; see the [Execution Models](#execution-models) table.

Agents must be able to read and write code without ever seeing VCS credentials directly.

### Clone Flow

1. When the trigger involves a coding task, `services/ai-agent` calls the **repository plugin adapter** endpoint (e.g., the GitHub plugin) with the project context.
2. The plugin returns a **short-lived scoped token** (e.g., a GitHub installation token with read/write on the repository, valid for 10 minutes) and the HTTPS clone URL.
3. The token is injected into the OpenHands `Conversation` via `conversation.update_secrets()` as a `SecretSource` that fetches a fresh token on demand — the token value never appears in any log or agent output.
4. The agent's first tool call clones the repository: `git clone https://x-access-token:$GIT_TOKEN@github.com/org/repo.git`.
5. When the conversation ends, the workspace is destroyed and the token expires automatically.

### PR Creation Flow

1. The agent completes coding work and signals readiness in its finish message.
2. `services/ai-agent` calls the repository plugin adapter's **create PR endpoint** with the branch name and description generated by the agent.
3. The plugin creates the PR and returns the PR URL.
4. The agent service posts the PR URL as a comment on the Paca task.

This design means:
- Agents never store credentials.
- Credentials are not readable from container logs (masked by `SecretSource`).
- Plugin plugins remain the single source of truth for VCS auth.

---

## Skill Templates

There is no longer a preset "agent type" that bundles an LLM, skills, and a system prompt together — every agent is configured individually (`llm` or `acp`, any LLM provider, any skills, any MCP servers). What used to be built-in agent types are now built-in **skill templates**: reusable skill content a user attaches to whichever agent they're configuring, listed via `GET /api/v1/agents/skill-templates` ([api-design.md](api-design.md#llm-models--skill-templates)).

| Slug | Name | Description |
|---|---|---|
| `developer` | Developer | Implements features, fixes bugs, writes tests, and creates pull requests. |
| `ba` | Business Analyst | Requirements analysis, gap analysis, process modelling, functional specifications. |
| `manual-tester` | Manual Tester | Test case design, test plans, defect report analysis, testing documentation. |
| `po-assistant` | Product Owner Assistant | Backlog grooming, acceptance criteria, prioritization, roadmap questions. |

Each template also carries a set of trigger keywords (e.g. `developer` triggers on "implement", "fix", "bug", "pr", ...) used to route `@mention`-less task assignments to the right skill context. Users can also write their own inline or GitHub-URL-sourced skills — templates are just a shortcut, not the only option.

---

## Customization

`llm` agents expose four customization axes; `acp` agents only the latter two, since MCP servers/skills for `acp` live in the user's own local CLI config rather than Paca (see [Execution Models](#execution-models)):

| Axis | Applies to | Description |
|---|---|---|
| **LLM Provider** | `llm` only | Any LiteLLM-supported provider: Anthropic, OpenAI, Azure, AWS Bedrock, Gemini, Groq, OpenRouter, local LLMs, etc. |
| **System Prompt** | both | Free-form Jinja2 template or plain text, optionally pre-filled from a skill template. |
| **Skills** | `llm` only | AgentSkills-standard `SKILL.md` directories or inline text skills. Stored in the DB, mounted into the container at runtime. |
| **MCP Servers** | `llm` only | JSON MCP config following the standard `mcpServers` format. Evaluated inside the container at conversation start. |

---

## Related Documents

- [database-schema.md](database-schema.md) — Agent tables and modifications to `project_members`
- [api-design.md](api-design.md) — REST endpoints for agent management
- [ai-agent-service.md](ai-agent-service.md) — `services/ai-agent` implementation details
- [repository-plugin-adapter.md](repository-plugin-adapter.md) — How agents access VCS credentials
- [realtime-events.md](realtime-events.md) — Socket.IO events emitted during conversations
