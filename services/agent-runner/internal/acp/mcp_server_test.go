package acp

import (
	"encoding/json"
	"testing"
)

func TestGooseHTTPServerWireShape(t *testing.T) {
	for _, headers := range [][]HTTPHeader{
		{},
		{{Name: "User-Agent", Value: "Paca/agent-runner (+https://github.com/Paca-AI/paca)"}},
	} {
		ext, ok := mcpServerToGooseExtension(MCPServerConfig{
			Type: McpServerHTTP, Name: "parallel-search", URL: "https://search.parallel.ai/mcp", Headers: &headers,
		})
		if !ok {
			t.Fatal("HTTP server was dropped")
		}
		raw, err := json.Marshal(ext)
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Type   string `json:"type"`
			Server struct {
				Type    string       `json:"type"`
				Name    string       `json:"name"`
				URL     string       `json:"url"`
				Headers []HTTPHeader `json:"headers"`
			} `json:"server"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Type != "mcp" || decoded.Server.Type != "http" || decoded.Server.Name != "parallel-search" || decoded.Server.URL != "https://search.parallel.ai/mcp" || decoded.Server.Headers == nil || len(decoded.Server.Headers) != len(headers) {
			t.Fatalf("invalid Goose HTTP extension: %s", raw)
		}
		for i, h := range headers {
			if decoded.Server.Headers[i] != h {
				t.Fatalf("HTTP header changed on the wire: %s", raw)
			}
		}
	}
}

// TestMCPServerConfigWireShape pins the wire format confirmed against ACP's
// real schema.json — an earlier version of this struct (no "type"
// discriminator, Env as a JSON object instead of an array) produced a
// request that made a real goose serve's session/new hang forever instead
// of returning an error. See executor.go's buildMCPServers.
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
