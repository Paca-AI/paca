package runner

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSessionKeyFor covers the scope→key mapping, including the
// task→chat_session→conversation fallback that keeps ScopeTask working
// (as ScopeConversation) against a server that doesn't send task_id yet.
func TestSessionKeyFor(t *testing.T) {
	cases := []struct {
		name  string
		scope string
		msg   map[string]any
		want  string
	}{
		{"conversation scope ignores task", ScopeConversation,
			map[string]any{"conversation_id": "c1", "task_id": "t1"}, "c1"},
		{"agent scope collapses everything", ScopeAgent,
			map[string]any{"conversation_id": "c1", "task_id": "t1"}, "agent"},
		{"task scope keys by task", ScopeTask,
			map[string]any{"conversation_id": "c1", "task_id": "t1"}, "task:t1"},
		{"task scope falls back to chat session", ScopeTask,
			map[string]any{"conversation_id": "c1", "chat_session_id": "s1"}, "chat:s1"},
		{"task scope falls back to conversation (unpatched server)", ScopeTask,
			map[string]any{"conversation_id": "c1"}, "c1"},
		{"task prefers task over chat when both present", ScopeTask,
			map[string]any{"conversation_id": "c1", "task_id": "t1", "chat_session_id": "s1"}, "task:t1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{scope: tc.scope}
			if got := r.sessionKeyFor(tc.msg); got != tc.want {
				t.Fatalf("sessionKeyFor(%s) = %q, want %q", tc.scope, got, tc.want)
			}
		})
	}
}

// TestConfigScopeNormalizesUnknown proves an empty/unknown scope becomes the
// fork's default (task), so a zero Config is a valid "per-task" runner.
func TestConfigScopeNormalizesUnknown(t *testing.T) {
	for _, in := range []string{"", "bogus"} {
		if got := (Config{Scope: in}).scope(); got != ScopeTask {
			t.Fatalf("Config{Scope:%q}.scope() = %q, want %q", in, got, ScopeTask)
		}
	}
	for _, in := range []string{ScopeConversation, ScopeAgent, ScopeTask} {
		if got := (Config{Scope: in}).scope(); got != in {
			t.Fatalf("Config{Scope:%q}.scope() = %q, want it unchanged", in, got)
		}
	}
}

// TestPerTaskSessionReuseAndAttribution is the core acceptance test: two
// conversations sharing a task_id run through ONE session (one subprocess,
// reused), and each turn's events are attributed to the conversation actually
// in flight — not the one that first spawned the subprocess.
func TestPerTaskSessionReuseAndAttribution(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	setFakeAgentEnv(t, "normal")

	r, sent := newTestRunner(t) // Config{} → ScopeTask
	// Pre-seed the task's session with the fake-agent command, exactly as the
	// upstream E2E tests seed a conversation — avoids depending on provider
	// resolution. Key is the task key, since both conversations share task T1.
	r.mu.Lock()
	r.conversations["task:T1"] = &conversationState{
		chunks:      newChunkBuffer(),
		acpProvider: "custom",
		command:     []string{os.Args[0]},
	}
	r.mu.Unlock()

	// Turn 1 — conversation conv-1 on task T1.
	r.StartTurn(context.Background(), map[string]any{
		"conversation_id": "conv-1",
		"project_id":      "proj-1",
		"task_id":         "T1",
		"message":         "first",
	})
	waitForStatus(t, sent, "finished")

	r.mu.Lock()
	state := r.conversations["task:T1"]
	r.mu.Unlock()
	state.mu.Lock()
	clientAfter1 := state.client
	state.mu.Unlock()
	if clientAfter1 == nil {
		t.Fatal("expected a live ACP client after turn 1")
	}

	// Turn 2 — a DIFFERENT conversation conv-2 on the SAME task T1.
	r.StartTurn(context.Background(), map[string]any{
		"conversation_id": "conv-2",
		"project_id":      "proj-1",
		"task_id":         "T1",
		"message":         "second",
	})
	waitForStatus(t, sent, "finished")

	// Exactly one session for the task — conv-2 reused conv-1's subprocess.
	r.mu.Lock()
	nSessions := len(r.conversations)
	k1, k2 := r.convIndex["conv-1"], r.convIndex["conv-2"]
	r.mu.Unlock()
	if nSessions != 1 {
		t.Fatalf("expected 1 shared session for the task, got %d", nSessions)
	}
	if k1 != "task:T1" || k2 != "task:T1" {
		t.Fatalf("convIndex = {conv-1:%q, conv-2:%q}, want both task:T1", k1, k2)
	}
	state.mu.Lock()
	clientAfter2 := state.client
	state.mu.Unlock()
	if clientAfter2 != clientAfter1 {
		t.Fatal("turn 2 spawned a new subprocess instead of reusing the task's session")
	}

	// Attribution: turn 2's events/status must carry conversation_id conv-2,
	// and conv-2 must have produced its own user_message (proving the onUpdate
	// closure read the in-flight conversation, not the spawn-time one).
	var conv2UserMsg, conv2Finished bool
	for _, m := range sent.all() {
		if m["conversation_id"] != "conv-2" {
			continue
		}
		if m["type"] == "event" && m["event_type"] == "user_message" {
			conv2UserMsg = true
		}
		if m["type"] == "turn_status" && m["status"] == "finished" {
			conv2Finished = true
		}
	}
	if !conv2UserMsg {
		t.Fatal("no user_message attributed to conv-2 (attribution bug)")
	}
	if !conv2Finished {
		t.Fatal("no finished turn_status attributed to conv-2")
	}
}

// TestInterruptResolvesSharedTaskSession proves Interrupt — which the bridge
// calls with a conversation_id — still finds the session when the map is keyed
// by task. Without the convIndex fix this is a silent no-op and the turn never
// stops. Mirrors the upstream "interrupt suppresses turn_status" contract.
func TestInterruptResolvesSharedTaskSession(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	setFakeAgentEnv(t, "cancel")

	r, sent := newTestRunner(t)
	r.mu.Lock()
	r.conversations["task:T1"] = &conversationState{
		chunks:      newChunkBuffer(),
		acpProvider: "custom",
		command:     []string{os.Args[0]},
	}
	r.mu.Unlock()

	r.StartTurn(context.Background(), map[string]any{
		"conversation_id": "conv-9",
		"project_id":      "proj-1",
		"task_id":         "T1",
		"message":         "work",
	})
	// Wait until the turn is actually in flight (user_message emitted) so the
	// cancel isn't racing session setup.
	waitForEventType(t, sent, "user_message")

	// Interrupt by CONVERSATION id; the session is keyed by TASK.
	r.Interrupt("conv-9")

	// Cooperative cancel → no terminal turn_status (upstream contract). Give
	// the turn a moment to unwind, then assert none was reported.
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		st := r.conversations["task:T1"]
		r.mu.Unlock()
		st.mu.Lock()
		running := st.turnRunning
		st.mu.Unlock()
		if !running {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, m := range sent.all() {
		if m["type"] == "turn_status" {
			t.Fatalf("expected no turn_status after cooperative interrupt, got %v", m["status"])
		}
	}
}

// TestEvictIdleReclaimsAndCleansIndex proves the sweeper removes a session
// idle past the timeout and forgets every convIndex entry pointing at it,
// while leaving a recently-active session (and a running one) untouched.
func TestEvictIdleReclaimsAndCleansIndex(t *testing.T) {
	r := &Runner{
		conversations: make(map[string]*conversationState),
		convIndex:     make(map[string]string),
		idleTimeout:   30 * time.Minute,
	}
	now := time.Now()

	// Idle session shared by two conversations (nil client → no real Close).
	idle := &conversationState{chunks: newChunkBuffer(), lastActivity: now.Add(-time.Hour)}
	r.conversations["task:OLD"] = idle
	r.convIndex["conv-a"] = "task:OLD"
	r.convIndex["conv-b"] = "task:OLD"

	// Recently active session — must survive.
	fresh := &conversationState{chunks: newChunkBuffer(), lastActivity: now.Add(-time.Minute)}
	r.conversations["task:NEW"] = fresh
	r.convIndex["conv-c"] = "task:NEW"

	// Running session, idle-looking timestamp but a turn in flight — must survive.
	busy := &conversationState{chunks: newChunkBuffer(), lastActivity: now.Add(-time.Hour), turnRunning: true}
	r.conversations["task:BUSY"] = busy
	r.convIndex["conv-d"] = "task:BUSY"

	r.evictIdle(now)

	if _, ok := r.conversations["task:OLD"]; ok {
		t.Fatal("idle session was not evicted")
	}
	if _, ok := r.convIndex["conv-a"]; ok {
		t.Fatal("convIndex conv-a not cleaned after eviction")
	}
	if _, ok := r.convIndex["conv-b"]; ok {
		t.Fatal("convIndex conv-b not cleaned after eviction")
	}
	if _, ok := r.conversations["task:NEW"]; !ok {
		t.Fatal("recently-active session was wrongly evicted")
	}
	if _, ok := r.conversations["task:BUSY"]; !ok {
		t.Fatal("running session was wrongly evicted")
	}
}
