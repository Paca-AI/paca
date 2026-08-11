package acp

import (
	"encoding/json"
	"testing"
)

// TestMCPServerConfigWireShape pins the wire format confirmed against ACP's
// real schema.json — an earlier version of this struct (no "type"
// discriminator, Env as a JSON object instead of an array) produced a
// request that made a real goose serve's session/new hang forever instead
// of returning an error. See docs/ai-agent/goose-migration.md and
// executor.go's buildMCPServers.
func TestMCPServerConfigWireShape(t *testing.T) {
	args := []string{}
	envVal := []EnvVariable{{Name: "PACA_API_KEY", Value: "secret"}}
	cfg := MCPServerConfig{
		Type:    McpServerStdio,
		Name:    "paca",
		Command: "/usr/bin/paca",
		Args:    &args,
		Env:     &envVal,
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded["type"] != "stdio" {
		t.Errorf(`"type" = %v, want "stdio" — the schema's required discriminator`, decoded["type"])
	}

	// Confirmed live against a real goose serve: an omitted "args" (Go's
	// omitempty silently drops an empty-but-non-nil slice, same as it
	// would a nil one) makes session/new either hang indefinitely or
	// return a 200 whose SSE stream never carries a response — neither
	// surfaces as a JSON-RPC error. Must be present, even when empty.
	if _, present := decoded["args"]; !present {
		t.Error(`"args" field missing — schema.json marks it required for the stdio variant, confirmed live (a missing "args" makes session/new hang or silently stall)`)
	}

	env, ok := decoded["env"].([]any)
	if !ok {
		t.Fatalf(`"env" = %T, want a JSON array (schema.json's McpServerStdio.env is EnvVariable[], not an object)`, decoded["env"])
	}
	if len(env) != 1 {
		t.Fatalf("env has %d entries, want 1", len(env))
	}
	entry, ok := env[0].(map[string]any)
	if !ok || entry["name"] != "PACA_API_KEY" || entry["value"] != "secret" {
		t.Errorf("env[0] = %v, want {name: PACA_API_KEY, value: secret}", env[0])
	}
}

// TestMCPServerConfigStdio_OmittedArgsOrEnvAreDroppedByOmitempty is a
// regression test for the exact live-confirmed bug TestMCPServerConfigWireShape's
// "args" assertion guards against: Args/Env being plain slices (rather than
// pointers, like Headers already was) meant an empty-but-non-nil slice and
// a nil one were indistinguishable to encoding/json's omitempty, so a
// caller that forgot to explicitly allocate one could accidentally send a
// stdio entry with "args"/"env" missing entirely with no compiler or
// runtime signal — exactly what happened here.
func TestMCPServerConfigStdio_OmittedArgsOrEnvAreDroppedByOmitempty(t *testing.T) {
	cfg := MCPServerConfig{Type: McpServerStdio, Name: "x", Command: "/bin/x"}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// This is what NOT setting Args/Env produces — documenting the failure
	// mode, not endorsing it: every real caller (executor.go's
	// buildMCPServers) must always set both explicitly for a stdio entry.
	if _, present := decoded["args"]; present {
		t.Error(`unexpected "args" present with Args left nil — this test documents the zero-value case buildMCPServers must never hit, not a desired shape`)
	}
	if _, present := decoded["env"]; present {
		t.Error(`unexpected "env" present with Env left nil — this test documents the zero-value case buildMCPServers must never hit, not a desired shape`)
	}
}

func TestMCPServerConfigHTTP_IncludesRequiredHeadersField(t *testing.T) {
	cfg := MCPServerConfig{
		Type:    McpServerHTTP,
		Name:    "remote",
		URL:     "https://example.com/mcp",
		Headers: &[]HTTPHeader{},
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// schema.json requires "headers" on McpServerHttp even when empty — a
	// nil/omitted slice here would omit the field entirely (omitempty).
	if _, present := decoded["headers"]; !present {
		t.Error(`"headers" field missing — schema.json marks it required for the http variant`)
	}
}
