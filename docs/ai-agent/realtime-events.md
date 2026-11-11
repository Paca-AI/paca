# AI Agent — Real-time Events

What `services/realtime` delivers to web clients while an agent conversation runs, and how `apps/web` consumes it. The delivery model itself (rooms, per-event authorization, the resumable change feed) is described in the [realtime service README](../../services/realtime/README.md); this page covers the agent-specific messages.

All of them are Socket.IO `event` messages of the shape `{ "type": "<type>", "payload": { ... } }` published by `services/agent-runner` (and, for `agent.session.started`, `services/api`) on the `paca.events` Valkey Pub/Sub channel. They are **ephemeral**: they are not part of the project's change log, so a client that missed one (a dropped connection) learns the final state from the persisted conversation events and a refetch, not from replay. A conversation's history lives in `agent_conversation_events`, written by `agent-runner` directly.

---

## Routing and authorization

| Conversation | Delivered to |
|---|---|
| Project-shared | Sockets in the project's `conversations` room whose account holds `conversations:read` on `project/<p>/conversation/<id>`. `agent-runner` puts that descriptor in the message (`authz`), so the check is per conversation, not per room. |
| Owner-private, or a global agent's chat | Only the owner's own `user:<id>:agent-chat` room (the payload carries `actor_account_id`). Never a project room. |

There is no per-conversation room: a client filters by `payload.conversation_id`.

---

## Events

### `agent.<event_type>` — a persisted conversation event

One message for every event written to `agent_conversation_events`; `<event_type>` is the lower-cased ACP event type. This is the live stream of an agent's output and actions, relayed from the agent (an in-process Goose session for `llm`-type agents, a WebSocket-relayed local subprocess for `acp`-type agents — both speak the same [Agent Client Protocol](https://agentclientprotocol.com) vocabulary).

**Payload**

```json
{
  "conversation_id": "uuid",
  "project_id": "uuid",
  "id": "uuid",
  "event_index": 5,
  "event_type": "tool_call",
  "event_source": "agent",
  "payload": { "...": "the event body" },
  "created_at": "2026-05-19T10:01:12.123456789Z"
}
```

`actor_account_id` is present (and `project_id` may be absent) for an owner-private or global conversation.

| `event_type` | Source | Description |
|---|---|---|
| `user_message` | user | The user's message that started this turn |
| `agent_message_chunk` | agent | Streamed piece of the agent's reply text |
| `agent_thought_chunk` | agent | Streamed piece of the agent's reasoning text |
| `tool_call` | agent | A new tool call the agent has started |
| `tool_call_update` | agent | Status change for an existing tool call |
| `turn_end` | system | The turn finished; the payload carries the stop reason |

A conversation from before the move to ACP can still have legacy OpenHands-shaped rows in its history (`MessageEvent`, `ActionEvent`, `ObservationEvent`, ...); `apps/web`'s `conversation-to-thread-messages.ts` still renders them, but no agent produces them any more.

### `agent.conversation.<status>` — a status change

Sent when a conversation pauses or ends: `paused`, `finished`, `failed`, `stopped`. Payload: `conversation_id`, plus `project_id` or `actor_account_id` as above. No event row, so no `id` or `event_index`.

| Status | Description |
|---|---|
| `paused` | The conversation is paused and can be resumed |
| `finished` | The agent completed the turn successfully |
| `stopped` | A user stopped it |
| `failed` | A container or agent error, or the agent could not be started |

### `agent.session.started`

Sent by `services/api` when an agent begins a session on a task; it is also recorded as a task activity. The full message is checked as `conversations:read` on the conversation; a **payload-free copy** (`project_id`, `conversation_id` only) also goes to the project's `tasks` room so task boards refresh without learning the conversation's content.

### `agent.acp_bridge.status`

A local bridge (`apps/acp-bridge`) connected or disconnected. Payload: `agent_id`, `connected`. It carries no conversation content, so it goes to the `tasks` room as a project-wide read.

### Related, not agent events

Work an agent does arrives as ordinary `task.*`, `doc.*` and `git.task.updated` changes made with the agent as actor (so a pull request an agent opens shows up as `git.task.updated` on its task, and a comment it posts as `task.comment.added`). Those are in the change log like any other change.

---

## Consuming them in the frontend

`useProjectRealtime` and `useGlobalAgentRealtime` hand every `agent.*` message to `applyRealtimeAgentEvent` (`apps/web/src/lib/agent-api.ts`):

- A message with an `id` and `event_index` is a persisted event: it is appended to the conversation's in-memory tail (`conversationEventsTailQueryOptions`), which `useConversationEventWindow` merges over the pages it fetched, so a streamed reply renders without an HTTP request per chunk. A message already applied (a reconnect can replay one) is ignored.
- A message without them is a status notification: the conversation's event window and the project's conversation list are invalidated, which is the safety net for anything a dropped message missed. `agent.session.started` also refreshes task views.

```ts
socket.on("event", ({ type, payload }) => {
  if (type.startsWith("agent.") && payload.conversation_id === activeConversationId) {
    // append payload to the monitor panel, or refresh on a status message
  }
});
```
