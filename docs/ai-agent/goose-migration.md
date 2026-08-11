# Migrating `llm`-type Agents from OpenHands to Goose

**Status:** All five phases of the full-cutover push are code-complete and
individually verified against real infrastructure (Docker, the dev
Postgres, the dev Valkey, and — as of dev-environment wiring — the actual
`docker compose` dev stack itself, including a real `goose serve`
container reached through it). `services/ai-agent`'s source has been fully
removed from the repository and its deployment/CI surfaces (compose files,
CD image jobs, install/upgrade scripts, PR CI) — see
[Full Removal of services/ai-agent](#full-removal-of-servicesai-agent).
**The real production stack has still not been cut over** — a real,
currently-running `docker compose` deployment on this machine
(`/home/haihuynh/paca-production/`) is deliberately untouched pending
explicit confirmation; see [Production Cutover: In Progress](#production-cutover-in-progress).
A real, confirmed functional gap was found in the removal process, not
just a docs mismatch: `agent-runner` never merges Paca's default skill set
or plugin-contributed skills into a conversation the way `services/ai-agent`
did — see the gap notice in [default-skills.md](default-skills.md).
Earlier: asked directly about a full cutover, a gap analysis found real
missing functionality (repo cloning/PR creation, chat-conversation
continuity, `acp`-type dispatch), not just polish, plus three more found
only while pursuing that cutover (conversation events never reached
Postgres; both services would double-execute any agent gated to
agent-runner; a fresh agent-runner deployment would replay a Valkey
stream's entire history on first start). All fixed and verified — see
[Dev Environment Wiring](#dev-environment-wiring) and
[Open Risks / Follow-ups](#open-risks--follow-ups), the latter also
covering a real-conversation failure mode traced to an invalid test
fixture's API key, not a code bug. See
[Gap Analysis: What's Still Missing for a Full Cutover](#gap-analysis-whats-still-missing-for-a-full-cutover)
for the original list,
[Closing the Gaps: Realtime Events, Conversation Status, and Control Messages](#closing-the-gaps-realtime-events-conversation-status-and-control-messages),
[Repository Tools for Coding Tasks](#repository-tools-for-coding-tasks),
[Conversation Event Persistence](#conversation-event-persistence),
[Chat Conversation Continuity](#chat-conversation-continuity),
[ACP-Type Agent Dispatch](#acp-type-agent-dispatch),
and
[Double-Execution Coordination](#double-execution-coordination)
for what's been closed so far.

Step 1 (protocol spike): a Go
ACP client package (`services/agent-runner/internal/acp`) exists and is
verified against a live `goose serve` container. Step 2 (bridge): Goose is
now a first-class `acp_provider` in `apps/acp-bridge`, verified against a
real `goose acp` subprocess end to end. Step 3 (container orchestration):
`services/agent-runner` exists as a runnable Go service — Docker container
lifecycle, the executor tying it to the ACP client, Postgres repositories,
and a Valkey trigger consumer/event publisher are all built and each
verified for real, including both Dockerfiles (the Go binary and the
Goose-based sandbox image, the latter digest-pinned since no versioned
release image exists upstream) and the MCP-server wiring, which needed a
real fix — a genuinely wrong wire format that made `goose serve` hang
rather than error. See
[services/agent-runner: What Was Built](#servicesagent-runner-what-was-built)
and
[Sandbox Image and the mcpServers Wire-Format Bug](#sandbox-image-and-the-mcpservers-wire-format-bug)
below.

This document captures the research and a hands-on spike behind replacing
OpenHands (the Python SDK + `services/ai-agent` + the OpenHands `agent-server`
Docker image) with [Goose](https://github.com/block/goose) as the execution
engine for `llm`-type agent conversations, with `services/api` (Go) driving
container lifecycle and the agent protocol directly instead of through a
separate Python orchestration service.

## Table of Contents

- [Motivation](#motivation)
- [Current State](#current-state)
- [What Goose Provides](#what-goose-provides)
- [Verdict on the Proposed Design](#verdict-on-the-proposed-design)
- [Target Architecture](#target-architecture)
- [Component Mapping](#component-mapping)
- [What Doesn't Change](#what-doesnt-change)
- [Spike: Verifying the ACP-over-HTTP Protocol](#spike-verifying-the-acp-over-http-protocol)
- [Go ACP Client Package](#go-acp-client-package)
- [Migration Plan](#migration-plan)
- [Goose as an ACP Bridge Provider](#goose-as-an-acp-bridge-provider)
- [services/agent-runner: What Was Built](#servicesagent-runner-what-was-built)
- [Sandbox Image and the mcpServers Wire-Format Bug](#sandbox-image-and-the-mcpservers-wire-format-bug)
- [Gap Analysis: What's Still Missing for a Full Cutover](#gap-analysis-whats-still-missing-for-a-full-cutover)
- [Closing the Gaps: Realtime Events, Conversation Status, and Control Messages](#closing-the-gaps-realtime-events-conversation-status-and-control-messages)
- [Repository Tools for Coding Tasks](#repository-tools-for-coding-tasks)
- [Conversation Event Persistence](#conversation-event-persistence)
- [Chat Conversation Continuity](#chat-conversation-continuity)
- [ACP-Type Agent Dispatch](#acp-type-agent-dispatch)
- [Double-Execution Coordination](#double-execution-coordination)
- [Dev Environment Wiring](#dev-environment-wiring)
- [Production Cutover: In Progress](#production-cutover-in-progress)
- [Full Removal of services/ai-agent](#full-removal-of-servicesai-agent)
- [Chat UX Follow-up: Missing User Messages, Per-Chunk Refetching, Row-Per-Chunk Storage](#chat-ux-follow-up-missing-user-messages-per-chunk-refetching-row-per-chunk-storage)
- [Open Risks / Follow-ups](#open-risks--follow-ups)

---

## Motivation

`services/ai-agent` runs conversations inside a container built `FROM
ghcr.io/openhands/agent-server` — a full Python + npm toolchain per
conversation — orchestrated by a Python FastAPI service that exists mainly to
drive the OpenHands SDK. The ask: drop OpenHands for `llm`-type agents,
remove `services/ai-agent`, and have Go control agent containers directly
using the [Agent Client Protocol (ACP)](https://agentclientprotocol.com) to
talk to Goose running inside them, for lower per-conversation overhead and
one fewer service in the deploy.

## Current State

Two execution models exist today (see [overview.md](overview.md) for the
full picture):

- **`llm`** — [`docker_workspace.py`](../../services/ai-agent/src/agent/docker_workspace.py)
  spawns a container from `ghcr.io/paca-ai/paca-agent-server` (a thin wrapper
  around the pinned OpenHands `agent-server` image — see its
  [Dockerfile](../../services/agent-server/Dockerfile)), waits for `/health`,
  then drives it over HTTP via the OpenHands SDK's `RemoteWorkspace`. This is
  the path being replaced.
- **`acp`** — [`apps/acp-bridge`](../../apps/acp-bridge/README.md) is a local
  daemon the user runs themselves. It already uses the OpenHands SDK's
  `ACPAgent` purely as an **ACP client** to spawn Claude Code / Codex /
  Gemini CLI / a custom command as a subprocess, streaming events back to
  `services/ai-agent` over an authenticated WebSocket. This path is
  effectively unaffected — Goose just becomes another selectable
  `acp_provider` (`goose acp` over stdio).

Both `services/api`'s `go.mod` and its `internal/worker` package already have
what this migration needs: a Valkey stream client (`redis/go-redis/v9`,
matching the consumer-group pattern in
[`automation_consumer.go`](../../services/api/internal/worker/automation_consumer.go)),
and — via `testcontainers-go`, currently used only for integration tests —
transitive access to the official Docker Engine Go client. No new dependency
class needs to be introduced to do container orchestration from Go.

## What Goose Provides

- **`goose acp`** — runs Goose as an ACP server over stdio JSON-RPC. Drop-in
  replacement for "any ACP-compliant CLI" in the existing bridge model.
- **`goose serve`** — runs Goose as an ACP server over **HTTP + SSE** on
  `/acp`, authenticated via `X-Secret-Key` (`GOOSE_SERVER__SECRET_KEY`), with
  a `/status` health check. This is the piece that matters for the sandboxed
  path — network-reachable, matching the shape `docker_workspace.py` already
  expects.
- **`github.com/coder/acp-go-sdk`** (Apache-2.0, `v0.13.5`) — a Go ACP SDK
  with typed requests/responses for both Agent and Client roles, transport-
  agnostic (`NewClientSideConnection(client, reader, writer)` takes arbitrary
  `io.Reader`/`io.Writer`, not just stdio). In practice its `Connection` type
  assumes a single continuous duplex stream (stdio-shaped); `goose serve`'s
  wire format is one-POST-per-call with an SSE-framed response, not a
  continuous stream — see [Spike](#spike-verifying-the-acp-over-http-protocol)
  for why the client sketch here hand-rolls the transport instead of forcing
  that mismatch.
- **`ghcr.io/block/goose`** — an official image, Debian-bookworm-slim based,
  non-root (`uid 1000`, user `goose`) by default. Meaningfully lighter than
  the OpenHands agent-server image — but see
  [Open Risks](#open-risks--follow-ups) for a real gap in how it's tagged.
- Config/extensions/system-prompt map directly onto existing concepts:
  `config.yaml` (`active_provider`/`providers` for LLM, `extensions:` for MCP
  servers), env var overrides (`GOOSE_PROVIDER`, `GOOSE_MODEL`, provider API
  key env vars), and **recipes** (prompt + extensions + params bundle) as the
  closest match for per-agent system prompt + skill injection.
- Not in OpenHands today: Goose ships **adversary mode** (an independent
  reviewer watching tool calls) and **prompt-injection detection** guides — a
  security upgrade, not just parity.

## Verdict on the Proposed Design

The proposed shape — kill `services/ai-agent`, Go controls containers
directly, an ACP layer talks to Goose inside them — is sound and not a
stretch for this codebase; the pieces already exist (see
[Current State](#current-state)). One refinement: **keep agent orchestration
as its own deployable** rather than folding it into `services/api` itself —
call it `services/agent-runner`. Two reasons:

1. **Docker socket access is a distinct trust boundary.** Whatever process
   holds `/var/run/docker.sock` can typically escalate to host root — the
   same reason `services/ai-agent` is separate today and not exposed through
   the public gateway (see [service-boundaries.md](../architecture/service-boundaries.md)).
   Merging it into the service that also handles arbitrary authenticated
   HTTP traffic widens that blast radius for no real benefit.
2. **Different scaling axis.** `services/api` scales on request/response
   load; the agent runner scales on concurrent conversations (container
   lifecycle, long-lived SSE reads). Coupling them removes the ability to
   tune either independently.

Same boundary as today, same external contracts (Valkey topics, DB tables,
internal REST surface) — just a Go service instead of a Python one.

## Target Architecture

```
services/api (Go)                     — unchanged: agent CRUD, trigger publishing,
                                          conversation summaries, bridge-token issuance
        │ Valkey Stream "paca:agent:triggers"        (unchanged wire format)
        ▼
services/agent-runner (Go, replaces services/ai-agent)
  • Valkey stream consumer (same consumer-group pattern already used in Go)
  • llm-type: spawns container via Docker Engine Go SDK, runs `goose serve`,
    speaks ACP over HTTP+SSE (internal/acp package — see below)
  • acp-type: unchanged in shape — still dispatches to apps/acp-bridge over
    Valkey pub/sub + WebSocket
  • Publishes to Valkey Stream "paca:agent:events"    (unchanged wire format)
        │
        ▼
services/realtime  — completely unchanged, still just fans Valkey → Socket.IO
        │
        ▼
Agent containers: ghcr.io/block/goose:<pinned build>, Paca MCP preinstalled
  (replaces ghcr.io/paca-ai/paca-agent-server FROM ghcr.io/openhands/agent-server)
```

## Component Mapping

| Today (OpenHands) | Goose equivalent | Notes |
|---|---|---|
| `agent-server` image, `RemoteWorkspace` over HTTP | `ghcr.io/block/goose` image, `goose serve` over ACP/HTTP+SSE | Verified live in the spike below |
| `Agent(mcp_config=...)` in [`builder.py`](../../services/ai-agent/src/agent/builder.py) | `extensions:` in Goose config / `session/new`'s `mcpServers` param | `AgentMCPServer{Transport, Command, Args, URL, Env}` matches this shape closely, but the exact `mcpServers` param wire format was **not** exercised in the spike (always sent empty) — verify before relying on it |
| `AgentContext(skills=..., system_message_suffix=...)` | Recipe `prompt:` field + always-inline skill content | Keyword-triggered skills (`KeywordTrigger`) have no exact analog — closest fit is Goose's custom slash commands, the same pattern the `acp` path already uses (`/paca` prefix). Needs a small prototype, not a clean swap. |
| `RepoTokenSecretSource` (masks token in event output) | Not confirmed in Goose | **Security review needed** before shipping — don't assume Goose masks injected secrets in its event/log stream the way OpenHands' `SecretSource` does |
| `OH_EXTRA_PYTHON_PATH` import hack for `list_repositories`/`clone_repository` | Expose as two more tools on the existing `apps/mcp` Paca MCP server | Real simplification, not just parity |
| `conversation.run(max_iterations=...)` | **Must be enforced by the Go client itself** | Confirmed in the spike: Goose has no built-in turn cap — see below |
| Pause/resume/stop via in-memory `Conversation` registry | `session/prompt` calls against one `session/new` per container | Container-per-conversation model carries over unchanged; the multi-replica "who owns this conversation" problem is the same kind `acp_bridge.py`'s presence/dispatch system already solves — reuse that pattern |

## What Doesn't Change

Valkey stream schemas (`paca:agent:triggers` / `paca:agent:events`), the DB
schema, `services/realtime`, `apps/web`, the trigger model, and the `llm` vs
`acp` agent-type distinction at the product level. This is a swap of the
execution engine underneath an existing, working event pipeline.

---

## Spike: Verifying the ACP-over-HTTP Protocol

Goal: confirm `goose serve`'s `/acp` endpoint actually behaves the way the
docs describe, end to end, including a real tool call — before writing any
Go against it.

### Setup

- Pulled `ghcr.io/block/goose:latest` (digest
  `sha256:d85a724ee487425f38ce015323adf2003591268ee515d9018ac89450ed7d3a5a`).
  `goose --version` inside the image reports `1.30.0`.
- Ran `goose serve --host 0.0.0.0 --port 3284` in a container with
  `GOOSE_SERVER__SECRET_KEY` set, port-mapped to the host.
- Pointed it at Paca's own existing OpenAI-compatible mock —
  [`fake_llm_server.py`](../../services/ai-agent/tests/e2e/fake_llm_server.py),
  already used to test the OpenHands e2e path — via `GOOSE_PROVIDER=openai`,
  `OPENAI_HOST=http://172.17.0.1:8901` (the Docker bridge gateway IP),
  `OPENAI_API_KEY=fake-key`. No real API key or spend involved.
- Drove the whole thing with `curl`, no SDK, to get ground truth on the wire
  format before writing a client against it.

### Confirmed protocol shape

1. **Auth.** `X-Secret-Key: <secret>` on every request. Missing/wrong →
   `401`. `/status` returns bare `ok` with `200`.

2. **`initialize` needs no provider configured.**

   ```
   POST /acp
   X-Secret-Key: <secret>
   Content-Type: application/json
   Accept: application/json, text/event-stream

   {"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientCapabilities":{}}}
   ```

   Response is itself `Content-Type: text/event-stream` — every `/acp`
   response is SSE-framed, even a single-shot call:

   ```
   HTTP/1.1 200 OK
   content-type: text/event-stream
   acp-session-id: 803b1ade-8b25-416d-98a5-ba7cabcca107

   data: {"jsonrpc":"2.0","result":{"protocolVersion":1,"agentCapabilities":{...},"authMethods":[{"id":"goose-provider","name":"Configure Provider","description":"Run `goose configure` to set up your AI provider and API key"}]},"id":1}
   ```

   **Two distinct session concepts, easy to conflate:** the `Acp-Session-Id`
   *response header* from `initialize` is a transport/connection-scoped id
   that must be echoed as a *request header* on every subsequent call. The
   ACP `sessionId` in the JSON-RPC body (returned by `session/new`, e.g.
   `"20260810_1"`) is the protocol-level conversation id used in
   `session/prompt` params. They are not the same value and both are
   required.

3. **`session/new` hard-fails without a configured provider**
   (`"Failed to set provider: Could not configure agent: missing provider"`),
   confirming provider env vars must be set at container start, before Go
   ever calls this. With a provider configured:

   ```
   POST /acp
   Acp-Session-Id: 803b1ade-8b25-416d-98a5-ba7cabcca107   ← from initialize
   {"jsonrpc":"2.0","id":2,"method":"session/new","params":{"cwd":"/home/goose","mcpServers":[]}}

   → {"jsonrpc":"2.0","result":{"sessionId":"20260810_1","modes":{"currentModeId":"auto",...},...},"id":2}
   ```

   Default mode is `auto` (auto-approve tool calls) — no permission-prompt
   gating to route through in a headless flow, matching how the OpenHands
   sandbox behaves today.

   **Gotcha (my bug, not Goose's):** the image's default user is `goose`
   (uid 1000), not root. `cwd: "/root"` (`drwx------`, root-only) makes every
   tool call that spawns a subprocess fail with `Permission denied (os error
   13)` — the `chdir()` under the hood fails before the command ever runs.
   `/home/goose` (owned by `goose:goose`) works. Whatever spawns the
   container needs to set `cwd` to a directory the container's actual user
   can access — the same category of detail `docker_workspace.py` already
   gets right by mounting a real, owned workspace.

4. **`session/prompt` streams tool calls as two-part notifications, then a
   terminal response:**

   ```
   data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260810_1","update":{"sessionUpdate":"tool_call","toolCallId":"call_fake_1","title":"Developer: Shell"}}}

   data: {"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"20260810_1","update":{"sessionUpdate":"tool_call_update","toolCallId":"call_fake_1","status":"completed","content":[{"type":"content","content":{"type":"text","text":"hello-from-goose-acp-spike"}}]}}}

   data: {"jsonrpc":"2.0","result":{"stopReason":"end_turn"},"id":3}
   ```

   Plain-text replies stream as `"sessionUpdate":"agent_message_chunk"` with
   `content` as a single object (`{"type":"text","text":"..."}"`) — note this
   is a **different shape** than `tool_call_update`'s `content`, which is an
   *array* of `{"type":"content","content":{...}}` wrappers. Same JSON key,
   different shape depending on the `sessionUpdate` discriminator — a decoder
   has to switch on the discriminator before touching `content`, not treat it
   as one shared field.

   This maps cleanly onto `AgentConversationEvent`: forward every
   `session/update` notification as one event (same role as
   `make_event_callback` in today's `executor.py`), and treat the arrival of
   a response frame whose `id` matches the request as "turn complete."

5. **No built-in turn cap.** With the fake LLM scripted to return the exact
   same tool call unconditionally (no memory of prior results — a real model
   would behave differently once it sees the tool's output in context), the
   server produced **621 tool-call cycles in 20 seconds with no backoff and
   no limit**, and never reached a terminal response before the test was cut
   off. This is expected given a memoryless mock, not a Goose defect — but it
   confirms Goose does not itself impose anything like `max_iterations`.
   **That responsibility has to move into the Go client**, counting
   `tool_call` notifications per `session/prompt` call and cancelling past
   `agent_config.MaxIterations`. Skipping this is a real runaway-spend risk
   once a live LLM key is behind it.

---

## Go ACP Client Package

`services/agent-runner/internal/acp` implements the client side of the wire
format verified above:

- `types.go` — JSON-RPC envelope + ACP message shapes, decoded exactly as
  captured (including the `content`-field shape split between
  `agent_message_chunk` and `tool_call_update` — see its doc comments).
- `sse.go` — a minimal SSE frame reader (`data:` lines only; comment lines
  ignored).
- `client.go` — `Client.Initialize` / `NewSession` / `Prompt`. `Prompt`
  streams `session/update` notifications to a caller-supplied callback and
  enforces `maxToolCalls` itself, since `goose serve` doesn't (see the
  spike's point 5).
- `client_test.go` — unit tests built from the captured transcripts,
  including a reproduction of the runaway-loop scenario asserting
  `ErrMaxToolCalls` actually cuts a non-terminating turn off.
- `livecheck/main.go` — a small manual-run program (not part of the library
  build) for re-verifying the client against a real `goose serve` container
  whenever the pinned Goose version changes: `go run ./internal/acp/livecheck
  <baseURL> <secretKey>`.

All of the above was validated twice: once via `go test` against
hand-transcribed copies of the spike's captures, and once for real —
`livecheck` run against a live container (not the httptest mock) completed
`Initialize` → `NewSession` → five real `session/prompt` tool-call cycles
(genuine shell output, `hello-from-goose-acp-spike`, each time) before its
`maxToolCalls` guard correctly stopped it with `ErrMaxToolCalls` — the same
protection curl-only testing showed doesn't exist on the Goose side.

Not yet built: the container lifecycle piece (Docker Engine SDK spawn/stop,
`cwd`/env wiring, the Valkey trigger consumer, and the `AgentConversationEvent`
publishing that consumes `Client.Prompt`'s `onEvent` callback) — that's
step 3 of the migration plan below.

## Migration Plan

1. ~~**Spike**~~ — done, this document.
2. ~~**Bridge first, low risk**: add Goose as an `acp_provider` in
   `apps/acp-bridge`~~ — done. See
   [Goose as an ACP Bridge Provider](#goose-as-an-acp-bridge-provider) below.
3. ~~**New `services/agent-runner`** consuming `paca:agent:triggers` in
   parallel with the existing Python service, gated per-agent or
   per-project; port `docker_workspace.py`'s container lifecycle logic to
   Go.~~ — built and individually verified against real Docker/Postgres/
   Valkey. See
   [services/agent-runner: What Was Built](#servicesagent-runner-what-was-built)
   below. **Not yet done:** deploying it, or running it against real
   trigger traffic instead of hand-seeded test rows.
4. **Cut over `llm`-type agents**, keep the old service running (not
   consuming) as rollback insurance for one release cycle.
5. **Decommission `services/ai-agent`** and the OpenHands-based
   `agent-server` image once conversation volume has run clean on the new
   path.

## Goose as an ACP Bridge Provider

`apps/acp-bridge`'s `resolve_acp_command` (the function that decides what
subprocess `ACPAgent` spawns for an ACP-type agent) already had a generic
`acp_provider: "custom"` + explicit `acp_command` escape hatch. Before
writing any code, that escape hatch was tested directly against a real,
natively-extracted `goose` binary (`docker cp`'d out of
`ghcr.io/block/goose:latest` — no native install needed) driven through the
bridge's actual `ConversationRunner` class, pointed at the same fake
OpenAI-compatible mock used in the container spike. It worked with zero
code changes: `ACPAgent` (OpenHands SDK) spawned `goose acp` as a subprocess,
completed the ACP handshake, and the scripted reply came back through
`turn_status: finished` exactly as expected.

Two non-fatal warnings logged during that run, worth knowing about:

- `ACP server offers auth methods ['goose-provider'] but no matching env var
  is set` — `ACPAgent` tries to match `initialize`'s `authMethods` against
  its own known provider→env-var table; it doesn't recognize
  `"goose-provider"`, so it warns and proceeds. Harmless as long as the
  user's own environment already has a working provider configured (via
  `goose configure` or the provider's own env vars) before the bridge starts
  — same precondition as Claude Code's `claude setup-token`.
- `UsageUpdate not received within 2.0s` — `ACPAgent` optionally expects a
  usage/cost `session/update` variant that Goose doesn't send. Didn't block
  the turn.

Since `get_acp_provider("goose")` returns `None` — the OpenHands SDK's own
ACP provider registry only knows `claude-code`/`codex`/`gemini-cli`, verified
directly against SDK 1.36.1 — making Goose a **named** provider (so users
don't have to type `custom` + the raw command themselves) needed a small
local override rather than an SDK change:

- `apps/acp-bridge/src/paca_acp_bridge/runner.py`: a
  `_LOCAL_ACP_PROVIDER_COMMANDS` dict resolving `"goose"` → `["goose",
  "acp"]`, checked before falling through to `get_acp_provider`.
- `services/api/internal/domain/agent/entity.go`: `ACPProviderGoose =
  "goose"` added to `ValidACPProviders`.
- `services/api/migrations/000037_add_goose_acp_provider.sql`: widens the
  `acp_provider` CHECK constraint.
- `apps/web`: `"goose"` added to the `ACPProvider` type, both agent-creation
  and agent-detail provider dropdowns, and `acp-bridge-setup.tsx`'s
  `providerLabel()` switch — the last one matters beyond cosmetics: that
  switch's `default` case returns the Claude Code label, so leaving `goose`
  unhandled there would have silently mislabeled every Goose agent's bridge
  setup panel as Claude Code.
- i18n: `acpProviderGoose` added to all 9 locales in both the
  `agents.createDialog` and `agents.detail.overview` namespaces. Kept as the
  literal string `"Goose"` everywhere — matching the existing precedent that
  brand names in this key family (`"Claude Code"`, `"Gemini CLI"`) aren't
  translated.

**Deliberately left as-is, not fixed**: `acp-bridge-setup.tsx`'s
`localAuthExportCommand()` returns `null` for `goose` (same as `custom`),
so the bridge-setup UI shows no one-line copy-paste auth command for it.
Codex/Gemini CLI each need exactly one env var, which fits that UI; Goose's
own setup is either interactive (`goose configure`) or one of several
possible underlying-provider env vars depending on what the user picks
inside it — neither fits a single non-interactive export line, and
guessing wrong there would be worse than saying nothing. Also left alone:
`provider-logos.ts` has no Goose entry, since no logo asset has been
sourced from `@lobehub/icons-static-svg` yet — falls back to initials,
same as any other unmapped provider.

**Confirmed, not just assumed**: Goose reads project-scoped `AGENTS.md` by
default (same as Codex), so `install-paca-skills.sh`'s existing `agents`
platform target already works for Goose users with no script changes —
verified via Goose's own docs, not guessed.

## services/agent-runner: What Was Built

A full, runnable Go service — its own module, `github.com/Paca-AI/agent-runner`
— replacing `services/ai-agent`'s `llm`-type execution path. Package layout:

| Package | Role | Go analog of |
|---|---|---|
| `internal/acp` | ACP-over-HTTP client (from the step 1 spike) | — |
| `internal/agent` | `Trigger`/`Config` domain types | `agentdom.Agent` (a separate copy — see its doc comment for why) |
| `internal/secret` | AES-256-GCM decrypt, byte-compatible with services/api | `internal/platform/secret` |
| `internal/sandbox` | Docker container lifecycle for one `goose serve` per conversation | `docker_workspace.py` |
| `internal/executor` | Ties sandbox + acp.Client into one conversation run | `executor.py`'s `run_conversation` |
| `internal/repository/postgres` | Reads agent config, writes conversation status | `repositories/conversation_repository.py` + parts of `agent_repository.py` |
| `internal/messaging` | Valkey trigger consumer + event publisher | `core/streams.py` + `worker.py` |
| `internal/config` | Env-var settings + the per-agent rollout `Gate` | `config.py` |
| `cmd/agent-runner` | Wires all of the above into one process | `main.py` + `worker.py`'s dispatch |

Every layer was verified against real infrastructure, not mocks, each with
its own `livecheck/main.go` (kept in the tree for re-verification whenever
the pinned Goose version changes):

- **Secret compatibility**: a value encrypted by services/api's *actual*
  `Encryptor` (run from inside that module, since it's otherwise
  unimportable) was decrypted successfully by this package's ported copy —
  pinned as a fixed test vector in `encryptor_test.go` so a future edit
  breaking the shared format fails a test instead of failing silently in
  production.
- **Sandbox lifecycle**: `sandbox.Manager.Start`/`Stop` spawns and tears
  down a real container from `ghcr.io/block/goose`, confirmed reachable at
  `/status` and confirmed actually removed afterward (`AutoRemove`).
- **Executor**: a full `Initialize` → `NewSession` → `Prompt` turn through
  a real sandbox, provider env resolved from `agent.Config`, the LLM API
  key decrypted from an encrypted value, pointed at the same fake
  OpenAI-compatible mock used throughout this document — real container,
  fake model, no spend.
- **Postgres repositories**: every SQL statement in both repository files
  run verbatim against the real dev database's real schema, inside a
  transaction that always rolls back (`BEGIN; ...; ROLLBACK;`) — zero
  persistent risk to shared dev data, but real confirmation the column
  names/types match. Caught one bug in the *test scaffolding* (a missing
  `project_members` FK for a throwaway row) — not in the repository code,
  which had no INSERT for `agent_conversations` to begin with (creation
  stays owned by services/api; this service only reads and updates status,
  matching what `conversation_repository.py` actually does today despite
  [service-boundaries.md](../architecture/service-boundaries.md)'s Boundary
  Rule reading as "ai-agent doesn't write to the DB" — the real Python code
  writes directly, and this mirrors that reality, not the doc).
- **Valkey stream mechanics**: `XGroupCreateMkStream`/`XAdd`/`XReadGroup`/
  `XAck` run against the real dev Valkey — but against a throwaway stream
  key (`paca:agent:triggers:LIVECHECK-DELETE-ME`), deliberately never the
  real `paca:agent:triggers`. That stream already has a live consumer (the
  Python `ai-agent-workers` group) that would receive and act on any test
  message published to it — Valkey Streams deliver an independent full
  copy to every consumer group, not just one.
- **Full wiring, real infrastructure, still without touching the shared
  trigger stream**: `cmd/agent-runner/livecheck` drives the same sequence
  `triggerHandler.Handle` does — gate check, agent lookup, status →
  `running`, executor run, event publish, status → `finished` — by
  constructing the `Trigger` directly in Go rather than publishing it, so
  it exercises the real dev Postgres and Docker without the Python
  service ever seeing it. One throwaway agent + conversation row
  (referencing a real project via FK, since `agent_conversations.project_id`
  is `NOT NULL`) is inserted, used, and deleted (cascades) — confirmed
  zero rows left behind afterward. Publishing to the real
  `paca:agent:events` stream was left in this one check (unlike the
  triggers stream, only `services/realtime` reads it, and it only fans
  out to a Socket.IO room for this test's fabricated `conversation_id`
  that no real browser tab is subscribed to).

**Deliberately out of scope for this pass**, called out here rather than
silently missing:

- **Cross-service double-processing.** A `Gate` (`AGENT_RUNNER_ALLOWED_AGENT_IDS`)
  stops *this* service from acting on an agent outside its rollout scope,
  but does nothing to stop `services/ai-agent` from also acting on the same
  trigger — both consumer groups get an independent full copy of every
  message. Actually cutting an agent over needs a matching change on the
  Python side (or retiring its consumption of that agent's triggers
  entirely) — see `config.Gate`'s doc comment.
- **Event persistence.** This service publishes to `paca:agent:events` but
  never writes to `agent_conversation_events` directly — `ConversationRepository.InsertEvent`/
  `NextEventIndex` exist (verified against the real schema, see above) but
  aren't wired into the main path, matching the documented split where
  `services/api` persists events by consuming the same stream it already
  subscribes to.
- **Global-scope agents** (`ProjectID == uuid.Nil`, `ActorUserID` instead of
  `ActorMemberID`) decode correctly (see `messaging/decode_test.go`) but the
  executor path hasn't been exercised end to end for one — every live check
  above used a project-scoped agent.
- ~~`mcpServers` wire shape~~ — **resolved**, see
  [Sandbox Image and the mcpServers Wire-Format Bug](#sandbox-image-and-the-mcpservers-wire-format-bug)
  below.

## Sandbox Image and the mcpServers Wire-Format Bug

Two more pieces landed after the section above, closing two of the open
risks it left behind — both found by actually building and running things,
not by re-reading docs more carefully.

**`services/agent-server/Dockerfile.goose`** — the Goose-based sandbox
image `executor.Options.Image` points at, sibling to the existing OpenHands
`Dockerfile` (not replacing it — the Python path still uses that one until
the migration plan's step 4 cutover). Pinned by **digest**, not a tag:
confirmed `ghcr.io/block/goose` has no current versioned release image
(only floating `main`/`main-<shortsha>` CI builds and one stale `1.9.0`),
so a digest is the strongest reproducibility guarantee actually available —
immutable regardless of what upstream does to `main` afterward, though
unlike the OpenHands `Dockerfile`'s pin it doesn't correspond to any git tag
or changelog entry. Building from a pinned source tag instead was
considered and not attempted this pass (Rust build times weren't worth the
risk of an unverifiable multi-minute build in this environment).

Built and run for real, twice, catching two genuine bugs neither could have
been caught by re-reading documentation more carefully:

1. **No Node.js in the base image at all** (`node`/`npm`: "not found" —
   confirmed directly, not assumed). `executor.go`'s `buildMCPServers`
   spawns the Paca MCP server via `npx`, so every MCP-enabled Goose
   conversation would have failed outright on the stock image. Fixed by
   installing Node 22 from NodeSource in the Dockerfile.
2. **`npm install -g` (run as root during the build) silently wrote its
   *cache* to `/home/goose/.npm`, root-owned** — because on this base image
   root's own `$HOME` is already `/home/goose`, not `/root`. At runtime,
   `npx` (running as the unprivileged `goose` user) refused to touch a
   cache containing root-owned files and failed with `EACCES` — confirmed
   by running `npx -y @paca-ai/paca-mcp` directly inside the built image as
   the `goose` user. Worse: **`goose serve`'s `session/new` didn't surface
   this MCP-subprocess failure as an ACP error — it just hung forever**,
   which is what actually made this hard to diagnose (see finding 3).
   Fixed with `rm -rf /home/goose/.npm` right after the install; the global
   package itself (correctly root-owned, under `/usr/lib/node_modules`)
   is unaffected.
3. **The bigger one: `session/new`'s `mcpServers` wire format was
   guessed wrong**, and guessed-wrong here doesn't fail — it hangs.
   `acp.MCPServerConfig` (written in the step-1 spike, before any non-empty
   `mcpServers` call had ever actually been tried) was missing a
   required `"type"` discriminator and modeled `env` as a JSON object
   instead of an array. Confirmed by downloading and reading ACP's actual
   `schema.json`
   ([agentclientprotocol/agent-client-protocol](https://github.com/agentclientprotocol/agent-client-protocol)) —
   `$defs.McpServer` is a Rust internally-tagged enum (`stdio` | `http` |
   `sse`, discriminated by a required `"type"` field), and
   `McpServerStdio.env` is `EnvVariable[]` (`{name, value}` objects), not a
   map. The schema also documents `command` as "**Absolute** path to the
   MCP server executable" — confirmed load-bearing, not just documentation
   flavor text: a bare `"npx"` wouldn't necessarily resolve via PATH inside
   whatever spawns the subprocess.

   Root-caused by elimination, in this order, each ruled out with a live
   `goose serve` before moving to the next: DNS-resolution hang on an
   unresolvable `PACA_API_URL` (ruled out — same hang with an
   instantly-refusing `127.0.0.1:1` target); the MCP server retrying
   forever against an unreachable backend (ruled out — same hang even
   pointed at the real, reachable dev API over the `paca_default` Docker
   network using the same dev credential `services/ai-agent` already uses
   for this); only then the wire format itself, confirmed by downloading
   the real schema rather than guessing further. Once the request matched
   the schema (`type`, `env` as an array, an absolute `npx` path), the
   fix was immediate and total — `session/new` returned right away, no
   partial improvement, confirming this was the entire cause.

   Fixed in `acp.MCPServerConfig`/`buildMCPServers`, with the corrected
   shape pinned by two new tests in `internal/acp/mcp_server_test.go`. One
   of those tests caught a second, smaller bug in the fix itself: `Headers
   []HTTPHeader` with `omitempty` can never satisfy "always present (even
   empty) for http/sse, always absent for stdio" — Go's `omitempty` drops a
   zero-length slice the same as a nil one. Changed to `*[]HTTPHeader` so a
   nil pointer omits the field but `&[]HTTPHeader{}` still serializes as
   `[]`.

   Re-verified against the *fixed* image afterward with a full
   `executor/livecheck` run using the real dev `PACA_API_KEY` (the same
   `dev-agent-api-key-change-in-production` credential
   `services/ai-agent` already uses for its own Paca MCP server, over the
   `paca_default` network) — first successful end-to-end run of this
   codebase's MCP-server wiring at all, LLM response and all.

**`services/agent-runner/Dockerfile`** — multi-stage Go build, mirroring
`services/api/Dockerfile`'s pattern (cross-compiling builder stage, `alpine`
runtime). One deliberate difference: runs as **root**, not a dedicated
non-root user — this process needs to read/write the mounted Docker socket
to spawn sandbox containers, the same requirement (and the same choice)
`services/ai-agent`'s own `Dockerfile` already has. Built and run for real;
confirmed it fails cleanly on missing required config
(`config: DATABASE_URL is required`) rather than crashing opaquely.

**Still not done**: wiring either Dockerfile into `deploy/docker-compose.dev.yml`
as an actual service. Both were built and run standalone
(`docker build`/`docker run`), not through compose — adding compose entries
starts to shade into actual deployment, which stayed out of scope for this
pass; see the "Not done" line in the status section at the top of this
document.

## Gap Analysis: What's Still Missing for a Full Cutover

Asked directly to "totally replace" `services/ai-agent`. Before touching
anything live, that claim was checked against what `services/agent-runner`
actually does versus what `services/api` actually depends on from the
Python service — not what the (partly stale) docs describe. Real gaps
found, verified against the real source rather than assumed:

- ~~**Repository cloning & PR creation.**~~ **Closed** — see
  [Repository Tools for Coding Tasks](#repository-tools-for-coding-tasks).
  `executor.py`'s real flow clones the repo before the turn starts (via
  the repository plugin adapter's short-lived token) and creates a PR
  after it finishes; `services/agent-runner`'s executor had none of this.
  Ported as MCP tools in `apps/mcp` (not Go) since that's what actually
  runs inside the sandbox container.
- **Pause/stop/heartbeat control never got processed** (now fixed — see
  below — but this was a real, confirmed gap, not a hypothetical one):
  `TopicAgentStop`/`TopicAgentPause`/`TopicAgentHeartbeat` flow through the
  *same* `paca:agent:triggers` stream as new-conversation triggers,
  published with only `conversation_id`+`project_id` (no `agent_id`) —
  confirmed directly in `agent_service.go`. The trigger decoder requires
  `agent_id`, so these were silently rejected as malformed. A user clicking
  "stop" on a live conversation did nothing.
- ~~**No chat continuity.**~~ **Closed** — see
  [Chat Conversation Continuity](#chat-conversation-continuity).
  `services/ai-agent` keeps a sandbox alive across multiple turns of the
  same chat (`chat_sandboxes` registry in `core/registry.py`,
  `_keep_sandbox_alive` in `executor.py`); `services/agent-runner` used to
  spawn a fresh container and tear it down after every single turn.
- ~~**`acp`-type agents aren't handled at all.**~~ **Closed** — see
  [ACP-Type Agent Dispatch](#acp-type-agent-dispatch). `services/ai-agent`
  is also what receives `acp`-type triggers and dispatches them to a
  user's connected `apps/acp-bridge` over WebSocket (`routes/bridge.py`,
  `acp_dispatch.py`, the presence/dispatch registry in `acp_bridge.py`);
  `services/agent-runner` was only ever scoped to `llm`-type. Was the
  single largest remaining piece — an entire WebSocket server + Valkey
  pub/sub presence registry, ported to Go as `internal/acpbridge`.
- **Event-shape compatibility with `services/api`** — the concern going
  in was that `services/api` might pattern-match on OpenHands-specific
  event type names (`AgentFinishAction`, etc.) to write reply comments or
  PR links, in which case Goose's differently-named event types would
  silently never trigger that. **Checked directly against the real Python
  source, not assumed either way**: there is no such infrastructure-level
  pattern-matching. Reading `executor.py` end to end, reply-writing to
  tasks/comments happens via the agent's *own* MCP tool calls (through the
  Paca MCP server, driven by its skills/system-prompt instructions), not
  as an automatic side effect of detecting a specific event type. This
  particular concern turned out to be unfounded — but two *different*,
  real gaps were found instead while checking it, both closed this pass;
  see the next section.

## Closing the Gaps: Realtime Events, Conversation Status, and Control Messages

**Two Valkey destinations `services/agent-runner` never wrote to**, found
while chasing the event-shape-compatibility question above by reading
`core/streams.py` directly:

- **`paca.events`** (`ChannelRealtime`, Pub/Sub) — the *actual* live-UI-
  update path. `services/realtime`'s `subscriber.ts` listens here, not on
  `paca:agent:events` (which nothing currently consumes — confirmed by
  grepping all of `services/api`'s Go source for it and finding only the
  constant's own declaration). Without publishing here, a user watching a
  conversation in the web UI would see nothing update live. Routing was
  independently confirmed compatible by reading `permissions.ts`'s
  `eventNamespace`: it's a prefix match (`type.startsWith("agent.")` →
  the `tasks` room), not an exact-name lookup, so Goose's own event type
  strings (`agent.agent_message_chunk`, `agent.tool_call`, …) route
  correctly with no special-casing needed. Verified live, not just read:
  `messaging.Publisher.PublishRealtime` was round-tripped against the real
  dev Valkey with a raw subscriber capturing the wire bytes —
  `{"payload":{"conversation_id":"...","event_index":3,"project_id":"..."},"type":"agent.agent_message_chunk"}`
  — byte-exact match for what `subscriber.ts` expects.
- **`paca:agent:conversation_status`** (durable stream) — consumed by
  `services/api`'s automation engine to resume a graph walk paused at a
  `trigger_ai_agent` node once the conversation it's waiting on reaches a
  terminal status. Without this, an automation-triggered agent
  conversation would leave its workflow stuck forever, even though the
  conversation itself completed fine.

Both are now published by `handler.Handler` (`internal/messaging/publisher.go`'s
`PublishRealtime`/`PublishConversationStatus`) — per-event during a turn,
and on every terminal status transition (finished/failed/stopped).

**Control messages (stop/pause/heartbeat) are now handled**, which
required more than just decoding them correctly:

- `messaging.Consumer` used to process one stream message at a time,
  fully sequentially — meaning it could never even *read* a stop signal
  for a conversation still in flight, since reading the next stream entry
  was blocked behind finishing the current `Handle` call. Fixed by running
  each trigger in its own goroutine (bounded by a new `WORKER_CONCURRENCY`
  semaphore) while control messages run unbounded and immediately, so a
  stop signal is never stuck behind a full pool of long-running
  conversations.
- A new `internal/registry` package (`Conversations`) tracks each
  in-flight conversation's `context.CancelFunc`, keyed by conversation ID
  — the Go analog of `core/registry.py`'s `active_conversations`/
  `stop_events`/`pause_events`, collapsed into one map since
  `context.CancelFunc` already unifies what three separate dicts did in
  Python. `handler.Handler.HandleControl` looks up and cancels; `Handle`
  derives a cancellable `context.Context` for each run and distinguishes
  `context.Canceled` (mark "stopped") from `context.DeadlineExceeded` (the
  turn's own timeout — mark "failed", a deliberate departure from
  `executor.py`'s `_post_turn_status`, which maps an analogous polling
  timeout to a *successful* "finished" status — that read as more
  confusing than useful to carry forward).
- **Simplification, stated plainly**: stop and pause are currently handled
  identically (a full interrupt). `services/ai-agent` distinguishes them
  for chat-type conversations — pause keeps the sandbox alive for the next
  reply, stop tears it down — but that distinction only matters once a
  conversation's sandbox can survive between turns at all, which doesn't
  exist yet (see the chat-continuity gap above). Heartbeat is currently a
  no-op for the same reason.
- `triggerHandler` (previously private to `cmd/agent-runner`) was pulled
  out into an importable `internal/handler` package specifically so this
  logic could be driven directly by tests and livecheck programs, not only
  reachable by running the whole binary.

**Verified live, and it caught two real bugs, not zero:**

`cmd/agent-runner/livecheck-stop` drives a genuinely non-converging
conversation (the same "loop forever" fake-LLM script from the
mcpServers-bug spike) against real Docker/Postgres/Valkey, sends an
interrupt mid-flight, and asserts both that `Handle` returns promptly and
that the conversation ends up `"stopped"` in the DB — not `"failed"`.

The first run failed: `Handle` took **~30 seconds** to return after the
interrupt. Root-caused by elimination rather than guessed:

1. First hypothesis — the ACP client's SSE read loop only checked
   `ctx.Err()` when `sse.Next()` itself returned a read error, not on
   every loop iteration. Against a server producing frames far faster than
   `onEvent`'s two blocking Redis calls could drain them, a `bufio.Reader`
   full of already-buffered frames could in principle keep returning data
   with no read error — and therefore no chance to notice cancellation —
   for a while. Fixed by checking `ctx.Err()` at the top of every loop
   iteration in both `Prompt` and `call` (`internal/acp/client.go`).
   **This fix was applied and is correct defensive code, but a targeted
   regression test proved it was *not* what caused the 30s delay** — the
   test passed identically with the fix reverted, disproving the
   hypothesis rather than confirming it. Left in anyway: it's real,
   correct hardening against a scenario that could matter under different
   timing, and the disproof doesn't undo that.
2. Actual root cause, found by timing `docker stop` directly against a
   bare `goose serve` container: **10 seconds**, every time — even using
   the Docker CLI's own default grace period, not this codebase's
   (then-)30s configured one. `goose serve` does not appear to handle
   `SIGTERM` for a fast graceful exit. `sandbox.go`'s `stopTimeout`
   constant had been set to 30s without this data behind it; since
   `Handle` doesn't return until the deferred `sandboxMgr.Stop` call does,
   that 30s ceiling was, in practice, exactly how long every stop took.
   Reduced to 3s — there's nothing left worth preserving inside the
   container by the time `Stop` is called anyway, since the in-flight ACP
   turn was already aborted via context cancellation first. Re-ran the
   same live check afterward: **~3 seconds**, confirmed.

## Repository Tools for Coding Tasks

Closes the "Repository cloning & PR creation" gap from
[Gap Analysis](#gap-analysis-whats-still-missing-for-a-full-cutover) — the
gap was specifically that Goose conversations had no way to check out a
repository at all, so every coding task would run against an empty
container.

**Where the logic actually lives.** Unlike most of this migration, this
isn't Go code in `services/agent-runner` — it's TypeScript in `apps/mcp`
(the Paca MCP server), because that's the process already running *inside*
the sandbox container with a filesystem and a `git` binary, spawned fresh
per conversation via `npx -y @paca-ai/paca-mcp`. `services/agent-runner`
itself never touches a checked-out repo.

Ported from `services/ai-agent/src/agent/repo_tools.py`'s three tools
(`list_repositories`, `clone_repository`, `push_branch`) into
`apps/mcp/src/tools/repo-tools.ts`, following the same request shapes:

- **The backend endpoints these call already exist and needed no new Go
  code.** `GET /api/v1/plugins/{pluginId}/projects/{projectId}/repositories`
  and `.../repositories/{repoId}/clone-info` are not core `services/api`
  routes — they're proxied straight through to whatever a repository
  plugin's own backend implements, via the generic
  `/api/v1/plugins/{pluginId}/*` route (`PluginHandler.ProxyRequest`,
  matched against the plugin's own `manifest.Backend.Routes`). Confirmed by
  reading `runtime.go`'s own comment on why `github_repositories` isn't a
  core-schema table: it moved into the GitHub plugin's own
  `plugin_data_com_paca_github` schema back in
  `migrations/000007_remove_github_tables.sql`. So `PacaAPIClient` in
  `apps/mcp/src/api/client.ts` just needed two new thin GET methods
  (`listPluginRepositories`, `getRepositoryCloneInfo`) hitting the same
  URLs `repo_tools.py` already calls with the same `X-API-Key` auth — no
  new backend endpoint to design or build.
- **`repo_plugin_ids` was already flowing as far as the trigger.**
  `services/api`'s `agent_service.go` already publishes a comma-separated
  `repo_plugin_ids` field on every trigger (`gatherRepoPluginIDs`), and an
  earlier pass of this migration had already added decoding it onto
  `agent.Trigger.RepoPluginIDs` in `internal/messaging/decode.go` — but
  `executor.go`'s `buildMCPServers` never actually forwarded it anywhere,
  so it was decoded and then dropped. Fixed by setting a new
  `PACA_REPO_PLUGIN_IDS` (comma-joined) env var on the Paca MCP server's
  entry, read once at MCP-server startup into `PacaConfig.repoPluginIds`
  (`apps/mcp/src/index.ts`) — the same "fixed at process start" lifecycle
  every other `PACA_*` config value already has, rather than something
  threaded per tool call.
- **Tool visibility, not just permission, is gated on having any
  repository plugin at all.** `executor.py` only ever *attaches* these
  tools to the agent when `trigger.repo_plugin_ids` is non-empty
  (`has_repos` in `executor.py`) — a project with no repository plugin
  never sees `clone_repository` in its tool list. `apps/mcp`'s tool
  listing is a static catalog (`getAllTools()`) filtered at request time
  instead, so the same behavior is reproduced as an extra filter step in
  `server.ts`'s `ListToolsRequestSchema` handler: `list_repositories` /
  `clone_repository` / `push_branch` are stripped out entirely when
  `config.repoPluginIds` is empty, on top of (not instead of) the existing
  permission filter. These three tools were deliberately left out of
  `permissions.ts`'s `TOOL_PERMISSIONS` table — there's no established
  `repos.*` permission category on the backend to check against, and the
  plugin proxy route enforces its own project-membership middleware
  regardless — so they fall through `server.ts`'s existing "no permission
  mapping, allow by default" branch, same as the intentional fallback
  every other unmapped tool already gets.
- **Git runs directly via `child_process.execFile("git", [...])`**, not a
  shell string — args are passed as an array with no shell interpretation
  at all, which is a stronger guarantee against injection than
  `repo_tools.py`'s own approach (`shlex.quote` into a shell command
  string executed through OpenHands' `TerminalExecutor`, because that path
  has no argv-array option). `rm -rf <target>` before cloning was likewise
  replaced with `fs.rm(target, {recursive:true,force:true})` instead of
  shelling out for it.
- **Token scrubbing ported directly**: `scrubToken` in `repo-tools.ts`
  mirrors `_scrub_token`'s three passes (raw token, percent-encoded token,
  the general `x-access-token:...@` pattern) so a failed `git clone`/`git
  push`'s stderr never reaches the LLM's context or a log with the
  short-lived token still in it. **Verified live** against a real public
  GitHub repo (`git clone https://x-access-token:@github.com/...`, empty
  credential — confirms the always-embed-a-credential construction
  inherited from `repo_tools.py` doesn't break public-repo cloning) and
  against real git failure output (`git clone` with an invalid token, and
  against an unresolvable host) — in both cases, this git version (2.39.5)
  didn't actually echo the credential back in its own stderr, so
  `scrubToken`'s test coverage for the "credential *is* echoed" case rests
  on constructed fixtures, not a reproduced failure. Left in as
  defense-in-depth regardless — the risk is credential leakage, and other
  git versions, proxies, or third-party git hosts are not guaranteed to
  behave the same way.
- **PR creation is still not this tool set's job**, exactly like
  `repo_tools.py`: `push_branch`'s success message instructs the agent to
  call a repository plugin's own PR-creation tool (e.g.
  `github_create_pull_request`) as a separate step. Confirmed this
  delegation actually works for Goose the same way it does for OpenHands
  by reading `apps/mcp/src/plugin-loader.ts`: plugin-contributed MCP tools
  (loaded from each enabled plugin's `manifest.mcp.remoteEntryUrl`) are
  merged into the same tool list and the same `CallToolRequestSchema`
  dispatch as these core tools in `server.ts` — there is no separate
  code path only OpenHands' agent runtime can reach.
- Default clone/repo directory is `/home/goose/repo` (under
  `executor.go`'s `sandboxWorkdir`), not `repo_tools.py`'s
  `/workspace/repo` — Goose's sandbox user's home directory, matching
  where the ACP session's own `cwd` is set.

Covered by `apps/mcp/src/__tests__/tools/repo-tools.test.ts` (token
scrubbing, multi-plugin aggregation, partial-plugin-failure handling,
clone/push success and failure paths, the git-exec calls themselves via an
injected `GitExec` fake) and
`services/agent-runner/internal/executor/executor_test.go` (`PACA_REPO_PLUGIN_IDS`
set when `trigger.RepoPluginIDs` is non-empty, omitted when it's empty).

## Conversation Event Persistence

Found while starting the push toward a full cutover (not one of the original
four gaps): `handler.Handle`'s `onEvent` callback published every event to
Valkey (`PublishEvent` → the durable `paca:agent:events` stream,
`PublishRealtime` → the live-UI pub/sub channel) but never wrote to
`agent_conversation_events` in Postgres. `services/api`'s
`ListConversationEvents` handler (`agent_repository.go`) reads *only* from
that table — no HTTP call into either agent service — confirmed by direct
read, not assumption. Net effect: reload the page for any agent-runner-run
conversation and its history was empty, since nothing but a currently-
connected live subscriber ever saw those events at all.

Python's `services/ai-agent` doesn't have this gap: `executor.py`'s
`_persist_event`/`persist_conversation_event` writes to Postgres directly, not
through any stream — this is the same "ai-agent writes to the DB despite what
the boundary doc says" reality noted earlier in this document, just a
different call site.

Fixed by wiring `internal/handler/handler.go`'s already-built (but previously
unused) `ConversationRepository.NextEventIndex`/`InsertEvent` into `Handle`:
`eventIndex` is now seeded from `NextEventIndex` instead of always starting at
0 (matters once a conversation can span multiple turns — see chat continuity,
next), and every event — including the synthetic `turn_end` event — is
inserted into Postgres alongside the existing Valkey publishes.

**Verified live**: extended `cmd/agent-runner/livecheck` to assert, against a
real run through the real `Handler.Handle` (not a hand-copied subset), that
`agent_conversation_events` actually gets rows, that `event_index` is a
contiguous 0-based sequence with no gaps or duplicates, and that the last
persisted event is `turn_end`. Ran against the real dev Postgres/Valkey and a
real Goose container:

```
OK   2 event(s) persisted to agent_conversation_events
OK   event_index sequence is contiguous: [0 1]
OK   last persisted event is turn_end
```

## Chat Conversation Continuity

Closes the "chat-conversation continuity" gap: `services/ai-agent` keeps a
chat conversation's sandbox alive across turns (`core/registry.py`'s
`chat_sandboxes`); `services/agent-runner` used to cold-start and tear down
a fresh container on every single reply, meaning a chat conversation lost
all context between messages — functionally, "chat" didn't actually work.

**Where the sandbox-lifecycle decision moved.** Before this, `executor.Run`
owned its sandbox's full lifecycle internally (`sandboxMgr.Start`, deferred
`sandboxMgr.Stop`). Continuity breaks that encapsulation: whether to keep a
sandbox alive depends on whether the trigger is chat-type and, if
interrupted, whether that was a pause or a full stop — information `Run`
itself never has. `Run` no longer tears anything down; it returns the
sandbox it used (`Result.Handle`/`Client`/`SessionID`) and a new
`Executor.StopSandbox` method lets the caller decide. `handler.Handle` is
now the single place that makes this call, for both chat and non-chat
triggers — matching `executor.py`'s own structure, where `_run_sync` (not
`run_conversation`) is the one place that both runs a turn and decides
`_keep_sandbox_alive`.

**New `internal/chatsandbox` package** — `Registry` (`Get`/`Set`/`Pop`/
`Touch`/`FindIdle`), same mutex-protected-map shape as the existing
`internal/registry.Conversations`. `chatsandbox.State` holds the *live*
`*acp.Client` object (already past `Initialize()`), not just connection
details — `Prompt` takes `sessionID` as a plain parameter with no other
client-side state, so a resumed turn never needs to re-`Initialize()`
against the same container, sidestepping an unverified protocol question
(does a second `initialize` reset a prior `session/new`'s state on the
`goose serve` side?) entirely rather than answering it.

**`registry.Conversations` gained pause-vs-stop.** `HandleControl`'s stop
and pause cases used to both call the same `Interrupt`, indistinguishable
once observed as `context.Canceled` on the other end. Added
`InterruptReason` (`ReasonStop`/`ReasonPause`), `InterruptWithReason`, and
`TakeReason` — `Handle` reads the reason back after observing cancellation
to decide "paused, keep the container" vs "stopped, tear it down" for a
chat trigger. Non-chat triggers ignore the reason entirely (they never
pause) — a stop and a pause interrupt look identical to them, same as
before.

**Status semantics, mirroring `_keep_sandbox_alive`/`_post_turn_status`
exactly:** a chat trigger reaching a natural finish *or* an interrupt-only
pause now writes `"paused"` (already a valid, DB-backed status in
`services/api`'s domain — no migration needed) and keeps the sandbox
registered in `ChatSandboxes` instead of tearing it down; a full stop, an
error, or any non-chat trigger tears down and writes
`"stopped"`/`"failed"`/`"finished"` as before. `"paused"` is deliberately
routed through a new `publishNonTerminalStatus` (`PublishRealtime` only) —
`PublishConversationStatus`'s own doc comment is explicit that "paused"
must never appear on that stream, since it isn't terminal and nothing
should resume an automation graph walk on it.

**Stopping a chat conversation that's paused *between* turns** — no turn
in flight, so `HandleControl`'s `InterruptWithReason` finds nothing to
cancel. Added `Handler.TeardownPausedChatSandbox` (mirrors
`teardown_paused_chat_sandbox`), checked as a fallback in `HandleControl`'s
stop case, and reused as-is by the idle reaper below rather than
duplicating the same stop/status-write/publish sequence a second time.

**Idle reaper** — a goroutine in `cmd/agent-runner/main.go`, started
alongside `consumer.Run`, polling `ChatSandboxes.FindIdle` every 20s
(mirrors `reap_idle_chat_sandboxes`'s `asyncio.sleep(20)`) and tearing down
whatever it finds via the same `TeardownPausedChatSandbox` `HandleControl`
uses. New config: `CHAT_SANDBOX_IDLE_TIMEOUT_MINUTES` (default 3, matching
Python's own default). `FindIdle` excludes any conversation with an
in-flight turn (`registry.Conversations.IsRegistered`, new) — a heartbeat
only refreshes `LastActiveAt` at turn-end and via explicit heartbeats, not
continuously while a turn runs, so an in-flight conversation can look
idle by that field alone without actually being idle.

**Verified live**, with a new `cmd/agent-runner/livecheck-chat` covering
the full flow against real Docker/Postgres/Valkey — two real turns of one
chat conversation, a heartbeat in between, then the idle-teardown path
(calling the exact `FindIdle`/`TeardownPausedChatSandbox` the real reaper
goroutine calls, not a hand-copied subset):

```
OK   turn 1: status is "paused"
OK   turn 1: chat sandbox registered, container=8da2848f...
OK   heartbeat control message refreshed LastActiveAt
OK   turn 2: status is "paused"
OK   turn 2: reused turn 1's container (8da2848f...)
OK   event_index sequence spans both turns contiguously: [0 1 2 3]
OK   FindIdle reports the paused conversation as idle once past the timeout
OK   idle teardown stopped the sandbox and recorded status "stopped"
```

The event-index contiguity check matters beyond just "no crash": it
confirms Phase 1's `NextEventIndex` seeding does the right thing across a
*resumed* turn too, not just a single one — turn 2 continued numbering from
where turn 1 left off (`[0 1 2 3]`, not `[0 1]` twice) exactly as
`executor.py`'s `_AtomicCounter` docstring requires.

The existing `cmd/agent-runner/livecheck` and `livecheck-stop` programs both
still pass — `livecheck` was switched from a chat-type trigger to
`TriggerTaskAssigned` (its own purpose is the general orchestration path,
not chat continuity specifically; a chat trigger completing naturally now
pauses instead of tearing down, which would have silently leaked a running
container in that short-lived program). `livecheck-stop` kept its
chat-type trigger and confirms a full `agent.stop` interrupt still tears
a chat conversation's sandbox down completely rather than pausing it,
exactly as before.

**A real bug caught while building `livecheck-stop`'s fix, unrelated to
chat continuity itself**: it used `fatalf` → `os.Exit(1)` on a failed
assertion, which skips deferred cleanup entirely — an initial run against a
*misconfigured* fake LLM server (one that converged immediately instead of
looping, a test-harness mistake) hit exactly that path and left both a
throwaway agent row and a running sandbox container behind, found and
cleaned up manually. Restructured both `livecheck-stop` and (already fixed
for the same reason while updating it for `Executor.Run`'s new contract)
`internal/executor/livecheck` around a `run() error` pattern instead, so
`main` never calls `os.Exit` after a defer that matters has been
registered.

## ACP-Type Agent Dispatch

Closes the largest of the original four gaps: `services/ai-agent` is also
what receives `acp`-type triggers and dispatches them to a user's connected
local `apps/acp-bridge` daemon over WebSocket (`routes/bridge.py`,
`acp_dispatch.py`, the presence/dispatch registry in `acp_bridge.py`).
`services/agent-runner` was only ever scoped to `llm`-type — an acp-type
trigger just silently no-oped.

**Design decision — same process, new HTTP server.** In Python this all
lives in the one `ai-agent` process/event loop alongside `run_conversation`.
Ported the same way in Go: a new `net/http` server
(`internal/acpbridge.Server`) runs in its own goroutine in
`cmd/agent-runner/main.go`, alongside `consumer.Run`. One binary now covers
both roles `services/ai-agent` did, not just in the docs.

**Design decision — identical Valkey key/channel naming.** `internal/acpbridge`
uses the exact same prefixes `acp_bridge.py` does
(`paca:acp-bridge:online:`, `paca:acp-bridge:dispatch:`,
`paca:acp-bridge:control:`). Presence/dispatch/eviction therefore
interoperate regardless of which service's process currently holds a given
agent's WebSocket connection — `is_online`/`dispatch` read and write a
shared key either service can see. Practical upshot, not yet exercised for
real since dev routing isn't wired up until the next section: the two
internal status/disconnect REST endpoints work correctly no matter which
service `AI_AGENT_URL` currently points at, even mid-migration.

**New package `internal/acpbridge`** — a close function-for-function port:
- `registry.go`: `Register`/`Unregister`/`Evict`/`Heartbeat`/`IsOnline`/
  `Dispatch`, mirroring `acp_bridge.py`'s module-level functions and dicts.
  `Conn` is a small interface (`SendJSON`/`Close`) so the real
  `*websocket.Conn` (via `github.com/coder/websocket` — no existing Go
  WebSocket dependency anywhere in this repo, checked before adding one)
  and a test fake both satisfy it.
- `forwarders.go`: the two background goroutines every registered
  connection gets (`forwardDispatchedMessages`, `watchForEviction`),
  reconnect-with-backoff on a dropped Pub/Sub connection, mirroring
  Python's `while True: ... except Exception: ... sleep(...)` shape.
- `message.go`: `BuildACPMessage`, a close port of `prompt.py`'s
  `build_acp_message`/`build_trigger_suffix`/`build_project_context_suffix`
  — folds project/trigger context directly into the turn's message
  (prefixed with the `/paca` skill trigger) since apps/acp-bridge's
  `ConversationRunner` has no separate system-suffix channel, the same
  reason `executor/prompt.go`'s `buildInitialMessage` exists for the
  LLM/sandbox path. Checked the real `_GLOBAL_CONTEXT_SUFFIX`/action-type-
  label text directly rather than approximating it.
- `dispatch.go`: `Dispatcher.DispatchTrigger`, a close port of
  `acp_dispatch.py`'s `dispatch_acp_trigger` + its watchdog — fails a
  conversation immediately if the bridge isn't connected (checked before
  *and* after the dispatch publish, since Valkey Pub/Sub drops undelivered
  messages rather than queuing them), otherwise schedules a background
  watchdog that fails the conversation if no `turn_status` arrives within
  its timeout (race-safe against a legitimate late status via
  `ConvRepo.FailIfNotTerminal`'s conditional UPDATE).
- `server.go`: the WebSocket endpoint (`GET /agent-bridge/ws` — hello
  handshake, `event`/`turn_status`/`ping` message relay) plus the two
  internal REST endpoints (`GET /agent-bridge/status/{agentId}`,
  `POST /agent-bridge/disconnect/{agentId}`, `X-Internal-Token` gated,
  constant-time compared) and `GET /llm/models` (a static catalog, not a
  live call — the same `data/llm_models.json` file
  `services/ai-agent/data/llm_models.json` is, kept as a second copy
  rather than a shared mount; a duplicated static JSON file is an
  acceptable simplification here, unlike logic duplication).

**Repository additions**: `AgentRepository.FindByBridgeTokenHash`/
`FindACPByID` (the acp-type sibling of the existing llm-only `FindByID`,
which explicitly rejects non-llm agents), `ConversationRepository
.GetConversationAgentType`/`GetConversationRealtimeContext` — both ported
directly from `repositories/conversation_repository.py`.

**Wired into `handler.Handler`**: `Handle`'s `ErrNotLLMAgent` branch used to
just no-op ("not this service's concern"); now it calls a new
`dispatchACP` helper instead, mirroring `worker.py`'s `_process_trigger`
branching on `agent_config.agent_type`. `HandleControl`'s stop/pause cases
gained a `dispatchACPControl` fallback between the existing in-process
`InFlight` check and (for stop) the chat-sandbox-teardown fallback,
mirroring `worker.py`'s `_handle_control` checking
`get_conversation_agent_type` before forwarding through the bridge. The
existing per-agent `Gate` check at the top of `Handle` applies uniformly to
both trigger types — acp-type dispatch is staged through the same rollout
mechanism as llm-type, not a separate always-on path (see the
double-execution-coordination note below).

### A real, severe bug found live: `Unregister` could hang forever

Built a dedicated `cmd/agent-runner/livecheck-acp`: a fake bridge daemon
(using the same `coder/websocket` library) connects, authenticates with a
bridge token, receives a real `start_turn` dispatched through the actual
production path (`handler.Handler.Handle` → `dispatchACP` →
`Dispatcher.DispatchTrigger`, not a hand-copied subset), reports a
`turn_status` and an `event` back, and the two internal REST endpoints are
exercised against the same running server.

The first full run passed every functional check but took **~25 seconds**
between the disconnect endpoint being called and the presence key actually
clearing — traced with two temporary timing probes (removed once diagnosed)
rather than guessed at:

1. `Registry.Evict`'s broadcast reaches `watchForEviction`, which calls
   `conn.Close(4409, "evicted")` — measured directly: **under 1ms**. Not
   the bottleneck.
2. The server's `relayMessages` read loop, blocked on the same
   `*websocket.Conn`, correctly unblocks once `Close` completes — measured
   directly: **~89ms**. Also not the bottleneck. `coder/websocket`'s own
   cross-goroutine cancellation works exactly as documented.
3. The actual ~25s was entirely inside `Registry.Unregister`'s
   `<-entry.done` wait, which requires both background goroutines
   (`forwardDispatchedMessages`, `watchForEviction`) to have exited.
   `watchForEviction` already had, from step 1. `forwardDispatchedMessages`
   hadn't — it was still blocked in `pubsub.ReceiveMessage(connCtx)`, and
   `connCtx` gets cancelled by `Unregister` itself, which hadn't been
   reached yet (it's called *after* the blocked read in step 2 returns —
   true, but only for that one connection's own read; the *other*
   goroutine's blocked pubsub read is a separate wait entirely).

Root cause, found by reading `go-redis`'s own source
(`internal/pool.Conn.deadline`), not assumed: `PubSub.ReceiveMessage(ctx)`
calls `WithReader(ctx, timeout=0, ...)`, which sets the socket read
deadline from `ctx.Deadline()` — and a plain `context.WithCancel`-derived
context (exactly what `connCtx` is) has no `Deadline()` at all. The
resolved deadline is `noDeadline`, i.e. **no socket-level timeout
whatsoever**. Go's `net.Conn.Read` is not interrupted by context
cancellation on its own — only an actual deadline (or closing the
connection) aborts a blocking read. So cancelling `connCtx` did *nothing*
to the in-flight `ReceiveMessage` call; the goroutine — and therefore
`Unregister`, and therefore every bridge disconnect, not just eviction —
was relying entirely on some *other* event to eventually close that
connection. In the live test, that other event was the test harness's own
`redis.Client.Close()` call, called only once `waitForBridgeOffline` gave
up and the whole check failed — which is why the delay tracked whatever
deadline the test happened to be given, not a fixed number.

**Fixed** in `acpbridge.drainMessages`: a side goroutine now watches
`ctx.Done()` and calls `pubsub.Close()` the moment it fires, forcibly
tearing down the connection the blocked `ReceiveMessage` call is waiting
on. Re-ran the same live check afterward: the full register → dispatch →
turn_status → event → status → disconnect → offline sequence now completes
in **~220ms** end to end, confirmed by tightening the check's own timeout
from a padded 25s down to 5s and it still passing comfortably.

This is a materially different kind of bug than the sandbox-stop-timeout
one from the chat-continuity section — that one was slow (30s, then 3s);
this one had no upper bound at all short of something else forcing the
connection closed. Any real bridge disconnect — a daemon's laptop going to
sleep, a network drop, an ordinary `paca-acp-bridge` shutdown — would have
hit the exact same hang before this fix.

**A second, smaller finding while writing `internal/acpbridge/registry_test.go`**:
eviction (both the "second `Register` for the same agent" case and
`Evict`) is fire-and-forget Valkey Pub/Sub — a message published before the
existing connection's `watchForEviction` goroutine has actually completed
its `Subscribe` call is silently dropped, not queued, exactly like `acp_bridge.py`'s
identical design. In real usage the several other `await`ed calls
`Register`/`register` makes before reaching the eviction broadcast give
that goroutine's first scheduling slot time to run; a test calling
`Register` twice back-to-back with nothing else in between doesn't have
that natural buffer. Not a new bug introduced by this port — an inherent
property of the fire-and-forget design carried over faithfully from
Python — but worth naming explicitly rather than leaving a flaky-looking
`time.Sleep(50 * time.Millisecond)` unexplained in the test.

## Double-Execution Coordination

Found while re-reading `internal/config/gate.go`'s own doc comment during
the push toward a full cutover — not one of the original four gaps, and not
something either side's code visibly breaks on, which is exactly why it was
easy to miss: both `services/ai-agent` and `services/agent-runner` read
*every* message on `paca:agent:triggers`, each via its own independent
Valkey Streams consumer group (a stream delivers a full, independent copy
of every entry to each group — this is by design, not a bug, and is how
the two services are able to run side by side at all during the
migration). `agent-runner`'s `Gate` (`AGENT_RUNNER_ALLOWED_AGENT_IDS`)
stops *that process* from acting on a trigger for an agent outside its
rollout scope — but does nothing to stop `services/ai-agent` from *also*
acting on the exact same trigger. Before this fix, gating even one agent
to agent-runner for a real, live rollout would have made both services run
that agent's conversations simultaneously — two containers, two
conflicting status writers, corrupted event ordering.

**Fixed**, in Python: a new `Settings.agent_runner_owned_agent_ids` field
(`config.py`, comma-separated agent UUIDs) parsed once at import time into
`worker.py`'s `_AGENT_RUNNER_OWNED_AGENT_IDS`. `_process_trigger` skips
(acks without running) any trigger naming an owned agent, before even
loading its config. `_handle_control`'s stop/pause branches skip the same
way, at the point where they'd otherwise resolve the conversation's owning
agent via `get_conversation_agent_type` — checked before the existing
ACP-bridge-forwarding fallback, so an owned agent's stop/pause is left
entirely to agent-runner's own `HandleControl` rather than *also* being
acted on here.

**`agent.heartbeat` deliberately has no matching check.** It's pure
in-memory (`chat_sandboxes.get(cid)`, no DB lookup at all) and highest-
frequency of the three control types (~every 30s per open chat tab) — adding
a lookup here would cost a DB round-trip on every single heartbeat. Not
needed: `chat_sandboxes` only ever gets an entry for a conversation this
process itself started via `run_conversation`, and `_process_trigger`'s own
skip means that never happens for an owned agent — so `chat_sandboxes.get(cid)`
naturally returns `None` and the heartbeat is already a no-op, by
construction rather than by an explicit check.

**This is a manual operational invariant, not automated**: the same agent
ID list must be set on both services, in opposite roles
(`AGENT_RUNNER_ALLOWED_AGENT_IDS` on agent-runner,
`AGENT_RUNNER_OWNED_AGENT_IDS` on ai-agent — deliberately different names,
same values) until `ai-agent` is fully decommissioned. A shared source of
truth (e.g. a DB flag both services read) would remove the "two configs
must agree" foot-gun entirely, but that's a larger design question
explicitly out of scope for this pass — noted here as a known limitation,
not solved.

Covered by new tests in `services/ai-agent/tests/test_worker.py`
(`test_process_trigger_skips_agent_runner_owned_agents` and its stop/pause
counterparts, plus a companion test confirming an agent *not* in the owned
set is still processed normally) — all 221 tests in the existing Python
suite continue to pass.

## Dev Environment Wiring

Adds `agent-runner` to `deploy/docker-compose.dev.yml` alongside the
existing `ai-agent` block (both run side by side — the "parallel during
migration" model the code already assumed), and points
`deploy/caddy/Caddyfile.dev`'s `/agent-bridge/ws` route at `agent-runner:8080`
instead of `ai-agent:8080` — a deliberate dev-only test cutover so a real
`apps/acp-bridge` daemon pointed at the dev stack actually exercises the
new path end to end. The prod Caddyfile is untouched; this is explicitly
not a signal that prod has moved. `services/agent-runner/Dockerfile` (the
production one) is used for both.

**Update**: dev now uses `Dockerfile.dev` + `air` instead — see
[Dev Hot Reload for agent-runner](#dev-hot-reload-for-agent-runner) below;
a code change no longer needs a full `docker compose up --build
agent-runner`, just a save.

**A real bug caught missing `data/`**: `Dockerfile` only ever copied the
compiled binary into the runtime image, never the `data/llm_models.json`
file `internal/acpbridge.Server`'s `/llm/models` endpoint reads by default
path — that endpoint would have 404'd in any real deployment, dev or prod.
Fixed with an added `COPY --from=builder /build/data ./data`; confirmed the
file is actually present in a real built image (`docker build` +
`docker run ... ls /app/data/`), not just that the Dockerfile parses.

**A real bug caught in `AGENT_SERVER_IMAGE` itself**: the dev compose entry
pointed straight at the raw `ghcr.io/block/goose` digest — the same one
every prior livecheck in this document used. Every one of those livechecks
happened to leave `PacaAPIKey` unset (the built-in Paca MCP server's own
opt-in gate — see `buildMCPServers`), so none of them ever actually
exercised spawning it inside a real container. The raw upstream image has
no Node.js at all (confirmed directly, same finding `Dockerfile.goose`'s
own comments already recorded from building that Dockerfile originally),
so `npx -y @paca-ai/paca-mcp` can never start there — and per the
mcpServers wire-format section above, Goose does not surface an MCP
subprocess failure as an ACP error, it just hangs `session/new` forever.
Any real agent trigger (which does set `PacaAPIKey`, since the Paca MCP
server is central to what makes a Paca agent useful at all) against the
raw image would have hung indefinitely. Fixed: `AGENT_SERVER_IMAGE` now
points at a locally built `paca-agent-server-goose:dev`, built from
`Dockerfile.goose` — confirmed directly that image has Node.js
(`docker run ... which node npx` → `/usr/bin/node` / `/usr/bin/npx`,
`node --version` → `v22.23.2`). No CD job publishes a pre-built version of
this image yet (unlike ai-agent's own `AGENT_SERVER_IMAGE`, pulled
pre-built from `ghcr.io/paca-ai/paca-agent-server`) — the dev compose
entry now documents the one-time local `docker build` command as a
prerequisite instead.

### A third real bug, found by actually bringing the stack up: consumer group replayed the entire trigger stream history

Built and started `agent-runner` for real inside `docker compose up
--build agent-runner` against the dev stack's real Valkey (`api`/`mcp`
brought up alongside it, matching agent-runner's `depends_on`). It started
cleanly — but immediately began processing a burst of ~28 old
conversations in rapid succession, several failing with `executor: acp
session/new: acp: session/new: Internal error` (a separate, not-yet-
investigated issue — see Open Risks below).

Root cause, confirmed by reading `core/streams.py`'s own
`ensure_consumer_group` rather than assumed: it creates its consumer group
with `id="$"` — meaning "only entries appended after this group exists."
`internal/messaging.Consumer.Run` created its group with `id="0"` instead —
"start from the very first entry still in the stream." Since this was
`agent-runner-workers`' *first ever* creation against this Valkey, "0"
meant replaying every trigger the stream had ever retained, not just a
recent backlog — a materially different failure mode from ai-agent's own
established group, which has been advancing its position continuously
since it was first created and would never re-read old entries regardless
of downtime in between.

**Fixed**: changed `"0"` to `"$"` in `Consumer.Run`, matching
`ensure_consumer_group` exactly. Existing messaging tests
(`TestConsumer_RoutesControlAndTriggerMessagesSeparately`) still pass
unaffected — they publish their test messages *after* waiting for the
group to exist, so both starting positions were equivalent from that
test's point of view; the bug only manifests against a stream that
already held entries *before* the group's first creation, which no
existing test set up.

**Real, if low-stakes, consequence already occurred before this was
caught**: 28 conversations belonging to a single test agent ("Paca Agent"
/ `paca-agent`, "Test Project") in the dev database had their status
overwritten (`running` → `failed`, mostly) by this replay before it was
stopped. Confirmed via direct query that every touched row belongs to
that one test agent/project — not any other project's data. Left as-is
rather than reverted; flagged for the user rather than unilaterally
"fixed" one way or the other, since there's no way to know from here
whether those specific rows' prior state mattered for anything.

## Production Cutover: In Progress

Started at the user's explicit request to fully remove `services/ai-agent`
in favor of `agent-runner`. Updated the deployment *templates* in the repo
— safe, git-reviewable file edits, not yet applied to any real running
stack:

- `deploy/docker-compose.prod.yml`: `ai-agent` service replaced with
  `agent-runner` (mirrors the dev block from the previous section, adapted
  to this file's `${VAR:-default}` convention); `api`'s environment gained
  an explicit `AI_AGENT_URL: http://agent-runner:8080` (previously relied
  on the Go code's own `http://ai-agent:8080` default, which would have
  silently broken once the compose service was renamed); `AI_AGENT_INTERNAL_KEY`
  kept its historical name.
- `deploy/caddy/Caddyfile`: `/agent-bridge/ws` now proxies to
  `agent-runner:8080`.
- `deploy/.env.production.example`: `PACA_AI_AGENT_IMAGE` →
  `PACA_AGENT_RUNNER_IMAGE`, doc comments updated.
- `deploy/docker-compose.e2e.yml`: `ai-agent` service replaced with
  `agent-runner` (same treatment as prod's block above — this stack is a
  standalone, manually-run mirror of prod topology, not wired into any
  GitHub Actions workflow, so this edit doesn't touch live CI).

All four files validated with the real tools (`docker compose config`,
`caddy validate`), not just eyeballed.

`.github/workflows/cd.yml` now builds/publishes `paca-agent-runner`
(`agent-runner-image` job, replacing `ai-agent-image`) and the
Goose-based sandbox image as `paca-agent-server-goose`
(`agent-server-goose-image` job, building
`services/agent-server/Dockerfile.goose` instead of
`services/agent-server/Dockerfile`) — so the prod compose file's image
references now correspond to real published images once a release runs.
`release-assets`/`promote-release`'s `needs:` lists and `promote-release`'s
retag loop were updated accordingly.

`services/agent-runner` also has its own PR CI now
(`.github/workflows/agent-runner-pr-ci.yml`, triggered on
`services/agent-runner/**`): a `lint` job running `golangci-lint` against a
config mirroring `services/api`'s (`services/agent-runner/.golangci.yml` —
enables `bodyclose`, `gocritic`, `revive`, `noctx`, `exhaustive`, `misspell`
on top of the defaults), a `build` job, and a `test` job running
`go test -race`. It deliberately has no `test-e2e` job the way
`api-pr-ci.yml` does — this service's coverage against real
Docker/Postgres/Valkey lives in its livecheck programs
(`cmd/agent-runner/livecheck*`, `internal/*/livecheck`), which are meant to
be run by hand against a real dev stack (see each program's own doc
comment), not wired into CI. Getting `services/agent-runner` to a clean
`golangci-lint run` first (fixing ~50 issues across `errcheck`, `staticcheck`
ST1005, `bodyclose`, `gocritic` `exitAfterDefer`, and `revive` exported-doc-
comment findings) surfaced one real bug in the process: three tests in
`internal/messaging/publisher_test.go` raced a background goroutine's
`t.Errorf` call against the test function returning, which reliably panicked
under `go test -race` ("Fail in goroutine after Test has completed") even
though it passed without `-race`. Fixed by replacing the old
sleep-then-race `subscribeOne` helper with `subscribeAndPublish`, which
subscribes before starting the publish goroutine and joins that goroutine
before returning, so its error (if any) is always reported from the test's
own goroutine.

`.github/workflows/ai-agent-pr-ci.yml` was removed — `services/ai-agent` is
being decommissioned from the deployment path, so its dedicated PR CI
(including the `services/agent-server/Dockerfile` — the old, non-Goose
image — build check) no longer needs to gate merges.

**Still open at the end of this pass:**
- **A real, currently-running production `docker compose` stack exists on
  this machine** (`/home/haihuynh/paca-production/docker-compose.yml`,
  compose project `paca` — discovered earlier while looking for a dev
  Postgres to test against, and deliberately not touched since). The
  template edits above do not affect it; actually cutting it over means
  applying an updated compose file to that real stack and restarting real
  containers, which has not been done and needs explicit confirmation
  first given the stakes.

## Full Removal of `services/ai-agent`

Done at the user's explicit request, after confirming (via the real-prod-stack
risk above) that they wanted to proceed even though `/home/haihuynh/paca-production/`
is still serving traffic through `ai-agent`. Nothing about the live production
stack was touched by any of this — deleting source files from this repository
has no effect on a container already running from a previously-pulled image.

**Deleted:**
- `services/ai-agent/` in full — 56 git-tracked files (source, tests,
  Dockerfile/Dockerfile.dev, `pyproject.toml`/`uv.lock`, README) plus
  untracked build artifacts (`.venv`, `__pycache__`, lint caches).
- `services/agent-server/Dockerfile` — the old OpenHands-based sandbox
  image, used only by `ai-agent`. `Dockerfile.goose` (agent-runner's own
  sandbox image) is untouched.
- `deploy/docker-compose.dev.yml`'s `ai-agent:` service block — dev no
  longer runs the two services side by side (the parallel-run model from
  the earlier [Dev Environment Wiring](#dev-environment-wiring) section is
  now moot). `agent-runner`'s `PORT_POOL_START` default reverted from
  `11000` (chosen specifically to stay disjoint from `ai-agent`'s own
  range) back to `10000`, in both the compose file and `config.go`'s own
  default.

**`scripts/install.sh`/`scripts/upgrade.sh` reworked, not just renamed** —
this was flagged as a separate, deferred task earlier in this document
specifically because a mechanical rename would have been wrong. The actual
risk: real self-hosted installs already have a `.env` on disk with
`PACA_AI_AGENT`/`PACA_AI_AGENT_IMAGE` in it, and an install this old running
the new `upgrade.sh` needed its existing enable/disable choice and pinned
version preserved, not silently reset. `upgrade.sh` now has an explicit
migration step, ahead of everything else that reads either name: if
`PACA_AGENT_RUNNER`/`PACA_AGENT_RUNNER_IMAGE` are absent but the legacy
names are present, they're backfilled from the legacy values (verified with
a standalone harness exercising just the `set_env_var`/`get_env_var`/
`has_env_var` logic against a synthetic old `.env` — confirmed a
`PACA_AI_AGENT=no` + `PACA_AI_AGENT_IMAGE=...:1.2.3` install correctly
produces `PACA_AGENT_RUNNER=no` + `PACA_AGENT_RUNNER_IMAGE=pacaai/paca-agent-runner:1.2.3`).
Separately, `AGENT_SERVER_IMAGE` migration (previously only handled the
ancient raw-upstream-OpenHands default) now also catches an install still
on Paca's own pre-Goose `paca-agent-server` image — genuinely necessary,
not cosmetic, since Agent Runner cannot execute against either old image at
all. `install-paca-skills.sh` had one stale comment cross-reference fixed
(an SSRF-guard mirror pointing at a now-deleted Python function).

**`docs/ai-agent/*.md` replaced, not just edited** — per the user's
explicit choice. `ai-agent-service.md` (entirely a description of the
Python service's internals) was deleted outright and replaced with
[agent-runner-service.md](agent-runner-service.md), written from the actual
Go source (`internal/executor`, `internal/sandbox`, `internal/acpbridge`,
`internal/messaging`, `config.go`), not adapted from the old doc's prose.
`overview.md`, `api-design.md`, `database-schema.md`, `realtime-events.md`,
and `repository-plugin-adapter.md` had their architecture
diagrams/flows/`event_type` value tables updated to match agent-runner's
actual mechanisms — several turned out to be **genuinely different
protocols**, not just a renamed service, discovered by reading the real
current source rather than assuming a 1:1 port:
- Repository access is no longer orchestrator-initiated. `agent-runner`
  never clones a repo itself — the agent calls Paca MCP server tools
  (`apps/mcp/src/tools/repo-tools.ts`, running inside its own sandbox) that
  fetch a token and run `git` as a subprocess, scrubbing the token from any
  output. `services/ai-agent` used to fetch the token itself, before the
  agent's first turn, via `SecretSource`.
- There is no REST-based pause/resume/stop the way `services/ai-agent`
  exposed. Control is entirely message-driven over `paca:agent:triggers`;
  see [agent-runner-service.md](agent-runner-service.md#pause--resume--stop--heartbeat).
- `event_type` values for `llm`-type agents are ACP session/update kinds
  (`agent_message_chunk`, `tool_call`, `tool_call_update`, `turn_end`), not
  OpenHands SDK class names. `acp`-type agents (via `apps/acp-bridge`,
  unaffected by this migration and still genuinely built on the OpenHands
  SDK) still use the old class names — `realtime-events.md` now documents
  both value spaces separately rather than presenting one stale table.

**A real, confirmed functional gap was found in the process, not just a
docs mismatch**: `services/agent-runner` has no equivalent to
`services/ai-agent`'s default-skill and plugin-skill merge at all.
`internal/repository/postgres/agent_repository.go`'s `listSkills` reads
only that specific agent's own `agent_skills` rows — nothing calls
`GET /api/v1/skills?target=agent` (Paca's bundled defaults) or queries
enabled plugins for `manifest.skills`, the way `services/ai-agent`'s
`builder.load_default_skills()`/`load_plugin_skills()` did. An `llm`-type
agent with no skills explicitly attached to it gets **no guidance at all**
on `agent-runner` — not even the baseline `paca` routing skill every agent
used to receive automatically. [default-skills.md](default-skills.md) and
[docs/plugins/skills-plugin-system.md](../plugins/skills-plugin-system.md)
were rewritten to document this as a known gap (design intent vs. current
reality) rather than silently describing the old, no-longer-true behavior
as current. Porting this to `agent-runner` is real, unscheduled follow-up
work — flagged here rather than fixed in this pass, since it's a
non-trivial feature port, not a rename.

**Repo-wide comment/string sweep**: fixed every `ai-agent` mention that
either (a) was user-visible — four API error-message strings in
`services/api/internal/transport/http/handler/agent_handler.go`
(`"ai-agent service URL not configured"` etc. → `"agent-runner service ..."`)
— or (b) described current architecture incorrectly, across
`CONTRIBUTING.md`, `README.md`, `services/README.md`,
`docs/architecture/overview.md`, `docs/architecture/service-boundaries.md`
(also corrected a pre-existing inaccuracy there: agent-runner, like
`ai-agent` before it, writes conversation status/events directly to
Postgres — the doc previously claimed neither did), `docs/guides/local-development.md`,
`docs/api/README.md`, `docs/architecture/repository-structure.md`, and
several frontend/`apps/mcp`/`apps/acp-bridge` comments describing the live
heartbeat/idle-reaper/bridge-status mechanisms. **Not exhaustive**:
`services/api`'s Go source still has roughly twenty files with
`services/ai-agent`-referencing comments (mostly "ported from
`services/ai-agent`'s `X.py`" provenance notes, plus the still-intentionally-kept
`AI_AGENT_URL`/`AI_AGENT_INTERNAL_KEY` env var names — see the "Production
Cutover" section above for why those specific names stay) that were left
as-is — internal comments, not user-facing or actively misleading about
current behavior, and rewriting all of them precisely would have meant
re-verifying each one's surrounding logic individually. Worth a dedicated
follow-up pass if `services/api`'s comment hygiene here matters enough to
prioritize.

**Follow-up: `services/agent-server/Dockerfile.goose` renamed to
`services/agent-server/Dockerfile`.** With the old OpenHands-based
`Dockerfile` already deleted above, the `.goose` suffix no longer
disambiguates anything — there's only one Dockerfile in that directory now.
Every *current-state* reference was updated to the new name (deploy compose
files, `cd.yml`'s build `file:` path, `agent-runner-service.md`,
`overview.md`, `docs/architecture/repository-structure.md`,
`docs/guides/local-development.md`) along with two internal comments in the
Dockerfile itself that compared it against "Dockerfile's OpenHands
equivalent" (which would otherwise have read as an odd self-reference).
Earlier *historical* narrative further up in this document — the original
"Sandbox Image" section, the dev-environment-wiring bug writeup, the CD-job
section — deliberately still says `Dockerfile.goose`, since that was its
real name at the time those events happened; rewriting past narrative to
use a name that didn't exist yet would misrepresent the timeline. Published
image tags (`paca-agent-server-goose`, the `agent-server-goose-image` CD
job name) are unaffected — those label the artifact's content, not the
source filename, and weren't part of this rename.

**A real, live regression from the earlier "Full Removal" pass was found
and fixed while checking for missing pieces**: `deploy/docker-compose.dev.yml`
and `deploy/docker-compose.e2e.yml`'s `api` service never set `AI_AGENT_URL`
explicitly (only `deploy/docker-compose.prod.yml` did). Once the `ai-agent:`
service block was removed from dev compose, `services/api`'s own
`AI_AGENT_URL` default (`http://ai-agent:8080`, in `config/load.go`) pointed
at a hostname that no longer resolves in that network — confirmed live via
`docker exec paca-dev-api-1 wget http://ai-agent:8080/...` → `bad address`.
Symptom: `GET /api/v1/agents/llm-models` and the ACP bridge status/disconnect
proxies returned `500 failed to reach agent-runner service` (the exact error
string fixed for user-facing accuracy earlier in this document) — while
actual agent conversations kept working fine, since trigger dispatch and
control messages go over Valkey, not this HTTP path. Root cause found by
investigating a specific stuck-looking dev conversation
(`014e89f4-1e0f-434c-b222-def2ddedbe2f`): the conversation itself had
actually completed successfully (`agent_conversations.status = paused`,
`stopReason: end_turn`, a real reply persisted in `agent_conversation_events`)
— the confusion was this separate, unrelated `AI_AGENT_URL` failure
happening around the same time on an unrelated endpoint. Fixed by adding
the same explicit `AI_AGENT_URL: http://agent-runner:8080` dev/e2e compose
already needed, verified live end to end (`GET /api/v1/agents/llm-models`
through the real gateway, authenticated, 200 with the real model catalog)
after recreating the `api` container to pick up the new env var.

**A second, much larger real bug found the same way — `llm`-type chat
replies were entirely invisible in the web UI.** Investigating a second
dev conversation (`261f1bd0-cc78-45e5-81e7-0367a1ce0302`, "no message after
send the message") ruled out the backend the same way as
`014e89f4-1e0f-434c-b222-def2ddedbe2f` above: the conversation had
genuinely completed (9 `agent_message_chunk` events plus `turn_end`
persisted, a real reply generated, no errors anywhere in agent-runner's,
`services/realtime`'s, or `services/api`'s logs) — `services/realtime`
correctly routed every event to the right Socket.IO room. The break was
one layer further in, client-side: `apps/web/src/components/projects/
agents/conversation-to-thread-messages.ts`'s `eventsToThreadMessages` — the
function every chat surface (`ai-chat-float.tsx`, `ai-chat-float-global.tsx`,
`conversation-view.tsx`) uses to turn persisted events into rendered
messages — only ever recognized OpenHands SDK event-type strings
(`MessageEvent`, `ActionEvent`, `ObservationEvent`, `AgentErrorEvent`,
`UserRejectObservation`) plus `ACPToolCallEvent` (still relevant — that one
is `apps/acp-bridge`'s, unaffected by this migration). It was never updated
for `agent-runner`'s actual ACP session/update types
(`agent_message_chunk` / `tool_call` / `tool_call_update` / `turn_end`),
so every one of those fell all the way through to the function's own
"legacy event type" fallback — which itself only handles a content
*array* or a bare string, not ACP's single `{type, text}` `ContentBlock`
object, so even the fallback produced nothing. Net effect: since
`services/ai-agent` was removed, **no `llm`-type chat reply has rendered
in the UI at all** — a conversation could complete successfully end to end
on the backend and still look completely stuck to a user watching the chat
panel.

Fixed by adding explicit handling for the four `agent-runner` event types
to `eventsToThreadMessages`, alongside the existing OpenHands-typed
branches rather than replacing them — a conversation's history predating
this migration can still hold OpenHands-typed rows from when
`services/ai-agent` produced them, and those still need to render
correctly. `agent_message_chunk` chunks are appended onto the
already-open text part instead of each becoming its own part, so a
streamed reply renders as one continuous message instead of one fragment
per chunk. Verified with 5 new unit tests in
`conversation-to-thread-messages.test.ts`, built directly from the exact
payload shapes pulled from `261f1bd0-cc78-45e5-81e7-0367a1ce0302`'s real
`agent_conversation_events` rows — including one asserting the streamed
reply from that conversation ("Hello! How...") collapses into a single
text part, and one exercising a full turn (reply chunks → tool call →
more reply chunks) end to end. The Vite dev server picked up the fix live
via HMR.

## Chat UX Follow-up: Missing User Messages, Per-Chunk Refetching, Row-Per-Chunk Storage

Three related gaps reported directly against a live conversation, fixed
together since they share the same root cause: `agent-runner`'s realtime
messages never carried more than an `event_index`, and it never recorded
the user's own message as an event at all.

**User's own message never rendered.** `services/ai-agent`'s OpenHands SDK
auto-generated a `MessageEvent` for the user side of a turn on every
`conversation.send_message` call; ACP has no equivalent (`session/prompt`
is one-way — the caller already knows what it sent, so Goose never echoes
it back). `agent-runner` never replaced that, so a chat conversation's
history had no record of what the user actually said, only the agent's
replies. Fixed: `internal/handler/handler.go`'s `Handle` now persists and
publishes a `user_message` event (`event_source: "user"`) at the start of
every turn with real message text — every trigger type, not just
`chat_message`, but deliberately *not* the synthesized default text a
bare task-assignment trigger falls back to (`prompt.buildInitialMessage`'s
`taskAssignedDefault`), since no human actually said anything in that
case. `eventsToThreadMessages` (`conversation-to-thread-messages.ts`)
renders it as a user bubble, mirroring how it already handled the
OpenHands-era `MessageEvent`/`event_source: "user"` case.

**One `GET .../events` HTTP call per realtime message.** Every persisted
event's realtime broadcast only ever carried `event_index` — a "something
changed, go re-fetch" signal, not the data itself.
`useConversationEventWindow`'s own tail-index-driven effect turned that
into a `fetchNextPage()` call *per event*, so a single streamed reply
could mean a dozen-plus HTTP round trips just to render it. Root cause was
structural, not a bug in that effect — the realtime payload genuinely
didn't have enough in it to append directly. Fixed on both ends:
- **Go**: `handler.go`'s `persistAndPublish` (and the equivalent path in
  `internal/acpbridge/server.go` for `acp`-type conversations) now
  generates the event's `id`/`created_at` once, writes it to Postgres via
  `ConversationRepository.InsertEvent` (whose signature changed to accept
  both rather than generating them in SQL), and puts the *same* values —
  plus `event_type`/`event_source`/`payload` — on the realtime message.
  Every persisted row now has an isomorphic realtime broadcast.
- **TypeScript**: `agent-api.ts` gained `applyRealtimeAgentEvent`, shared
  by `useProjectRealtime` and `useGlobalAgentRealtime` (previously two
  hand-copied implementations of the same tail-cache logic). A message
  carrying an `id` gets appended as a real `AgentConversationEvent` onto
  `ConversationEventsTail.events`; a status-only message (no `id` — e.g.
  `agent.conversation.paused`) just bumps `tick` and now triggers a real
  reconciling fetch of `conversationEventWindowKey`, replacing an
  `invalidateQueries` call at the wrong key that turned out to be a no-op
  all along (`["projects", …, "conversations", id, "events"]` was never
  the infinite query's actual cache key, `conversationEventWindowKey`
  was). `useConversationEventWindow` was simplified accordingly: it no
  longer drives `fetchNextPage()` off the tail index at all, just merges
  `tail.events` on top of whatever's paginated in — gated on `following`,
  so a reader scrolled into history still sees `newBelow`'s count grow
  instead of new content appearing under them.

**One Postgres row per raw ACP chunk.** Goose streams a reply as many
small `agent_message_chunk` notifications — a single two-paragraph reply
plus one tool call produced 9 rows in `agent_conversation_events` before
this fix (and would have been closer to 20+ for a longer reply, based on
live testing). Fixed by buffering chunk text in `handler.go` across
`onEvent` calls and only persisting/publishing once a paragraph boundary
(`"\n\n"`) is hit, a different event type interrupts the run (flushed
first, so `event_index` ordering still matches when things actually
happened), or the turn ends (flushed unconditionally on every exit path —
success, interrupted, or failed — so a partial in-progress paragraph is
never silently dropped). This turned out to also fix the realtime-message
volume, not just storage: since every realtime broadcast now corresponds
1:1 to a persisted row, batching persistence automatically batches
realtime too — no separate "stream every chunk live, persist every
paragraph" two-tier design was needed.

Verified live end to end against the real dev stack (not just unit
tests): sent a real chat message asking for a two-paragraph explanation
plus a tool call, then confirmed directly — `agent_conversation_events`
held exactly 9 rows (`user_message`, 3 reply-chunk rows, `tool_call`,
`tool_call_update`, 2 more reply-chunk rows, `turn_end`), each paragraph
row's text a real Markdown paragraph, not a fragment; `services/realtime`'s
logs showed 8 realtime messages for that same turn (one per row, plus one
`agent.conversation.paused`) — not one per raw ACP notification; and
`services/api`'s access logs showed **zero** `GET .../events` calls during
the entire live-streaming window, versus one connection to Valkey's
`paca.events` channel confirming the exact enriched payload shape on the
wire (`id`/`event_index`/`event_type`/`event_source`/`payload`/`created_at`,
matching what `applyRealtimeAgentEvent` expects byte-for-byte). Full
`go build`/`vet`/`golangci-lint`/`go test -race` and `vitest`/`tsc`/`biome`
passed clean throughout (529 frontend tests, 5 of them new).

## Cold-Start Latency and Token/Cost Tracking

Two follow-up asks: shrink the time between a trigger arriving and the
sandbox being ready to run, and surface token/cost usage per conversation.

### Three cold-start latency fixes

1. **Paca MCP server invoked via `npx -y @paca-ai/paca-mcp` on every cold
   start.** `npx` does a network registry version-check even though the
   package is already installed globally in the sandbox image (`npm install
   -g @paca-ai/paca-mcp` in `services/agent-server/Dockerfile`) and never has
   anything to install. Measured directly inside a real sandbox container
   (`docker exec`, repeated): `npx -y @paca-ai/paca-mcp` ≈ 2.67s total vs.
   invoking the package's own installed binary directly ≈ 1.45–1.48s — about
   1.2s of pure overhead, on every single conversation's cold start, for a
   check that can never find anything to do. Fixed in
   `executor.go`'s `buildMCPServers` by pointing `Command` at
   `/usr/bin/paca` directly — the executable symlink `npm install -g`
   creates from the package's own `package.json` `bin` field
   (`{"paca": "./build/index.js"}`), confirmed via `npm config get prefix`
   inside the image (`/usr`). Not yet pursued: the package's own ~1.4s
   Node.js startup/import cost (vs. bare Node's ~42ms) is a separate, deeper
   optimization in `apps/mcp`'s own source.
2. **`sandbox.go`'s readiness poll interval was a flat 1 second** — up to
   ~0.9s of dead waiting on every cold start past the moment `goose serve`
   actually became ready, for a poll that's just a cheap local HTTP GET
   against a container this process already owns. Dropped
   `readyPollEvery` to 100ms.
3. **A real, latent correctness bug found while fixing #1** (not something
   deliberately hunted for): pointing `Command` at the direct binary meant
   `buildMCPServers` started constructing a stdio `MCPServerConfig` with
   explicitly-empty `Args`/`Env`. `MCPServerConfig.Args`/`.Env` were plain
   `[]string`/`[]EnvVariable` slices with `omitempty` — and Go's
   `encoding/json` `omitempty` on a plain slice checks `len() == 0`, not
   nil-ness, so an explicitly-allocated-but-empty slice is dropped from the
   wire output exactly like a nil one. ACP's schema requires `args`/`env` to
   be present (even as `[]`) for the stdio variant; omitting either made a
   real `goose serve` either hang `session/new` forever or return an HTTP
   200 with a permanently-empty SSE stream — never a JSON-RPC error, so it
   surfaced as a live conversation failing with `"stream closed before a
   response for id=2"` rather than anything actionable. Root-caused by
   hand-driving raw ACP calls via curl against a manually-started sandbox
   container (capturing `Acp-Session-Id` from `initialize`'s response
   header, echoing it back on subsequent calls) and A/B-ing the request
   body with and without an explicit `"args": []`. Fixed the same way
   `Headers *[]HTTPHeader` already solved this exact problem for the
   http/sse variants: changed `Args`/`Env` to pointer types (`*[]string`,
   `*[]EnvVariable`), so "present but empty" (`&[]string{}`) and "absent"
   (`nil`) are distinguishable again. This was a pre-existing exposure for
   *any* stdio MCP server configured with empty args/env, not something
   introduced by fix #1 — fix #1 just happened to be what exercised it live.
   Regression-tested in `internal/acp/mcp_server_test.go`
   (`TestMCPServerConfigStdio_OmittedArgsOrEnvAreDroppedByOmitempty` pins
   the old, broken zero-value shape as documentation of what must never
   ship again).

Net effect verified live end to end (all three fixes deployed together): a
real conversation completed with no errors. Isolated component-level
timings (the npx and poll-interval numbers above) are trustworthy in
isolation, but a couple of full end-to-end cold-start runs taken right
after redeploying landed at 24–37s — anomalously slow compared to the
per-fix savings alone. Traced to host-level Docker contention from this
same session's own repeated `docker compose up --build` calls (`docker
system df`: 69 images/39GB with 52% reclaimable, 184 build-cache
entries/20.64GB with 93% reclaimable; `uptime` load average ~8–9 on what
should be a mostly-idle box), not a regression from the code changes —
worth re-measuring end-to-end once the host is quiet, and worth considering
a periodic `docker builder prune`/`docker image prune` in dev if this
recurs.

### Token/cost tracking: sessions.db is the right source, not the JSONL logs

Goose's ACP implementation does not send token usage over `session/update`
— confirmed again this pass by inspecting raw ACP traffic, matching the
"`UsageUpdate` not received within 2.0s" finding earlier in this document.
Goose does track it internally, in two places inside each sandbox
container:

- `/home/goose/.local/state/goose/logs/llm_request.N.jsonl` — full raw
  request/response log per LLM call, including a trailing `usage` object
  (`{"input_tokens":370,"output_tokens":5,"total_tokens":375,"cache_read_input_tokens":null,"cache_write_input_tokens":null}`).
  Usable but log-scraping: one file per session, needs parsing every line
  to find `usage` objects, no natural place to read a single "totals for
  this conversation" number.
- `/home/goose/.local/share/goose/sessions/sessions.db` — a SQLite database
  (WAL mode; the `-wal`/`-shm` companion files must be copied alongside the
  `.db` file via `docker cp` for a standalone client to see recent data,
  otherwise it reads as empty). This is the better source. Its `sessions`
  table has exactly the shape wanted:
  ```
  total_tokens, input_tokens, output_tokens                      -- last turn
  accumulated_total_tokens, accumulated_input_tokens,
  accumulated_output_tokens                                       -- whole session
  provider_name, model_config_json (incl. model_name)
  ```
  confirmed against a real row: `provider_name='mistral'`,
  `model_config_json.model_name='devstral-medium-latest'`,
  `accumulated_total_tokens=33179` (`input=33169`, `output=10`) after a
  one-line "say hi" conversation — the large input count is the full system
  prompt/tool-schema payload Goose resends every turn, not a bug. The
  `messages` table stores each turn's `content_json`/`role` with a `tokens`
  column that was `NULL` on both rows in this sample — per-message token
  counts don't look reliably populated; treat `sessions.total_tokens` as
  the trustworthy per-turn number and `accumulated_*` as the trustworthy
  running total.
  
  No dollar cost anywhere — Goose reports tokens, not price. Computing
  `cost_usd` needs a pricing table keyed by `(provider_name, model_name)`
  maintained on the Paca side, applied to `accumulated_input_tokens`/
  `accumulated_output_tokens` at the point of reading.

**Not yet implemented** — this pass was investigation only. Open design
question for whoever picks this up: how agent-runner reads `sessions.db`
out of a container it doesn't keep alive after the conversation ends
(`sandbox.go`'s `AutoRemove: true` means the file is gone once the
container exits, so this has to be read *before* teardown — e.g. a
`docker cp` + query as the last step of `executor.Run`, keyed off the
Goose session ID already visible in `session/new`'s params/response,
before `sandboxMgr.Stop` runs) and where the numbers get persisted
(new columns on `agent_conversations`, e.g. `input_tokens`/`output_tokens`/
`total_tokens`/`cost_usd`, versus a field on the `turn_end` event for a
live per-turn readout — the `accumulated_*` columns make the former
straightforward: one read at the end of the conversation is enough,
no running total needs to be maintained turn-by-turn).

## Dev Hot Reload for agent-runner

`agent-runner` used the production `Dockerfile` in dev too — a code change
needed a full `docker compose up --build agent-runner` (rebuild image +
recreate container) before it took effect, unlike `api`, which already had
`Dockerfile.dev` + `air` for reload-on-save. Brought `agent-runner` in line
with that existing pattern rather than inventing a new one:

- `services/agent-runner/Dockerfile.dev` — same shape as
  `services/api/Dockerfile.dev`: `golang:1.26-alpine`, `air` installed at
  image-build time, `go mod download` baked into the layer so a fresh
  container doesn't re-fetch modules.
- `services/agent-runner/.air.toml` — same shape as `services/api/.air.toml`
  (including `poll = true`, needed because Docker Desktop doesn't forward
  inotify events across the VM boundary to a bind-mounted source dir), just
  pointed at `./cmd/agent-runner` instead of `./cmd/api`.
- `deploy/docker-compose.dev.yml`'s `agent-runner` service: switched
  `dockerfile: Dockerfile` → `Dockerfile.dev`, added `command: air` and
  `working_dir: /workspace`, bind-mounted the service source
  (`../services/agent-runner:/workspace`) in place of the old build-context-only
  setup, and added the same `go_build_cache` named volume `api` already
  uses (Go's build cache is content-addressed, so sharing one volume across
  both modules' containers is safe and lets shared dependency packages
  compile once instead of twice).

`data/llm_models.json` (see the `Dockerfile` COPY bug documented above)
needed no extra handling: it's part of the source tree, so bind-mounting
`services/agent-runner` into `/workspace` brings it along automatically,
same as any other file air doesn't rebuild for.

Not done: the Docker socket mount (`/var/run/docker.sock`) and
`AGENT_SERVER_IMAGE` requirement are unchanged from before — `air`
restarting the Go process on a source change doesn't touch already-running
sandbox containers spawned by the previous process, same as a full
container rebuild never did either.

## Open Risks / Follow-ups

- ~~**`acp: session/new: Internal error` against real dev agent configs**~~
  — **resolved, not a bug.** Root-caused directly: spun up a throwaway
  `goose serve` container with the same `llm_provider` ("mistral") the
  affected test agent uses and drove the real ACP calls by hand
  (`initialize` then `session/new`) rather than guessing. `session/new`'s
  actual error, visible once queried directly instead of only seeing the
  generic JSON-RPC `-32603 "Internal error"`:
  `"Authentication error: Authentication failed. Status: 401 Unauthorized.
  Response: {\"detail\":\"Invalid API Key\"}"` — Goose reached Mistral's
  real API and got a real 401. The "Paca Agent" / "Test Project" fixture's
  stored `llm_api_key_secret` is a placeholder, not a working credential —
  exactly what you'd expect from a fixture never meant to make a real LLM
  call. Nothing agent-runner- or Goose-specific: any execution engine
  driving this same agent with this same key would hit the identical 401.
  The 28 conversations' `"failed"` status (see
  [Dev Environment Wiring](#dev-environment-wiring)) was therefore
  already correct, not something the earlier consumer-group bug corrupted
  — left as-is.
- **Secret masking**: verify whether Goose redacts injected secrets (e.g. a
  git token) from anything it echoes back over `session/update` before
  routing real credentials through it.
- **Skill → recipe/slash-command mapping** needs its own small prototype;
  keyword-triggered on-demand skill loading has no direct Goose equivalent.
- ~~`goose acp` as a `custom` `acp_provider`~~ — resolved, see
  [Goose as an ACP Bridge Provider](#goose-as-an-acp-bridge-provider): it
  already worked with zero code changes.
- ~~`session/new` hanging forever on a downstream MCP-subprocess
  failure~~ — **resolved**: `executor.Run` now derives a bounded
  `context.WithTimeout` from `cfg.TimeoutMinutes` (falling back to 30
  minutes, matching the `agents` table's own DB default) and uses it for
  `Initialize`/`NewSession`/`Prompt`, instead of passing through whatever
  undeadlined context the caller supplied. The actual mechanism this
  relies on — that `acp.Client` genuinely aborts a call once its context
  expires, rather than blocking regardless — is pinned by
  `TestPrompt_RespectsContextDeadlineOnAHungServer` in
  `internal/acp/client_test.go`: a server that accepts the connection and
  then never writes another byte, the same shape as the real hang found
  building the sandbox image, confirmed the client returns promptly on
  deadline instead of blocking. `sandboxMgr.Start` is deliberately left
  outside this bound — it has its own internal ready-timeout (see
  `sandbox.go`'s `readyTimeout`).
- **`oauth`-transport user-configured MCP servers** are skipped entirely by
  `buildMCPServers` — no ACP equivalent was identified for Paca's fourth
  `agent_mcp_servers.transport` value; mapping it to an `http` entry's
  bearer-token header wasn't attempted this pass.
