package executor

import (
	"testing"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

func TestResolveProviderEnv_KnownProvider(t *testing.T) {
	provider, envVar := resolveProviderEnv("Anthropic")
	if provider != "anthropic" || envVar != "ANTHROPIC_API_KEY" {
		t.Errorf("got (%q, %q), want (anthropic, ANTHROPIC_API_KEY)", provider, envVar)
	}
}

func TestResolveProviderEnv_UnknownProviderFallsBackToOpenAI(t *testing.T) {
	provider, envVar := resolveProviderEnv("some-custom-litellm-provider")
	if provider != "openai" || envVar != "OPENAI_API_KEY" {
		t.Errorf("got (%q, %q), want (openai, OPENAI_API_KEY)", provider, envVar)
	}
}

func TestResolveProviderEnv_GeminiMapsToGoosesGoogleProviderID(t *testing.T) {
	provider, envVar := resolveProviderEnv("gemini")
	if provider != "google" || envVar != "GOOGLE_API_KEY" {
		t.Errorf("got (%q, %q), want (google, GOOGLE_API_KEY) — GOOSE_PROVIDER must be Goose's own provider id, not Paca's llm_provider value", provider, envVar)
	}
}

func TestResolveProviderEnv_GoogleStillResolvesToItself(t *testing.T) {
	provider, envVar := resolveProviderEnv("google")
	if provider != "google" || envVar != "GOOGLE_API_KEY" {
		t.Errorf("got (%q, %q), want (google, GOOGLE_API_KEY)", provider, envVar)
	}
}

// TestResolveProviderEnv_DeepseekMapsToGoosesCustomDeepseekProviderID is a
// regression test verified directly against block/goose's declarative
// provider definition (declarative/definitions/deepseek.json), which
// registers DeepSeek under the provider id "custom_deepseek" — passing the
// bare "deepseek" as GOOSE_PROVIDER fails to initialize, exactly like the
// unmapped "gemini" case this same alias table already fixes.
func TestResolveProviderEnv_DeepseekMapsToGoosesCustomDeepseekProviderID(t *testing.T) {
	provider, envVar := resolveProviderEnv("deepseek")
	if provider != "custom_deepseek" || envVar != "DEEPSEEK_API_KEY" {
		t.Errorf("got (%q, %q), want (custom_deepseek, DEEPSEEK_API_KEY)", provider, envVar)
	}
}

// TestResolveProviderEnv_MistralResolvesToItself confirms mistral needs no
// alias — verified against declarative/definitions/mistral.json, whose
// provider id is the plain "mistral", matching Paca's own llm_provider
// value for it.
func TestResolveProviderEnv_MistralResolvesToItself(t *testing.T) {
	provider, envVar := resolveProviderEnv("mistral")
	if provider != "mistral" || envVar != "MISTRAL_API_KEY" {
		t.Errorf("got (%q, %q), want (mistral, MISTRAL_API_KEY)", provider, envVar)
	}
}

// TestResolveCLIProviderEnv_SetsISSandbox is a regression test for a live
// incident: GOOSE_MODE=auto makes Goose's claude-code provider spawn
// `claude` with --dangerously-skip-permissions (see
// aaif-goose/goose's crates/goose/src/providers/claude_code.rs,
// apply_permission_flags), and Claude Code CLI refuses that flag outright
// when running as root — confirmed live: "--dangerously-skip-permissions
// cannot be used with root/sudo privileges for security reasons", which
// surfaced to the user as "Claude CLI process terminated unexpectedly"
// (Goose's read loop sees immediate EOF on the child's closed stdout).
// This entire sandbox image runs as root deliberately (so a conversation
// can run apt-get/etc. without sudo — see
// services/agent-server/Dockerfile's own doc comment), so IS_SANDBOX=1 —
// Claude Code's own confirmed escape hatch for "I know I'm root, I know
// I'm in an isolated sandbox" — must always be set for every provider_cli
// agent, not just Claude Code (harmless for a CLI that doesn't look at
// it). Uses cursor-agent specifically (an unmapped provider — see
// TestResolveCLIProviderEnv_UnmappedProvidersPassThroughUnchanged below) so
// this test's own pass/fail is independent of the claude-code/codex ACP
// aliasing covered separately.
func TestResolveCLIProviderEnv_SetsISSandbox(t *testing.T) {
	env := resolveCLIProviderEnv(agent.Config{CLIProvider: "cursor-agent", CLIModel: "sonnet"})
	if env["IS_SANDBOX"] != "1" {
		t.Errorf("resolveCLIProviderEnv IS_SANDBOX = %q, want \"1\"", env["IS_SANDBOX"])
	}
	if env["GOOSE_PROVIDER"] != "cursor-agent" {
		t.Errorf("resolveCLIProviderEnv GOOSE_PROVIDER = %q, want cursor-agent", env["GOOSE_PROVIDER"])
	}
	if env["GOOSE_MODEL"] != "sonnet" {
		t.Errorf("resolveCLIProviderEnv GOOSE_MODEL = %q, want sonnet", env["GOOSE_MODEL"])
	}
	if env["GOOSE_MODE"] != "auto" {
		t.Errorf("resolveCLIProviderEnv GOOSE_MODE = %q, want auto", env["GOOSE_MODE"])
	}
}

// TestResolveCLIProviderEnv_ClaudeCodeAndCodexUseACPProviders is a
// regression test for a live incident: a Paca conversation using the
// claude-code cli_provider lost all memory of prior turns on every new
// message, and separately never surfaced any tool_call/tool_call_update
// events even on turns where the agent's own reply made clear it had used
// tools — both traced directly to Goose 1.46.0's own source for the raw
// claude-code/codex providers (see gooseCLIProviderID's own doc comment for
// the full trace and citations) and fixed by aliasing onto their real ACP
// implementations instead. cfg.CLIProvider itself — Paca's own stable
// domain value, used elsewhere for auth-status checks and config sync —
// must NOT change; only the GOOSE_PROVIDER env value goes through the
// alias.
func TestResolveCLIProviderEnv_ClaudeCodeAndCodexUseACPProviders(t *testing.T) {
	cases := []struct {
		cliProvider  string
		wantProvider string
	}{
		{"claude-code", "claude-acp"},
		{"codex", "codex-acp"},
	}
	for _, c := range cases {
		env := resolveCLIProviderEnv(agent.Config{CLIProvider: c.cliProvider, CLIModel: "sonnet"})
		if env["GOOSE_PROVIDER"] != c.wantProvider {
			t.Errorf("resolveCLIProviderEnv(%q) GOOSE_PROVIDER = %q, want %q (goose's raw %s provider is deprecated, drops tool calls, and has no real session resume)",
				c.cliProvider, env["GOOSE_PROVIDER"], c.wantProvider, c.cliProvider)
		}
		if env["IS_SANDBOX"] != "1" {
			t.Errorf("resolveCLIProviderEnv(%q) IS_SANDBOX = %q, want \"1\"", c.cliProvider, env["IS_SANDBOX"])
		}
		if env["GOOSE_MODEL"] != "sonnet" {
			t.Errorf("resolveCLIProviderEnv(%q) GOOSE_MODEL = %q, want sonnet", c.cliProvider, env["GOOSE_MODEL"])
		}
	}
}

// TestResolveCLIProviderEnv_UnmappedProvidersPassThroughUnchanged guards
// against cursor-agent/gemini-cli silently picking up an ACP alias they
// don't have — Goose has no ACP equivalent for either (confirmed against
// https://goose-docs.ai/docs/guides/acp-providers's own provider list: only
// amp-acp/claude-acp/codex-acp/pi-acp exist). This is exactly the class of
// bug a mixed-up case order caused elsewhere in this codebase (agent-picker.
// tsx's useEnvironmentPicker briefly matched isChat before provider_cli, so
// a provider_cli conversation's resume lookup silently checked the wrong
// registry) — worth its own explicit guard here too.
func TestResolveCLIProviderEnv_UnmappedProvidersPassThroughUnchanged(t *testing.T) {
	for _, provider := range []string{"cursor-agent", "gemini-cli"} {
		env := resolveCLIProviderEnv(agent.Config{CLIProvider: provider})
		if env["GOOSE_PROVIDER"] != provider {
			t.Errorf("resolveCLIProviderEnv(%q) GOOSE_PROVIDER = %q, want unchanged %q — no ACP equivalent exists for this provider",
				provider, env["GOOSE_PROVIDER"], provider)
		}
	}
}
