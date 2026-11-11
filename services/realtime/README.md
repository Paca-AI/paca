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
| `COLLAB_CLUSTER` | No     | `true`                | Set `false` to keep every collaborative-editing room in this process even with several replicas (see [Running more than one replica](#running-more-than-one-replica)). |
| `REPLICA_ID`   | No       | `$HOSTNAME-<random>`  | This replica's name in the collaborative-editing cluster; must be unique per running replica. |
| `ACCESS_CACHE_TTL_MS` | No | `30000`            | How long an access decision (account, action, resource) is cached. Kept short on purpose: IAM conditions on attributes (a task's sprint, status, assignee, a doc's folder) change without any role change, so a longer cache would show or hide data after it should not. |
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

### Guarantees added on top of the feed

- **Transactional outbox.** Triggers on `tasks` and `sprints` write a row to `change_outbox` in the same transaction as the change, and the API's change reconciler (`internal/worker/change_reconciler.go`) announces what the normal path did not. After the commit the service announces the change itself and the reconciler finds nothing to do; if the process dies in between (or the Valkey write fails), the reconciler announces it a few seconds later. It compares each row's `version` with the version last logged for the entity (`paca:changes:{<project>}:last`, a hash `entity id -> "<ms>:<version>"`), so a second change made right after an announced one is not hidden by the first. Rows are claimed with `FOR UPDATE SKIP LOCKED`, so several API replicas share the work, and a delete is announced as a delete. Rows older than the change log's retention (7 days) are pruned unhandled, so a long Valkey outage cannot grow the table without bound; the reconciler logs a warning when it drops any.
- **Row versions and `If-Match`.** `tasks` and `sprints` carry a `version` (a `BEFORE UPDATE` trigger adds one on every write; it is in the REST body, the change's snapshot and the realtime payload). `PATCH` on a task or a sprint accepts `If-Match: "<version>"` and answers `412` (`VERSION_CONFLICT`) when the row has moved on, so a save cannot silently overwrite an agent's write. Without the header, behaviour is unchanged. Docs have no version column yet.
- **Self-echo suppression.** The web client stamps every mutation with `X-Client-Op-Id`; the API copies it into the change (`client_op_id`) and the change reaches every socket with it. The client that sent it already applied its own response and skips the echo (`client-ops.ts`, `cache-patch.ts`); everyone else applies it. A snapshot older than what the cache holds (by `version`) is also dropped.
- **No window before the first join.** Responses under `/projects/{id}` carry `X-Change-Seq` and `X-Change-Epoch`, the log head read *before* the handler ran (so the data in the body is at least that new). The web client remembers the head of the first response per project (`change-heads.ts`) and seeds its cursor from it, so a change made between that first read and the acknowledged join is replayed instead of missed.
- **One event registry.** `services/api/internal/events/registry.json` (generated from the Go tables by `go test ./internal/events -run TestRegistryIsUpToDate -update-registry`) lists each event type with its authorization, room and cache effect. Tests in the API, this service (`registry.test.ts`) and the web app (`project-event-rules.registry.test.ts`) read it, so a type added in one place and forgotten in another fails a build. Regenerate it after changing an event.

### Limits and what is not built

- **The plain path is still there.** Removing it (and the Pub/Sub nudge) should wait until the rest of `apps/e2e` has run with the flag on, the realtime logs of an internal environment show no `sync access check failed` or unexpected resets, and the default is flipped.
- **Optimistic mutations are not built.** Versions, `If-Match` and the client op id make a status drag or a reorder safe to apply before the server answers and reconcile after, but no screen does it yet.
- **The per-event access check** (cached 30 s, see `ACCESS_CACHE_TTL_MS`) still costs an API round trip per account and resource. It was left as is deliberately: IAM attribute conditions change without a role change, so a longer cache, or a snapshot keyed on `iam_epoch` alone, would serve a stale answer.
- **Versions cover tasks and sprints.** Other entities are logged and patched but have no `version`, so a stale-snapshot guard and `If-Match` do not apply to them. The outbox covers tasks and sprints (a sprint completion's task moves included); other entities rely on the after-commit announcement. Documents are deliberately left out: a document's sync (content hash, `If-Match`, a change feed with tombstones) belongs to the docs pull/push work (discussion #722), and collaborative editing persists them too often for a per-write outbox row.
- **Same origin.** `X-Change-Seq` / `X-Change-Epoch` (response) and `If-Match` / `X-Client-Op-Id` (request) assume the web app reaches the API through the same origin, as the bundled proxy and Helm ingress do. A cross-origin deployment must add them to the CORS `Access-Control-Allow-Headers` / `Access-Control-Expose-Headers`.
- **Retention is a choice, not a guarantee.** About 10 000 entries and 7 days per project bound the longest absence a client can resume from; beyond that it gets `reset` and refetches.
- **Snapshots are entity-level.** An `entity` is checked with the event's own action and resource, so it is right only for data that is the same for every viewer; there is no field-level filtering. Plugins cannot yet emit entity changes of their own.
- **Project-level presence** (who is viewing which task) is not built. It is a product feature rather than a sync gap; it would reuse the awareness idea of collaborative editing and stay out of the log.

## Running more than one replica

Every replica reads the same Valkey and holds only its own sockets:

- **Events and the sync protocol.** Each replica subscribes to `paca.events` and reads the change log from each of its own sockets' cursors; replicas never talk to each other for this.
- **Authorization and IAM changes.** Decisions come from the API; every replica gets `iam.changed` and also polls the `paca:iam:epoch` counter every 5 s, so an announcement a replica missed is noticed in seconds, not at the next 60 s pass.
- **Collaborative editing.** A room's `Y.Doc` still lives in one process, which keeps seeding and the merge of agent writes as they are, but editors of one document may now be on different replicas (`src/collab-cluster.ts`):
  - A room has an **owner**: the replica holding its lease (`paca:collab:owner:<room>`, `SET NX` with a 20 s TTL the owner renews every 5 s). The first replica to see a join takes it; a join on another replica makes that socket a *remote member*, whose join, updates, awareness and leave are forwarded to the owner over a per-replica channel (`paca:collab:in:<replica>`), and what the hub sends it goes back the same way. The owner's hub treats it like any member, so access checks, seeding, rate limits and external-write merges are unchanged.
  - Replicas heartbeat (`paca:collab:replica:<id>`, every 3 s, 10 s TTL). An owner drops the members of a replica that stopped; a member whose room lost its owner is told `collab:error unavailable`, so its client re-joins, finds or takes the lease and re-syncs from the state it holds. Content only the dead owner held is lost, as after a restart of a single replica; clients keep their copy and re-send it.
  - If Valkey cannot be reached a replica acts as owner of what it is asked about (the old single-process behaviour); `COLLAB_CLUSTER=false` forces it. Give each running replica a distinct `REPLICA_ID` (the default is the hostname plus a random suffix, right for Kubernetes pods).

What still needs care:

- **Socket.IO's polling transport** (the client's fallback) needs sticky sessions at the gateway, as the handshake spans several requests. WebSocket connections need nothing special.
- A room's traffic crosses Valkey once per remote member, so a gateway that keeps the editors of a resource on one replica (hash routing) is cheaper, but is no longer needed for correctness.

The Helm chart still refuses `realtime.replicaCount > 1` unless `realtime.allowMultipleReplicas=true`, now because of the polling transport's stickiness; it is a deliberate acknowledgement, not a known divergence. `apps/e2e/tests/realtime/sync-replicas.spec.ts` starts a second replica next to the stack's and checks edits, late joiners and the loss of a replica across the two.

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
- **Scaling**: a room lives in one process (its owner's memory) and other replicas forward to it; see [Running more than one replica](#running-more-than-one-replica).
- **REST writes (agent / MCP / scripts)**: the API announces every `doc.updated` / `task.updated` with the changed field's old and new block JSON, and the hub folds a write made by someone who is *not* in the room into the live `Y.Doc` (`src/collab-blocks.ts`) as a block-level three-way merge (base = old, theirs = new, ours = the room), then broadcasts the result to every editor:
  - added blocks are inserted after their previous sibling; changed blocks are replaced, and removed blocks deleted, **only while the room's copy is still what it was before the write**. A block someone edited live keeps their version (logged as a conflict) and their next save stores it. Everything else, including cursors, is untouched.
  - blocks need no ids in the stored JSON: id-less (or invented) ids are recovered by aligning siblings on content (web saves strip ids, so this is the normal path). Leftover blocks between two aligned ones are paired by type, in order, when both sides have equally many; with unequal counts they are paired only when their text is alike (shared prefix/suffix, ≥ 50 %), the rest being additions / removals. So an inserted block is not mistaken for an edited neighbour unless it reads like it.
  - a merge waits for editor updates already received but still in their access check, and external writes count against the room's byte budget like editor updates.
  - the save of an editor who is in the room comes back as the same event and is ignored. It is recognised by the editor's socket, not by the account: the web client names its connected socket on every mutation (`X-Collab-Socket`), the API passes it on as the envelope's `collab_socket`, and only a socket that is a member of the room counts as its echo. A write with the same account but no editor socket (an agent or MCP using that user's API key) is merged like anyone else's. With no room open nothing happens: the next editor seeds from the stored content.
  - limits: moves / reorders are not merged; the stored copy still holds the agent's version until an editor next saves, and a save that races the merge by a few milliseconds can still overwrite the agent's write in the database for a **doc** (a task's save can name the version it read with `If-Match` and get a `412`; the web editor does not send it yet).
  - "Write with AI" on task descriptions is still applied once from the requesting client.
