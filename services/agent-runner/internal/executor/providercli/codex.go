package providercli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

const (
	codexHomeDir    = ".codex"
	codexConfigFile = ".codex/config.toml"
)

// codex is the OpenAI Codex CLI adapter. Confidence: MEDIUM for the MCP
// config shape — $HOME/.codex/config.toml with [mcp_servers.<name>]
// tables is documented in its own CLI docs as of this writing, but the
// exact table shape (command/args/env keys) should be spot-checked
// against the actual installed Codex CLI version before shipping. Auth
// status (AuthStatusCommand/ParseAuthStatus below) is confidence HIGH —
// confirmed directly against a real container.
type codex struct{}

func init() { Register(codex{}) }

func (codex) Name() string        { return "codex" }
func (codex) HomeDirName() string { return codexHomeDir }

func (codex) APIKeyEnvVar() (string, bool) { return "OPENAI_API_KEY", true }

// AuthStatusCommand: confidence HIGH — confirmed directly inside a real
// environment container. `codex login status` is Codex's own status
// subcommand: no network call, prints plain text ("Not logged in" /
// logged-in details), exits 1 when logged out.
func (codex) AuthStatusCommand() []string { return []string{"codex", "login", "status"} }

// ParseAuthStatus trusts the exit code alone — confirmed exit 1 for "Not
// logged in"; this is a dedicated status subcommand, so a 0 exit is taken
// as authenticated without also pattern-matching the text (which isn't
// confirmed stable across releases the way the exit code convention is).
func (codex) ParseAuthStatus(exitCode int, _ string) bool { return exitCode == 0 }

// SupportsSkillSync is false — see the package doc comment: Codex's native
// skill-file format (if any) isn't confirmed with enough confidence to
// sync blindly in this version.
func (codex) SupportsSkillSync() bool { return false }

func (codex) MergeableFiles() []string { return []string{codexConfigFile} }

// SyncFiles merges the MCP server list into config.toml's
// [mcp_servers.<name>] tables via a real TOML library (parse-modify-
// rewrite, not string templating) — config.toml commonly carries other
// user/CLI-set keys (model, sandbox policy, etc.) this sync must never
// clobber. Only stdio-transport servers are written: Codex's mcp_servers
// config is a launch-command config (command/args/env) with no known
// remote/URL equivalent as of this writing.
func (codex) SyncFiles(existing map[string]string, mcpServers []agent.MCPServer, _ []agent.Skill) ([]FileEntry, error) {
	doc := map[string]any{}
	if raw := existing[codexConfigFile]; strings.TrimSpace(raw) != "" {
		if _, err := toml.Decode(raw, &doc); err != nil {
			return nil, fmt.Errorf("codex: parse existing %s: %w", codexConfigFile, err)
		}
	}

	servers := map[string]any{}
	for _, s := range mcpServers {
		if !s.IsEnabled || s.Transport != "stdio" {
			continue
		}
		entry := map[string]any{"command": s.Command}
		if len(s.Args) > 0 {
			entry["args"] = s.Args
		}
		if len(s.Env) > 0 {
			entry["env"] = s.Env
		}
		servers[s.ServerName] = entry
	}
	doc["mcp_servers"] = servers

	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return nil, fmt.Errorf("codex: encode %s: %w", codexConfigFile, err)
	}
	return []FileEntry{{RelPath: codexConfigFile, Content: buf.String()}}, nil
}
