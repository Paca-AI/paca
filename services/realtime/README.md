# Realtime Service

Socket.IO server that fans out real-time events to web clients by subscribing
to the `paca.events` Valkey Pub/Sub channel published by `services/api`.

## Responsibilities

- Accept authenticated Socket.IO client connections (JWT via cookie or handshake auth).
- Delegate access-token verification to `services/api` (`GET /api/v1/users/me/global-permissions`) — no local JWT validation.
- Subscribe to the `paca.events` Valkey Pub/Sub channel.
- Place each socket in the namespace-scoped rooms of a project (`project:<projectId>:tasks`, `…:docs`, `…:conversations`, `…:views`, …) whose `<domain>:read` action the API lists among the account's effective actions. A room only says an account may *possibly* see that kind of event.
- Deliver every event only to the sockets whose account the API confirms holds the event's action on the event's resource (see [Authorization](#authorization)).
- Re-check on change: drop cached decisions and re-sync rooms when the API announces an IAM change, and end a socket when its access token expires.
- Relay real-time collaborative editing (Yjs) for project docs and task descriptions (see [Collaborative editing](#collaborative-editing)).
- Expose a `/healthz` endpoint for container health checks.

## Stack

| Concern       | Tool                        |
|---------------|-----------------------------|
| Runtime       | [Bun](https://bun.sh) 1.x   |
| Language      | TypeScript (strict)         |
| WebSockets    | [Socket.IO](https://socket.io) 4.x |
| Valkey client | [ioredis](https://github.com/redis/ioredis) 5.x |
| Logging       | [pino](https://getpino.io)  |

## Local development

### Outside Docker (fastest feedback loop)

```sh
# 1. Copy environment file and adjust values if needed.
cp .env.example .env

# 2. Install dependencies.
bun install

# 3. Start with hot-reload (requires Valkey running locally on port 6379).
bun run dev
```

### Via Docker Compose (full stack)

```sh
# From the repo root — starts postgres, valkey, api, web, realtime, and gateway.
docker compose -f deploy/docker-compose.dev.yml up -d
```

The service is reachable through the Caddy gateway at `http://localhost/ws/`.

## Environment variables

| Variable       | Required | Default               | Description                                      |
|----------------|----------|-----------------------|--------------------------------------------------|
| `PORT`         | No       | `3001`                | HTTP port the server listens on.                 |
| `REDIS_URL`    | **Yes**  | —                     | Valkey/Redis connection URL (same as API).       |
| `API_URL`      | **Yes**  | —                     | Internal base URL of `services/api` used for token verification and permissions. |
| `CORS_ORIGINS` | No       | `http://localhost:3000` | Comma-separated allowed origins.               |
| `LOG_LEVEL`    | No       | `info`                | Pino log level (`trace`…`fatal`).                |
| `NODE_ENV`     | No       | `development`         | `production` enables structured JSON logging.    |

## Client-side connection

```ts
import { io } from "socket.io-client";

const socket = io("http://localhost", {
  path: "/ws/socket.io",   // Caddy strips the /ws prefix
  withCredentials: true,   // sends the access_token cookie automatically
});

// Join a project's rooms to receive scoped events; the ack says which.
socket.emit("join", { projectId: "<uuid>" }, ({ namespaces }) => {});

socket.on("permissions:changed", ({ projectId }) => { /* refetch permissions */ });
socket.on("reauth", () => { /* refresh the session, then socket.disconnect().connect() */ });

// Listen for API events.
socket.on("event", ({ type, payload }) => {
  console.log(type, payload);
});
```

## Event format

Every event received from Valkey (and re-emitted to Socket.IO clients) follows
the shape published by `services/api`:

```json
{
  "type": "task.comment.added",
  "payload": {
    "id": "...",
    "task_id": "...",
    "project_id": "...",
    "activity_type": "...",
    "content": "...",
    "actor_id": "...",
    "created_at": "...",
    "updated_at": "..."
  }
}
```

Project events are delivered only to sockets that joined the project and whose
account may read the event's resource (see Authorization).

## Authorization

This service holds no policy. Every decision is the API's IAM engine answering "may this account perform this action on this resource", the same engine that gates the REST routes, so entity-level `Deny` statements and conditions apply to live events exactly as they do to requests.

1. **What an event needs.** The API puts an `authz` descriptor in each message (`events.AuthzFor` in `services/api`): `{"action": "tasks:read", "resource": "project/<p>/task/<t>"}`. The project comes from the resource, the room from the action (`NAMESPACE_PERMISSIONS`). `services/agent-runner` attaches `conversations:read` on the conversation to the events of a project-shared conversation, and the API does the same for `agent.session.started`. An owner-private or global conversation is routed to its owner's room and carries none. A project event with no descriptor (a publisher that predates it) is checked as `conversations:read` on the conversation if it is an `agent.*` conversation event, and otherwise falls back to prefix routing and whole-room delivery, logged once per type.
2. **Rooms prefilter.** On `join` the socket's effective project actions (`GET /projects/:id/members/me/permissions`, which includes an action allowed on only some resource, such as one sprint's tasks) decide which namespace rooms it joins. The optional ack receives `{projectId, namespaces}`.
3. **Each event is checked.** For every event the sockets of the room are checked on the descriptor (`POST /projects/:id/members/me/permissions/check`, as that socket's own account) and only allowed ones get it. Questions asked in the same turn of the event loop share one request per account and project; answers are cached 30 seconds per account, action and resource; failures are not cached and deliver nothing (fail closed); a 401 disconnects the socket so it reconnects with a fresh token. Events of one resource are delivered in order.
4. **Changes take effect.** When a role or an attachment changes the API publishes `iam.changed`; this service drops every cached decision, re-syncs the rooms of all open sockets (one request per account and project) and emits `permissions:changed` `{projectId, namespaces}` so the client refetches its permissions. The same re-sync runs every 60 seconds as a backstop for a missed message.
5. **A socket ends with its token.** About a minute before the access token's `exp` the server emits `reauth` (refresh the session, then reconnect so the handshake carries the new cookie); at `exp` it disconnects, and the client reconnects on a server-initiated disconnect. One shared scan every 5 seconds handles all sockets, not a timer per socket.

Exceptions: a payload-free `agent.session.started` (`project_id`, `conversation_id` only) also goes to the tasks room so task boards refresh, and `agent.acp_bridge.status` (no conversation content) goes to the tasks room. Owner-private agent events and notifications are routed to the owner's / recipient's own room.

Nothing is persisted: sessions live in memory for the life of the socket and the service no longer uses Valkey except to subscribe.

## Linting & type checking

```sh
bun run lint    # biome check
bun run check   # tsc --noEmit
```

## Collaborative editing

Project docs and task descriptions (the BlockNote editors in `apps/web`) are
edited together through Yjs. The service keeps one in-memory `Y.Doc` per edited
resource and relays updates and cursor awareness between its editors over the
existing Socket.IO connection (`src/collab.ts`):

| Event (client → server) | Meaning |
|---|---|
| `collab:join` `{projectId, kind: "doc"\|"task", id, stateVector}` (ack) | Join; requires `docs:write` / `tasks:write` on the resource. The ack carries the missing state and `seed` |
| `collab:update` / `collab:awareness` `{room, update}` | Document / cursor updates, relayed to the other members |
| `collab:leave` `{room}` | Leave |

Server → client: `collab:update`, `collab:awareness`, `collab:revoked` (access lost).

- **Authorization**: every update is checked against IAM through the shared `AccessDecider`; IAM changes re-check all members, and a periodic pass (60 s) covers a missed announcement.
- **Persistence**: none in this service. Clients still save block JSON through the REST API, so MCP, search and exports are unchanged. An empty room is dropped 30 s after its last editor leaves and re-seeded from the stored content.
- **Seeding**: the first joiner of an empty room gets `seed: true` and is the only client that writes the stored content into the Y.Doc. If that client leaves, or does not produce content within 10 s, the grant moves to another member (`collab:seed`). Clients never write the stored copy over content already in the room.
- **Limits**: 5 MB per update, 2 000 rooms overall and 10 rooms per account.
- **Scaling**: rooms live in one process memory; running several replicas needs sticky routing by resource (or a Socket.IO adapter plus shared doc state).
- **Limits**: a write by REST (an agent or MCP) while editors have the room open is not merged into the live doc; the next save from an editor overwrites it. "Write with AI" on task descriptions is handled by applying that result once from the requesting client.
