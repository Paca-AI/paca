// Package handler wires a decoded trigger or control message to the
// executor, Postgres, and the Valkey publishers — the actual behavior
// cmd/agent-runner's main.go just constructs and hands to
// messaging.Consumer. Split out from package main so it's importable by
// tests (including test/e2e's real-infra suite) instead of only reachable
// by running the whole binary.
package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/acp"
	"github.com/Paca-AI/agent-runner/internal/acpbridge"
	"github.com/Paca-AI/agent-runner/internal/agent"
	"github.com/Paca-AI/agent-runner/internal/bundledskills"
	"github.com/Paca-AI/agent-runner/internal/chatsandbox"
	"github.com/Paca-AI/agent-runner/internal/config"
	"github.com/Paca-AI/agent-runner/internal/convlock"
	"github.com/Paca-AI/agent-runner/internal/executor"
	"github.com/Paca-AI/agent-runner/internal/messaging"
	"github.com/Paca-AI/agent-runner/internal/registry"
	"github.com/Paca-AI/agent-runner/internal/repository/postgres"
)

// Handler is messaging.Handler and messaging.ControlHandler both — one
// instance shared across every trigger and control message the process
// handles. Exported (unlike the "Handler" name might suggest is needed for
// a single-binary command) specifically so tests can drive Handle/
// HandleControl directly against a real or fake backing store — see
// test/e2e/trigger_orchestration_test.go for Handle driven against real
// infrastructure.
type Handler struct {
	Gate          config.Gate
	AgentRepo     *postgres.AgentRepository
	ConvRepo      *postgres.ConversationRepository
	BundledSkills *bundledskills.Client
	Publisher     *messaging.Publisher
	Executor      *executor.Executor
	InFlight      *registry.Conversations
	ChatSandboxes *chatsandbox.Registry
	ACPDispatcher *acpbridge.Dispatcher
	ACPRegistry   *acpbridge.Registry
	Log           *slog.Logger

	// resumeLocks serializes, per conversation_id, Handle's "register
	// in-flight then read the paused sandbox" sequence against
	// TeardownPausedChatSandbox's "check in-flight then pop the paused
	// sandbox" sequence — see resumeLock's doc comment for the race this
	// closes. Lazily built (not a constructor-only field) so every
	// existing Handler{...} struct literal — cmd/agent-runner/main.go and
	// four test/e2e files — keeps working unchanged.
	resumeLocksOnce sync.Once
	resumeLocks     *convlock.Locks
}

// resumeLock returns this Handler's per-conversation resume/teardown lock,
// building it on first use.
func (h *Handler) resumeLock() *convlock.Locks {
	h.resumeLocksOnce.Do(func() { h.resumeLocks = convlock.New() })
	return h.resumeLocks
}

// Handle runs one conversation turn for trigger, dispatching to the ACP
// bridge instead when the trigger's agent is an acp-type agent.
func (h *Handler) Handle(ctx context.Context, trigger agent.Trigger) error {
	if !h.Gate.Allowed(trigger.AgentID) {
		// Not ours to run — ack (via returning nil) and drop it. agent-runner
		// is the only consumer of paca:agent:triggers now, so a
		// disallowed agent's trigger is simply never processed by anyone —
		// see config.Gate's doc comment on what AllowedAgentIDs is actually
		// for now that it's no longer coordinating against a second
		// service. Applies uniformly to llm- and acp-type triggers alike.
		return nil
	}

	// Chat conversations are owner-private: their realtime events must route
	// to the owner's user room, not the project room. Resolve the owner user
	// id once here (sessionless triggers have ChatSessionID == nil and keep a
	// nil ActorUserID, staying project-shared) so both the llm path and the
	// acp dispatch path below publish the right room.
	if trigger.ChatSessionID != nil {
		_, ownerUserID, err := h.ConvRepo.GetConversationRealtimeContext(ctx, trigger.ConversationID)
		if err != nil {
			return fmt.Errorf("resolve realtime owner for conversation %s: %w", trigger.ConversationID, err)
		}
		trigger.ActorUserID = ownerUserID
	}

	cfg, err := h.AgentRepo.FindByID(ctx, trigger.AgentID)
	if err != nil {
		if errors.Is(err, postgres.ErrNotLLMAgent) {
			// Not an llm-type agent — dispatch_acp_trigger's job instead of
			// run_conversation's, mirrors worker.py's _process_trigger
			// branching on agent_config.agent_type.
			return h.dispatchACP(ctx, trigger)
		}
		// Anything else (DB down, agent genuinely missing) — leave the
		// message unacknowledged so it's retried once the transient
		// condition (if it is one) clears, matching Handler's contract.
		return fmt.Errorf("resolve agent %s: %w", trigger.AgentID, err)
	}

	// Marked "running" before loading bundled skills below, not after —
	// mirrors run_conversation's own ordering (it wrote RUNNING before
	// loading default skills) so a load failure is caught by this
	// function's own error handling and surfaced as a visible "failed"
	// status, rather than leaving the conversation's status untouched at
	// whatever it was pre-trigger. Without this ordering, a transient
	// services/api outage during BundledSkills.Load left conversations
	// stuck indefinitely with no visible error and no retry — this
	// service's Valkey consumer has no XCLAIM/XAUTOCLAIM reclaim logic, so
	// a message left unacknowledged on error is never actually redelivered.
	if err := h.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "running", nil); err != nil {
		return fmt.Errorf("mark conversation %s running: %w", trigger.ConversationID, err)
	}

	// Bundled skills (e.g. `paca`, `paca-do`) are always-active scaffolding
	// that mirrors services/ai-agent's builder.load_default_skills() —
	// without it, an agent has no instructions telling it a linked repo
	// exists or how to reach it (list_repositories/clone_repository), and
	// silently starts working against an empty sandbox. Prepended ahead of
	// cfg.Skills so per-agent customizations still render after it. Nil
	// only in tooling that doesn't wire a PACA_API_URL (see test/e2e's
	// Handler construction) — real deployments always set it.
	if h.BundledSkills != nil {
		bundled, err := h.BundledSkills.Load(ctx)
		if err != nil {
			errMsg := fmt.Sprintf("failed to load bundled skills: %v", err)
			if statusErr := h.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "failed", &errMsg); statusErr != nil {
				h.Log.Warn("agent-runner: failed to record failure status after a bundled-skills load error",
					"conversation_id", trigger.ConversationID, "error", statusErr)
			}
			h.publishTerminalStatus(ctx, trigger.ProjectID, trigger.ConversationID, trigger.ActorUserID, "failed", "agent.conversation.failed")
			h.Log.Error("agent-runner: failed to load bundled skills",
				"conversation_id", trigger.ConversationID, "agent_id", trigger.AgentID, "error", err)
			// The failure is already recorded and visible, so ack (return
			// nil) rather than leave it stuck redelivering forever — same
			// contract every other terminal failure in this function uses.
			return nil
		}
		cfg.Skills = append(bundled, cfg.Skills...)
	}

	h.Log.Info("agent-runner: running conversation",
		"conversation_id", trigger.ConversationID, "agent_id", trigger.AgentID)

	// isChat gates every piece of continuity behavior below — mirrors
	// executor.py's is_chat, computed once at the top of run_conversation
	// for the same reason. Only a chat_message conversation ever pauses
	// between turns; every other trigger type is torn down after its one
	// turn exactly as before this feature existed.
	isChat := trigger.TriggerType == agent.TriggerChatMessage

	// Derived, cancellable independently of ctx (the consumer's own
	// lifetime context) so HandleControl can interrupt just this one
	// conversation's turn via InFlight.Interrupt — see registry.Conversations.
	//
	// Registered *before* the ChatSandboxes.Get below, not after: this is
	// what makes this conversation visible as in-flight (InFlight.IsRegistered)
	// to a concurrent TeardownPausedChatSandbox call (the idle reaper or a
	// stop control message) before this turn reads — and starts relying on
	// — a reference to the paused sandbox. Registering afterward left a
	// window where a concurrent teardown could see IsRegistered==false and
	// stop/pop the very sandbox this turn had already read a handle to.
	runCtx, cancelRun := context.WithCancel(ctx)

	// Register-then-Get itself still isn't safe on its own: IsRegistered and
	// Pop in TeardownPausedChatSandbox are two separate operations too (see
	// that function's own comment), so a concurrent stop control message or
	// idle-reaper tick could observe IsRegistered==false *before* the
	// Register call below runs, decide to proceed, and then Pop (and tear
	// down) the very sandbox this turn reads a handle to via Get —
	// regardless of Register having already happened by the time Get runs.
	// resumeLock closes that gap by making "Register+Get" and
	// TeardownPausedChatSandbox's "IsRegistered-check+Pop" mutually
	// exclusive for this conversation_id: whichever side gets there first
	// completes its whole sequence before the other can begin, so they can
	// no longer interleave into a torn-down-mid-use sandbox. Held only
	// across these two fast, in-memory-map calls — not the rest of this
	// turn — so it doesn't stand between a stop message and an
	// already-in-flight turn the way the per-conversation trigger lock in
	// messaging.Consumer deliberately doesn't either.
	var resume *chatsandbox.State
	regToken := func() uint64 {
		unlock := h.resumeLock().Lock(trigger.ConversationID)
		defer unlock()

		tok := h.InFlight.Register(trigger.ConversationID, cancelRun)
		if isChat {
			// A plain Get, not Pop — the entry (if any) stays live in the
			// registry for this turn's *entire* duration, not just until
			// the run starts, so a heartbeat control message arriving
			// mid-turn can still find and refresh it (see HandleControl's
			// ControlHeartbeat case). Mirrors run_conversation's
			// resume_state = chat_sandboxes.get(...).
			resume, _ = h.ChatSandboxes.Get(trigger.ConversationID)
		}
		return tok
	}()

	defer func() {
		h.InFlight.Unregister(trigger.ConversationID, regToken)
		cancelRun()
	}()

	// Seeded from existing history, not always 0 — event_index is unique for
	// a conversation's entire lifetime, not just this turn. Only matters
	// once a conversation can span multiple turns (chat continuity), but
	// getting it right now avoids a silent duplicate-index bug the day that
	// lands. Mirrors executor.py's _AtomicCounter, seeded from
	// get_next_event_index.
	eventIndex, err := h.ConvRepo.NextEventIndex(ctx, trigger.ConversationID)
	if err != nil {
		return fmt.Errorf("get next event index for conversation %s: %w", trigger.ConversationID, err)
	}

	// persistAndPublish writes one durable conversation_events row and
	// broadcasts it in full over realtime — the common path for the user's
	// own message, each paragraph of the agent's reply, tool calls, and
	// tool call updates. id/createdAt are generated once here (not left to
	// InsertEvent's own defaults) specifically so the exact same identity
	// goes out over realtime too — see InsertEvent's doc comment on why
	// that matters: without it, the frontend has no way to construct the
	// same AgentConversationEvent client-side that a later GET
	// .../events call would return, and has to re-fetch instead of
	// appending the live event directly.
	persistAndPublish := func(eventType, eventSource string, payload []byte) {
		id := uuid.New()
		createdAt := time.Now().UTC()
		// The durable record read back by services/api's
		// ListConversationEvents (agent_repository.go) — the frontend's
		// conversation history on page load/reload. Mirrors executor.py's
		// persist_conversation_event, which writes here directly rather
		// than via any stream (nothing consumes paca:agent:events for this
		// purpose — see PublishEvent's own doc comment). Without this call,
		// a conversation run by this service has no history to show once
		// nobody is watching it live.
		if err := h.ConvRepo.InsertEvent(
			ctx, id, trigger.ConversationID, eventType, eventSource, eventIndex, payload, createdAt,
		); err != nil {
			h.Log.Warn("agent-runner: failed to persist event",
				"conversation_id", trigger.ConversationID, "error", err)
		}
		if pubErr := h.Publisher.PublishEvent(
			ctx, trigger.ConversationID, trigger.ProjectID,
			eventType, eventSource, eventIndex, payload, "running",
		); pubErr != nil {
			h.Log.Warn("agent-runner: failed to publish event",
				"conversation_id", trigger.ConversationID, "error", pubErr)
		}
		// The actual live-UI-update path — see PublishEvent's doc comment
		// on why that call alone doesn't reach the frontend. Carries the
		// full row (id/event_type/event_source/payload/created_at), not
		// just event_index, so the frontend can append it directly to its
		// local event list instead of treating this as a mere "something
		// changed, go re-fetch" signal — that used to mean one HTTP GET per
		// realtime message, which added up fast across a whole streamed
		// reply. See apps/web/src/hooks/use-project-realtime.ts.
		if pubErr := h.Publisher.PublishRealtime(
			ctx, trigger.ProjectID, trigger.ConversationID,
			"agent."+strings.ToLower(eventType),
			map[string]any{
				"id":           id.String(),
				"event_index":  eventIndex,
				"event_type":   eventType,
				"event_source": eventSource,
				"payload":      json.RawMessage(payload),
				"created_at":   createdAt.Format(time.RFC3339Nano),
			},
			trigger.ActorUserID,
		); pubErr != nil {
			h.Log.Warn("agent-runner: failed to publish realtime event",
				"conversation_id", trigger.ConversationID, "error", pubErr)
		}
		eventIndex++
	}

	// Records the user's own message as a conversation event before the
	// agent's reply starts. services/ai-agent's OpenHands SDK did this
	// automatically — every conversation.send_message call generated its
	// own MessageEvent; ACP has no equivalent (session/prompt is one-way,
	// so Goose never echoes back what it was asked), so this service has
	// to record it explicitly or a chat conversation's history never shows
	// what the user actually said. Every trigger type with real message
	// text gets one, not just chat_message — the empty-message default a
	// bare task-assignment trigger falls back to (see
	// prompt.buildInitialMessage's taskAssignedDefault) is deliberately
	// NOT recorded here, since no human actually said anything in that
	// case.
	if msg := strings.TrimSpace(trigger.Message); msg != "" {
		userPayload, _ := json.Marshal(map[string]any{
			"content": map[string]any{"type": "text", "text": msg},
		})
		persistAndPublish("user_message", "user", userPayload)
	}

	// Buffers an in-progress reply's text across the many small ACP
	// notifications Goose streams it as, and only persists/broadcasts it a
	// paragraph at a time — see flushChunkBuf and onEvent below for where
	// a paragraph boundary is decided. Persisting (and, worse, broadcasting
	// over realtime) every individual chunk produced dozens of rows and
	// realtime messages for what a user experiences as one continuous
	// reply; this collapses them back down without losing anything, since
	// eventsToThreadMessages already concatenates consecutive
	// agent_message_chunk parts into one block regardless of how many rows
	// they came from.
	var chunkBuf strings.Builder
	flushChunkBuf := func() {
		if chunkBuf.Len() == 0 {
			return
		}
		text := chunkBuf.String()
		chunkBuf.Reset()
		payload, _ := json.Marshal(map[string]any{
			"content": map[string]any{"type": "text", "text": text},
		})
		persistAndPublish("agent_message_chunk", "agent", payload)
	}
	// Set on every event the turn produces (a reply chunk or a tool call) —
	// used below to detect a turn that ends with no runErr and stopReason
	// "end_turn" but zero visible content. That combination is reachable:
	// goose can exhaust its own internal retries against a failing LLM
	// provider (observed live: an OpenRouter account out of credits) and
	// still answer session/prompt as an ordinary successful, empty turn
	// with no ACP-level error to catch — see acp.ClassifyProviderError's
	// doc comment for the error-carrying case this doesn't cover. Without
	// this check, such a turn leaves the conversation looking like the
	// agent simply never replied, with no error surfaced anywhere in the UI.
	producedOutput := false
	onEvent := func(e acp.Event) {
		producedOutput = true
		if e.Kind == acp.UpdateAgentMessageChunk {
			var chunk acp.AgentMessageChunk
			if err := json.Unmarshal(e.Raw, &chunk); err != nil {
				h.Log.Warn("agent-runner: failed to decode agent_message_chunk",
					"conversation_id", trigger.ConversationID, "error", err)
				return
			}
			chunkBuf.WriteString(chunk.Content.Text)
			// "\n\n" is the paragraph boundary — the same one Markdown
			// itself uses to separate blocks, so this lines up with how
			// the reply actually reads once rendered, not an arbitrary
			// chunk-count or byte-length cutoff.
			if strings.Contains(chunk.Content.Text, "\n\n") {
				flushChunkBuf()
			}
			return
		}
		// A tool call (or any other event type) interrupts whatever
		// paragraph of reply text was accumulating — flush it first so
		// event_index ordering in the persisted history matches the order
		// things actually happened in.
		flushChunkBuf()
		persistAndPublish(string(e.Kind), "agent", e.Raw)
	}

	result, runErr := h.Executor.Run(runCtx, *cfg, trigger, resume, onEvent)
	// Whatever's still buffered when the turn ends — successfully,
	// interrupted, or failed — is a genuine partial reply, not scratch
	// state to discard; flush it unconditionally before any of the
	// branches below run, so it's never silently lost.
	flushChunkBuf()
	if runErr != nil {
		// A canceled runCtx means HandleControl's Interrupt actually fired
		// for this conversation (agent.stop/agent.pause — see registry.
		// Conversations) or the process itself is shutting down (ctx, which
		// runCtx derives from) — either way that's an intentional
		// interruption, not a failure.
		//
		// A DeadlineExceeded runCtx is executor.go's own turn timeout (see
		// defaultTimeoutMinutes) — deliberately marked "failed" here rather
		// than mirroring executor.py's _post_turn_status, which maps an
		// analogous polling timeout to a *successful* "finished" status.
		// That read as more confusing than useful to carry forward: a
		// timed-out turn that never actually completed looks identical to
		// one that did, from the conversation's own status field.
		if errors.Is(runErr, context.Canceled) {
			// Which control message actually fired — see registry.
			// Conversations.TakeReason. Must be read before InFlight.
			// Unregister (deferred above) clears it; "not found" (e.g. ctx
			// itself was canceled by process shutdown, not a control
			// message) fails safe to a full stop rather than assuming a
			// pause and leaving a container no one will ever reattach to.
			reason, ok := h.InFlight.TakeReason(trigger.ConversationID)
			if !ok {
				reason = registry.ReasonStop
			}

			if isChat && reason == registry.ReasonPause {
				// Interrupt-only pause — mirrors _keep_sandbox_alive's
				// is_chat && !errored && !shutdown case (a pause is never
				// "errored" or "shutdown"): keep the sandbox alive for the
				// conversation's next reply instead of tearing it down.
				h.keepSandboxAlive(trigger, result)
				if err := h.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "paused", nil); err != nil {
					h.Log.Warn("agent-runner: failed to record paused status",
						"conversation_id", trigger.ConversationID, "error", err)
				}
				h.publishNonTerminalStatus(ctx, trigger, "agent.conversation.paused")
				h.Log.Info("agent-runner: conversation paused (chat sandbox kept alive)",
					"conversation_id", trigger.ConversationID)
				return nil
			}

			h.tearDownSandbox(ctx, trigger, result)
			if err := h.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "stopped", nil); err != nil {
				h.Log.Warn("agent-runner: failed to record stopped status",
					"conversation_id", trigger.ConversationID, "error", err)
			}
			h.publishTerminalStatus(ctx, trigger.ProjectID, trigger.ConversationID, trigger.ActorUserID, "stopped", "agent.conversation.stopped")
			h.Log.Info("agent-runner: conversation stopped",
				"conversation_id", trigger.ConversationID)
			return nil
		}

		h.tearDownSandbox(ctx, trigger, result)
		errMsg := runErr.Error()
		// A rate-limit or out-of-credits response from the LLM provider
		// reads, in its raw wrapped form, as an unhelpful blob of relayed
		// HTTP status/JSON (see acp.ClassifyProviderError's doc comment) —
		// swap it for a plain-language message so the user sees what
		// actually happened instead of a generic failure. The raw error is
		// still captured below via h.Log.Error for debugging.
		kind, classified := acp.ClassifyProviderError(runErr)
		if classified {
			errMsg = kind.FriendlyMessage()
		}

		// A classified provider error is something the user can fix outside
		// this conversation (top up billing, wait out a rate limit) and
		// then just retry — unlike an unrecognized failure, it shouldn't
		// dead-end the conversation the way "failed" does (canReply goes
		// false once a conversation is terminal). Left "paused" instead, the
		// same non-terminal status a normal successful turn ends on, so the
		// composer stays enabled; the next message cold-starts a fresh
		// sandbox exactly like a brand new conversation would, since this
		// turn's (broken) sandbox was already torn down above and never
		// registered in ChatSandboxes. Scoped to isChat: "paused" and
		// canReply are chat-specific concepts (see the natural-finish
		// branch below) — a task-triggered conversation has no retry path
		// through the UI regardless of status.
		if classified && isChat {
			if err := h.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "paused", &errMsg); err != nil {
				h.Log.Warn("agent-runner: failed to record paused-after-provider-error status",
					"conversation_id", trigger.ConversationID, "error", err)
			}
			h.publishNonTerminalStatus(ctx, trigger, "agent.conversation.paused")
			h.Log.Warn("agent-runner: conversation turn hit a recoverable provider error, left paused for retry",
				"conversation_id", trigger.ConversationID, "kind", kind, "error", runErr)
			return nil
		}

		if err := h.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "failed", &errMsg); err != nil {
			h.Log.Warn("agent-runner: failed to record failure status",
				"conversation_id", trigger.ConversationID, "error", err)
		}
		h.publishTerminalStatus(ctx, trigger.ProjectID, trigger.ConversationID, trigger.ActorUserID, "failed", "agent.conversation.failed")
		// The run itself failing isn't a transient infra condition to
		// retry — the failure is already recorded, so ack (return nil)
		// rather than leave it stuck redelivering forever.
		h.Log.Error("agent-runner: conversation failed",
			"conversation_id", trigger.ConversationID, "error", runErr)
		return nil
	}

	// Carried into the UpdateStatus calls below (paused/finished) instead of
	// being persisted as a conversation_events row: it used to render as an
	// ordinary chat bubble, which looked inconsistent with a classified
	// provider error (see acp.ClassifyProviderError) — the exact same "out
	// of tokens/credits" cause renders as a plain message here but as a
	// distinct ConversationErrorBox there, purely because goose swallowed
	// this one internally instead of surfacing it as an ACP error. Routing
	// both through conversation.error_message unifies the presentation.
	var noOutputMsg *string
	if !producedOutput {
		msg := "I wasn't able to generate a reply for this message. This can happen if the LLM provider was rate-limited, out of credits, or had a temporary outage. Please try again in a moment."
		noOutputMsg = &msg
	}

	stopReasonJSON, _ := json.Marshal(map[string]string{"stopReason": result.StopReason})
	if err := h.ConvRepo.InsertEvent(
		ctx, uuid.New(), trigger.ConversationID, "turn_end", "system", eventIndex, stopReasonJSON, time.Now().UTC(),
	); err != nil {
		h.Log.Warn("agent-runner: failed to persist turn_end event",
			"conversation_id", trigger.ConversationID, "error", err)
	}
	if pubErr := h.Publisher.PublishEvent(
		ctx, trigger.ConversationID, trigger.ProjectID,
		"turn_end", "system", eventIndex, stopReasonJSON, "finished",
	); pubErr != nil {
		h.Log.Warn("agent-runner: failed to publish turn_end event",
			"conversation_id", trigger.ConversationID, "error", pubErr)
	}

	if isChat {
		// A natural finish for a chat conversation pauses rather than
		// ends — mirrors _keep_sandbox_alive: is_chat && !errored &&
		// !shutdown is also true for an ordinary successful turn, not just
		// an interrupt-only pause.
		h.keepSandboxAlive(trigger, result)
		if err := h.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "paused", noOutputMsg); err != nil {
			return fmt.Errorf("mark conversation %s paused: %w", trigger.ConversationID, err)
		}
		h.publishNonTerminalStatus(ctx, trigger, "agent.conversation.paused")
		h.Log.Info("agent-runner: conversation paused (chat sandbox kept alive)",
			"conversation_id", trigger.ConversationID, "stop_reason", result.StopReason)
		return nil
	}

	h.tearDownSandbox(ctx, trigger, result)
	if err := h.ConvRepo.UpdateStatus(ctx, trigger.ConversationID, "finished", noOutputMsg); err != nil {
		return fmt.Errorf("mark conversation %s finished: %w", trigger.ConversationID, err)
	}
	h.publishTerminalStatus(ctx, trigger.ProjectID, trigger.ConversationID, trigger.ActorUserID, "finished", "agent.conversation.finished")

	h.Log.Info("agent-runner: conversation finished",
		"conversation_id", trigger.ConversationID, "stop_reason", result.StopReason)
	return nil
}

// dispatchACP hands trigger off to its acp-type agent's connected local
// bridge — mirrors worker.py's _process_trigger branching to
// dispatch_acp_trigger. Returns nil (not found) the same way the caller's
// prior ErrNotLLMAgent no-op did if trigger.AgentID turns out not to name
// an acp-type agent either (a routing bug upstream, not something to
// surface as a retryable failure).
func (h *Handler) dispatchACP(ctx context.Context, trigger agent.Trigger) error {
	cfg, err := h.AgentRepo.FindACPByID(ctx, trigger.AgentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("resolve acp agent %s: %w", trigger.AgentID, err)
	}
	return h.ACPDispatcher.DispatchTrigger(ctx, trigger, cfg)
}

// dispatchACPControl forwards a stop_turn/pause_turn control message to
// conversationID's owning agent's bridge, if that agent turns out to be
// acp-type — mirrors worker.py's _handle_control checking
// get_conversation_agent_type before falling through to
// acp_bridge.dispatch. Returns false (nothing dispatched) for an llm-type
// or unresolvable conversation, letting HandleControl's caller continue to
// its own next fallback.
func (h *Handler) dispatchACPControl(ctx context.Context, conversationID uuid.UUID, messageType string) bool {
	agentID, agentType, err := h.ConvRepo.GetConversationAgentType(ctx, conversationID)
	if err != nil {
		return false
	}
	if agentType != "acp" {
		return false
	}
	dispatched, err := h.ACPRegistry.Dispatch(ctx, agentID, map[string]any{
		"type":            messageType,
		"conversation_id": conversationID.String(),
	})
	if err != nil {
		h.Log.Warn("agent-runner: failed to dispatch control message to acp bridge",
			"conversation_id", conversationID, "agent_id", agentID, "type", messageType, "error", err)
		return false
	}
	return dispatched
}

// keepSandboxAlive registers result's sandbox in ChatSandboxes so the next
// reply in this chat conversation can reattach to it instead of cold-
// starting — call only for a chat trigger reaching a natural finish or an
// interrupt-only pause (never on error or a full stop).
func (h *Handler) keepSandboxAlive(trigger agent.Trigger, result executor.Result) {
	h.ChatSandboxes.Set(trigger.ConversationID, &chatsandbox.State{
		Handle:       result.Handle,
		Client:       result.Client,
		SessionID:    result.SessionID,
		ProjectID:    trigger.ProjectID,
		ActorUserID:  trigger.ActorUserID,
		LastActiveAt: time.Now(),
	})
}

// tearDownSandbox stops the sandbox in result (if any was actually reached)
// and removes any ChatSandboxes entry for this conversation — Pop is a
// harmless no-op for a non-chat trigger, or a chat trigger that was never
// paused, so this is safe to call unconditionally on every non-keep-alive
// path (error, full stop, or a non-chat trigger's ordinary finish).
func (h *Handler) tearDownSandbox(ctx context.Context, trigger agent.Trigger, result executor.Result) {
	h.ChatSandboxes.Pop(trigger.ConversationID)
	result.Client.Close()
	if result.Handle == nil {
		return
	}
	if err := h.Executor.StopSandbox(context.WithoutCancel(ctx), result.Handle); err != nil {
		h.Log.Warn("agent-runner: failed to stop sandbox",
			"conversation_id", trigger.ConversationID, "error", err)
	}
}

// HandleControl processes a stop/pause/heartbeat directive for a
// conversation. For stop/pause, if a turn is actually in flight on this
// replica, the real work (the "stopped"/"paused" status write and its
// publishes) happens in Handle itself once it observes runCtx cancel —
// HandleControl's job there is only to make that cancellation happen, with
// the right InterruptReason so Handle knows which one fired.
//
// Stop has a second case Handle can never see: a chat conversation *paused
// between turns*, with no turn in flight at all — mirrors worker.py's
// _handle_control falling through to teardown_paused_chat_sandbox when
// stop_events has no entry for this conversation.
func (h *Handler) HandleControl(ctx context.Context, c messaging.Control) error {
	switch c.Type {
	case messaging.ControlStop:
		if h.InFlight.InterruptWithReason(c.ConversationID, registry.ReasonStop) {
			return nil
		}
		if h.dispatchACPControl(ctx, c.ConversationID, "stop_turn") {
			return nil
		}
		if h.TeardownPausedChatSandbox(ctx, c.ConversationID) {
			h.Log.Info("agent-runner: stopped a paused chat sandbox via control message",
				"conversation_id", c.ConversationID)
			return nil
		}
		h.Log.Info("agent-runner: control message for a conversation not running on this replica",
			"conversation_id", c.ConversationID, "type", c.Type)

	case messaging.ControlPause:
		if h.InFlight.InterruptWithReason(c.ConversationID, registry.ReasonPause) {
			return nil
		}
		if h.dispatchACPControl(ctx, c.ConversationID, "pause_turn") {
			return nil
		}
		// No-op if nothing is running — this is the assistant-UI stop
		// button; a chat conversation already paused between turns has
		// nothing further to interrupt (mirrors worker.py's pause
		// branch, which has no teardown_paused_chat_sandbox-equivalent
		// fallback).
		h.Log.Info("agent-runner: control message for a conversation not running on this replica",
			"conversation_id", c.ConversationID, "type", c.Type)

	case messaging.ControlHeartbeat:
		// Refreshes a paused chat sandbox's idle timer — see
		// chatsandbox.Registry.Touch and the idle reaper in
		// cmd/agent-runner/main.go. A no-op (nothing to refresh) for every
		// other case: no chat sandbox for this conversation, or a mismatched
		// project_id (Touch's own cross-check).
		h.ChatSandboxes.Touch(c.ConversationID, c.ProjectID)
	}
	return nil
}

// TeardownPausedChatSandbox tears down conversationID's paused chat sandbox
// if one is registered — mirrors executor.py's teardown_paused_chat_sandbox.
// Returns false (nothing to do) if none is found, which HandleControl's
// stop case treats as "not running on this replica" the same as before this
// existed. Exported so cmd/agent-runner/main.go's idle reaper can call the
// same teardown HandleControl's stop path uses, instead of a second
// hand-copied version drifting out of sync with it.
//
// Safe against the race teardown_paused_chat_sandbox's own docstring calls
// out (a turn for this conversation starting concurrently on this replica):
// Handle registers with InFlight *before* it reads ChatSandboxes.Get (see
// Handle's own comment on that ordering), so the InFlight.IsRegistered check
// below reliably catches an in-progress turn that has already started
// relying on this sandbox and refuses to tear it down out from under it —
// callers (the idle reaper, HandleControl's stop path) get false back and
// simply treat that conversation as "not idle"/"nothing to stop" this time
// around. If no turn has registered yet, Pop still only succeeds once, so at
// most one of "a turn starts and reattaches" or "this stop tears it down"
// wins, never both partially.
//
// That "IsRegistered reliably catches it" claim only holds because the
// check and the Pop below share resumeLock with Handle's own
// Register-then-Get sequence: on its own, IsRegistered-then-Pop is just as
// much a check-then-act race as Register-then-Get is — a concurrent Handle
// call could Register and Get in the gap between this function's own check
// and its Pop, no less than this function could Pop in the gap Handle's
// comment describes. resumeLock makes the two sequences mutually exclusive
// per conversation_id instead of independently racy, so whichever of
// "resume" or "tear down" reaches the lock first is the one that actually
// happens.
func (h *Handler) TeardownPausedChatSandbox(ctx context.Context, conversationID uuid.UUID) bool {
	unlock := h.resumeLock().Lock(conversationID)
	registered := h.InFlight.IsRegistered(conversationID)
	var state *chatsandbox.State
	var ok bool
	if !registered {
		state, ok = h.ChatSandboxes.Pop(conversationID)
	}
	unlock()

	if registered || !ok {
		return false
	}
	state.Client.Close()
	if err := h.Executor.StopSandbox(context.WithoutCancel(ctx), state.Handle); err != nil {
		h.Log.Warn("agent-runner: failed to stop paused chat sandbox",
			"conversation_id", conversationID, "error", err)
	}
	if err := h.ConvRepo.UpdateStatus(ctx, conversationID, "stopped", nil); err != nil {
		h.Log.Warn("agent-runner: failed to record stopped status for paused chat sandbox",
			"conversation_id", conversationID, "error", err)
	}
	h.publishTerminalStatus(ctx, state.ProjectID, conversationID, state.ActorUserID, "stopped", "agent.conversation.stopped")
	return true
}

// publishTerminalStatus mirrors run_conversation's post-turn publishing for
// a terminal (finished/failed/stopped) status: PublishRealtime so any
// connected client updates immediately, plus PublishConversationStatus so
// services/api's automation engine can durably resume a graph walk paused
// waiting on this conversation — see StreamAgentConversationStatus's doc
// comment for why that needs its own durable stream rather than reusing
// the pub/sub realtime path. Best-effort: a failure here is logged, not
// returned — the conversation's own DB status is already the source of
// truth and was written by the caller before this runs.
func (h *Handler) publishTerminalStatus(ctx context.Context, projectID, conversationID uuid.UUID, actorUserID *uuid.UUID, status, realtimeEventType string) {
	if err := h.Publisher.PublishRealtime(
		ctx, projectID, conversationID, realtimeEventType, nil, actorUserID,
	); err != nil {
		h.Log.Warn("agent-runner: failed to publish realtime status",
			"conversation_id", conversationID, "status", status, "error", err)
	}
	if err := h.Publisher.PublishConversationStatus(ctx, conversationID, status); err != nil {
		h.Log.Warn("agent-runner: failed to publish conversation status",
			"conversation_id", conversationID, "status", status, "error", err)
	}
}

// publishNonTerminalStatus is publishTerminalStatus's "paused" counterpart:
// PublishRealtime only, never PublishConversationStatus — "paused" isn't a
// terminal status (see agentdom.ConversationStatus.IsTerminal in
// services/api and PublishConversationStatus's own doc comment), so nothing
// should durably resume an automation graph walk on it the way a real
// terminal status does.
func (h *Handler) publishNonTerminalStatus(ctx context.Context, trigger agent.Trigger, realtimeEventType string) {
	if err := h.Publisher.PublishRealtime(
		ctx, trigger.ProjectID, trigger.ConversationID, realtimeEventType, nil, trigger.ActorUserID,
	); err != nil {
		h.Log.Warn("agent-runner: failed to publish realtime status",
			"conversation_id", trigger.ConversationID, "event_type", realtimeEventType, "error", err)
	}
}
