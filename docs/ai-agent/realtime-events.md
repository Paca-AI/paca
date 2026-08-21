# AI Agent — Real-time Events

This document describes the Socket.IO events emitted by `services/realtime` to web clients during AI agent execution and owner-private Chat lifecycles.

---

## Room Subscription Model

Clients subscribe to project-scoped rooms to receive agent events:

```
project:<projectId>
```

Clients already join this room when they open a project (no additional subscription needed for agent events).

For conversation-level granularity (e.g., a monitoring panel), clients may also subscribe:

```
conversation:<conversationId>
```

This room receives only events for that specific conversation. Useful for the conversation monitoring view without noise from other agents in the same project.

---

## Events

### `agent:conversation:started`

Emitted when an agent conversation is initiated (trigger received and container is starting).

**Room:** `project:<projectId>`

**Payload:**
```json
{
  "conversation_id": "uuid",
  "agent_id": "uuid",
  "agent_name": "Dev Bot",
  "agent_handle": "dev-bot",
  "trigger_type": "task_assigned",
  "task_id": "uuid",
  "task_title": "Implement OAuth login",
  "chat_session_id": null,
  "started_at": "2026-05-19T10:00:00Z"
}
```

---

### `agent:conversation:event`

Emitted for each event produced during the conversation. This is the stream of agent "thoughts" and actions for the monitoring panel. `event_type`'s value space depends on the agent's `agent_type` — see the table below.

**Room:** `project:<projectId>` and `conversation:<conversationId>`

**Payload:**
```json
{
  "conversation_id": "uuid",
  "event_index": 5,
  "event_type": "CmdRunAction",
  "event_source": "agent",
  "payload": {
    "command": "grep -r 'OAuth' /workspace/repo/src --include='*.go'",
    "thought": "Let me find existing OAuth references in the codebase."
  },
  "iteration": 3,
  "timestamp": "2026-05-19T10:01:12Z"
}
```

**`event_type` values for both `llm`-type agents** (via `services/agent-runner`, driving Goose over ACP — see [agent-runner-service.md](agent-runner-service.md)) **and `acp`-type agents** (via `apps/acp-bridge`'s local Go daemon — see that app's own README): both ultimately relay the same underlying [Agent Client Protocol](https://agentclientprotocol.com), just over different transports (an in-process Goose HTTP+SSE session vs. a WebSocket-relayed local subprocess), so they share one event vocabulary — `apps/acp-bridge` forwards raw ACP `session/update` notifications through essentially as-is, the same way `internal/handler` does for the Goose path:

| Event Type | Source | Description |
|---|---|---|
| `user_message` | user | The user's message that started this turn |
| `agent_message_chunk` | agent | Streamed piece of the agent's reply text |
| `agent_thought_chunk` | agent | Streamed piece of the agent's reasoning/thinking text |
| `tool_call` | agent | A new tool call the agent has started |
| `tool_call_update` | agent | Status change for an existing tool call |
| `turn_end` | system | The turn finished — payload carries the stop reason |

A conversation from before either migration (`services/ai-agent` → `services/agent-runner` for `llm`-type; `apps/acp-bridge`'s OpenHands SDK → Go rewrite for `acp`-type) can still have legacy OpenHands-shaped rows in its history — `MessageEvent`, `ActionEvent`, `ObservationEvent`, `ACPToolCallEvent`, `AgentErrorEvent`, `UserRejectObservation`. `apps/web`'s `conversation-to-thread-messages.ts` still renders those for backward compatibility, but no agent produces them anymore.

---

### `agent:conversation:status`

Emitted when conversation status changes (paused, resumed, stopped, failed, finished).

**Room:** `project:<projectId>` and `conversation:<conversationId>`

**Payload:**
```json
{
  "conversation_id": "uuid",
  "agent_id": "uuid",
  "status": "finished",
  "iteration_count": 18,
  "timestamp": "2026-05-19T10:08:45Z"
}
```

**`status` values:**

| Value | Description |
|---|---|
| `running` | Conversation resumed after being paused |
| `paused` | User paused the conversation |
| `finished` | Agent completed the task successfully |
| `stopped` | User forcefully stopped the conversation |
| `failed` | Container error or unhandled exception |
| `timed_out` | Conversation exceeded `timeout_minutes` |
| `iteration_limit` | Agent hit `max_iterations` without finishing |

---

### `agent:conversation:pr_created`

Emitted when the agent successfully creates a pull request.

**Room:** `project:<projectId>` and `conversation:<conversationId>`

**Payload:**
```json
{
  "conversation_id": "uuid",
  "agent_id": "uuid",
  "task_id": "uuid",
  "pr_url": "https://github.com/org/repo/pull/42",
  "pr_number": 42,
  "branch_name": "agent/implement-oauth-login",
  "title": "feat: implement OAuth login flow (PACA-42)",
  "timestamp": "2026-05-19T10:09:00Z"
}
```

---

### `agent:task:commented`

Emitted when an agent posts a reply comment on a task (e.g., after completing a `comment_mention` trigger).

**Room:** `project:<projectId>`

**Payload:**
```json
{
  "task_id": "uuid",
  "comment_id": "uuid",
  "agent_id": "uuid",
  "conversation_id": "uuid",
  "timestamp": "2026-05-19T10:03:00Z"
}
```

Clients should re-fetch the task comments upon receiving this event to display the agent's response.

---

### `agent.turn.finished`

Emitted for an authoritative owner-private Chat turn terminal state.

**Room:** `user:<actorUserId>:agent-chat` only. It is never broadcast to the
project room.

**Payload:**

```json
{
  "turn_id": "uuid",
  "session_id": "uuid",
  "project_id": "uuid",
  "actor_user_id": "uuid",
  "status": "succeeded"
}
```

The payload deliberately contains no prompt, context, event text, or stable
output. Clients invalidate the session and turn queries, then fetch the
owner-gated immutable result. They must not synthesize an answer from realtime
data.

---

### `agent.conclusion.published`, `.revised`, `.withdrawn`

Emitted after a human-confirmed conclusion publication commits.
New Project Chats writebacks emit only `.published`; `.revised` and
`.withdrawn` remain documented for legacy outbox/read compatibility.

**Room:** task/project audience.

```json
{
  "publication_id": "uuid",
  "project_id": "uuid",
  "target_task_id": "uuid",
  "kind": "published"
}
```

Clients refetch the append-only conclusion history and task activity. The event
contains neither the frozen summary nor private source identifiers, so it
cannot bypass task audience checks or owner-only source-link redaction.

---

## Consuming Events in the Frontend

```ts
// Subscribe to agent events for the active project
socket.on("agent:conversation:started", (data) => {
  toast.info(`${data.agent_name} started working on "${data.task_title}"`);
});

socket.on("agent:conversation:event", (data) => {
  if (data.conversation_id === activeConversationId) {
    appendEventToMonitorPanel(data);
  }
});

socket.on("agent:conversation:status", (data) => {
  updateConversationStatus(data.conversation_id, data.status);
  if (data.status === "finished") {
    toast.success(`Agent finished the task`);
  }
});

socket.on("agent:conversation:pr_created", (data) => {
  linkPRToTask(data.task_id, data.pr_url);
  toast.success(`PR created: ${data.pr_url}`);
});

socket.on("agent.turn.finished", (data) => {
  invalidateChatSession(data.project_id, data.session_id);
  invalidateChatTurn(data.project_id, data.turn_id);
});

socket.on("agent.conclusion.published", (data) => {
  invalidateTaskConclusions(data.project_id, data.target_task_id);
});
```
