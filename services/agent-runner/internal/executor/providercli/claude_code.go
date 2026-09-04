package providercli

import (
	"encoding/json"
	"fmt"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

const (
	claudeCodeHomeDir    = ".claude"
	claudeCodeConfigFile = ".claude.json"
)

// claudeCode is the Claude Code CLI adapter. Confidence: MEDIUM-HIGH for
// the MCP/skill config shape — the ~/.claude.json config file and its
// mcpServers key are Claude Code's own well-documented, stable
// conventions, and Skills-as-SKILL.md-files is the exact format
// Anthropic's own "Agent Skills" feature uses (the same one Paca already
// generates for Goose's own skill discovery — see executor/skills.go).
// Auth status (AuthStatusCommand/ParseAuthStatus below) is confidence
// HIGH — confirmed directly against a real container, after an earlier,
// unconfirmed file-probe design turned out to be simply wrong (see that
// method's own doc comment for the live incident that caught it).
type claudeCode struct{}

func init() { Register(claudeCode{}) }

func (claudeCode) Name() string        { return "claude-code" }
func (claudeCode) HomeDirName() string { return claudeCodeHomeDir }

func (claudeCode) APIKeyEnvVar() (string, bool) { return "ANTHROPIC_API_KEY", true }

// AuthStatusCommand: confidence HIGH — confirmed directly inside a real
// environment container. `claude login` (bare) does not exist as a
// command at all (the actual subcommand is `claude auth login`); a
// prior version of this adapter probed for
// ~/.claude/.credentials.json, which turned out to never be created by a
// real, successful login either — that file-existence guess reported
// "not authenticated" even right after the user completed `claude auth
// login`. `claude auth status` is Claude Code's own status subcommand:
// no network call, prints JSON to stdout, exits 1 when logged out.
func (claudeCode) AuthStatusCommand() []string { return []string{"claude", "auth", "status"} }

// ParseAuthStatus reads AuthStatusCommand's JSON output — confirmed shape:
// `{"loggedIn": bool, "authMethod": "...", ...}`. Falls back to false
// (not authenticated) if the output isn't parseable JSON, rather than
// erroring — a malformed/unexpected response is exactly the "can't
// confirm login" case this check exists to surface.
func (claudeCode) ParseAuthStatus(_ int, output string) bool {
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		return false
	}
	return status.LoggedIn
}

func (claudeCode) SupportsSkillSync() bool { return true }

func (claudeCode) MergeableFiles() []string { return []string{claudeCodeConfigFile} }

// SyncFiles merges the MCP server list into ~/.claude.json's top-level
// mcpServers key (the global-scope config — chosen over a project-scoped
// .mcp.json specifically to keep every adapter's output uniformly
// $HOME-relative, and to avoid ever writing into a folder's own git
// checkout the way executor/skills.go's own doc comment explains Goose's
// skills sync avoids for the same reason) and, for every already-filtered,
// frontmatter-ensured skill the caller passes, a real SKILL.md under
// .claude/skills/<name>/ — written verbatim, since the caller
// (executor.syncProviderCLIConfig) already ran it through the same
// prepareFileSkills Goose's own sync uses.
func (claudeCode) SyncFiles(existing map[string]string, mcpServers []agent.MCPServer, skills []agent.Skill) ([]FileEntry, error) {
	merged, err := mergeJSONKey(existing[claudeCodeConfigFile], "mcpServers", buildMCPServersJSON(mcpServers))
	if err != nil {
		return nil, fmt.Errorf("claude-code: merge %s: %w", claudeCodeConfigFile, err)
	}
	files := []FileEntry{{RelPath: claudeCodeConfigFile, Content: merged}}
	for _, s := range skills {
		files = append(files, FileEntry{
			RelPath: claudeCodeHomeDir + "/skills/" + s.SkillName + "/SKILL.md",
			Content: s.SkillContent,
		})
	}
	return files, nil
}
