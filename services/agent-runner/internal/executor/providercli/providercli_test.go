package providercli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

func TestMergeJSONKeyPreservesUnrelatedKeys(t *testing.T) {
	existing := `{
		"oauthAccount": {"email": "user@example.com"},
		"mcpServers": {"stale": {"type": "stdio", "command": "old"}},
		"userID": "abc123"
	}`
	merged, err := mergeJSONKey(existing, "mcpServers", map[string]mcpServerJSON{
		"fresh": {Type: "stdio", Command: "new"},
	})
	if err != nil {
		t.Fatalf("mergeJSONKey: %v", err)
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(merged), &out); err != nil {
		t.Fatalf("unmarshal merged output: %v", err)
	}
	if _, ok := out["oauthAccount"]; !ok {
		t.Error("oauthAccount was dropped by the merge")
	}
	if _, ok := out["userID"]; !ok {
		t.Error("userID was dropped by the merge")
	}
	var servers map[string]mcpServerJSON
	if err := json.Unmarshal(out["mcpServers"], &servers); err != nil {
		t.Fatalf("unmarshal mcpServers: %v", err)
	}
	if _, ok := servers["stale"]; ok {
		t.Error("stale server entry should have been replaced wholesale, not merged itself")
	}
	if servers["fresh"].Command != "new" {
		t.Errorf("fresh server entry missing or wrong: %+v", servers["fresh"])
	}
}

func TestMergeJSONKeyEmptyExisting(t *testing.T) {
	merged, err := mergeJSONKey("", "mcpServers", map[string]mcpServerJSON{
		"only": {Type: "stdio", Command: "cmd"},
	})
	if err != nil {
		t.Fatalf("mergeJSONKey: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(merged), &out); err != nil {
		t.Fatalf("unmarshal merged output: %v", err)
	}
	if _, ok := out["mcpServers"]; !ok {
		t.Error("mcpServers key missing from freshly-created file")
	}
}

func TestBuildMCPServersJSONSkipsDisabledAndOAuth(t *testing.T) {
	servers := buildMCPServersJSON([]agent.MCPServer{
		{ServerName: "enabled-stdio", Transport: "stdio", Command: "run", IsEnabled: true},
		{ServerName: "disabled", Transport: "stdio", Command: "run", IsEnabled: false},
		{ServerName: "oauth-one", Transport: "oauth", IsEnabled: true},
		{ServerName: "remote", Transport: "http", URL: "https://example.com/mcp", IsEnabled: true},
	})
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers (disabled and oauth skipped), got %d: %+v", len(servers), servers)
	}
	if _, ok := servers["enabled-stdio"]; !ok {
		t.Error("enabled-stdio missing")
	}
	if _, ok := servers["remote"]; !ok {
		t.Error("remote missing")
	}
	if servers["remote"].Type != "http" || servers["remote"].URL != "https://example.com/mcp" {
		t.Errorf("remote entry wrong shape: %+v", servers["remote"])
	}
}

func TestClaudeCodeSyncFiles(t *testing.T) {
	a := claudeCode{}
	files, err := a.SyncFiles(
		map[string]string{claudeCodeConfigFile: `{"userID": "keep-me", "mcpServers": {}}`},
		[]agent.MCPServer{{ServerName: "srv", Transport: "stdio", Command: "run", IsEnabled: true}},
		[]agent.Skill{{SkillName: "my-skill", SkillContent: "---\nname: my-skill\n---\n\nBody."}},
	)
	if err != nil {
		t.Fatalf("SyncFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files (config + 1 skill), got %d: %+v", len(files), files)
	}

	var configFile *FileEntry
	var skillFile *FileEntry
	for i := range files {
		switch files[i].RelPath {
		case claudeCodeConfigFile:
			configFile = &files[i]
		case ".claude/skills/my-skill/SKILL.md":
			skillFile = &files[i]
		}
	}
	if configFile == nil {
		t.Fatalf("no %s in output: %+v", claudeCodeConfigFile, files)
	}
	if !strings.Contains(configFile.Content, "keep-me") {
		t.Errorf("existing userID was dropped: %s", configFile.Content)
	}
	if !strings.Contains(configFile.Content, `"srv"`) {
		t.Errorf("new mcp server missing: %s", configFile.Content)
	}
	if skillFile == nil {
		t.Fatalf("skill file missing from output: %+v", files)
	}
	if skillFile.Content != "---\nname: my-skill\n---\n\nBody." {
		t.Errorf("skill content not written verbatim: %q", skillFile.Content)
	}
}

func TestCodexSyncFilesPreservesUnrelatedTOMLKeys(t *testing.T) {
	a := codex{}
	existing := "model = \"gpt-5.2-codex\"\n\n[mcp_servers.stale]\ncommand = \"old\"\n"
	files, err := a.SyncFiles(
		map[string]string{codexConfigFile: existing},
		[]agent.MCPServer{{ServerName: "fresh", Transport: "stdio", Command: "new-cmd", Args: []string{"--flag"}, IsEnabled: true}},
		nil,
	)
	if err != nil {
		t.Fatalf("SyncFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	out := files[0].Content
	if !strings.Contains(out, `model = "gpt-5.2-codex"`) {
		t.Errorf("unrelated top-level key dropped: %s", out)
	}
	if strings.Contains(out, "stale") {
		t.Errorf("stale server entry should have been replaced wholesale: %s", out)
	}
	if !strings.Contains(out, "fresh") || !strings.Contains(out, "new-cmd") {
		t.Errorf("fresh server entry missing: %s", out)
	}
}

func TestCodexSyncFilesSkipsNonStdio(t *testing.T) {
	a := codex{}
	files, err := a.SyncFiles(nil, []agent.MCPServer{
		{ServerName: "remote", Transport: "http", URL: "https://example.com", IsEnabled: true},
	}, nil)
	if err != nil {
		t.Fatalf("SyncFiles: %v", err)
	}
	if strings.Contains(files[0].Content, "remote") {
		t.Errorf("http-transport server should have been skipped for codex (no known TOML equivalent): %s", files[0].Content)
	}
}

// TestParseAuthStatus_RealCapturedOutput locks down every adapter's
// ParseAuthStatus against output actually captured from a live container
// running each CLI — not hand-typed guesses. This is exactly the class of
// bug that shipped once already: an earlier, unconfirmed file-existence
// probe for Claude Code reported "not authenticated" even right after a
// real, successful `claude auth login`, because the guessed credential
// path was simply wrong.
func TestParseAuthStatus_RealCapturedOutput(t *testing.T) {
	cases := []struct {
		name     string
		adapter  Adapter
		exitCode int
		output   string
		want     bool
	}{
		{
			name:     "claude-code logged out",
			adapter:  claudeCode{},
			exitCode: 1,
			output:   `{"loggedIn": false, "authMethod": "none", "apiProvider": "firstParty", "analyticsDisabled": false, "projectsDirectory": "/home/goose/.claude/projects"}`,
			want:     false,
		},
		{
			name:     "claude-code logged in",
			adapter:  claudeCode{},
			exitCode: 0,
			output:   `{"loggedIn": true, "authMethod": "oauth", "apiProvider": "firstParty"}`,
			want:     true,
		},
		{
			name:     "claude-code malformed output treated as not authenticated",
			adapter:  claudeCode{},
			exitCode: 0,
			output:   "not json at all",
			want:     false,
		},
		{
			name:     "codex logged out (exit 1)",
			adapter:  codex{},
			exitCode: 1,
			output:   "Not logged in",
			want:     false,
		},
		{
			name:     "codex logged in (exit 0)",
			adapter:  codex{},
			exitCode: 0,
			output:   "Logged in using ChatGPT",
			want:     true,
		},
		{
			name:     "cursor-agent logged out (still exits 0 — must read JSON, not exit code)",
			adapter:  cursorAgent{},
			exitCode: 0,
			output:   `{"status": "unauthenticated", "isAuthenticated": false, "hasAccessToken": false, "hasRefreshToken": false, "message": "Not logged in"}`,
			want:     false,
		},
		{
			name:     "cursor-agent logged in",
			adapter:  cursorAgent{},
			exitCode: 0,
			output:   `{"status": "authenticated", "isAuthenticated": true, "hasAccessToken": true, "hasRefreshToken": true}`,
			want:     true,
		},
		{
			name:     "gemini-cli file-probe: exists",
			adapter:  geminiCLI{},
			exitCode: 0,
			output:   "",
			want:     true,
		},
		{
			name:     "gemini-cli file-probe: missing",
			adapter:  geminiCLI{},
			exitCode: 1,
			output:   "",
			want:     false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.adapter.ParseAuthStatus(c.exitCode, c.output); got != c.want {
				t.Errorf("%s.ParseAuthStatus(%d, %q) = %v, want %v", c.adapter.Name(), c.exitCode, c.output, got, c.want)
			}
		})
	}
}

func TestAuthStatusCommand_UsesRealSubcommandsNotGuessedFilePaths(t *testing.T) {
	// Regression guard: claude-code, codex, and cursor-agent all have
	// confirmed real status subcommands (see each adapter's own doc
	// comment) — none of them should silently regress back to a `test -f`
	// guess.
	cases := []struct {
		adapter Adapter
		want    []string
	}{
		{claudeCode{}, []string{"claude", "auth", "status"}},
		{codex{}, []string{"codex", "login", "status"}},
		{cursorAgent{}, []string{"cursor-agent", "status", "--format", "json"}},
	}
	for _, c := range cases {
		got := c.adapter.AuthStatusCommand()
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%s.AuthStatusCommand() = %v, want %v", c.adapter.Name(), got, c.want)
		}
	}
}

func TestRegistryHasAllFourProviders(t *testing.T) {
	for _, name := range []string{"claude-code", "codex", "cursor-agent", "gemini-cli"} {
		if _, ok := Get(name); !ok {
			t.Errorf("provider %q not registered", name)
		}
	}
}
