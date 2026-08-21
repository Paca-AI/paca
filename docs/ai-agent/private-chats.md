# Owner-private Project Chats

Project Chats are persistent, owner-private conversations between one human
project member and an agent. They are deliberately separate from task-triggered
agent executions and from task comments.

This document describes the contract introduced by migration `000040`. The SQL
schema remains the storage source of truth; the terms and invariants below are
the product and service contract.

## Scope

- A chat session belongs to one project, one human owner, and one agent.
- `llm` agents execute private chat turns through the authoritative turn
  protocol described below.
- Private `acp` chat turns currently fail before their input is dispatched to a
  local bridge with `acp_private_runtime_not_isolated`. A local ACP process can
  access host files, credentials, and network services, so it cannot satisfy the
  server-enforced no-task-mutation boundary without a real OS/container
  sandbox. Task-, comment-, and automation-triggered ACP executions keep their
  existing behavior.
- Version one accepts context and publication targets only from the same
  project. It does not support files, images, cross-project context, transcript
  publication, or multi-target publication.

## Terms

| Term | Contract |
|---|---|
| **Session** | The user-visible persistent conversation and permanent Chats URL. History is listed and opened by `session_id`. |
| **Turn** | One user input through one stable terminal result. A turn is the authoritative unit for status, result, context, cancellation, and conclusion eligibility. |
| **Conversation** | Runtime continuity used by an execution backend. It is not a user-visible thread and is not a stable answer boundary. |
| **Run** | One fenced execution attempt for a turn. Recovery creates another attempt; run records and events are diagnostic history. |
| **Result** | The immutable terminal outcome of a turn. Only `succeeded` may carry a stable output. |
| **Context snapshot** | The immutable, bounded, canonical manifest and rendered input captured for one turn. Context is untrusted data and never grants tools. |
| **Internal handoff** | A task-triggered execution result used by later task executions. It is not a user publication. |
| **Writeback publication** | The immutable audit anchor for one human-confirmed writeback to exactly one task. It records either a description proposal or a frozen activity summary; the private transcript is never shared. |

## Starting a chat

The task-detail, board, list, and timeline entry points always open a new local
draft. The first submit creates a new session and includes the current task as a
required context source. They never look up or resume a recent session. Existing
sessions are opened explicitly from Chats history.

A normal new-chat draft may select multiple same-project task, session, or run
references. The client sends references, not copied source text. Source
selection is authorized when it is saved, again when a turn snapshot is built,
and again before a conclusion is confirmed.

Create and append commands require an `Idempotency-Key`. A retry with the same
raw command returns the original frozen bundle even when live source text,
agent configuration, runtime reuse state, or the derived execution deadline has
changed. Reusing a key for a different command returns
`IDEMPOTENCY_CONFLICT`.

## Transaction and context boundary

Creating the first turn writes the session, turn, canonical snapshot,
conversation, run, and `agent.turn.requested` outbox record atomically. Appending
a turn writes the corresponding turn-scoped records atomically. There is at most
one queued or running turn per session.

The snapshot builder:

1. resolves every reference server-side under the current viewer;
2. rejects cross-project, inaccessible, missing, duplicate, or over-limit
   sources;
3. preserves source order and records source type, version, audience, capture
   time, canonical content hash, rendered-text hash, byte counts, and the
   complete manifest hash;
4. bounds the rendered snapshot and every item; and
5. marks the material as untrusted context rather than instructions.

Private turns use a canonical typed, deny-by-default tool policy. Only the
read-only capabilities listed in that policy may be exposed. Task mutation,
plugin mutation, wildcard capabilities, agent-provided MCP configuration,
skills, and long-lived Paca credentials are excluded from authoritative private
LLM turns. The database stores and validates the canonical policy and its hash;
the runner enforces the same contract when constructing the sandbox.

## Execution and recovery

The API outbox publisher appends `agent.turn.requested` to the durable turn
stream. A runner claims the current run with a random claim token and lease.
Every event, lease renewal, finalization, and control action is fenced by the
exact turn, run, attempt, and claim token.

An expired lease is retired before another attempt is created. Late events and
finalization from an older attempt are rejected. Deadlines, authorization
revocation, user stop, and runner terminalization all create exactly one
immutable result and a minimal `agent.turn.finished` outbox event. Stop also
creates a durable control event so the physical LLM/ACP process is cancelled,
not merely the database row.

Terminal states are:

- `succeeded`: requires a persisted stable-output event and matching stable
  output/hash;
- `failed`, `stopped`, `cancelled`, `timed_out`, or `no_output`: may carry an
  error code/message but never a stable output.

The Chats transcript renders the user message from the turn and the assistant
answer only from a successful immutable result. Event text is execution detail
and is never promoted to a second answer.

## Writing back to a task

Private completion never creates task-visible content. A human with
`agents.read`, `tasks.read`, and `tasks.write` starts writeback through the
composer command menu. Clicking its shortcut icon or typing `/` opens the same
menu. Choosing **Update description** or **Record conclusion** only inserts the
stable English protocol command (`/update-description` or
`/record-conclusion`) into the composer; the human may add free-form
instructions and sends it like any other message. The persisted user input and
assistant result remain ordinary, visible transcript turns. Menu labels are
localized and do not repeat the protocol token.

The runner recognizes the stable English tokens, plus the previously exposed
Chinese aliases for compatibility, only in owner-private chat turns. For
**Update description**, it instructs the current chat agent to combine the
current task description with the full discussion and return a complete
standalone replacement rather than a chat summary. For **Record conclusion**,
it requests a concise standalone activity-history conclusion. Text after the
command is treated as revision guidance. In both cases the agent returns
content only, does not call mutation tools, and the stable result becomes the
immutable source/audit anchor.

The assistant reply is the review surface and is rendered unchanged. Once the
command turn succeeds, a generic choice card appears immediately above the
composer; it does not duplicate the assistant reply. The card contains only
the applicable actions as full-width vertical rows, one unlabeled revision
input, and Cancel/Confirm controls. The revision input is an implicit alternative
to the action rows rather than a separate "Revise" choice. Focusing the input
immediately deselects the action row and selects the revise path; selecting an
action clears the input. The primary control keeps the generic Confirm label in
every path. Confirming the revise path appends another visible command turn.
Confirming writeback runs the safe prepare/confirm boundary against the visible
stable result. Cancel is a local UI dismissal. A publication-history check
prevents a published source turn from being offered again after reload or on
another client.

Target selection is scoped to task context explicitly attached to the chat. A
single related task is selected without asking again. Multiple related tasks
show only those candidates in the same compact card; unrelated project tasks
are never listed. With no task context the writeback commands remain unavailable
until one is added.

The composer's `+` action is independent from commands and opens an extensible
resource menu rather than launching a picker directly. Its only current item
adds supplemental task/session/run context; the same menu is the reserved
expansion point for later image/file support instead of adding header controls.
The picker uses the user-facing language **Add context**, describes the selected
content as reference material for the agent, and avoids exposing turn-snapshot,
authorization, or other execution terminology.

The two commands are mutually exclusive workflows:

- **Update description** converts and freezes the agent-written standalone
  BlockNote proposal. Confirmation changes the description and creates one
  normal `task.updated` activity. The activity reuses the ordinary description
  diff and revert presentation; it does not add a source link or a separate
  conclusion entry.
- **Record conclusion** leaves the description unchanged and publishes the
  confirmed frozen summary in an ordinary task-activity row. When the current
  viewer still owns and may read the source chat, that row also offers the
  standard conversation-history link.

Writeback is two phase:

1. `prepare` selects one same-project target task and freezes the command turn's
   stable output, version, hash, expiry, and idempotency record. Description
   mode also freezes the current description and hash plus the proposed
   BlockNote description and hash. Summary-only mode freezes the same visible
   stable output as the activity summary.
2. `confirm` revalidates current ownership and permissions, locks the source and
   target, and atomically inserts the append-only audit publication and outbox
   record. Description mode locks the target task, rejects a changed baseline,
   applies the proposal, and inserts only the `task.updated` activity. Summary
   mode inserts only the minimal `agent.conclusion.published` activity
   projection. A conflict publishes nothing.

Each publication records the source turn, target task, human publisher, source
agent, frozen summary/version/hash, and idempotency key. New Project Chats
writebacks accept only `published` preparations without a related publication.
The schema and read models retain revision/withdrawal relations only for legacy
audit compatibility.

Task activity is chronological, append-only history rather than an editing
surface. Description writebacks look exactly like ordinary description updates
and retain the existing diff/revert behavior. Summary-only writebacks use the
existing activity-row presentation with their frozen summary and optional
owner-authorized conversation link; they do not introduce a dedicated card or
revise/withdraw actions. A correction is a new writeback event.
Description-mode publications also omit their internal summary fields from the
task-audience projection.
Source session/turn identifiers are returned only when the current viewer is the
chat owner and may still read the source. Other viewers receive
`source_accessible: false` with no private IDs or link.

## HTTP surface

All mutation routes below require JWT human authentication. Project permission
middleware and the service/repository owner checks both apply.

| Method | Route | Purpose |
|---|---|---|
| `GET` | `/api/v1/projects/:projectId/chat-sessions` | Owner session history, cursor-paginated across agents. |
| `POST` | `/api/v1/projects/:projectId/chat-sessions` | Create session + first turn. Requires `Idempotency-Key`. |
| `GET` | `/api/v1/projects/:projectId/chat-sessions/:sessionId` | Owner session permanent-link record. |
| `GET` | `/api/v1/projects/:projectId/chat-sessions/:sessionId/turns` | Turn history, newest first, paginated by `before_index`. |
| `GET` | `/api/v1/projects/:projectId/chat-sessions/:sessionId/turns/:turnId` | Turn, attempts, immutable result, and snapshot. |
| `POST` | `/api/v1/projects/:projectId/chat-sessions/:sessionId/turns` | Append a turn. Requires `Idempotency-Key`. |
| `POST` | `/api/v1/projects/:projectId/chat-sessions/:sessionId/turns/:turnId/stop` | Idempotently stop the owner turn. |
| `GET` | `/api/v1/projects/:projectId/turns/:turnId/events` | Fenced run events, cursor-paginated. |
| `GET/PUT` | `/api/v1/projects/:projectId/chat-sessions/:sessionId/context-sources` | Read or replace the live selection for the next turn. |
| `POST` | `/api/v1/projects/:projectId/turns/:turnId/conclusion-publications/prepare` | Freeze a new published writeback preparation. |
| `POST` | `/api/v1/projects/:projectId/conclusion-publications/confirm` | Confirm one preparation. Requires a separate `Idempotency-Key`. |
| `GET` | `/api/v1/projects/:projectId/tasks/:taskId/conclusion-publications` | Task-audience publication history with source redaction. |

Legacy session-backed conversations remain available as owner-gated, read-only
execution history. Project conversation message/control writes reject all
session-backed records; sessionless task and automation executions retain the
legacy execution API.

## Realtime contract

`agent.turn.finished` is routed only to `user:<actor_user_id>:agent-chat` and
contains identifiers and terminal status, not output text. The Web client
invalidates the session/turn queries and fetches the authoritative result.

Summary-only `agent.conclusion.published` events (and legacy compatible
`.revised`/`.withdrawn` events) contain only publication, project, target-task,
and kind identifiers. Description mode uses the same minimal invalidation event
but projects only `task.updated` into activity.
The task client refetches publication history and task activity. Realtime data
is never used to synthesize a stable answer or bypass source redaction.
