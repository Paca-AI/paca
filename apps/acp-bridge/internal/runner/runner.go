// Package runner drives local ACP conversations and reports events back
// over the bridge. It's the Go, OpenHands-free replacement for the old
// bridge's runner.py: instead of wrapping the OpenHands SDK's
// Conversation/ACPAgent, it speaks ACP directly (via internal/acpclient) to
// the spawned coding CLI and forwards session/update notifications using
// the same event vocabulary services/agent-runner/internal/handler already
// established for Goose-in-sandbox conversations (user_message /
// agent_message_chunk / agent_thought_chunk / tool_call / tool_call_update
// / turn_end) — no OpenHands-shaped event types, and no need for the old
// bridge's text-reordering relay, since ACP already streams narration in
// position relative to the tool calls it describes.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Paca-AI/paca/apps/acp-bridge/internal/acpclient"
	"github.com/Paca-AI/paca/apps/acp-bridge/internal/bridge"
	"github.com/Paca-AI/paca/apps/acp-bridge/internal/provider"
)

// interruptGracePeriod bounds how long Interrupt waits for the agent to
// honor session/cancel and let the in-flight Prompt call return normally
// before forcing the issue (cancelling the turn's context and killing the
// subprocess). session/cancel is defined as a notification the agent is
// expected to act on promptly, but nothing guarantees a given ACP CLI
// actually does.
const interruptGracePeriod = 15 * time.Second

// outboundQueueSize bounds each conversation's own local event queue (see
// conversationState.outbound) — deliberately far larger than
// bridge.outboxSize, since this buffer's job is to absorb the entire gap
// between event production and the bridge connection coming back, not just
// smooth over a brief hiccup. Still finite rather than truly unbounded so a
// conversation can't grow without limit forever, but reaching this many
// undelivered messages needs a genuinely extreme, sustained outage.
const outboundQueueSize = 100_000

// conversationState is one active or resumable conversation — mirrors the
// old bridge's _ConversationHandle, but the ACP session (subprocess +
// sessionID) is established lazily on the first turn rather than eagerly at
// start_turn time, and torn down explicitly rather than implicitly by the
// OpenHands SDK.
type conversationState struct {
	// acpProvider/command are set once, on the conversation's first
	// start_turn, and read (without further locking) only by the single
	// goroutine runTurn spawns per turn — safe under Go's memory model
	// because they're written before the `go` statement that starts that
	// goroutine.
	acpProvider string
	command     []string

	mu          sync.Mutex
	turnRunning bool
	turnCancel  context.CancelFunc
	// turnDone is closed when the current turn's goroutine finishes —
	// lets Interrupt's grace-period timer stop waiting early instead of
	// always sleeping the full interruptGracePeriod.
	turnDone chan struct{}

	// currentConversationID/currentProjectID identify the conversation whose
	// turn is running right now. With per-task (or per-agent) session scope a
	// single conversationState — one ACP subprocess — is reused across MORE
	// than one Paca conversation, so the conversation an event belongs to
	// changes turn-to-turn and can no longer be captured once in the spawn
	// closure. runTurn stamps these under mu before doing any work; the ACP
	// stdout goroutine's onUpdate closure reads them (via currentIDs) so each
	// event is attributed to the turn actually in flight. Guarded by mu.
	currentConversationID string
	currentProjectID      string

	// lastActivity is refreshed whenever a turn starts or finishes; the idle
	// sweeper (see Runner.sweepIdle) evicts a session that has been idle
	// longer than the configured timeout. Guarded by mu.
	lastActivity time.Time

	// client/sessionID are set once the ACP session is established and
	// reused across every later turn of this same conversation — a chat
	// conversation reattaches to the same subprocess rather than spawning
	// a new one per turn, mirroring the old bridge's one-Conversation-
	// per-conversation_id model.
	client    *acpclient.Client
	sessionID string

	chunks *chunkBuffer

	// turnCostUSD is the current turn's latest reported cost, set from
	// "usage_update" session/update notifications (see recordUsageUpdate)
	// and read back once the turn's client.Prompt call returns (see
	// emitTurnUsage) — mirrors handler.Handler's latestCostUSD local
	// variable, but must live on conversationState rather than as a runTurn
	// local since handleUpdate (which observes the notifications) runs on
	// the ACP client's own read goroutine, not runTurn's. Reset to nil at
	// the start of every turn so a later turn with no usage_update at all
	// doesn't inherit a stale cost from an earlier one.
	turnCostUSD *float64

	// outbound is this conversation's own local queue of not-yet-delivered
	// bridge messages (events and turn_status), drained in order by a
	// single dedicated forwardEvents goroutine — see ensureOutbound's doc
	// comment for why sending here must never call bridge.SendFunc
	// directly. Lazily created (via outboundOnce) rather than at
	// conversationState construction time so tests that build one as a
	// plain struct literal still work correctly.
	outboundOnce sync.Once
	outbound     chan map[string]any
}

// Session scope names — how a start_turn is mapped to the ACP subprocess
// ("session") that serves it. See Runner.sessionKeyFor.
const (
	// ScopeConversation is the upstream behavior: one ACP session per Paca
	// conversation. Maximum isolation, but no shared memory even between two
	// conversations on the same task.
	ScopeConversation = "conversation"
	// ScopeTask keys the session by task_id when the trigger carries one
	// (falling back to chat_session_id, then conversation_id), so every
	// conversation on one task — its task_assigned turn, later comment
	// @mentions, task chat — shares a single Claude Code process and its
	// accumulated context. This is the fork's default and reason for being.
	ScopeTask = "task"
	// ScopeAgent keys every conversation to a single session — one persistent
	// process for the whole agent, matching the "channel" model. Maximum
	// memory, zero isolation between tasks.
	ScopeAgent = "agent"
)

// Config tunes how the Runner maps conversations to ACP sessions and when it
// reclaims idle ones. The zero value is valid: Scope defaults to ScopeTask
// and IdleTimeout to 0 (no eviction).
type Config struct {
	// Scope selects the session-keying strategy (see the Scope* constants).
	// An unknown or empty value is normalized to ScopeTask.
	Scope string
	// IdleTimeout, when > 0, reclaims a session that has had no turn for at
	// least this long. Zero disables eviction (upstream's behavior).
	IdleTimeout time.Duration
}

func (c Config) scope() string {
	switch c.Scope {
	case ScopeConversation, ScopeTask, ScopeAgent:
		return c.Scope
	default:
		return ScopeTask
	}
}

// Runner implements bridge.Handler.
type Runner struct {
	workspace   string
	send        bridge.SendFunc
	log         *slog.Logger
	scope       string
	idleTimeout time.Duration

	mu sync.Mutex
	// conversations is keyed by session key (see sessionKeyFor), NOT by
	// conversation_id — under ScopeTask several conversation_ids map to one
	// entry.
	conversations map[string]*conversationState
	// convIndex maps a conversation_id to its session key, so Interrupt
	// (which the bridge calls with a conversation_id) can find the session
	// even when the map is keyed by task. Populated on every StartTurn.
	convIndex map[string]string
}

// New builds a Runner rooted at workspace, using send to report events and
// turn_status back over the bridge. Suitable as the newHandler argument to
// bridge.New.
func New(workspace string, cfg Config, log *slog.Logger) func(bridge.SendFunc) bridge.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(send bridge.SendFunc) bridge.Handler {
		r := &Runner{
			workspace:     workspace,
			send:          send,
			log:           log,
			scope:         cfg.scope(),
			idleTimeout:   cfg.IdleTimeout,
			conversations: make(map[string]*conversationState),
			convIndex:     make(map[string]string),
		}
		if r.idleTimeout > 0 {
			go r.sweepIdle()
		}
		return r
	}
}

// sessionKeyFor derives the session key for a start_turn message under the
// configured scope. The task→chat_session→conversation fallback is what makes
// the fork safe against an unpatched Paca: a server that doesn't send task_id
// yet leaves ScopeTask degrading to exactly ScopeConversation, with no error.
func (r *Runner) sessionKeyFor(msg map[string]any) string {
	conversationID, _ := msg["conversation_id"].(string)
	switch r.scope {
	case ScopeAgent:
		return "agent"
	case ScopeTask:
		if taskID, _ := msg["task_id"].(string); taskID != "" {
			return "task:" + taskID
		}
		if chatID, _ := msg["chat_session_id"].(string); chatID != "" {
			return "chat:" + chatID
		}
		return conversationID
	default: // ScopeConversation
		return conversationID
	}
}

// StartTurn handles a start_turn message: begins a new conversation or
// resumes an existing one, rejecting only when a previous turn for the same
// conversation is still in flight. Returns quickly — the actual ACP work
// runs on a goroutine it spawns.
func (r *Runner) StartTurn(ctx context.Context, msg map[string]any) {
	conversationID, _ := msg["conversation_id"].(string)
	projectID, _ := msg["project_id"].(string)
	message, _ := msg["message"].(string)
	if conversationID == "" {
		return
	}

	key := r.sessionKeyFor(msg)

	r.mu.Lock()
	state, exists := r.conversations[key]
	if !exists {
		state = &conversationState{chunks: newChunkBuffer()}
		r.conversations[key] = state
	}
	// Index this conversation to its session so Interrupt (called by
	// conversation_id) can find the shared session under task keying.
	r.convIndex[conversationID] = key
	r.mu.Unlock()

	state.mu.Lock()
	if state.turnRunning {
		state.mu.Unlock()
		// Under ScopeTask two conversations on the same task share one
		// session's single turnRunning gate, so an @mention arriving mid-turn
		// is rejected here rather than queued — the server's watchdog would
		// otherwise fail a queued-and-delayed conversation out from under us.
		r.log.Warn("runner: ignoring start_turn: a previous turn is still running",
			"conversation_id", conversationID, "session_key", key)
		r.reportStatus(state, conversationID, projectID, "failed",
			"The agent is busy with another turn on this task; please retry.")
		return
	}

	if state.client == nil && state.command == nil {
		acpProvider, _ := msg["acp_provider"].(string)
		command, err := provider.ResolveCommand(acpProvider, stringSlice(msg["acp_command"]))
		if err != nil {
			state.mu.Unlock()
			r.log.Error("runner: cannot start conversation", "conversation_id", conversationID, "error", err)
			r.mu.Lock()
			// Only drop the session if this failed first turn left it empty;
			// never yank a session a sibling conversation is already using.
			if s, ok := r.conversations[key]; ok && s == state && s.client == nil {
				delete(r.conversations, key)
			}
			delete(r.convIndex, conversationID)
			r.mu.Unlock()
			r.reportStatus(state, conversationID, projectID, "failed", err.Error())
			return
		}
		state.acpProvider = acpProvider
		state.command = command
	}

	state.turnRunning = true
	state.currentConversationID = conversationID
	state.currentProjectID = projectID
	state.lastActivity = time.Now()
	turnCtx, cancel := context.WithCancel(context.Background())
	state.turnCancel = cancel
	state.turnDone = make(chan struct{})
	state.mu.Unlock()

	go r.runTurn(turnCtx, state, conversationID, projectID, message)
}

// Interrupt handles a stop_turn/pause_turn message — both just interrupt
// whatever turn is in flight; there's no sandbox lifecycle to additionally
// tear down (unlike the cloud path's full stop-vs-pause distinction). A
// no-op for an unknown or already-idle conversation.
func (r *Runner) Interrupt(conversationID string) {
	if conversationID == "" {
		return
	}
	r.mu.Lock()
	key, indexed := r.convIndex[conversationID]
	var state *conversationState
	if indexed {
		state = r.conversations[key]
	}
	r.mu.Unlock()
	if state == nil {
		return
	}

	state.mu.Lock()
	running := state.turnRunning
	client := state.client
	sessionID := state.sessionID
	cancel := state.turnCancel
	done := state.turnDone
	state.mu.Unlock()
	if !running {
		return
	}

	if client == nil {
		// Still spawning/initializing — nothing at the ACP level to
		// cancel yet; aborting the turn's own context is enough to unwind
		// the in-flight spawn/initialize/session/new sequence promptly.
		if cancel != nil {
			cancel()
		}
		return
	}

	if err := client.Cancel(sessionID); err != nil {
		r.log.Warn("runner: failed to send session/cancel", "conversation_id", conversationID, "error", err)
	}

	go func() {
		select {
		case <-done:
			return
		case <-time.After(interruptGracePeriod):
		}
		r.log.Warn("runner: ACP provider did not honor cancellation in time; killing subprocess",
			"conversation_id", conversationID)
		if cancel != nil {
			cancel()
		}
		_ = client.Close()
		state.mu.Lock()
		if state.client == client {
			state.client = nil
			state.sessionID = ""
		}
		state.mu.Unlock()
	}()
}

func (r *Runner) runTurn(ctx context.Context, state *conversationState, conversationID, projectID, message string) {
	defer r.finishTurn(state)

	state.mu.Lock()
	state.turnCostUSD = nil
	state.mu.Unlock()

	client, sessionID, err := r.ensureSession(ctx, state, conversationID, projectID)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Interrupted before the session was even established (still
			// spawning the subprocess, or mid initialize/session/new) —
			// same "no turn_status" treatment as an interrupt during
			// client.Prompt below; Interrupt's own "still spawning" branch
			// is exactly what cancelled ctx here.
			return
		}
		r.log.Error("runner: failed to start ACP session", "conversation_id", conversationID, "error", err)
		r.reportStatus(state, conversationID, projectID, "failed", err.Error())
		return
	}

	r.emitUserMessage(state, conversationID, projectID, message)

	stopReason, usage, err := client.Prompt(ctx, sessionID, message)
	// Whatever's still buffered when the turn ends — successfully,
	// interrupted, or failed — is genuine partial output, not scratch
	// state to discard.
	r.flushChunks(state, conversationID, projectID)

	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Interrupted before the agent ever answered (still spawning,
			// or it didn't honor session/cancel within the grace period
			// and got killed) — no turn_status for this case, matching the
			// cooperative-cancel case just below.
			return
		}
		r.log.Error("runner: conversation turn failed", "conversation_id", conversationID, "error", err)
		r.reportStatus(state, conversationID, projectID, "failed", err.Error())
		return
	}

	r.emitTurnEnd(state, conversationID, projectID, stopReason)
	r.emitTurnUsage(state, conversationID, projectID, usage)
	if stopReason == "cancelled" {
		// The agent honored session/cancel cooperatively and answered
		// session/prompt normally — a real response, not a transport
		// error, so it doesn't hit the context.Canceled case above. The
		// old bridge reports no turn_status for an interrupted turn
		// either; dispatching stop_turn/pause_turn is treated
		// server-side as sufficient on its own.
		return
	}
	r.reportStatus(state, conversationID, projectID, "finished", "")
}

// ensureSession returns the conversation's ACP client and session id,
// spawning the subprocess and performing the initialize/session/new/
// set_mode handshake on the first call for a given conversation. Later
// turns reuse the same client.
func (r *Runner) ensureSession(
	ctx context.Context, state *conversationState, conversationID, projectID string,
) (*acpclient.Client, string, error) {
	state.mu.Lock()
	client := state.client
	sessionID := state.sessionID
	state.mu.Unlock()
	if client != nil {
		return client, sessionID, nil
	}

	// The onUpdate closure must NOT capture conversationID/projectID: this
	// subprocess outlives the turn that spawned it and, under task keying,
	// serves later turns of other conversations. Read the conversation in
	// flight from state at each callback instead, so every event is
	// attributed to the right Paca conversation thread.
	client, err := acpclient.Spawn(state.command, r.workspace, func(u acpclient.Update) {
		cid, pid := state.currentIDs()
		r.handleUpdate(state, cid, pid, u)
	}, r.log)
	if err != nil {
		return nil, "", fmt.Errorf("spawning %v: %w", state.command, err)
	}

	if err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, "", err
	}

	sessionID, modes, err := client.NewSession(ctx, r.workspace)
	if err != nil {
		_ = client.Close()
		return nil, "", err
	}

	if mode := provider.PermissionMode(state.acpProvider); mode != "" && modes.Offers(mode) {
		if err := client.SetMode(ctx, sessionID, mode); err != nil {
			r.log.Warn("runner: failed to set permission-bypass session mode",
				"conversation_id", conversationID, "mode", mode, "error", err)
		}
	}

	state.mu.Lock()
	state.client = client
	state.sessionID = sessionID
	state.mu.Unlock()
	return client, sessionID, nil
}

func (r *Runner) finishTurn(state *conversationState) {
	state.mu.Lock()
	state.turnRunning = false
	state.turnCancel = nil
	state.lastActivity = time.Now()
	done := state.turnDone
	state.turnDone = nil
	state.mu.Unlock()
	if done != nil {
		close(done)
	}
}

// currentIDs returns the conversation/project the session's turn is serving
// right now, read under mu — see conversationState.currentConversationID.
func (s *conversationState) currentIDs() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentConversationID, s.currentProjectID
}

// sweepIdle periodically reclaims sessions with no turn for longer than
// r.idleTimeout, closing the ACP subprocess and forgetting the session (and
// every convIndex entry pointing at it). Upstream never evicts, which is why
// abandoned sessions pile up as long-lived CC processes; per-task keying
// reduces the count but doesn't bound it, so the fork closes the leak. A
// revisited task simply spawns a fresh session — durable per-task memory
// across an eviction is a later concern (Paca's persistence_dir + ACP
// session/load), deliberately out of scope here.
func (r *Runner) sweepIdle() {
	interval := r.idleTimeout / 2
	if interval < time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		r.evictIdle(time.Now())
	}
}

func (r *Runner) evictIdle(now time.Time) {
	r.mu.Lock()
	var victims []*conversationState
	for key, state := range r.conversations {
		state.mu.Lock()
		idle := !state.turnRunning && !state.lastActivity.IsZero() &&
			now.Sub(state.lastActivity) >= r.idleTimeout
		client := state.client
		state.mu.Unlock()
		if !idle {
			continue
		}
		delete(r.conversations, key)
		for convID, k := range r.convIndex {
			if k == key {
				delete(r.convIndex, convID)
			}
		}
		if client != nil {
			victims = append(victims, state)
		}
	}
	r.mu.Unlock()

	// Close subprocesses outside r.mu — Close can block, and nothing else can
	// reach these states now that they're unlinked from both maps.
	for _, state := range victims {
		state.mu.Lock()
		client := state.client
		state.client = nil
		state.sessionID = ""
		state.mu.Unlock()
		if client != nil {
			_ = client.Close()
			r.log.Info("runner: evicted idle ACP session")
		}
	}
}

// handleUpdate is the ACP client's onUpdate callback for one conversation.
// Text-bearing kinds are paragraph-buffered (see chunkBuffer); everything
// else is forwarded as its own event, flushing any pending text first so
// the persisted order matches the order things actually happened in. Called
// synchronously on the ACP client's own stdout-reading goroutine (see
// acpclient.Spawn's doc comment), so everything reachable from here enqueues
// onto state.outbound (see ensureOutbound) rather than ever calling
// bridge.SendFunc directly.
func (r *Runner) handleUpdate(state *conversationState, conversationID, projectID string, u acpclient.Update) {
	if u.Kind == "agent_message_chunk" || u.Kind == "agent_thought_chunk" {
		text := extractChunkText(u.Raw)
		state.chunks.append(u.Kind, text)
		if strings.Contains(text, "\n\n") {
			r.flushChunks(state, conversationID, projectID)
		}
		return
	}
	if u.Kind == "usage_update" {
		// Recorded onto state.turnCostUSD for emitTurnUsage to pick up once
		// the turn ends, not forwarded as its own event: a usage snapshot
		// has no place in the chat transcript — mirrors
		// services/agent-runner/internal/handler.Handler's onEvent, which
		// likewise returns before ever routing a usage_update through
		// persistAndPublish (and, notably, before flushing pending chunk
		// text the way every other update kind below does).
		r.recordUsageUpdate(state, u.Raw)
		return
	}
	r.flushChunks(state, conversationID, projectID)
	r.emitEvent(state, conversationID, projectID, u.Kind, "agent", u.Raw)
}

// recordUsageUpdate decodes a "usage_update" notification's cost (ACP's
// $defs.UsageUpdate) and stores it on state.turnCostUSD. Verified against
// the same schema services/agent-runner/internal/acp.UsageUpdate targets;
// see that type's doc comment for why Cost — when a given ACP agent reports
// it the way goose does — is typically a session-running total rather than
// a per-turn delta, and why capturing only the LATEST notification seen
// during the turn (last write wins, no accumulation here) is the correct
// read of that regardless.
func (r *Runner) recordUsageUpdate(state *conversationState, raw json.RawMessage) {
	var payload struct {
		Cost *struct {
			Amount float64 `json:"amount"`
		} `json:"cost,omitempty"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		r.log.Warn("runner: failed to decode usage_update", "error", err)
		return
	}
	if payload.Cost == nil {
		return
	}
	cost := payload.Cost.Amount
	state.mu.Lock()
	state.turnCostUSD = &cost
	state.mu.Unlock()
}

// emitTurnUsage reports this turn's token/cost accounting as its own
// "turn_usage"/"system" event — the identical {input_tokens, output_tokens,
// total_tokens, cost_usd} JSON shape
// services/agent-runner/internal/handler.Handler already persists for
// llm-type (Goose-in-sandbox) conversations, so services/api's
// conversationCols query (already agent_type-agnostic — it just sums/reads
// 'turn_usage' rows under agent_conversation_events) picks this up for
// acp-type conversations with no changes needed on that end. usage is this
// turn's own token count straight from session/prompt's result (nil if the
// agent reported none); cost comes from state.turnCostUSD, populated by
// recordUsageUpdate above. A no-op (nothing emitted) when neither is
// present, matching handler.Handler's own `if result.Usage != nil ||
// latestCostUSD != nil` gate.
func (r *Runner) emitTurnUsage(state *conversationState, conversationID, projectID string, usage *acpclient.Usage) {
	state.mu.Lock()
	costUSD := state.turnCostUSD
	state.mu.Unlock()

	if usage == nil && costUSD == nil {
		return
	}
	payload := map[string]any{}
	if usage != nil {
		payload["input_tokens"] = usage.InputTokens
		payload["output_tokens"] = usage.OutputTokens
		payload["total_tokens"] = usage.TotalTokens
	}
	if costUSD != nil {
		payload["cost_usd"] = *costUSD
	}
	payloadJSON, _ := json.Marshal(payload)
	r.emitEvent(state, conversationID, projectID, "turn_usage", "system", payloadJSON)
}

func (r *Runner) flushChunks(state *conversationState, conversationID, projectID string) {
	// Deterministic order when both happen to have pending text at the
	// same flush point; real ACP turns don't interleave the two without an
	// intervening tool call (which itself triggers a flush), so this only
	// matters for a rare edge case, not the common path.
	for _, kind := range [...]string{"agent_message_chunk", "agent_thought_chunk"} {
		text := state.chunks.take(kind)
		if text == "" {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"content": map[string]any{"type": "text", "text": text},
		})
		r.emitEvent(state, conversationID, projectID, kind, "agent", payload)
	}
}

func (r *Runner) emitUserMessage(state *conversationState, conversationID, projectID, message string) {
	text := strings.TrimSpace(message)
	if text == "" {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"content": map[string]any{"type": "text", "text": text},
	})
	r.emitEvent(state, conversationID, projectID, "user_message", "user", payload)
}

func (r *Runner) emitTurnEnd(state *conversationState, conversationID, projectID, stopReason string) {
	payload, _ := json.Marshal(map[string]string{"stopReason": stopReason})
	r.emitEvent(state, conversationID, projectID, "turn_end", "system", payload)
}

func (r *Runner) emitEvent(state *conversationState, conversationID, projectID, eventType, eventSource string, payload json.RawMessage) {
	r.enqueue(state, map[string]any{
		"type":            "event",
		"conversation_id": conversationID,
		"project_id":      projectID,
		"event_type":      eventType,
		"event_source":    eventSource,
		"payload":         payload,
	})
}

// reportStatus hands a turn_status message to state's outbound queue: a
// failure to *deliver* status must never be conflated with the conversation
// itself failing, and must never panic or propagate out of a turn's
// goroutine — see forwardEvents for how delivery failures are handled.
func (r *Runner) reportStatus(state *conversationState, conversationID, projectID, status, errorMessage string) {
	msg := map[string]any{
		"type":            "turn_status",
		"conversation_id": conversationID,
		"project_id":      projectID,
		"status":          status,
	}
	if errorMessage != "" {
		msg["error_message"] = errorMessage
	}
	r.enqueue(state, msg)
}

// enqueue hands msg to state's local outbound queue — never to bridge.SendFunc
// directly. See ensureOutbound for why: everything that reaches enqueue can
// run on the ACP client's own stdout-reading goroutine, which must never
// block on the network.
func (r *Runner) enqueue(state *conversationState, msg map[string]any) {
	r.ensureOutbound(state) <- msg
}

// ensureOutbound lazily starts state's local event queue and its single
// forwardEvents goroutine, exactly once. handleUpdate — called synchronously
// on the ACP client's own stdout-reading goroutine (see acpclient.Spawn's
// doc comment) — must never block on the network-bound bridge connection: a
// prolonged disconnect combined with a chatty turn could otherwise fill
// bridge's own (deliberately bounded, see bridge.outboxSize) outbox and
// stall draining the ACP subprocess's own stdout pipe indefinitely, which
// can in turn prevent even a pending Cancel's follow-up response from ever
// being read. Routing every outbound message through this per-conversation
// queue instead keeps r.send's blocking entirely on forwardEvents, well
// away from the ACP read loop. sync.Once (rather than initializing outbound
// in a constructor) so a conversationState built as a plain struct literal —
// as several tests do — still works correctly.
func (r *Runner) ensureOutbound(state *conversationState) chan map[string]any {
	state.outboundOnce.Do(func() {
		state.outbound = make(chan map[string]any, outboundQueueSize)
		go r.forwardEvents(state)
	})
	return state.outbound
}

// forwardEvents drains state's outbound queue in order for the lifetime of
// the process — the only thing that ever calls bridge.SendFunc, and so the
// only thing that ever blocks on the network connection being down.
func (r *Runner) forwardEvents(state *conversationState) {
	for msg := range state.outbound {
		if err := r.send(context.Background(), msg); err != nil {
			r.log.Warn("runner: failed to report message", "type", msg["type"], "error", err)
		}
	}
}

// contentChunk is the shape of an agent_message_chunk/agent_thought_chunk
// update's raw payload: {"sessionUpdate":"...", "content": {"type":"text","text":"..."}}.
type contentChunk struct {
	Content struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func extractChunkText(raw json.RawMessage) string {
	var c contentChunk
	if err := json.Unmarshal(raw, &c); err != nil || c.Content.Type != "text" {
		return ""
	}
	return c.Content.Text
}

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
