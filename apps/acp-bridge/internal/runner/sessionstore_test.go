package runner

import "testing"

func TestSessionStorePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	newSessionStore(dir).set("claude-code", "task:1", "sess-abc")
	if got := newSessionStore(dir).get("claude-code", "task:1"); got != "sess-abc" {
		t.Fatalf("got %q", got)
	}
	if got := newSessionStore(dir).get("codex", "task:1"); got != "" {
		t.Fatalf("provider isolation broken: %q", got)
	}
}
