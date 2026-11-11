# Realtime Service

Socket.IO server that delivers live updates to web clients. `services/api`
publishes every change on the `paca.events` Valkey Pub/Sub channel and appends
it to a per-project change log (a capped Valkey Stream); this service pushes
changes to the sockets whose account may read them, and lets a client resume
from the last change it applied (see [Sync protocol](#sync-protocol-resumable-change-feed)).

## Responsibilities

- Accept authenticated Socket.IO client connections (JWT via cookie or handshake auth).
- Delegate access-token verification to `services/api` (`GET /api/v1/users/me/global-permissions`) — no local JWT validation.
- Subscribe to the `paca.events` Valkey Pub/Sub channel and read the API's per-project change log, so a client can resume after any absence.
- Place each socket in the namespace-scoped rooms of a project (`project:<projectId>:tasks`, `…:docs`, `…:conversations`, `…:views`, `…:project`, `…:members`, `…:agents`, …) whose `<domain>:read` action the API lists among the account's effective actions. A room only says an account may *possibly* see that kind of event.
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
| `SYNC_ENABLED` | No       | `true`                | Set `false` to refuse `sync:join` (clients fall back to `join`). |
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

// Or join with the resumable feed (see "Sync protocol"): same rooms, plus every
// change since `cursor`, delivered as "sync:changes" instead of "event".
socket.emit("sync:join", { projectId: "<uuid>", cursor: null, epoch: null }, (ack) => {});
socket.on("sync:changes", (batch) => { /* apply when batch.from === cursor */ });

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

The API also writes the same event to the project's change log with a `seq`,
the operation (`create`, `update`, `delete`, `move`) and, where a client can
patch its cache with it, the entity after the change. That is the shape a
syncing socket receives (see [Sync protocol](#sync-protocol-resumable-change-feed)).

Adding an event type touches three tables, each owning one question:

| Question | Where | Guard |
|---|---|---|
| What must IAM allow before a client sees it? | `events.AuthzFor` in `services/api` | `TestEveryTopicIsClassified` fails until a new topic has a rule or is listed as not realtime |
| Which room does that action belong to? | `NAMESPACE_PERMISSIONS` / `eventNamespace` in `src/permissions.ts` | `api-client.test.ts` |
| What does the web cache do with it? | `EVENT_RULES` in `apps/web/src/lib/project-event-rules.ts` | `project-event-rules.test.ts` pins which rule takes which type |

A change that has no activity-log entry (a doc folder, a member's role change)
is announced with `events.PublishRealtime` only, so the activity feed needs no
text for it.

## Authorization

This service holds no policy. Every decision is the API's IAM engine answering "may this account perform this action on this resource", the same engine that gates the REST routes, so entity-level `Deny` statements and conditions apply to live events exactly as they do to requests.

1. **What an event needs.** The API puts an `authz` descriptor in each message (`events.AuthzFor` in `services/api`): `{"action": "tasks:read", "resource": "project/<p>/task/<t>"}`. The project comes from the resource, the room from the action (`NAMESPACE_PERMISSIONS`). `services/agent-runner` attaches `conversations:read` on the conversation to the events of a project-shared conversation, and the API does the same for `agent.session.started`. An owner-private or global conversation is routed to its owner's room and carries none. A project event with no descriptor (a publisher that predates it) is checked as `conversations:read` on the conversation if it is an `agent.*` conversation event, and otherwise falls back to prefix routing and whole-room delivery, logged once per type.
2. **Rooms prefilter.** On `join` the socket's effective project actions (`GET /projects/:id/members/me/permissions`, which includes an action allowed on only some resource, such as one sprint's tasks) decide which namespace rooms it joins. The optional ack receives `{projectId, namespaces}`.
3. **Each event is checked.** For every event the sockets of the room are checked on the descriptor (`POST /projects/:id/members/me/permissions/check`, as that socket's own account) and only allowed ones get it. Questions asked in the same turn of the event loop share one request per account and project; answers are cached 30 seconds per account, action and resource; failures are not cached and deliver nothing (fail closed); a 401 disconnects the socket so it reconnects with a fresh token. Events of one resource are delivered in order.
4. **Changes take effect.** When a role or an attachment changes the API bumps the `paca:iam:epoch` counter and publishes `iam.changed` carrying the new value; this service drops every cached decision, re-syncs the rooms of all open sockets (one request per account and project) and emits `permissions:changed` `{projectId, namespaces}` so the client refetches its permissions. Pub/Sub keeps nothing, so every replica also polls the counter every 5 seconds (`src/iam-epoch.ts`): a value above the last one seen means an announcement went by unseen, and it reacts as it would to the announcement (an announcement that did arrive is recorded, so the poll does not react twice). The same re-sync also runs every 60 seconds as a last backstop.
5. **A socket ends with its token.** About a minute before the access token's `exp` the server emits `reauth` (refresh the session, then reconnect so the handshake carries the new cookie); at `exp` it disconnects, and the client reconnects on a server-initiated disconnect. One shared scan every 5 seconds handles all sockets, not a timer per socket.

Exceptions: a payload-free `agent.session.started` (`project_id`, `conversation_id` only) also goes to the tasks room so task boards refresh, and `agent.acp_bridge.status` (no conversation content) goes to the tasks room. Owner-private agent events and notifications are routed to the owner's / recipient's own room.

Nothing is persisted: sessions live in memory for the life of the socket. Valkey is used to subscribe to `paca.events` and, for the sync protocol below, to read the API's per-project change log.

## Sync protocol (resumable change feed)

The plain `event` path is at-most-once: Pub/Sub keeps nothing, so a dropped
socket, a `reauth` reconnect or a restart loses events, and a client's only
defence is to refetch everything after it reconnects. The sync protocol gives
each project an ordered, replayable log of changes; a client that joins with
`sync:join` receives them in order and resumes from its cursor after any
absence, refetching only when the history cannot be replayed. It is the model
collaborative editing already follows (a delta, resume by state vector, an
explicit lifecycle), applied to entity data, which needs ordered deltas rather
than a CRDT.

It is off in the web app unless `localStorage["paca.realtime.sync"] = "1"` (or
`VITE_REALTIME_SYNC=true`; `E2E_SYNC=1` turns it on for
`apps/e2e/tests/realtime/live-updates-ui.spec.ts`), and off on the server with
`SYNC_ENABLED=false`. A client falls back to `join` when the server refuses.

### The change log

`services/api` writes it from `events.Fanout` / `events.PublishRealtime`, for
every event that has a project-room authorization rule (`internal/events/changes.go`,
`internal/platform/messaging/changelog.go`). Per project, two keys sharing a
hash tag (one cluster slot):

| Key | Content |
|---|---|
| `paca:changes:{<project>}:meta` | hash `seq` (last allocated) and `epoch` (ms when the counter was created). Never expires. |
| `paca:changes:{<project>}:stream` | the entries, id `<seq>-0`, capped at about 10 000 and expiring after 7 days without a write |

One Lua script allocates the `seq` and appends the entry, so stream order is
exactly `seq` order even with concurrent writers, and there is never a hole. A
cursor therefore turns into a range (`(<cursor>-0` to `+`) with no scan. The
`epoch` changes if the counter is lost (a flushed Valkey): it tells a reader its
cursor means nothing any more. Each entry holds `id`, `type`, `op`, `ts`, the
payload, the `authz` descriptor, the actor and origin, and optionally `entity`.

`entity` is the entity as it is after the change, so a client patches its cache
instead of refetching (`events.Event.Snapshot`). It goes into the change log
only, never into the activity stream, plugin events or the Pub/Sub message, is
dropped above 64 KB (the client then refetches), is never used to decide who
may see the event, and leaves out what a viewer must not inherit from the
actor: a task's description (collaborative editing owns it), and anything
personal, which is why a view's `config` is not attached. Today tasks and
sprints carry one; their snapshots are built next to the route response so the
two cannot drift (`TestSprintSnapshot_MatchesTheRouteResponse`).

The Pub/Sub message carries the same `seq` and `epoch`; the log is written
first, so a message with a `seq` is already replayable. A failed log write is
not fatal: the legacy message still goes out, without a `seq`.

### Wire protocol

| Event | Direction | Meaning |
|---|---|---|
| `sync:join` `{projectId, cursor, epoch}` (ack) | C→S | Same rooms as `join`, plus the feed from `cursor`. Ack: `{ok, namespaces, epoch, head, mode}`; `mode` is `live` (up to date; also a first join, which starts at the head), `replay` (the missed changes were emitted before the ack) or `reset` (cannot be replayed: refetch). `{ok: false}` makes the client use `join`. |
| `sync:changes` `{projectId, epoch, from, to, changes}` | S→C | Changes `from+1 … to`, gapless, batched. A change the account may not see (or whose room it is not in) is `{seq, skip: true}`. Apply only if `from` equals the client's cursor. |
| `sync:reset` `{projectId, epoch, head}` | S→C | The log was recreated or the client's entries were trimmed: refetch. |
| `sync:heartbeat` `{projectId, epoch, head}`, every 15 s | S→C | Lets the client notice a lost last batch. |
| `sync:leave` | C→S | Same as `leave`. |

A client applies a batch only when `from` equals its cursor, drops one it has
already applied, asks to re-join (with its cursor) on a gap or a changed epoch,
and does the same after two heartbeats in a row show it behind the head. A join
more than 1 000 changes behind, behind the oldest kept entry, with a different
epoch, or ahead of the log is answered with `reset`.

### The hub (`src/sync.ts`)

- The log is the data. The Pub/Sub message with a `seq` only nudges the hub, and a 5 s poll heals a missed nudge, so a lost message costs at most one interval, never a change. Each replica reads the log from each of its own sockets' cursors; replicas never talk to each other.
- Joins, replays and pumps of one project run one after another, so a socket never sees batches out of order.
- Authorization is the same question the plain path asks, answered by the API through the `AccessDecider`: the entry names an action and a resource. What the account may not read arrives as a bare skip, so its sequence stays gapless. A replay is judged by what the account may read **now**, not when the change was made.
- An account that holds nothing in the project (a non-member, or a member who lost every role) gets no feed at all, not even skips, which would still show how active somebody else's project is. Its cursor moves on silently; what it gains later is announced by `permissions:changed`, which makes the client refetch.
- If the API cannot be reached for an access check, the socket's cursor stays where it was and the next pump retries: a change is never dropped because a check failed.
- A syncing socket does not get the plain `event` for a change that is in the log. Events that are not logged (agent conversation content, notifications) still arrive as before.

### The web client

- `apps/web/src/lib/sync-engine.ts` keeps the epoch and cursor per project and applies the rules above; `socket-client.ts` joins with `sync:join`. A join that gets **no answer** keeps the cursor and is retried, so a client that was away resumes from where it was; only a refusal (or exhausted retries) uses the plain `join`.
- Resumed or not, a change takes the same path as a plain event: the ordered rule table `EVENT_RULES` in `src/lib/project-event-rules.ts`. The first rule that matches the type and can handle the event wins; a rule with nothing to act on (a comment that names no task) passes to the broader rule after it.
- `src/lib/cache-patch.ts` patches the cache from `entity` where there is one. A task's own query and every cached list row holding it are updated in place; lists are refetched (active ones only) only when a field a list can filter, group or sort on changed, or the task was created or deleted, because lists are filtered on the server and the client cannot decide membership itself. A title-only edit refetches nothing. A sprint is patched the same way; an environment's status is written straight from its event.
- Events that name their entity refresh only that entity's own part: a task comment refreshes the task's timeline, an attachment its attachments, a link its links, a doc comment that doc's timeline, `view.task_moved` that view's positions. A change with no snapshot (an automation or worker edit, a bulk move) falls back to invalidating, as the plain path does.
- The task schema (types, statuses, custom fields) refreshes its own query; tasks hold ids, so a rename, reorder or new default does not refetch task views, while a delete or a status edit (which can change what counts as done in subtask counts) does. Members and project agents are refetched, not patched (their lists are small and the event has no member shape), but the id to name and avatar lookups (`MemberDirectory`, `AgentDirectory`) are refreshed, which only the acting client's own mutations used to do.

### Limits and what is not built

- **Not transactional with the commit.** The log is written just after the database commit, because the repositories each run their own transaction and none takes one from the context. A crash between the commit and the log write still loses the change. Closing that needs a transactional outbox table written in the mutation's transaction, and with it a per-entity `version` column (rows have none) for duplicate and stale-replay guarding, optimistic mutations and `If-Match` on REST writes (which would also close the known race where a save overwrites an agent's write just merged into a live document).
- **No self-echo suppression.** The client that made a change also receives it, and applies it a second time (idempotently, over what its own mutation response already wrote). Stamping a mutation with a client operation id (a header the API echoes into the change) would let it skip its own echo, and together with the entity `version` would make optimistic mutations (a status drag, a reorder) safe to reconcile.
- **The first join starts at the head.** A change made after a page's first requests but before its join is acknowledged is not in the feed (the plain protocol has the same window). Closing it needs the log head to travel with REST responses.
- **Three hand-kept tables** map an event type to its authorization, its room and its cache effect (see Event format). Only the API side has a completeness test; one registry generated for all three would turn a forgotten rule into a failing build.
- **The plain path is still there.** Removing it (and the Pub/Sub nudge) should wait until the rest of `apps/e2e` has run with the flag on, the realtime logs of an internal environment show no `sync access check failed` or unexpected resets, and the default is flipped.
- The per-event access check (cached 30 s) still costs an API round trip per account and resource; an `iam_epoch`-keyed local snapshot would remove it.
- **Retention is a choice, not a guarantee.** About 10 000 entries and 7 days per project bound the longest absence a client can resume from; beyond that it gets `reset` and refetches.
- **Snapshots are entity-level.** An `entity` is checked with the event's own action and resource, so it is right only for data that is the same for every viewer; there is no field-level filtering. Plugins cannot yet emit entity changes of their own.
- Project-level presence (who is viewing which task) is not built; it would reuse the awareness idea of collaborative editing and stay out of the log.

## Running more than one replica

What already works across replicas, because every replica reads the same Valkey and holds only its own sockets:

- **Events and the sync protocol.** Each replica subscribes to `paca.events` and reads the change log from each of its own sockets' cursors; replicas never talk to each other.
- **Authorization and IAM changes.** Decisions come from the API; every replica gets `iam.changed` and also polls the `paca:iam:epoch` counter every 5 s, so an announcement a replica missed is noticed in seconds, not at the next 60 s pass.

What does **not** work yet:

- **Collaborative editing.** A document's `Y.Doc` lives in one process. Two editors of the same document on different replicas would not see each other and would diverge. Fixing it needs either routing all editors of a resource to one replica (the Socket.IO connection carries many resources, so this needs a connection per edited resource with hash routing on it) or sharing the document state through a Socket.IO adapter. Neither is built.
- **Socket.IO's polling transport** (the client's fallback) needs sticky sessions at the gateway, as the handshake spans several requests.

So the Helm chart refuses `realtime.replicaCount > 1` unless `realtime.allowMultipleReplicas=true`. Set it only with a gateway that pins a connection to a replica and with collaborative editing out of use, or accept the divergence.

## Linting & type checking

```sh
bun run lint    # biome check
bun run check   # tsc --noEmit
```

## Collaborative editing

Project docs and task descriptions (the BlockNote editors in `apps/web`) are
edited together through Yjs. This is the model the [sync protocol](#sync-protocol-resumable-change-feed)
generalises to the rest of the data: the update *is* the delta (no refetch), it
is idempotent, a reconnect resumes by state vector, sessions have an explicit
lifecycle (`ready` / `offline` / `lost`, `collab:revoked`, `collab:error`), each
resource has one in-memory authority with backpressure, and access is
re-checked when IAM changes. The two meet in two places: a document's or task
description's *content* stays in Yjs, so no snapshot is attached to a doc change
and a task's description is left out of its snapshot, and a REST write by
an agent is folded into the live document (below) while the same change reaches
other pages as an ordinary change. The service keeps one in-memory `Y.Doc` per edited
resource and relays updates and cursor awareness between its editors over the
existing Socket.IO connection (`src/collab.ts`):

| Event (client → server) | Meaning |
|---|---|
| `collab:join` `{projectId, kind: "doc"\|"task", id, stateVector}` (ack) | Join; requires `docs:write` / `tasks:write` on the resource. The ack carries the missing state and `seed` |
| `collab:update` / `collab:awareness` `{room, update}` | Document / cursor updates, relayed to the other members |
| `collab:leave` `{room}` | Leave |

Server → client: `collab:update`, `collab:awareness`, `collab:revoked` (access lost), `collab:seed-cancel` (the seed grant moved on: stop writing), `collab:error` `{room, reason}` (an update was dropped: `unavailable`, `rate`, `too_large` or `backlog`; the client re-joins to resync).

Limits: 5 MB per update, 64 MB of accepted updates per room, a per-socket message rate limit, 100 updates waiting on an access check per socket+room, 10 rooms per account. An access check that *fails* (API unreachable) never evicts an editor; only an explicit denial does.

- **Authorization**: every update is checked against IAM through the shared `AccessDecider`; IAM changes re-check all members, and a periodic pass (60 s) covers a missed announcement.
- **Persistence**: none in this service. Clients still save block JSON through the REST API, so MCP, search and exports are unchanged. An empty room is dropped 30 s after its last editor leaves and re-seeded from the stored content.
- **Seeding**: the first joiner of an empty room gets `seed: true` and is the only client that writes the stored content into the Y.Doc. If that client leaves, or does not produce content within 10 s, the grant moves to another member (`collab:seed`). Clients never write the stored copy over content already in the room.
- **Capacity**: 5 MB per update, 2 000 rooms overall and 10 rooms per account.
- **Scaling**: rooms live in one process memory; running several replicas needs sticky routing by resource (or a Socket.IO adapter plus shared doc state); see [Running more than one replica](#running-more-than-one-replica).
- **REST writes (agent / MCP / scripts)**: the API announces every `doc.updated` / `task.updated` with the changed field's old and new block JSON, and the hub folds a write made by someone who is *not* in the room into the live `Y.Doc` (`src/collab-blocks.ts`) as a block-level three-way merge (base = old, theirs = new, ours = the room), then broadcasts the result to every editor:
  - added blocks are inserted after their previous sibling; changed blocks are replaced, and removed blocks deleted, **only while the room's copy is still what it was before the write**. A block someone edited live keeps their version (logged as a conflict) and their next save stores it. Everything else, including cursors, is untouched.
  - blocks need no ids in the stored JSON: id-less (or invented) ids are recovered by aligning siblings on content (web saves strip ids, so this is the normal path). Leftover blocks between two aligned ones are paired by type, in order, when both sides have equally many; with unequal counts they are paired only when their text is alike (shared prefix/suffix, ≥ 50 %), the rest being additions / removals. So an inserted block is not mistaken for an edited neighbour unless it reads like it.
  - a merge waits for editor updates already received but still in their access check, and external writes count against the room's byte budget like editor updates.
  - the save of an editor who is in the room comes back as the same event and is ignored. With no room open nothing happens: the next editor seeds from the stored content.
  - limits: moves / reorders are not merged; the stored copy still holds the agent's version until an editor next saves, and a save that races the merge by a few milliseconds can still overwrite the agent's write in the database (closing that needs a version check on the REST write).
  - "Write with AI" on task descriptions is still applied once from the requesting client.
