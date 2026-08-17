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

	// client/sessionID are set once the ACP session is established and
	// reused across every later turn of this same conversation — a chat
	// conversation reattaches to the same subprocess rather than spawning
	// a new one per turn, mirroring the old bridge's one-Conversation-
	// per-conversation_id model.
	client    *acpclient.Client
	sessionID string

	chunks *chunkBuffer

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

// Runner implements bridge.Handler.
type Runner struct {
	workspace string
	send      bridge.SendFunc
	log       *slog.Logger

	mu            sync.Mutex
	conversations map[string]*conversationState
}

// New builds a Runner rooted at workspace, using send to report events and
// turn_status back over the bridge. Suitable as the newHandler argument to
// bridge.New.
func New(workspace string, log *slog.Logger) func(bridge.SendFunc) bridge.Handler {
	if log == nil {
		log = slog.Default()
	}
	return func(send bridge.SendFunc) bridge.Handler {
		return &Runner{
			workspace:     workspace,
			send:          send,
			log:           log,
			conversations: make(map[string]*conversationState),
		}
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

	r.mu.Lock()
	state, exists := r.conversations[conversationID]
	if !exists {
		state = &conversationState{chunks: newChunkBuffer()}
		r.conversations[conversationID] = state
	}
	r.mu.Unlock()

	state.mu.Lock()
	if state.turnRunning {
		state.mu.Unlock()
		r.log.Warn("runner: ignoring start_turn: a previous turn is still running",
			"conversation_id", conversationID)
		r.reportStatus(state, conversationID, projectID, "failed",
			"A previous turn for this conversation is still running; please retry.")
		return
	}

	if !exists {
		acpProvider, _ := msg["acp_provider"].(string)
		command, err := provider.ResolveCommand(acpProvider, stringSlice(msg["acp_command"]))
		if err != nil {
			state.mu.Unlock()
			r.log.Error("runner: cannot start conversation", "conversation_id", conversationID, "error", err)
			r.mu.Lock()
			delete(r.conversations, conversationID)
			r.mu.Unlock()
			r.reportStatus(state, conversationID, projectID, "failed", err.Error())
			return
		}
		state.acpProvider = acpProvider
		state.command = command
	}

	state.turnRunning = true
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
	state, ok := r.conversations[conversationID]
	r.mu.Unlock()
	if !ok {
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

	stopReason, err := client.Prompt(ctx, sessionID, message)
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

	client, err := acpclient.Spawn(state.command, r.workspace, func(u acpclient.Update) {
		r.handleUpdate(state, conversationID, projectID, u)
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
	done := state.turnDone
	state.turnDone = nil
	state.mu.Unlock()
	if done != nil {
		close(done)
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
	r.flushChunks(state, conversationID, projectID)
	r.emitEvent(state, conversationID, projectID, u.Kind, "agent", u.Raw)
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
