package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionStorePersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	newSessionStore(path).set("claude-code", "task:1", "sess-abc")
	if got := newSessionStore(path).get("claude-code", "task:1"); got != "sess-abc" {
		t.Fatalf("got %q", got)
	}
	if got := newSessionStore(path).get("codex", "task:1"); got != "" {
		t.Fatalf("provider isolation broken: %q", got)
	}
}

func TestSessionStoreMergesConcurrentWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	a, b := newSessionStore(path), newSessionStore(path)
	a.set("p", "k1", "one")
	b.set("p", "k2", "two") // b's startup snapshot predates a's write
	if got := newSessionStore(path).get("p", "k1"); got != "one" {
		t.Fatalf("k1 clobbered: %q", got)
	}
}

func TestStorePathIsolatesWorkspaceAndAgentOutsideWorkspace(t *testing.T) {
	state, ws := t.TempDir(), t.TempDir()
	a := storePath(state, ws, "agent-a")
	if a == storePath(state, ws, "agent-b") || a == storePath(state, t.TempDir(), "agent-a") {
		t.Fatal("paths must differ per agent and per workspace")
	}
	if strings.HasPrefix(a, ws) {
		t.Fatalf("store path %q is inside workspace", a)
	}
}

// resumeTurn runs one turn against the fake agent with a pre-saved session id
// and returns the runner, the sent messages and the session methods called.
func resumeTurn(t *testing.T, behavior string) (*Runner, *capturedSends, []string) {
	t.Helper()
	setFakeAgentEnv(t, behavior)
	logPath := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("PACA_FAKE_AGENT_LOG", logPath)

	r, sent := newTestRunner(t)
	r.sessions.set("custom", "conv-1", "sess-old")
	r.mu.Lock()
	r.conversations["conv-1"] = &conversationState{
		chunks: newChunkBuffer(), key: "conv-1", acpProvider: "custom", command: []string{os.Args[0]},
	}
	r.mu.Unlock()
	r.StartTurn(t.Context(), map[string]any{"conversation_id": "conv-1", "project_id": "p", "message": "hi"})
	waitForStatus(t, sent, "finished")

	b, _ := os.ReadFile(logPath)
	return r, sent, strings.Fields(string(b))
}

func hasEvent(sent *capturedSends, typ string) int {
	n := 0
	for _, m := range sent.all() {
		if m["type"] == "event" && m["event_type"] == typ {
			n++
		}
	}
	return n
}

func TestRespawnLoadsSavedSessionAndDropsReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	r, sent, calls := resumeTurn(t, "load")
	if len(calls) != 1 || calls[0] != "session/load" {
		t.Fatalf("calls = %v, want only session/load", calls)
	}
	// The normal turn emits exactly one agent_message_chunk ("Done."); the
	// replayed history chunk must have been dropped.
	if n := hasEvent(sent, "agent_message_chunk"); n != 1 {
		t.Fatalf("agent_message_chunk events = %d, want 1 (replay leaked)", n)
	}
	if got := r.sessions.get("custom", "conv-1"); got != "sess-old" {
		t.Fatalf("stored id = %q, want sess-old", got)
	}
}

func TestRespawnFallsBackToNewSessionWhenLoadFails(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	r, _, calls := resumeTurn(t, "load_fail")
	if len(calls) != 2 || calls[0] != "session/load" || calls[1] != "session/new" {
		t.Fatalf("calls = %v, want [session/load session/new]", calls)
	}
	if got := r.sessions.get("custom", "conv-1"); got != "sess-1" {
		t.Fatalf("stored id = %q, want sess-1", got)
	}
}

func TestRespawnSkipsLoadWithoutCapability(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a real subprocess")
	}
	_, _, calls := resumeTurn(t, "normal")
	if len(calls) != 1 || calls[0] != "session/new" {
		t.Fatalf("calls = %v, want only session/new", calls)
	}
}

func TestStorePathRejectsTraversalAgentID(t *testing.T) {
	state, ws := t.TempDir(), t.TempDir()
	for _, id := range []string{"..", ".", "../x", ""} {
		p := storePath(state, ws, id)
		if !strings.HasPrefix(p, state) || strings.Contains(p, "..") {
			t.Fatalf("agent %q escaped: %q", id, p)
		}
	}
}

func TestSessionStorePrunesStaleEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	s := newSessionStore(path)
	s.set("p", "old", "x")
	s.mu.Lock()
	s.ids[storeKey("p", "old")] = sessionEntry{ID: "x", Updated: time.Now().Add(-2 * sessionMaxAge)}
	b, _ := json.Marshal(s.ids)
	s.mu.Unlock()
	_ = os.WriteFile(path, b, 0o600)
	s.set("p", "new", "y")
	if got := newSessionStore(path).get("p", "old"); got != "" {
		t.Fatalf("stale entry kept: %q", got)
	}
}
