# AI Agent — Real-time Events

This document describes the Socket.IO events emitted by `services/realtime` to web clients during AI agent conversation lifecycle.

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

**`event_type` values for `llm`-type agents** (via `services/agent-runner`, driving Goose over ACP — see [agent-runner-service.md](agent-runner-service.md)):

| Event Type | Source | Description |
|---|---|---|
| `agent_message_chunk` | agent | Streamed piece of the agent's reply text |
| `tool_call` | agent | A new tool call the agent has started |
| `tool_call_update` | agent | Status change for an existing tool call |
| `turn_end` | system | The turn finished — payload carries the stop reason |

**`event_type` values for `acp`-type agents** (via `apps/acp-bridge`, still built on the OpenHands SDK — unaffected by the `services/ai-agent` → `services/agent-runner` migration, see that app's own README):

| Event Type | Source | Description |
|---|---|---|
| `MessageAction` | user / agent | Conversational message |
| `CmdRunAction` | agent | Shell command execution |
| `CmdOutputObservation` | system | Output of a shell command |
| `FileReadAction` | agent | File read |
| `FileWriteAction` | agent | File write |
| `FileEditAction` | agent | In-place file edit |
| `BrowseURLAction` | agent | Web page fetch |
| `AgentThinkAction` | agent | Internal reasoning (not executed) |
| `AgentFinishAction` | agent | Conversation complete |
| `ErrorObservation` | system | Tool execution error |

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

### `agent:chat:reply`

Emitted when an agent sends a reply in a direct chat session.

**Room:** `project:<projectId>`

> The client should also filter by `session_id` to match the active chat session.

**Payload:**
```json
{
  "session_id": "uuid",
  "conversation_id": "uuid",
  "agent_id": "uuid",
  "message": "Here are the acceptance criteria for PACA-42:\n\n**Given** the user is on the login page...",
  "timestamp": "2026-05-19T10:02:30Z"
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

socket.on("agent:chat:reply", (data) => {
  if (data.session_id === activeChatSessionId) {
    appendChatMessage({ role: "agent", content: data.message });
  }
});
```
