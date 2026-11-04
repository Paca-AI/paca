package provider

import (
	"reflect"
	"testing"
)

func TestResolveBuiltinProviderUsesDefaultCommand(t *testing.T) {
	cmd, err := ResolveCommand("claude-code", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd[0] != "npx" || cmd[1] != "-y" {
		t.Fatalf("command = %v, want it to start with [npx -y ...]", cmd)
	}
}

func TestResolveGooseUsesLocalOverride(t *testing.T) {
	// goose isn't an OpenHands built-in — this is a local addition, so it's
	// tested for the exact literal command rather than a prefix.
	cmd, err := ResolveCommand("goose", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"goose", "acp"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("command = %v, want %v", cmd, want)
	}
}

func TestResolveNamedProviderIgnoresExplicitCommand(t *testing.T) {
	// A named built-in's default always wins over an explicit command —
	// only "custom" ever consults it.
	cmd, err := ResolveCommand("goose", []string{"should-be-ignored"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"goose", "acp"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("command = %v, want %v", cmd, want)
	}
}

func TestResolveCustomProviderUsesExplicitCommand(t *testing.T) {
	cmd, err := ResolveCommand("custom", []string{"my-server", "--flag"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"my-server", "--flag"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("command = %v, want %v", cmd, want)
	}
}

func TestResolveCustomProviderWithoutCommandErrors(t *testing.T) {
	if _, err := ResolveCommand("custom", nil); err == nil {
		t.Fatal("expected an error for custom provider with no explicit command")
	}
}

func TestResolveUnknownProviderFallsBackToExplicitCommand(t *testing.T) {
	cmd, err := ResolveCommand("not-a-real-provider", []string{"fallback-cmd"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"fallback-cmd"}
	if !reflect.DeepEqual(cmd, want) {
		t.Fatalf("command = %v, want %v", cmd, want)
	}
}

func TestResolveOpenCodeDefaultCommandWinsOverExplicit(t *testing.T) {
	want := []string{"opencode", "acp"}
	for _, explicit := range [][]string{nil, {"should-be-ignored"}} {
		cmd, err := ResolveCommand("opencode", explicit)
		if err != nil {
			t.Fatalf("ResolveCommand: %v", err)
		}
		if !reflect.DeepEqual(cmd, want) {
			t.Errorf("ResolveCommand(opencode, %v) = %v, want %v", explicit, cmd, want)
		}
	}
}

func TestResolveUnknownProviderWithoutExplicitCommandErrors(t *testing.T) {
	if _, err := ResolveCommand("not-a-real-provider", nil); err == nil {
		t.Fatal("expected an error for an unknown provider with no explicit command")
	}
}

func TestPermissionModeKnownAndUnknownProviders(t *testing.T) {
	cases := map[string]string{
		"claude-code": "bypassPermissions",
		"codex":       "agent-full-access",
		"gemini-cli":  "default",
		"goose":       "",
		"opencode":    "",
		"custom":      "",
		"unknown":     "",
	}
	for provider, want := range cases {
		if got := PermissionMode(provider); got != want {
			t.Errorf("PermissionMode(%q) = %q, want %q", provider, got, want)
		}
	}
}

func TestModelEnv(t *testing.T) {
	if got := ModelEnv("claude-code", "my-model"); len(got) != 1 || got[0] != "ANTHROPIC_MODEL=my-model" {
		t.Errorf("claude-code ModelEnv = %v", got)
	}
	if got := ModelEnv("claude-code", ""); got != nil {
		t.Errorf("empty model must add no env, got %v", got)
	}
	if got := ModelEnv("codex", "gpt-5"); got != nil {
		t.Errorf("codex has no env mechanism, got %v", got)
	}
}
