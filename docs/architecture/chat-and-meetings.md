# Chat and Meetings

Design and delivery plan for built-in team chat (a Slack alternative) and voice/video meetings (a Discord alternative) in Paca v1.0.0.

> **Status:** proposal; Phase 1b (presence and task-comment UX) is built, see [Phase 1b status](#phase-1b-status). Phase 1 is partly built (project channels, direct messages, replies from global agents in direct messages, messages, threads, reactions, mentions, followed threads, saved messages, read state, the sidebar and Inbox, IAM variable and realtime channel rooms, uncommitted). Everything else here is not implemented. The Phase 0 spikes ran on 2026-11-17 and all five came back go; see [Phase 0 results](#phase-0-results). The roadmap entry is in [ROADMAP.md](../../ROADMAP.md) under *Beyond v1.0 — Exploring*.

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
- UI: "Mina is typing…" (or "Mina and 2 others…") under the comment composer, plus viewer avatars in the task header ("2 viewing"). An agent working on the task shows "Atlas is working…" from its conversation status instead of typing events.
- A per-user setting can turn typing broadcast off. Nothing is sent for a cleared composer or a read-only viewer.
- The same primitive later serves doc comments and sprint pages.

### Mention notifications that say where

Today a mention notification reads "Mina mentioned you in APO-142 Fix token refresh": no project, no sprint, no excerpt. Proposed card, in the bell and in a full `/notifications` page:

- **Header:** actor avatar, "Mina mentioned you", time.
- **Context row:** project chip, sprint chip (`Sprint 12`, or `Backlog` when the task has none; resolved when the notification is read, so it stays right if the task moves), task key and title, and the task's current status.
- **Excerpt:** the first ~140 characters of the comment with your mention highlighted. Stored with the notification, so it survives edits and deletion.
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
livekit ──webhooks──▶ api (/internal/livekit/webhook) ──▶ realtime (who is in which room)
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
- Opt-in "meet" profile in `docker-compose.{dev,prod,e2e}.yml`, the install script, and the Helm chart (LiveKit's own chart as a dependency, or a small template).

### Later (v1.1 candidates)

- Recording via LiveKit Egress (a separate Chrome-based container), written to RustFS/S3 and attached to the channel or a doc.
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

1. **Agents framework worker (recommended):** a new optional container, `services/meeting-agent`, in the "meet" profile. It registers with `LIVEKIT_URL`, key and secret and receives dispatches. **The LiveKit agent runs the reasoning** (its STT stream, its LLM loop, its function tools, including the forwarded ones). It is configured by a Paca agent of the new `meeting` type (see [Meeting agent type](#meeting-agent-type)): at dispatch it fetches that agent's decrypted settings from the API over an internal route, and afterwards it calls the Paca API with the agent's own API key, so everything it writes is judged by the agent's roles. `services/agent-runner` is not involved.
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
| **3b. Meeting assistant (v1.0.0, needs Phase 3)** | New `meeting` agent type (domain, validation, `agent_meeting_settings`, STT catalog, create/edit UI, role template, picker exclusions); `services/meeting-agent` worker and dispatch; invite from the lobby and participants panel (`calls:invite_agent`); per-participant STT, live captions, transcript persistence, consent indicator; `agent_proposals` with events and guards; RPC forwarding (`paca.proposal.show` / `paca.proposal.resolved`); Assistant panel and cards; notes summary posted to the channel. | In a real call, the assistant transcribes and proposes a task; the requester approves on their own screen; the task exists with the user as actor and "suggested by" origin. |
| **4. v1.1 extras** | Recording, voice-interactive agent (TTS, turn detection), realtime speech models, scheduled meetings, server-side approval. | |

Phases 1 and 3 can run mostly in parallel once the `channels` table and IAM resources land: the LiveKit infra and token service touch different files from the chat UI. Each phase also updates the docs (`docs/architecture`, API docs, roadmap) and adds e2e specs. The e2e suite includes mobile projects, and calls need a mocked media device in Playwright.

## Phase 1b status

Built (uncommitted at the time of writing; unit-tested, not yet run through the Playwright suite):

- **Presence layer** (names in the viewer list and typing indicator are resolved from the project's member list, so they need `project.members:read`; without it the browser has ids it cannot name) in `services/realtime/src/presence.ts` (live rooms, access checked at join, typing leases, cross-replica merge over Valkey); protocol and limits are in [services/realtime/README.md](../../services/realtime/README.md#presence-viewers-and-typing).
- **Task UI:** "X is typing…" under the comment composer, viewer avatars in the task header, and a per-browser switch to stop sending typing (Account → Profile).
- **Mention notifications that say where:** migration `000095` adds `comment_id`, `mention_source` and a stored `excerpt` to `notifications`. The list response gains `comment_id`, `mention_source`, `excerpt`, `task_status_name/color`, `sprint_id` and `sprint_name`; status and sprint are joined at read time so they stay right after the task moves. Cards show project, sprint (or *Backlog*), task, status and the excerpt with the mention highlighted; runs of mentions on one task collapse to "3 mentions in #142". Clicking opens the task with `?comment=<id>`, scrolled to and briefly highlighting that comment (only when it is on the newest loaded page of the activity feed).
- **`/notifications` page** with All / Mentions / Assigned / Agents tabs and a project filter (`GET /users/me/notifications?type=&project_id=&actor=agent`), and a toast when a mention arrives while you are online.
- **Unread cues:** `GET /users/me/notifications/unread-mentions` feeds a badge on task rows and a count on sprint headers. `task_reads` plus `PUT /projects/{id}/tasks/{taskId}/read` (gated on `tasks:read`) record the visit, read the caller's mentions on the task, and return the previous visit time for the *New since your last visit* divider in the activity pane.
- **Description and document mentions** now create in-app rows (`mention_source` `description` / `document`) as well as the plugin event.

Not done: the Watch toggle on tasks; the sprint-count shortcut in the Interactions sidebar (only sprint headers in the list view show it); badges on board cards; Playwright specs for typing, the `/notifications` page and the unread divider; copy for the mention excerpt beyond the first 140 characters of text. Typing indicators do not cover doc comments yet.

## Risks and open questions

- **Highest risk:** the private-channel IAM change and the realtime fan-out model. Both are covered by Phase 0.
- **LiveKit on a lightweight self-host:** UDP/TURN setup is the hardest part for operators, which is why it is opt-in.
- **Composer format:** reusing BlockNote makes mentions and references behave exactly like comments, but it may feel heavy as a chat input. Spike (b) decides.

**Assumed defaults (to confirm):**

1. Workspace-level channels and project channels both ship in v1.0.0.
2. Meetings ship in v1.0.0 as opt-in, including the silent meeting assistant with transcripts and forwarded tool calls; recording, voice-interactive agents and scheduled meetings move to v1.1.
3. The Go server SDK and `livekit/protocol` handle token minting and webhook verification.

## References

- [LiveKit: Self-hosting deployment](https://docs.livekit.io/transport/self-hosting/deployment/)
- [LiveKit: Self-hosting on Kubernetes](https://docs.livekit.io/transport/self-hosting/kubernetes.md)
- [LiveKit: Tokens and grants](https://docs.livekit.io/frontends/authentication/tokens.md)
- [livekit/server-sdk-go](https://github.com/livekit/server-sdk-go)
- Related: [Authorization](authorization.md), [Service boundaries](service-boundaries.md), [Realtime service](../../services/realtime/README.md)
