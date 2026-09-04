package providercli

import (
	"encoding/json"
	"fmt"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

const (
	cursorAgentHomeDir = ".cursor"
	cursorAgentMCPFile = ".cursor/mcp.json"
)

// cursorAgent is the Cursor Agent CLI (`cursor-agent`) adapter. Confidence:
// LOW-MEDIUM for the MCP config shape — ~/.cursor/mcp.json with a
// mcpServers key is the documented format for Cursor's IDE-integrated MCP
// config; whether the standalone cursor-agent CLI reads that exact same
// file/path, versus its own separate location, is the biggest open
// question there. Auth status (AuthStatusCommand/ParseAuthStatus below) is
// confidence HIGH — confirmed directly against a real container. Treated
// as login-only (no APIKeyEnvVar) even though a CURSOR_API_KEY env var is
// confirmed to exist — see that method's own doc comment for why it's not
// wired up yet.
type cursorAgent struct{}

func init() { Register(cursorAgent{}) }

func (cursorAgent) Name() string        { return "cursor-agent" }
func (cursorAgent) HomeDirName() string { return cursorAgentHomeDir }

// APIKeyEnvVar: kept as "not supported" for now even though
// `cursor-agent --help` actually confirms a CURSOR_API_KEY env var exists
// (`--api-key <key> ... can also use CURSOR_API_KEY env var`) —
// contradicts this adapter's original "login-only" assumption, but
// flipping it also means updating agentdom.CLIProvidersWithAPIKeyAuth
// (services/api) and the frontend's hardcoded `!== "cursor-agent"` checks
// in create-agent-dialog.tsx/agent-detail.tsx in lockstep; deliberately
// left as a follow-up rather than bundled into this auth-status fix.
func (cursorAgent) APIKeyEnvVar() (string, bool) { return "", false }

// AuthStatusCommand: confidence HIGH — confirmed directly inside a real
// environment container. `cursor-agent status --format json` is Cursor's
// own status subcommand — note it exits 0 regardless of login state
// (confirmed: "Not logged in" still exits 0), so ParseAuthStatus below
// must read the JSON body, unlike Claude Code/Codex where the exit code
// alone (or the JSON) is enough.
func (cursorAgent) AuthStatusCommand() []string {
	return []string{"cursor-agent", "status", "--format", "json"}
}

// ParseAuthStatus reads AuthStatusCommand's JSON output — confirmed shape:
// `{"status": "...", "isAuthenticated": bool, ...}`.
func (cursorAgent) ParseAuthStatus(_ int, output string) bool {
	var status struct {
		IsAuthenticated bool `json:"isAuthenticated"`
	}
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		return false
	}
	return status.IsAuthenticated
}

// SupportsSkillSync is false — no confirmed native skill-file format for
// cursor-agent as of this writing (Cursor has "Rules", not Agent Skills).
func (cursorAgent) SupportsSkillSync() bool { return false }

func (cursorAgent) MergeableFiles() []string { return []string{cursorAgentMCPFile} }

// SyncFiles merges the MCP server list into ~/.cursor/mcp.json's
// mcpServers key, same merge-not-overwrite caution as every other
// JSON-based adapter here.
func (cursorAgent) SyncFiles(existing map[string]string, mcpServers []agent.MCPServer, _ []agent.Skill) ([]FileEntry, error) {
	merged, err := mergeJSONKey(existing[cursorAgentMCPFile], "mcpServers", buildMCPServersJSON(mcpServers))
	if err != nil {
		return nil, fmt.Errorf("cursor-agent: merge %s: %w", cursorAgentMCPFile, err)
	}
	return []FileEntry{{RelPath: cursorAgentMCPFile, Content: merged}}, nil
}
