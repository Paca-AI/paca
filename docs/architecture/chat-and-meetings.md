# Chat and Meetings

Design and delivery plan for built-in team chat (a Slack alternative) and voice/video meetings (a Discord alternative) in Paca v1.0.0.

> **Status:** proposal; Phase 3b (meeting assistant) is built, unit-tested and covered by a Playwright spec (real browser, real API, worker and LiveKit; only the speech-to-text and the model are faked), see [Phase 3b status](#phase-3b-status). Phase 3 (meetings) is built, unit-tested and covered by a Playwright spec, see [Phase 3 status](#phase-3-status). Phase 2 is mostly built, see [Phase 2 status](#phase-2-status). Phase 1b (presence and task-comment UX) is built, see [Phase 1b status](#phase-1b-status). Phase 1 is partly built (project channels, direct messages, replies from global agents in direct messages, messages, threads, reactions, mentions, followed threads, saved messages, read state, the sidebar and Inbox, IAM variable and realtime channel rooms, uncommitted). Everything else here is not implemented. The Phase 0 spikes ran on 2026-11-17 and all five came back go; see [Phase 0 results](#phase-0-results). The roadmap entry is in [ROADMAP.md](../../ROADMAP.md) under *Beyond v1.0 — Exploring*.

## Goals

- Channels, threads, direct messages, mentions and reactions, scoped to the workspace and to projects.
- Task-aware chat: link, create and update tasks from a conversation.
- Voice and video rooms people can drop into, with screen sharing.
- Agents as first-class participants in chat (and, later, in meetings).
- Use an open-source media server for meetings instead of building one.

## Non-goals (v1.0.0)

Federation, end-to-end encryption, Slack/Discord import, incoming chat webhooks (later, through the automation webhook trigger), live voice agents.

## Key decisions

| Decision | Choice | Why |
|---|---|---|
| Chat: core or plugin? | **Core** | Plugins are WASM-sandboxed extensions. Chat needs IAM, the realtime service, notifications, agents and automation, and its message volume does not fit the plugin model. |
| Meeting engine | **[LiveKit](https://github.com/livekit/livekit) server** (Apache 2.0, self-hosted SFU) | Built-in TURN, a Go server SDK, a React component library, and Egress / Agents add-ons for recording and AI participants later. |
| Voice and video model | **A voice room is a channel** (`kind = voice`). A "huddle" in any channel or DM is the same thing started ad hoc. | Every room gets a text channel for free: messages, permissions, unread state and mentions are reused. Chat history is never stored in LiveKit. |
| Who issues LiveKit access | **`services/api` mints short-lived LiveKit tokens after an IAM check.** The LiveKit API secret never reaches the browser. | Keeps all authorization in router gates. |
| LiveKit in the default stack | **Opt-in** compose profile and Helm toggle | Paca is lightweight, and LiveKit needs UDP ports and TURN setup. Chat works without it. |

## Chat

### Data model

Migration `000094`. Scopes are `workspace` and `project`; every project gets a `#general` channel. Channel kinds: `public`, `private`, `dm`, `group_dm`, `voice`.

| Table | Holds |
|---|---|
| `channels` | `id`, `scope`, `project_id?`, `kind`, `name`, `topic`, `archived_at`, `created_by` |
| `channel_members` | `channel_id`, `account_id`, `last_read_seq`, notification preference. Required for private channels and DMs; an optional "follow" for public ones. |
| `messages` | `channel_id`, gapless per-channel `seq`, `thread_root_id?`, `author_account_id`, `content` (JSON), `content_text` (search), `edited_at`, `deleted_at` |
| `message_reactions`, `message_mentions`, `message_links`, `pins` | Reactions, denormalised mentions for unread/mention counts, task and doc references, pinned messages |
| `message_attachments` | Reuses the existing `files` / attachment storage |

Authors are `accounts`, so humans, agents and service accounts post the same way. A composite index on `(channel_id, seq)` serves history; hash-partitioning `messages` is a later option.

### Realtime

The existing pipeline (API → Valkey pub/sub + capped per-project change log → per-event access check per socket) is a poor fit for message traffic: message bodies would crowd out task and doc changes in the capped log, and a per-event API access check per socket is expensive in a busy channel.

- `channel:<id>` rooms. Access is checked **at join** and re-checked when the existing IAM-change resync fires, not per event.
- Messages live in Postgres only. Clients catch up with `GET …/messages?after_seq=`, following the cursor idea of the existing sync protocol. Valkey pub/sub carries only a lightweight "new seq" nudge.
- Typing and presence are ephemeral, held in the realtime service with TTL keys. They never touch the API or the database.

### Authorization

Every decision stays in router gates (`require.Channel(...)`, modelled on `ProjectEntity`); none in handlers or services.

- New actions (the complete set; the registry and its coverage test are written from this list): `channels:read|write`, `messages:write|moderate`, `calls:join|record|moderate|invite_agent`, `proposals:write|resolve`. `calls:invite_agent` is checked on `project/<p>/agent/<id>`, and `proposals:*` on `project/<p>/proposal/*` (see the meeting assistant sections).
- New resources: `project/<p>/channel/<id>`, and a new platform root `channel/<id>` for workspace channels and cross-project DMs (touches `iam.PlatformRootFor` and its sync test).
- **The one new engine feature.** Conditions compare against literal values only, and `principal.id` is an attribute, not a variable. "Private channel visible only to its members" therefore cannot be written in a policy today. Proposal: add a small, general **policy variable** (`${principal.id}`) and loader attributes `channel.kind` and `channel.member_ids`. The shipped roles then use two statements:
  1. Allow `channels:read` on public channels.
  2. Allow it on private channels and DMs when `channel.member_ids` contains `${principal.id}`.

  This keeps the rule in role definitions rather than in code.

### Task-aware features

- Reuse the BlockNote mention parser (`internal/pkg/mention`) for `@user`, `#task` and `#doc`. Task chips update live from existing task events.
- "Create task from message" and "convert thread to task or doc", with a stored back-link.
- Slash commands such as `/task`.
- Notifications: new types `chat_mention` and `dm`, with a separate unread badge (not the bell).
- Search: Postgres full-text search on `content_text`, scoped through the existing SQL-pushdown scoping.

### Agents as participants

- An `@agent` mention in a channel starts an `agent-runner` conversation with trigger source "chat"; the reply is posted as that agent's account.
- MCP tools: `chat_list_channels`, `chat_read_channel`, `chat_send_message`, plus a skill.
- Core-owned automation nodes (like the `git.*` ones): triggers `chat.message_posted` and `chat.mention`, action `chat.send_message`.

## UX: two levels of chat

Slack and Discord have one flat space. Paca has two scopes, **workspace** (instance-wide channels and every DM) and **project** (one set of channels per project), inside an app shell whose sidebar already switches between a workspace context and a project context. The design must let people track conversations across both without making the project sidebar noisy.

### Principles

1. **The sidebar follows context; tracking does not.** The sidebar lists only the channels of the scope you are in. Tracking across scopes is the job of the **Inbox**.
2. **Nothing is lost "elsewhere".** Unread and mention state of every other project is visible from wherever you are (project switcher, "All projects", Home project cards).
3. **DMs are workspace-level and reachable from every context.** Opening a DM inside a project keeps the project sidebar.
4. **Chat is next to the work.** A dockable chat pane on project pages (same pattern as the AI chat float) lets you talk while looking at the board.
5. **Calls survive navigation.** The LiveKit session lives in the authenticated layout, not in a page, so switching project or context never drops a call.

### Sidebar

- **Project context:** a *Chat* group above the project nav: Inbox, project channels (joined, starred or unread; "Browse channels" for the rest), voice channels with live participant avatars, then Direct messages. The project name carries an aggregate "unread elsewhere" badge.
- **Workspace context:** Inbox, workspace channels, Direct messages, and a Projects entry that expands to per-project unread counts.
- Unread channels are bold, mentions show a count, muted channels are dimmed. A persistent call bar sits at the bottom while in a call.
- Naming: the existing *Conversations* entry means agent conversations and stays as is. The new feature is called *Chat* in the nav and *Inbox* for the tracker.

### Inbox (the cross-scope tracker)

One screen, reachable from both contexts, with tabs **Mentions**, **Threads** (followed), **Unread**, **Saved** and **Drafts**. Every row carries a project chip (or *Workspace*) and the channel; a project filter narrows it. Opening a row navigates to `/projects/$projectId/chat/$channelId` and so switches the sidebar context automatically. Row actions: mark read, follow/unfollow, save.

- **Following:** you auto-follow a thread you post in or are mentioned in, and can follow any thread by hand. Followed threads are what the Threads tab lists, ordered by latest activity.
- **Mark read** works per channel, per project and for everything.

### Switching and finding

- The project switcher shows unread and mention counts per project, so an urgent mention in another project is visible before you switch.
- A command palette (Ctrl+K) jumps to any channel or DM across projects, with a project chip to tell same-named channels apart (`#release` in two projects), and switches context on open. There is no command palette in the web app today (`components/ui/command.tsx` is an unused cmdk primitive), so this is new work, included in the Phase 1 UI scope.
- Chat view header has a **This project / Everywhere** scope toggle for channel lists and search.

### Cross-scope rules

- Channel visibility and DM membership come from IAM, not from the UI. A task chip pasted into a DM to someone without access to that project renders as a locked chip, never leaking the title.
- Mentioning someone who is not in the project offers "Add to project" (if the author may), otherwise the mention is stored but not delivered with project content.
- Removing a member from a project drops their project-channel memberships. Archiving a project makes its channels read-only.
- Channels are created in one scope. Promoting a project channel to workspace, or moving it, is out of scope for v1.0.0.
- Task detail gets a **Discussion** section listing the threads that link to it, so conversation and work stay connected both ways.

### Notifications

Preference hierarchy: workspace default, then project override, then channel override, each one of *all*, *mentions only* or *nothing*. DMs and direct mentions always notify unless Do Not Disturb is on. A whole project can be muted. Chat notifications use a separate unread badge and do not share the bell's list.

### What this needs from the backend and realtime

- Realtime joins project rooms per project, so a user in project A would never hear about project B. Add a per-user room (`user:<id>:chat`, auto-joined at connect like `user:<id>:notifications`) that carries only small unread and mention deltas across every project the account may read. Message bodies still flow through `channel:<id>` rooms.
- `GET /chat/unread-summary` returns counts per project, workspace and DM in one call (powers badges, switcher and Home cards).
- Tables: `thread_follows`, `saved_messages`; `channel_members.last_read_seq` and `message_mentions` back unread counts; drafts stay local at first (see `composer-draft.ts`).
- Routes: `/chat` (workspace shell and Inbox), `/projects/$projectId/chat/$channelId`, and DMs opened under whichever context you are in.

### Mobile

The sidebar sheet keeps the same structure. Chat opens full-screen with Inbox as the default entry, and the call bar becomes a mini player above the tab area.

### Decisions

- A DM with an agent is a plain chat entry point that triggers agent conversations, so humans and agents share one DM model (details below).
- The Inbox stays chat-only for v1.0.0. Task and doc mentions stay in the notification bell, which gets a context upgrade (see "Task comments and mentions").

## Agent DMs: conversations

An agent DM is a DM channel with an agent account, plus a **conversation list**, so starting a new conversation or resuming an old one takes one click.

- **Layout:** left, the conversation list for this agent (title, last message preview, relative time, status chip: running, needs your answer, finished, failed); right, the selected conversation using the existing agent `Thread` renderer (streaming, tool calls, reasoning). On mobile the list is the master view and the conversation the detail, reusing the existing master-detail behaviour of the conversations layout.
- **Start:** a **New conversation** button at the top of the list, `Ctrl+Shift+N`, a `/new` command in the composer, and a command-palette entry "New conversation with <agent>". Opening an agent DM resumes the most recent conversation; a blank composer is one click away.
- **Continue:** pick any conversation and send a message; finished conversations accept replies wherever the agent runtime allows it (`canReplyToConversation`). Conversations auto-title after the first exchange; rename, pin and archive are available from the row menu. A search box filters the list by title and content.
- **One list for every origin.** The list is the existing Unified Conversations filtered to this agent: direct chats, conversations started by an `@agent` mention in a channel (tagged "from #backend", with a link back to the channel thread), task-triggered and automation-triggered ones. The cross-agent *Conversations* page stays as the all-agents view and links to the same rows.
- **Project binding:** a project agent's conversations belong to its project. A global agent shows a **Working in** selector (default: the project you are in, or none from the workspace context); the list groups by project chip.
- **Storage:** conversation events stay in `agent_conversation_events` and are rendered by the agent `Thread`. They are not copied into `messages`. `agent_conversations` gains a nullable `channel_id` (the DM or channel it belongs to) and an optional `source_message_id` for channel-triggered runs. When an agent is mentioned in a channel, its final answer is also posted there as a message with a "View full run" link.
- **Attention:** an agent question or a finished run raises an unread mark on that conversation row and an entry in the Inbox ("Needs your answer"), building on the existing question notification.

### Built so far (first slice)

A DM with a **global agent** is answered by it. This is a simpler shape than the two-pane layout above, and the rest of this section still describes where it is going.

- **One thread.** The DM is a normal channel; the agent's replies are messages of its own account. Each carries `run_id` and a "View run" link to the full conversation page, where reasoning and tool calls stay.
- **Bridge.** `chat_agent_dms` links the channel to the agent's global chat session. Posting in the DM hands the person's unsent messages to `SendGlobalChatMessage` (or `StartGlobalChatSession` for the first). The API's `ChatAgentConsumer` reads the agent event stream; on a `turn_end` it joins the turn's `agent_message_chunk` text into one message from the agent. A message written while the agent is working waits and goes out when the turn ends. A repeated `turn_end` does nothing (`bridged_index`). No new authorization: any signed-in person may already chat with any global agent.
- **Runs panel.** `GET /chat/dms/{id}/agent` lists the session's runs (status, title) and whether the agent is working. The DM header shows "working…", a "Runs (N)" popover offers search and status filters, and each run opens in the conversation page.
- **Not built:** project agents (their DM only keeps what you write, and says so), the `channel_id` column on `agent_conversations`, `@agent` triggers in channels, rename/pin/archive of runs, `/new`, working or needs-you marks in the sidebar and Inbox, streaming the answer into the thread, and removing the Conversations page. Markdown in an agent reply is shown as plain paragraphs.

## Task comments and mentions

### Typing and presence

Task comments get the same "X is typing…" indicator as chat. Both use one ephemeral presence layer in `services/realtime`.

- The client joins a **live room** for what it is viewing (`task:<id>`, `channel:<id>`; docs later). Access is checked once at join through the API's IAM (the existing access check and its short cache), not per event.
- `presence:typing` events are throttled on the client (on start, then every few seconds while typing), expire after about 5 seconds server-side, are fanned out across replicas through Valkey like the collaborative-editing cluster, and are never persisted.
- UI: "Mina is typing…" (or "Mina and 2 others…") above the comment composer, plus viewer avatars in the task header ("2 viewing"). An agent working on the task shows "Atlas is working…" from its conversation status instead of typing events.
- A per-user setting can turn typing broadcast off. Nothing is sent for a cleared composer or a read-only viewer.
- The same primitive serves doc comments and chat channels (built); sprint pages and chat threads can follow.

### Mention notifications that say where

Today a mention notification reads "Mina mentioned you in APO-142 Fix token refresh": no project, no sprint, no excerpt. Proposed card, in the bell and in a full `/notifications` page:

- **Header:** actor avatar, "Mina mentioned you", time.
- **Context row:** project chip, sprint chip (`Sprint 12`, or `Backlog` when the task has none; resolved when the notification is read, so it stays right if the task moves), task key and title, and the task's current status.
- **Excerpt:** the first ~140 characters of the comment with your mention highlighted. Stored with the notification, so it survives edits and deletion. Because it is a stored copy, it stays readable to the recipient after the comment is deleted or after they lose access to the project, and the unread-mention counts (task and sprint badges) likewise keep counting until the notification is read or the task is deleted. Both are deliberate: a notification is the recipient's own record. Anyone who needs mentions purged on access loss must delete the rows; nothing does it automatically.
- **Click:** opens the task panel scrolled to that comment and highlights it briefly, then marks the notification read.
- **Grouping:** several mentions on one task collapse to "3 mentions in APO-142".
- **Filters:** All, Mentions, Assigned, Agents, plus a project filter.

In-context cues, so you can see it without opening the bell:

- A small mention badge on the task card and list row while it has an unread mention for you.
- Sprint headers and the Sprints entry in the Interactions sidebar show a count of tasks that mention you.
- In the task activity pane an **unread since** divider marks comments newer than your last visit (new table `task_reads`: account, task, last seen activity).
- A toast when a mention arrives while you are online, with the same project, sprint and task context.

Gaps to close while here: `NotifyTaskDescriptionMentioned` and `NotifyDocMentioned` currently publish only a plugin event and create no in-app row. They should create notifications too. Optional follow-up: a **Watch** toggle on tasks (auto-watch when assigned, commenting or mentioned) so comment notifications follow the same rules as chat thread follows.

## Meetings (LiveKit)

### Topology

```
Browser ──HTTPS──▶ Caddy ─/api──▶ api   (IAM check → mint LiveKit JWT)
   │                    └─/livekit (WS signaling)──▶ livekit:7880
   └─── media: UDP (single mux port) + TCP 7881 fallback + TURN ──▶ livekit   (bypasses Caddy)
livekit ──webhooks──▶ api (/api/v1/webhooks/livekit) ──▶ realtime (who is in which room)
livekit ◀──▶ Valkey (required for multi-node; optional on a single node)
```

### Join flow

1. Client calls `POST …/channels/{id}/call/join`.
2. The router gates it on `calls:join`.
3. The API mints a JWT with a short TTL (about 10 minutes): identity is the account id, metadata carries kind and avatar, room is the channel id, publish/subscribe grants derive from IAM.
4. The client connects with `livekit-client` and `@livekit/components-react` (tiles, grid, control bar), styled with shadcn. Screen share and device selection come with them.

### Webhooks and moderation

The API verifies LiveKit webhooks (Go server SDK / `livekit/protocol`) and records `call_sessions` and `call_participants`. It publishes join/leave events so the sidebar shows who is in each voice room. Mute and kick call LiveKit's RoomService API behind `calls:moderate`.

### Infrastructure changes

- `deploy/caddy/Caddyfile` sends `Permissions-Policy: camera=(), microphone=(), geolocation=()`. It must allow `camera=(self), microphone=(self), display-capture=(self)`, or calls fail silently. `deploy/helm/paca/files/Caddyfile` is a hand-synced copy and `deploy/caddy/Caddyfile.dev` (mounted by `docker-compose.dev.yml`) carries the same header; all three change together. The e2e stack mounts the main `Caddyfile`.
- Add a `/livekit/*` signaling route.
- Caddy owns 443, so TURN/TLS cannot also use it. Use TURN/UDP (3478) plus TCP 7881 fallback; TURN/TLS needs its own port or domain and a trusted certificate.
- LiveKit does not run on serverless or private-NAT clusters, and needs a UDP media range (or single mux port) reachable from clients. Verify exact port options against LiveKit's config reference when building.
- A `livekit` service in `docker-compose.{dev,prod,e2e}.yml` (no separate profile; prod skips it with `--scale livekit=0`), the install script, and the Helm chart (LiveKit's own chart as a dependency, or a small template).

### Later (v1.1 candidates)

- ~~Recording via LiveKit Egress~~: built in Phase 3b (see [Phase 3b status](#phase-3b-status)); what remains is retention and deletion, audio-only recording and attaching a recording to a doc.
- Voice-interactive agent (TTS, turn detection) and realtime speech models. The silent meeting assistant, transcripts and forwarded tool calls are v1.0.0 (see below).
- Scheduled meetings.

## LiveKit Agents: fit assessment

Researched from LiveKit's public docs (Agents framework is Apache 2.0; Python and Node.js). Items marked *unverified* need a Phase 0 spike.

### What it is, and how it relates to Paca's agents

A LiveKit "agent" is a program that registers with the LiveKit server as an **agent server**, waits for dispatch, and for each dispatch starts a **job** (its own process) that joins a room as a participant. It is built for realtime voice/video: an STT, LLM, TTS pipeline (or a realtime speech model), turn detection, interruption handling, tools.

Paca's agents are different: accounts with IAM roles, run by `services/agent-runner` (Goose over ACP in a sandbox, or the user's own CLI), with MCP and skills. **Do not replace agent-runner.** The meeting agent is a thin bridge: LiveKit owns audio, STT, TTS and turn-taking, while the reasoning stays a Paca agent conversation reached through the Paca API and MCP. The agent keeps its own account and roles, so it can only do in a meeting what it could do anywhere else.

### Feature by feature

| LiveKit Agents feature | Apply? | Notes |
|---|---|---|
| Per-participant STT transcription, `lk.transcription` text streams | **Yes, first** | Official `multi-user-transcriber` example runs one session per participant (better than diarizing a mixed feed). Live captions render with `useTranscriptions` in `@livekit/components-react`. The framework does not persist transcripts: the worker must send finalized turns to the Paca API (flush periodically, not only at session end). |
| Notes and summary agent | **Yes, first** | Transcript stored as a doc or attachment in the call's channel; the meeting agent (the LiveKit worker, configured through the new `meeting` agent type) summarises it and proposes tasks through forwarded tool calls. Covers the roadmap line "take notes, summarize, turn discussion into tasks". |
| Explicit dispatch with `agent_name` and metadata | **Yes** | The API dispatches (AgentDispatchService) when someone invites an agent to a call. Token-based dispatch only fires when a room is first created, and voice channels are long-lived rooms, so use API dispatch. Metadata (JSON, up to 512 KiB) carries ids only (channel, call session, Paca agent id), never secrets. The invite is an IAM-gated route (`calls:invite_agent`, checked on the agent's own resource so a `Deny` on one agent applies; see [Meeting agent type](#meeting-agent-type)). |
| MCP in agents (`MCPToolset`, HTTP with bearer headers, `allowed_tools`) | **Yes** | A voice agent talks to `apps/mcp` using the agent's own API key, so authorization is the agent's roles. Filter tools per meeting. |
| Function tools and forwarding tool calls to the frontend (LiveKit RPC) | **Yes, v1.0.0** | The assistant proposes a task or update; the call UI shows a confirm card on the right person's screen and the browser performs the write. See [Forwarded tool calls](#forwarded-tool-calls-v100). |
| Voice-interactive agent (STT, LLM, TTS, VAD, turn detection) | **Later (v1.1+)** | Needs STT and TTS provider keys or a self-hosted OpenAI-compatible endpoint. Turn-detector models use a separate LiveKit Model License (not Apache): check it before bundling. |
| Realtime speech models (OpenAI, Gemini Live and others) | Optional | Lowest latency, but needs a provider key; keep behind a per-instance setting. |
| Text input (`lk.chat`) and text-only sessions | Partly | An agent can join without audio. Channel messages stay in Paca's `messages`; do not use `lk.chat` for persistence. |
| Multi-agent handoff, tasks and workflows | Skip for now | Matches the roadmap's multi-agent orchestration, which is a separate effort. |
| Speaker diarization | Skip | Each participant has their own track, so the speaker is known. Only useful for several people on one microphone in a room. |
| Telephony (SIP dial-in) | Skip for v1.x | Needs LiveKit's SIP service and a trunk provider. |
| Vision (agent sees screen share or camera) | Skip for now | Possible later; token cost is high. |
| Virtual avatars, Agent Builder, Embed Widget, LiveKit Inference, cloud observability | Skip | Cloud or vendor features. LiveKit Inference (STT and LLM through the LiveKit key) exists only on LiveKit Cloud; self-hosted instances bring their own provider keys. |
| Testing framework with LLM judges, `console` / `dev` run modes | Dev only | Useful for CI of the meeting agent; not a product feature. |

### Architecture options

1. **Agents framework worker (recommended):** a new optional container, `services/meeting-agent`, next to the `livekit` service. It registers with `LIVEKIT_URL`, key and secret and receives dispatches. **The LiveKit agent runs the reasoning** (its STT stream, its LLM loop, its function tools, including the forwarded ones). It is configured by a Paca agent of the new `meeting` type (see [Meeting agent type](#meeting-agent-type)): at dispatch it fetches that agent's decrypted settings from the API over an internal route, and afterwards it calls the Paca API with the agent's own API key, so everything it writes is judged by the agent's roles. `services/agent-runner` is not involved.
2. **Batch transcription after the call:** record with LiveKit Egress, then run STT on the audio file. Cheaper and simpler (no live worker, no always-on agent server) but no live captions and Egress is a heavy container. A reasonable lighter tier for operators who do not want a worker.
3. **Hand-rolled Go participant** with the server SDK: rejected. It would re-implement VAD, turn detection and interruption handling.

Runtime choice is open. Python has the widest plugin and feature coverage (diarization, multi-speaker adapter, the multi-user transcriber example), but the repo deliberately removed its earlier Python agent service (`services/ai-agent`). Node.js (`@livekit/agents`) fits the TypeScript toolchain, but Node parity, and Bun compatibility, are *unverified*, and a community report says a single Node `AgentSession` transcribed only its linked participant. Spike Node first, fall back to Python in an isolated container.

### Risks and requirements

- **Trust boundary:** the worker holds the LiveKit API secret (it can join any room). Treat it as infrastructure, never give it user powers. Authorize through Paca API routes and the agent's account, never from dispatch metadata. Because the LiveKit agent also runs the LLM loop, the worker holds the meeting agent's decrypted LLM and STT keys in memory for the length of a job. The API decrypts and hands them over per job; the worker never holds `ENCRYPTION_KEY` (anyone with it can derive every agent's API key).
- **Privacy and consent:** transcription must be explicit. Show a visible "transcribing" indicator and the agent as a participant, make it an opt-in per call, and let admins disable it workspace-wide. Retention follows the channel's retention rules.
- **Capacity:** one STT stream per participant and one job process per room. LiveKit reports 10-20 participants works in its own meetings, with no official ceiling. Avoid burstable instance types for the worker.
- **Resilience:** if a job drops, the LiveKit server re-dispatches within about 15 seconds, so flush transcript turns incrementally to survive it.
- **Self-hosting details *unverified*:** how a self-hosted LiveKit server needs to be configured for agent dispatch, per-job CPU and memory, and local STT. There is no official Whisper plugin; the community route is the OpenAI plugin against a local OpenAI-compatible server, wrapped with VAD because Whisper is not streaming. Ollama and Kokoro appear in LiveKit's model docs.

### Suggested order

v1.0.0 (a silent **meeting assistant**: it listens and proposes, it does not speak):

1. Worker, explicit dispatch, and inviting the assistant from the participants panel (IAM-gated).
2. Live captions and transcript persistence (STT keys, consent UI).
3. Forwarded tool calls with confirm cards (see below), including "suggest tasks from this discussion".
4. Notes and summary written to the channel.

v1.1: voice-interactive agent (TTS, turn detection, interruptions), realtime speech models, recording, server-side approval.

## Meeting agent type

A fourth `agent_type`, `meeting`, next to `llm`, `acp` and `provider_cli`. Its job: let a user configure an assistant to invite into calls without writing code or touching LiveKit.

**What it is.** An ordinary agent account (handle, avatar, project or global scope, roles, API key) whose runtime is the LiveKit worker instead of `agent-runner`. Everything that picks agents must learn the difference: `agent-runner` never runs it, it cannot be assigned to tasks, and it is excluded from the chat/conversation start pickers, the automation "trigger AI agent" action, auto-assignment and Jev's auto mode. In an agent DM it shows "invite me to a call" instead of a conversation list.

**Configuration**

| Group | Fields |
|---|---|
| Reused from the agent entity | name, handle, description, avatar, scope, roles; `llm_provider`, `llm_model`, `llm_api_key_secret`, `llm_base_url` (the reasoning model, same provider catalog as `llm` agents); `system_prompt` (instructions); MCP servers and skills (read-only tools); `parallelism_limit` (calls it may join at once). |
| Rejected for this type | ACP and CLI fields, environment and folder, Docker, git identity, max iterations. |
| New: `agent_meeting_settings` (one row per agent) | `stt_provider`, `stt_model`, `stt_api_key_secret`, `stt_base_url` (OpenAI-compatible endpoint, for self-hosted Whisper), `stt_languages` (empty = auto-detect); `mode`; `suggest_interval_seconds`; `proposal_kinds` (enabled subset) and `max_proposals_per_call`; `store_transcript`, `transcript_retention_days`, `announcement` (text shown to everyone when it joins); `summary_target` (channel message or a doc folder); `default_target` (requester or host). |

`mode` is one of `on_demand` (default: acts when someone asks or clicks "Suggest tasks"), `auto_suggest` (proposes every N seconds from the latest transcript window), or `notes_only` (transcript and summary, no proposals). Secrets use the same AES-GCM encryptor as other agent keys and are never returned by the API. The STT provider list comes from a catalog file next to `llm_models.json`, limited to the plugins the worker bundles (chosen in the Phase 0 spike).

**Runtime handoff**

1. Someone invites the agent; the API checks `calls:invite_agent`, records a `call_agent_sessions` row (call, agent, invited by, joined and left times, dispatch id) and dispatches with metadata ids only.
2. The worker calls an internal route with the dispatch ids and gets the agent's decrypted LLM and STT settings plus a session API key for the agent's account. Settings live in job memory only.
3. The worker joins as a visible participant (identity = the agent's account id, attribute `kind=agent`), runs STT per participant, and calls Paca with the agent's key.

**Create and edit UI.** The Create agent dialog gets a *Meeting assistant* option. The agent's detail page has tabs:

- **Reasoning:** provider, model, key and a *Test connection* button (the existing LLM fields component).
- **Listening:** STT provider, model, key, languages, *Test connection*.
- **Behavior:** mode, instructions, enabled proposal kinds, limits.
- **Privacy:** store transcript, retention, join announcement.
- **Access:** roles (a new *Meeting assistant* project role template granting `proposals:write` and read actions on tasks, sprints and docs), MCP servers and skills.
- **Try it:** a private test room containing only the user, with live captions and a sample proposal card, so configuration can be checked before a real call.

**Invite UX**

- **Lobby:** an "Add assistant" toggle with a picker of the project's meeting agents the user may invite. Each shows its mode and a privacy summary ("Transcribes this call, kept 30 days").
- **In a call:** the participants panel has *Invite assistant* and, once joined, *Remove from call* (`calls:moderate`). The assistant appears as a participant with an agent badge and a listening indicator, and everyone sees its join announcement.
- **Channel default:** a channel can name a default assistant that the lobby suggests; it never joins without someone confirming.
- **Instance switch:** an admin setting turns meeting assistants off for the whole workspace (default off until the first meeting agent is created).
- **Gate:** `calls:invite_agent` on `project/<p>/agent/<id>`, in `router.go` like the other agent gates. The agent must be a project member (a project agent, or a global agent attached to the project).

## Forwarded tool calls (v1.0.0)

LiveKit's forwarding model: the agent does not run the tool itself. It calls a named RPC method on a client participant, the client does the work and returns a string. We use it so that **the assistant proposes and a human's browser performs the write**.

### Why the browser performs the write

- The write runs through the existing REST endpoints with the clicking user's session, so it is judged by the user's own roles at the existing router gates. No new authorization path, and the agent needs no write roles in the project.
- It cannot exceed what that person may do. A user without `tasks:write` simply gets the normal 403 on the card.
- The activity feed records the human as the actor, with the origin "suggested by Atlas in #standup" (the activity log already has an `origin` column).
- The same card code is reused when an agent proposes in a channel, and for the existing analyst agent whose drafts only a human can approve.

Read-only tools (look up a task, list the sprint, read a doc) are **not** forwarded: they run in the worker through the Paca MCP server with the agent's own API key.

### Flow

```
participant speaks ─▶ worker STT ─▶ LLM decides: propose_task(...)
 1. worker ─▶ API  POST /projects/{p}/proposals with the agent's own API key
                   (router gate proposals:write; typed payload, validated, stored: status pending)
 2. worker ─▶ target browser  RPC "paca.proposal.show" {proposal_id, summary}   (small, fast ack)
 3. browser ─▶ API  GET proposal (authorized) ─▶ renders confirm card (editable fields)
 4. user clicks Create ─▶ browser claims the proposal, calls the normal endpoint (e.g. create task)
 5. browser ─▶ API  resolve proposal {status, result}  ─▶ realtime event to the channel
 6. browser ─▶ worker RPC "paca.proposal.resolved" {proposal_id, status, task_key}   (tool result for the LLM)
```

The proposal row in the database is the source of truth. LiveKit RPC is only the live transport, because of its limits:

| RPC limit (LiveKit docs) | How we design around it |
|---|---|
| Payload 15 KiB each way; method name 64 bytes | RPC carries only an id and a short summary; the browser fetches the full proposal from the API. |
| Default 10 s timeout covering the whole request | The RPC is only an acknowledgement ("card shown"). Approval is asynchronous and arrives as a second RPC or is read from the proposal. |
| No documented delivery guarantees (the RPC page states none); no handler (error 1400), recipient gone (1401, 1503) | The proposal is persisted and also shown as a card in the channel, so a missed RPC loses nothing. The tool result tells the LLM "pending, user not reachable". |
| No authentication of the response, and the docs' example trusts the first remote participant | Never trust an RPC reply for state. Identities come from the token our API minted, so the worker targets one specific `account_id`; outcomes are read from the API row. |
| Hidden participants cannot call RPC | Call participants are never hidden; the worker joins visible and labelled as an agent. |

### Who receives it

The **requester**: the participant whose request triggered the proposal (button "Suggest tasks", or the person who invoked the assistant), falling back to the call's host. Everyone else sees a read-only status chip ("Atlas suggested: Create task. Mina is reviewing") from the proposal's realtime event, never the editable card.

### Proposal model

`agent_proposals`: `id`, `project_id`, `channel_id`, `call_session_id?`, `agent_account_id`, `target_account_id`, `kind`, `payload` (jsonb, validated per kind), `status` (`pending`, `claimed`, `resolved`, `rejected`, `expired`, `failed`), `result` (jsonb), `resolved_by`, `expires_at`, timestamps.

- Kinds are **typed, not arbitrary API calls**: v1.0.0 has `task.create`, `task.update` (status, assignee, priority, sprint), `task.comment`, `doc.create_from_notes`, and UI-only `task.open` (no approval). A new kind needs a payload schema, a card renderer and a mapping to existing endpoints.
- The card shows the **actual fields that will be written**, taken from the payload, never the LLM's description of them.
- Claiming (`pending` to `claimed`, compare-and-set) stops two tabs or two clicks from creating the task twice. Unresolved proposals expire (default 30 minutes) and are listed in the end-of-call summary ("3 suggestions pending").
- Authorization stays in router gates: creating a proposal needs `proposals:write` (held by the agent's role, on `project/<p>/proposal/*`); resolving needs `proposals:resolve` with a condition that the caller is the proposal's target (uses the `${principal.id}` policy variable from the private-channel work). The underlying write is judged by the normal task or doc gates.

### Security notes

- **Prompt injection:** a participant can say anything and steer the assistant. Human approval of explicit fields is the mitigation; rate-limit proposals per call and per agent.
- The worker acts as the meeting agent's own account (its API key, its roles). It cannot call write endpoints as a user, and the role template for meeting agents grants only `proposals:write` plus read actions.
- Proposals appear only to the target and to readers allowed by IAM on the channel.

### UI

- Call view gets an **Assistant** panel (side sheet on desktop, bottom sheet on mobile) listing cards; a toast announces a new one on the target's screen.
- Card: title, fields (project, sprint, assignee, priority, description) editable inline, **Create**, **Edit in full form**, **Dismiss**; after creation it becomes a task chip.
- The same card is posted into the call's channel as a `proposal` message, so unreviewed suggestions survive the call.
- Live captions appear above the controls; a visible "Atlas is listening and transcribing" indicator is mandatory.

### Scope this pulls into v1.0.0

The worker (`services/meeting-agent`), explicit dispatch, per-participant STT, transcript persistence, consent UI, `agent_proposals`, the proposal events and guards, the Assistant panel and card renderers. A voice-interactive agent stays out: the silent assistant needs no TTS, no turn detection, and so avoids the turn-detector model license question.

### Spike items

- Does RPC work between a browser and a **text-only, no-audio-output** agent on a self-hosted server? (LiveKit's forwarding page does not say.)
- Token grants: does the browser need `canPublishData` to reply to the agent, and the worker to call the browser? (The RPC page does not say.)
- Node vs Python worker, including registering RPC methods on the agent's local participant, and async tool results while a proposal is pending.
- Behaviour on reconnect: a user who reloads mid-card gets the card from the API, not from RPC.

## Phase 0 results

All five spikes ran on 2026-11-17 and came back **go**. The probes were throwaway and are not in the repo.

### (a) Private channels in IAM: go

A small engine change is enough: an operand of the form `${principal.<name>}` expands to the request's value for that key before the operator runs. An absent attribute expands to nothing, so positive operators fail closed. Only `principal.*` keys can be variables, so a variable never triggers a resource attribute load. Validation rejects any other `${...}` operand and rejects variables with `StringLike` or `Bool`.

A prototype with a test confirmed the two-statement policy (public: `channel.kind` equals `public`; private and DMs: `channel.member_ids` equals `${principal.id}`) gives the right decision for public non-member, private member, private non-member, DM without members and a missing principal.

Still to do in Phase 1: register `channel.kind` and `channel.member_ids` with a loader and labels in all locales, add the `channel` platform root and resource root, and document the variable in `docs/api/roles-and-policies.md` and [authorization.md](authorization.md). If `channel.member_ids` is slow for very large private channels, replace it with a principal-scoped boolean.

### (b) Composer format: go on BlockNote

`CommentEditor` (`apps/web/src/components/shared/comment-blocknote.tsx`) is already a compact composer with `@user`, `#task` and `#doc` menus, and `internal/pkg/mention` already parses that format. Chat only needs Enter to submit (Shift+Enter for a newline).

- Store BlockNote-compatible JSON in `messages.content`, with a small block set (paragraphs, lists, code, quote).
- Render read-only messages with a lightweight block renderer, not one BlockNote editor per message (`CommentDisplay` creates a full editor each). **Not measured:** benchmark 50 and 200 messages in Phase 1.

### (c) LiveKit behind Caddy with Valkey: go

A scratch stack (LiveKit 1.13.11, Valkey 8, Caddy 2) worked with `reverse_proxy livekit:7880` and no special directives. Two headless Chromium pages with fake media devices joined one room and each received the other's audio and video, so Playwright can run calls in CI.

- Media does not pass through Caddy. It needs the host's UDP range and TCP 7881, which is why LiveKit stays opt-in.
- `livekit/server-sdk-go/v2` only compiles with `replace` directives for LiveKit's pion forks (`pion/webrtc/v4` and `pion/ice/v4`, as in the SDK's own `go.mod`). Token minting alone (`livekit/protocol/auth`) does not need the SDK.
- Not verified: TURN, NAT traversal, TLS termination, Helm rendering.

### (d) Realtime fan-out: go, check access at join

A real Socket.IO server with 500 and 2,000 sockets in one room at 20 messages per second compared a room emit against the existing per-event `AccessDecider` path:

| Sockets | Strategy | Server CPU (ms) | Delivery p50 / p99 (ms) | API checks |
|---|---|---|---|---|
| 500 | room | 2686 | 8 / 25 | 0 |
| 500 | per-event, warm cache | 3277 | 9 / 27 | 500 |
| 500 | per-event, no cache | 4968 | 15 / 34 | 78,000 |
| 2000 | room | 3581 | 14 / 47 | 0 |
| 2000 | per-event, warm cache | 4664 | 17 / 55 | 2,000 |
| 2000 | per-event, no cache | 6642 | 21 / 50 | 312,000 |

Delivery is cheap either way (the warm path costs about 30% more CPU). The cost is API load, which the stubbed API (3 ms per batch) does not model. Even a warm cache sends one check per socket per channel every 30 s, and any miss or IAM flush sends one per socket per message. So: check at join and on the IAM-change resync, then `io.to("channel:<id>")`. A channel is the unit of access; a per-message deny is not supported. This was single-node on loopback; load-test the join path against the real API in Phase 1, since a reconnect storm becomes one check per socket per joined channel.

### (e) Forwarded tool calls over LiveKit RPC: go, with constraints

Tested against the same stack with a Go client (`server-sdk-go` v2.20): an agent participant called `paca.proposal.show` on a user participant that answered after 2 s.

- It works through Caddy and Valkey, with async replies. The handler sees the caller identity from the caller's token, set by the server.
- The caller's token needs `CanPublishData`. Without it the call fails after about 7 s as `1501 Connection timeout`, indistinguishable from an absent participant, so the worker must report it clearly. An unregistered method returns `1400`.
- Any participant with `CanPublishData` can call any other, so the browser handler must accept `paca.proposal.*` only from identities the API minted for agents (for example an `agent:` prefix) and treat payloads as untrusted.
- RPC timeouts are bounded (the SDK clamps to at least 8 s, default 15 s). A person may take minutes, so RPC only shows the card; the proposal state and its resolution live in `agent_proposals` and the API.
- Not decided: Node vs Python worker. No LiveKit Agents worker or STT was run. Decide it when the `services/meeting-agent` skeleton is written.

## Delivery plan

| Phase | Scope | Exit criteria |
|---|---|---|
| **0. Spikes (about 1 week)** | (a) IAM policy variable and `channel.member_ids` loader. (b) Composer format: compact BlockNote vs lighter markdown JSON. (c) LiveKit behind Caddy with Valkey, and a two-browser call. (d) Fan-out test: a busy channel with many sockets. (e) Meeting assistant: RPC with a text-only agent on self-hosted LiveKit, token grants for RPC, and Node vs Python worker (see "Forwarded tool calls"). | A go/no-go per spike. **Done**, see [Phase 0 results](#phase-0-results). |
| **1. Chat core** | Migrations; `domain/service/repository/handler/dto/router` for channels, messages, threads and reactions; unread/read state, mentions, attachments, notifications; event topics with `AuthzFor` and registry regeneration; realtime channel rooms plus the per-user `user:<id>:chat` room; `GET /chat/unread-summary`; web UI (command palette, sidebar Chat group, Inbox, channel view, thread pane, composer, project-switcher badges, mobile layout); default roles; i18n in all 9 locales. | Works end to end across two users, including private channels and DMs. Unit and e2e tests pass. |
| **1b. Presence and task-comment UX (independent track, can start first)** | Ephemeral presence layer in `services/realtime` (typing, viewers); typing indicator in task comments; enriched mention notifications (project, sprint, excerpt, deep link to the comment); task card and sprint badges; `task_reads` and the unread-since divider; in-app rows for description and doc mentions; `/notifications` page. | Typing and rich mentions work on tasks without any chat tables. The presence layer is then reused by chat. |
| **2. Task-aware and agents** | Task/doc chips, create-task-from-message, slash commands, search; `@agent` triggers, MCP tools, skill; automation nodes; agent DM conversation list (new, resume, search, pin, archive) with `agent_conversations.channel_id`. | An agent can be mentioned in chat, reply and create a task, and its DM lists every conversation. |
| **3. Meetings** | Compose/Helm/Caddy changes; token endpoint, webhooks, `call_*` tables; voice channels, huddles, screen share, sidebar presence, moderation. | Real calls work through the gateway on Compose; the Helm chart renders and deploys. |
| **3b. Meeting assistant (v1.0.0, needs Phase 3)**, built, see [Phase 3b status](#phase-3b-status) | New `meeting` agent type (domain, validation, `agent_meeting_settings`, STT catalog, create/edit UI, role template, picker exclusions); `services/meeting-agent` worker and dispatch; invite from the lobby and participants panel (`calls:invite_agent`); per-participant STT, live captions, transcript persistence, consent indicator; `agent_proposals` with events and guards; RPC forwarding (`paca.proposal.show` / `paca.proposal.resolved`); Assistant panel and cards; notes summary posted to the channel. | In a real call, the assistant transcribes and proposes a task; the requester approves on their own screen; the task exists with the user as actor and "suggested by" origin. |
| **4. v1.1 extras** | Recording, voice-interactive agent (TTS, turn detection), realtime speech models, scheduled meetings, server-side approval. | |

Phases 1 and 3 can run mostly in parallel once the `channels` table and IAM resources land: the LiveKit infra and token service touch different files from the chat UI. Each phase also updates the docs (`docs/architecture`, API docs, roadmap) and adds e2e specs. The e2e suite includes mobile projects, and calls need a mocked media device in Playwright.

## Phase 1b status

Built, unit-tested and covered by Playwright specs (`apps/e2e/tests/projects/task-presence.spec.ts`, `task-mentions.spec.ts`):

- **Presence layer** (task, doc and chat-channel rooms, direct messages included; names come from the project member list with the server-sent username as a fallback) in `services/realtime/src/presence.ts` (live rooms, access checked at join, typing leases, cross-replica merge over Valkey); protocol and limits are in [services/realtime/README.md](../../services/realtime/README.md#presence-viewers-and-typing).
- **Task, doc and chat UI:** "X is typing…" above the comment composer (at the bottom of the activity list) on tasks and docs, and above the message box in channels and direct messages; viewer avatars in the task, doc and channel headers; and a per-browser switch to stop sending typing (Account → Profile).
- **Mention notifications that say where:** migration `000095` adds `comment_id`, `mention_source` and a stored `excerpt` to `notifications`. The list response gains `comment_id`, `mention_source`, `excerpt`, `task_status_name/color`, `sprint_id` and `sprint_name`; status and sprint are joined at read time so they stay right after the task moves. Cards show project, sprint (or *Backlog*), task, status and the excerpt with the mention highlighted; runs of mentions on one task collapse to "3 mentions in #142". Clicking opens the task with `?comment=<id>`, scrolled to and briefly highlighting that comment (the activity pane pages back until the comment is loaded, and stops at the end of the feed or a failed page).
- **`/notifications` page** with All / Mentions / Assigned / Agents tabs and a project filter (`GET /users/me/notifications?type=&project_id=&actor=agent`), and a toast when a mention arrives while you are online.
- **Unread cues:** `GET /users/me/notifications/unread-mentions` feeds a badge on task rows and a count on sprint headers. `task_reads` plus `PUT /projects/{id}/tasks/{taskId}/read` (gated on `tasks:read`) record the visit, read the caller's mentions on the task, and return the previous visit time for the *New since your last visit* divider in the activity pane.
- **Description and document mentions** now create in-app rows (`mention_source` `description` / `document`) as well as the plugin event.

Not done: the Watch toggle on tasks; the sprint-count shortcut in the Interactions sidebar (only sprint headers in the list view show it); badges on board cards; a Playwright scenario for the sprint-header badge (it only exists in a list grouped by sprint); copy for the mention excerpt beyond the first 140 characters of text. Chat threads do not report typing yet.

## Phase 2 status

Built and unit-tested (Go, web, MCP); not run end to end against a real agent runtime or in Playwright.

- **Chips.** `#` in the composer suggests the project's tasks and documents. A chip stores ids only (`taskReference` `{id, projectId, number}`, `docReference` `{id, projectId}`), never the title: the title is read when the message is shown, with the reader's own access, so someone who may not open the item sees a locked chip. Task chips share the task query, so they follow live task updates. Message text keeps a `#<number>` / `#doc` label for previews and search.
- **Create task from message** (toolbar button, needs `tasks:write`) and **`/task <title>`**: the browser creates the task with the person's own session and posts a reply with the chip in the message's thread, which is the link back. The task side has no back-link yet (no `message_links` table, no *Discussion* section on the task).
- **Slash commands.** The composer takes a command list: `/task` in project channels, `/new` in a global agent's DM. `/` opens a menu only as the first word.
- **Search.** `GET /users/me/chat/search?q=&project_id=` (trigram `ILIKE` on `content_text`, migration `000096`, three characters or more), scoped by the same per-project channel scopes as the Inbox. A search box sits above the Inbox tabs.
- **`@agent` triggers.** Mentioning an agent that is a member of the project, in a project channel, starts a run through the gated `TriggerDirectMessage` (the author needs `conversations:write` on the agent). The agent is shown the mention and, in a public channel, the eight messages before it; a private channel shares only the message itself. When the run's first turn ends, `ChatAgentConsumer` posts its answer as a reply in the thread from the agent's own account, mentioning the asker, with a "View run" link. Link table `chat_agent_runs` (migration `000097`) replaces the `agent_conversations.channel_id` column this document first proposed: it needs no change to the agent runtime. The run uses the existing `automation_message` trigger type. An agent's own message never starts a run, so two agents cannot loop. A refused or failed run posts nothing (it is logged).
- **MCP tools and skill.** `chat_list_channels`, `chat_read_channel`, `chat_send_message` (message text returned inside untrusted-data fences) and the bundled `paca-chat` skill. They run on the agent's own roles.
- **Automation nodes (core-owned).** Triggers `chat.message_posted` and `chat.mention` (task-less; public channels only; never for an agent's message) and action `chat.send_message`, which posts as a chosen agent member. The engine renders `{{event.*}}` in the action's text, and a new rule in `NodeRequiresTask` lets it run in a task-less run.
- **Agent DM conversations.** The runs list of an agent DM now also holds the runs the person's channel mentions started, tagged *from #channel*, for project agents too; `/new` (`POST /chat/dms/{id}/agent/new`) detaches the DM from its session so the next message starts a new conversation.

Not done: the two-pane agent DM layout, rename/pin/archive of runs, run search over content, `agent_conversations.channel_id`, working or needs-you marks in the sidebar and Inbox, the task *Discussion* section and `message_links`, streaming an answer into the thread, a reply for a run that failed, an automation condition on the channel or text (filter with the agent's own judgement for now), a thread-pane `/task`, and Playwright specs for any of this.

## Phase 3 status

Built, unit-tested (Go, web) and covered by `apps/e2e/tests/projects/chat-calls.spec.ts`: two real Chromium contexts (fake microphone and camera) join a voice channel through the gateway, see each other, appear in the API's participant list through LiveKit's webhooks, one is removed by a moderator (that tab closes), and both leave. The e2e Compose file runs LiveKit by default; the spec skips itself if calls are off. Not verified: TURN and NAT traversal, TLS, a deployed Helm release.

- **Data.** Migration `000098`: `call_sessions` (one open session per channel, partial unique index) and `call_participants` (one open row per account and session). It also adds `calls:join` to the existing Editor roles and to the platform `USER` role's `DirectMessages` statement; the shipped templates carry it too. `calls:moderate` is held through `*` by the project Admin role only. `call_participants` carries LiveKit's connection `sid`, so a late `participant_left` from a replaced connection (the same account in a second tab) does not close the newer row; `call_removals` holds the short bar set by a removal.
- **LiveKit adapter** (`internal/platform/livekit`). Tokens, webhook verification and the RoomService calls are written against LiveKit's wire formats (HS256 access tokens, a signed body hash, Twirp JSON) rather than the server SDK, which brings a large dependency tree for three small features. Verified against `livekit-server` 1.13.11: its admin-token and Twirp calls are accepted, a wrong secret gets 401.
- **API.** The room is named after the channel id. `POST …/channels/{id}/call/join` (gated on `channels:read` + `calls:join`) opens the session and returns `{url, token, room, expires_at}`; the token lasts 10 minutes and grants publish and subscribe. `GET …/call` and `GET /projects/{id}/calls` (scoped like the channel list) say who is in a call. `POST …/call/participants/{accountId}/mute` and `DELETE …/call/participants/{accountId}` need `calls:moderate`. The same join and read routes exist under `/chat/dms/{id}/call` for huddles in direct messages, without moderation. Moderation is not ranked: anyone holding `calls:moderate` can mute or remove any participant, including an Admin (the grant is the policy's decision, not the service's). Mute silences the audio tracks being published at that moment and the person can unmute; it is not a persistent server mute. Removal also bars the account from the channel's call for the token lifetime (10 minutes), because LiveKit cannot revoke a token already issued: join answers 403 `CALL_REMOVED` and a reconnect with the old token is disconnected by the `participant_joined` webhook. Webhook deliveries are idempotent and bounded by the signed token's expiry; there is no event-id replay store. `GET /chat/calls/config` says whether LiveKit is configured; calls are on only when `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` are set and a signalling URL is known (`LIVEKIT_URL`, which defaults to `PUBLIC_URL` with `ws(s)://` and `/livekit`). Otherwise join, mute, remove and the webhook answer 503 `CALLS_DISABLED`, while the reads (`GET …/call`, `GET /projects/{id}/calls`) return an empty result and `GET /chat/calls/config` returns `enabled: false`, so the UI can hide calls.
- **Webhooks.** `POST /api/v1/webhooks/livekit` (open, per-IP rate limited) verifies LiveKit's signature and body hash, then records room start and finish and participant joins and leaves, and publishes `chat.call.changed` to the channel's realtime room. Rooms whose name is not a channel id are ignored.
- **Web.** `livekit-client` with Paca's own tiles and controls. Starting or joining a call opens it in its own browser tab (`/call/<project or dm>/<channel>`, a named window, so a second click focuses it); that tab owns the LiveKit room and closes itself when the person leaves or is removed, so browsing the app in the main tab never touches the call. The main tab shows who is in it and a Back to call button. Voice channels show a banner with who is in; any channel or direct message can start a huddle from its header; sidebar voice rows show the head count (polled every 20 seconds, plus the realtime announcement in the open channel). Camera, microphone and screen share toggles; mute and remove for people with `calls:moderate`.
- **Infra.** The three Caddyfiles (they stay in sync by hand) allow `camera`, `microphone` and `display-capture` for the site and proxy `/livekit/*` to LiveKit. `livekit` is a regular service in the dev, e2e and prod Compose files (prod idles until `LIVEKIT_API_SECRET` is set) (single node, no Redis, one UDP mux port 7882 and TCP 7881 published on the host). The Helm chart has `livekit.enabled` (off by default), a Deployment on the host network, a Service, a ConfigMap, and the API and gateway wiring.

Not done: the sidebar presence of huddles in non-voice channels (shown inside the channel only), moderation in direct messages, ending a call for everyone, a stale-session sweeper if a webhook is lost, TURN/TLS and NAT traversal (not verified), multi-node LiveKit with Valkey, an install-script prompt for the LiveKit settings, and everything in Phase 3b.

## Phase 3b status

Built, uncommitted. Verified: Go unit tests, the repository against a real Postgres (migration `000099` on a fresh database and on a copy of a live one), 33 worker tests, the web tests, three runs against a **real LiveKit server**: `services/meeting-agent/scripts/smoke.mjs` (the worker with fakes for the API, STT and LLM), `scripts/integration.mjs` (the real API and the worker over HTTP, 30 checks including who may read, claim and resolve a proposal), and `apps/e2e/tests/projects/chat-assistant.spec.ts` (Chromium: add the assistant from the picker, see the consent indicator and captions, ask for suggestions, edit and create the task, leave; create and configure a meeting agent from the Agents page; a member without `calls:invite_agent` is offered no way in). The existing agents, calls and roles specs still pass. **Not verified:** a real STT provider or LLM (everything above fakes both), Helm deployment, TURN/NAT, Firefox/WebKit/mobile (fake media is Chromium-only), more than one assistant or one call at a time.

- **Transcription first; notes and suggestions are options (revised).** Transcription is what a meeting assistant is for, and it needs only a transcription provider. The **notes** it posts after a call (`summary_target`, default `none`) and the **suggestions** of tasks and docs (`mode`, default `notes_only`, which now means *no suggestions*) are switches its owner turns on, and only they use a language model. So the model, key, base URL and instructions are optional: the create dialog shows its *Assistant* card only when either switch is on and then requires it, `POST /projects/{p}/agents` requires `llm_*` only when the `meeting_settings` it carries turn either on, and `PATCH …/meeting-settings` that turns one on for an agent with no model answers 400 `MEETING_MODEL_REQUIRED`. The invite check, the worker (it builds no LLM client) and the Overview tab (it hides the model fields) follow the same rule. With suggestions on, the owner picks *on demand* or *automatically*. The create dialog and the Assistant tab have both switches. People read what was said without opening the agent: `GET .../channels/{c}/meetings` lists the calls that were transcribed or recorded, `GET .../meetings/{callId}/transcript` returns the lines with who said them (`?format=txt` saves a file), both gated on reading the channel, like its messages. The call view has a *Transcript* panel that fills in while people talk, and the channel header a *Meeting history* dialog. Transcript lines still expire with the agent's retention.
- **Call recording.** Independent of the assistant: anyone holding `calls:record` (project Admin through `*`; add it to a role to share it) starts and stops a recording with `POST/DELETE .../call/recording` (not in direct messages). A recording is a LiveKit **egress** (`StartRoomCompositeEgress`, grid layout, one MP4) written straight to the bucket the API already uses (`recordings/<project>/<channel>/<id>.mp4`, same credentials), so no media passes through the API. `call_recordings` follows it through LiveKit's `egress_*` webhooks (starting, recording, stopping, complete, failed; one open recording per call). Everyone in the call sees a non-dismissable *This call is being recorded* notice (it is part of `GET .../call`, refreshed by the realtime announcement), and starting asks for confirmation first. Afterwards the recording is listed in the meeting history and plays or downloads through a one-hour presigned link fetched on demand (`GET .../recordings/{id}/url`), for anyone who can read the channel. It is switched on by `LIVEKIT_EGRESS_ENABLED=true` on the API, which needs the egress service (Compose `--profile recording`, `livekit/egress:v1.15.0`, about 4 GB, `SYS_ADMIN`) and LiveKit configured with Valkey. **`LIVEKIT_NODE_IP` must be an address the egress container can reach (the machine's LAN or public IP), not `127.0.0.1`:** the egress browser joins the room over WebRTC and would otherwise never connect ("Start signal not received"). Egress also needs its internal `ws_url` marked `insecure`, because its browser loads the page from an https template. Not done: Helm for egress (the chart has no `egress` or Valkey block for LiveKit), retention or deletion of recordings, audio-only or per-participant recording, recording from a direct-message huddle.
- **Agent type.** `agents.agent_type = 'meeting'` (project agents only; a global meeting agent is refused). It reuses `llm_provider`, `llm_model`, `llm_api_key_secret`, `llm_base_url` for its reasoning model and `system_prompt` for its instructions; git identity, Docker and an environment are rejected. `agent_meeting_settings` holds the rest (listening, mode, proposal kinds, retention, announcement, summary target, default reviewer). Its STT key is encrypted like the other agent keys and never returned (`has_stt_key` only). `GET/PATCH /projects/{p}/agents/{a}/meeting-settings` (gated like the agent: `agents:read` / `agents:write`). **A meeting agent is created together with how it listens:** `POST /projects/{p}/agents` with `agent_type: meeting` requires `meeting_settings` with a transcription provider, a model, and an API key or a self-hosted endpoint (`stt_base_url`), checked before anything is stored; if the settings cannot be saved the new agent is removed again. Later edits that touch a transcription field must leave a working setup (provider, model, and a key or endpoint), so an agent cannot be edited into one that cannot listen. The create dialog has a *Transcription* card for this. Agents created before this rule may still have no settings; they are refused at invite (`MEETING_NOT_CONFIGURED`) until configured. The type is excluded from everything that runs agents: `createConversation` and the chat-session starts answer 409 `AGENT_NOT_RUNNABLE`, auto-assignment skips it, and the web agent pickers filter it out.
- **Invite.** `POST/DELETE /projects/{p}/channels/{c}/call/assistants/{agentId}` and `GET …/call/assistants`; `GET /projects/{p}/meeting-agents` feeds the picker. Inviting needs the channel, `calls:join`, and `calls:invite_agent` **on that agent** (`project/<p>/agent/<id>`); the inviter must be in the call; taking one out needs `calls:moderate`. `call_agent_sessions` records each stay. The API dispatches by name (`paca-meeting`) through LiveKit's `AgentDispatchService` with metadata `{v, session}` (ids only). Assistants are on only when calls are on **and** `MEETING_AGENT_TOKEN` is set. Not done: assistants in direct-message huddles, a channel default assistant, the instance on/off setting (the token is the switch), `parallelism_limit` as a limit on calls joined at once.
- **Worker** (`services/meeting-agent`, Node 22, TypeScript). Uses `@livekit/agents` for registration, dispatch and the job lifecycle, and `@livekit/rtc-node` for audio. It does **not** use the framework's STT/LLM abstractions: a small energy-based segmenter cuts each participant's track into utterances (the speaker is the track's owner, no diarization), and one HTTP request per utterance goes to the provider (`openai`, which also covers any OpenAI-compatible endpoint through `stt_base_url`, or `deepgram`). The model is called over OpenAI-compatible chat completions or Anthropic Messages. This keeps it testable and free of model downloads, and gives up streaming partial results: captions appear when a turn ends. The job calls `POST /internal/meeting-agent/sessions/{id}/bootstrap` with the worker secret, receives the agent's decrypted keys (held in memory only) and a **short-lived API key of the agent's own account** (`meeting-agent:<session>`, 6 hours, revoked when the session ends), flushes finished turns every 10 s, and uses that key for `POST …/proposals` and for posting the notes message, so what it writes is judged by the agent's roles. The worker never holds `ENCRYPTION_KEY`.
- **Captions and consent.** Captions travel on the data channel (topic `paca.caption`, from the agent only; the browser ignores any other sender). The call view shows "Atlas is listening and transcribing" with what is kept (or "Nothing is stored"), and the join announcement. `call_transcript_segments` rows carry an expiry from the agent's retention and are swept every 5 minutes; with `store_transcript` off nothing is written.
- **Proposals.** `agent_proposals` and typed payloads (`task.create`, `task.update`, `task.comment`, `doc.create_from_notes`; strict validation, unknown fields rejected). An assistant files one with `POST /projects/{p}/proposals` (`proposals:write`); the API checks that it is in that call, that the kind is enabled and the mode is not notes-only, and the per-call limit, then picks the reviewer (the person it names if they are in the call, else the one who added it, else the host, per `default_target`). Channel readers get **chips** (`GET …/channels/{c}/proposals`: kind, status, who is reviewing, never the fields); the fields come only from `GET/POST /projects/{p}/proposals/{id}[/claim|/resolve]`, gated on `proposals:resolve` with the condition `proposal.target_id = ${principal.id}`, a new IAM attribute with a loader. The shipped Editor and Analyst roles carry that statement and `calls:invite_agent`; migration `000099` adds both to existing ones. Claim is a compare-and-set, the same person may claim again after a reload, only the claimer resolves, and open proposals expire after 30 minutes. The browser performs the write with the person's own session (`lib/proposal-actions.ts`) and reports the outcome; a failed write is recorded as `failed`.
- **Web.** Create dialog and agent page: a *Meeting assistant* type and an *Assistant* tab (listening, behavior, privacy). Call view: an Assistant section (consent indicator, *Add assistant* picker with a privacy summary, *Suggest tasks*, *Remove*), live captions, and proposal cards. LiveKit RPC: `paca.proposal.show` (agent to browser, only an id, accepted only from a participant LiveKit marked as an agent), `paca.proposal.resolved` and `paca.assistant.suggest` (browser to agent).
- **Infra.** `deploy/docker-compose.{dev,prod}.yml` gain a `meeting-agent` service under the `meeting` profile and `MEETING_AGENT_TOKEN` on the API (the e2e file only sets the token); the Helm chart has `meetingAgent.enabled` (needs `livekit.enabled`) and `secrets.meetingAgentToken`. `.github/workflows/meeting-agent-pr-ci.yml` lints and tests the worker.

**Deviations from the design above.** The worker offers only `task.create` and `doc.create_from_notes` to the model: `task.update` and `task.comment` need ids, which means read tools (the MCP server with the agent's key), and those are not wired in. The API, web cards and validation support all four. The "suggested by" origin on the activity feed is not set (it needs the origin plumbed through the task endpoints); the proposal id is recorded in the proposal's result instead. Proposals are not posted into the channel as `proposal` messages, so an unreviewed one survives the call only in the Assistant section and `GET …/proposals`. The notes summary is posted as an ordinary channel message by the agent, which needs `messages:write` on the channel: the *Meeting assistant* role grants it on public and voice channels, so calls in private channels get no posted notes. The *Try it* test room and *Test connection* buttons are not built. The doc's "meeting agents are attached to the project through a role" step is the *Meeting assistant* role template (read tasks, sprints and docs, `proposals:write`, channel read and `messages:write`).

**Not done.** Running the Playwright spec in CI (it needs the worker beside the stack and fake model endpoints reachable from it; it skips itself when assistants are off), the CD workflow that publishes the worker image, read tools for `task.update`/`task.comment`, an end-of-call "3 suggestions pending" line, the worker's health being reported in the UI (a failed dispatch is only visible as the assistant never going active), a `proposal` message type, and the items under Phase 3's "Not done".

## Risks and open questions

- **Highest risk:** the private-channel IAM change and the realtime fan-out model. Both are covered by Phase 0.
- **LiveKit on a lightweight self-host:** UDP/TURN setup is the hardest part for operators, which is why it is opt-in.
- **Composer format:** reusing BlockNote makes mentions and references behave exactly like comments, but it may feel heavy as a chat input. Spike (b) decides.

**Assumed defaults (to confirm):**

1. Workspace-level channels and project channels both ship in v1.0.0.
2. Meetings ship in v1.0.0 as opt-in, including the silent meeting assistant with transcripts and forwarded tool calls; voice-interactive agents and scheduled meetings move to v1.1 (recording moved into Phase 3b).
3. The Go server SDK and `livekit/protocol` handle token minting and webhook verification.

## References

- [LiveKit: Self-hosting deployment](https://docs.livekit.io/transport/self-hosting/deployment/)
- [LiveKit: Self-hosting on Kubernetes](https://docs.livekit.io/transport/self-hosting/kubernetes.md)
- [LiveKit: Tokens and grants](https://docs.livekit.io/frontends/authentication/tokens.md)
- [livekit/server-sdk-go](https://github.com/livekit/server-sdk-go)
- Related: [Authorization](authorization.md), [Service boundaries](service-boundaries.md), [Realtime service](../../services/realtime/README.md)
