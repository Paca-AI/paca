package providercli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

// mcpServerJSON is the "Claude Desktop config"-derived MCP server entry
// shape — Claude Code, Gemini CLI, and Cursor's own JSON MCP config files
// all converged on essentially these same fields (stdio: command/args/env;
// remote: type/url).
type mcpServerJSON struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
}

// buildMCPServersJSON renders enabled MCP servers into the shared
// mcpServerJSON shape, keyed by server name. oauth-transport servers are
// skipped — no equivalent here, matching executor.buildMCPServers, which
// skips them for Goose's own ACP-level MCP list too (no ACP equivalent
// either).
func buildMCPServersJSON(servers []agent.MCPServer) map[string]mcpServerJSON {
	out := map[string]mcpServerJSON{}
	for _, s := range servers {
		if !s.IsEnabled {
			continue
		}
		switch s.Transport {
		case "stdio":
			out[s.ServerName] = mcpServerJSON{Type: "stdio", Command: s.Command, Args: s.Args, Env: s.Env}
		case "http", "sse":
			out[s.ServerName] = mcpServerJSON{Type: s.Transport, URL: s.URL}
		}
	}
	return out
}

// mergeJSONKey parses existing (a JSON object; "" means the file doesn't
// exist yet) as a map of top-level keys, replaces/sets key to value, and
// returns the whole object re-marshaled, indented for a human who might
// open this file directly. Every other top-level key in existing is
// preserved byte-for-byte via json.RawMessage rather than round-tripped
// through `any` — critical for a file like Claude Code's ~/.claude.json,
// which also carries fragile state (oauthAccount, per-project settings,
// etc.) this sync must never touch or risk subtly retyping.
func mergeJSONKey(existing string, key string, value any) (string, error) {
	obj := map[string]json.RawMessage{}
	if strings.TrimSpace(existing) != "" {
		if err := json.Unmarshal([]byte(existing), &obj); err != nil {
			return "", fmt.Errorf("parse existing json: %w", err)
		}
	}
	valBytes, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal %s: %w", key, err)
	}
	obj[key] = valBytes
	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal merged json: %w", err)
	}
	return string(out), nil
}
