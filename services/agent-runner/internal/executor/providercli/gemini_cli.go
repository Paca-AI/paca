package providercli

import (
	"fmt"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

const (
	geminiCLIHomeDir      = ".gemini"
	geminiCLISettingsFile = ".gemini/settings.json"
)

// geminiCLI is the Google Gemini CLI adapter. Confidence: MEDIUM —
// ~/.gemini/settings.json with a top-level mcpServers key mirrors the same
// "Claude Desktop config"-derived shape the other JSON-based adapters use,
// which Gemini CLI's own docs describe as compatible.
type geminiCLI struct{}

func init() { Register(geminiCLI{}) }

func (geminiCLI) Name() string        { return "gemini-cli" }
func (geminiCLI) HomeDirName() string { return geminiCLIHomeDir }

func (geminiCLI) APIKeyEnvVar() (string, bool) { return "GEMINI_API_KEY", true }

// AuthStatusCommand: confidence LOW — the least-confirmed of the four,
// and the only one still on a guessed file-existence probe rather than a
// real status subcommand. `gemini --help` shows no auth/login/status
// subcommand at all (confirmed directly — unlike the other three, all of
// which turned out to have one once actually checked against a live
// container). ~/.gemini/oauth_creds.json is a best guess based on Gemini
// CLI's documented OAuth device flow; NEEDS RUNTIME CONFIRMATION against
// a real `gemini` login inside the actual sandbox image.
func (geminiCLI) AuthStatusCommand() []string {
	return []string{"test", "-f", sandboxHomeDir + "/.gemini/oauth_creds.json"}
}

// ParseAuthStatus: plain file-existence check (see AuthStatusCommand) —
// exitCode 0 from `test -f` means the file exists.
func (geminiCLI) ParseAuthStatus(exitCode int, _ string) bool { return exitCode == 0 }

// SupportsSkillSync is false — Gemini CLI DOES have a native "agent
// skills" concept (`gemini skills list/install/link`, confirmed directly
// — this adapter's original assumption that it didn't was wrong), but
// whether dropping SKILL.md files directly into some discovery directory
// is enough, versus needing an explicit `gemini skills install/link`
// registration step per skill, is UNCONFIRMED — worth a real follow-up,
// not guessed here.
func (geminiCLI) SupportsSkillSync() bool { return false }

func (geminiCLI) MergeableFiles() []string { return []string{geminiCLISettingsFile} }

// SyncFiles merges the MCP server list into settings.json's mcpServers key
// — settings.json carries other Gemini CLI settings this sync must not
// clobber, same merge-not-overwrite caution as every other JSON-based
// adapter here.
func (geminiCLI) SyncFiles(existing map[string]string, mcpServers []agent.MCPServer, _ []agent.Skill) ([]FileEntry, error) {
	merged, err := mergeJSONKey(existing[geminiCLISettingsFile], "mcpServers", buildMCPServersJSON(mcpServers))
	if err != nil {
		return nil, fmt.Errorf("gemini-cli: merge %s: %w", geminiCLISettingsFile, err)
	}
	return []FileEntry{{RelPath: geminiCLISettingsFile, Content: merged}}, nil
}
