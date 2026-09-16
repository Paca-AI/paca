package executor

import (
	"strings"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

// providerAPIKeyEnvVar maps an agent's llm_provider to the env var Goose
// expects the API key under. Best-effort, covering the providers with an
// obvious single env var — llm_catalog.py's Python equivalent covers a much
// larger LiteLLM-backed catalog than this; extend this table as real agents
// hit an unmapped provider rather than trying to enumerate every provider
// up front.
//
// Every key below was cross-checked directly against aaif-goose/goose
// v1.50.1's own provider registry (crates/goose-providers/src/*.rs plus
// .../declarative/definitions/*.json, and crates/goose/src/providers for
// anthropic/openai/google/openrouter/xai, which predate the goose-providers
// crate split) — not assumed from data/llm_models.json, which had drifted
// badly from what Goose can actually run: it was inherited wholesale from a
// generic ~90-provider LiteLLM catalog mixing in non-chat services
// (image/TTS/STT/web-search) and provider ids Goose has never registered.
// data/llm_models.json was trimmed to match this table's keys (2026-09-15,
// alongside the Goose 1.46.0 -> 1.50.1 bump — see services/agent-server/
// Dockerfile) so every option the model picker offers actually routes
// somewhere real; see this table's `gooseProviderID` sibling below for the
// handful whose GOOSE_PROVIDER id differs from Paca's own llm_provider name.
//
// Deliberately NOT added despite being real, working aaif-goose/goose
// providers: bedrock, databricks, azure_foundry, snowflake, sagemaker,
// ollama (local), github_copilot. Each needs more than a single bearer-token
// env var to actually authenticate — an AWS credential chain (bedrock,
// sagemaker), a host+token pair with the host itself required
// (DATABRICKS_HOST, confirmed required in goose-providers/src/databricks.rs;
// AZURE_FOUNDRY_ENDPOINT likewise in azure_foundry.rs), an OAuth
// device-code flow (github_copilot), or no secret at all, just a reachable
// host (ollama, OLLAMA_HOST) — this table and resolveProviderEnv only ever
// plumb through one API-key env var, so wiring any of these up for real
// needs a broader mechanism, not a model-list correction. Unlike "cohere"
// below, there's no single key that would even partially authenticate one
// of these, so they're left out of this map entirely rather than mapped to
// a var that can't work alone; all seven were also removed from
// data/llm_models.json's picker for the same reason "cohere" was kept out
// of it below — selecting one today would otherwise silently misroute
// through the "openai" fallback with an unset OPENAI_API_KEY.
//
// "cohere" is kept here even though Goose cannot actually run it (verified
// directly against aaif-goose/goose: no dedicated Rust provider, no
// declarative JSON definition under crates/goose-providers/src/declarative/
// definitions/ — "cohere" only ever appears there as a model-name prefix
// under other aggregator providers, e.g. an OpenRouter-style
// "cohere/command-r..." model id, never as a standalone GOOSE_PROVIDER
// value). Unlike gemini/deepseek below, there's no id gooseProviderID could
// alias it to that would work — removing the entry instead would silently
// reroute a Cohere-configured agent through the generic openai fallback
// below with OPENAI_API_KEY (almost certainly unset for such an agent),
// trading a clear "unknown provider: cohere" failure for a confusing OpenAI
// auth error. Left as a dead end deliberately, same as the bedrock/
// databricks/etc. reasoning above — cohere was also dropped from
// data/llm_models.json's picker (2026-09-15) since there's no way to
// configure it that actually works.
var providerAPIKeyEnvVar = map[string]string{
	"anthropic":         "ANTHROPIC_API_KEY",
	"openai":            "OPENAI_API_KEY",
	"google":            "GOOGLE_API_KEY",
	"gemini":            "GOOGLE_API_KEY",
	"groq":              "GROQ_API_KEY",
	"mistral":           "MISTRAL_API_KEY",
	"cohere":            "COHERE_API_KEY",
	"deepseek":          "DEEPSEEK_API_KEY",
	"openrouter":        "OPENROUTER_API_KEY",
	"xai":               "XAI_API_KEY",
	"cerebras":          "CEREBRAS_API_KEY",
	"moonshot":          "MOONSHOT_API_KEY",
	"minimax":           "MINIMAX_API_KEY",
	"novita":            "NOVITA_API_KEY",
	"ovhcloud":          "OVHCLOUD_API_KEY",
	"perplexity":        "PERPLEXITY_API_KEY",
	"inception":         "INCEPTION_API_KEY",
	"vercel_ai_gateway": "AI_GATEWAY_API_KEY",
	"zai":               "ZHIPU_API_KEY",
	"fireworks_ai":      "FIREWORKS_API_KEY",
	"friendliai":        "FRIENDLI_API_KEY",
	"together_ai":       "TOGETHER_API_KEY",
	"nvidia_nim":        "NVIDIA_API_KEY",
	"dashscope":         "DASHSCOPE_API_KEY",
	"aiml":              "AIMLAPI_API_KEY",
}

// gooseProviderID translates a Paca llm_provider value onto the provider id
// Goose actually registers it under, for the cases where the two names
// diverge — an unmapped GOOSE_PROVIDER value here means every conversation
// for that provider fails to initialize its LLM provider at all. Every
// entry verified directly against aaif-goose/goose v1.50.1's source, not
// just its docs (which get this wrong — see the "gemini" entry's own
// history):
//   - "gemini" -> "google": Paca accepts "gemini" (matching the model
//     catalog's naming), but Goose's own provider registry
//     (crates/goose-providers/src/google.rs's GOOGLE_PROVIDER_NAME) calls it
//     "google". Confirmed via the provider's own source, not the public
//     docs site — a page claiming to be Goose's provider docs
//     (goose-docs.ai) asserted the internal id is "gemini" itself, which
//     doesn't match any of the three real Gemini-related provider names in
//     the source ("google", "gemini-cli", "gemini_oauth").
//   - "deepseek" -> "custom_deepseek": Goose's declarative provider
//     definition for DeepSeek (declarative/definitions/deepseek.json) is
//     registered under "custom_deepseek", not "deepseek".
//   - "fireworks_ai" -> "fireworks-ai": declarative/definitions/fireworks.json
//     registers a hyphen, not an underscore.
//   - "friendliai" -> "friendli", "together_ai" -> "together",
//     "nvidia_nim" -> "nvidia": Paca kept its own pre-existing catalog key
//     (avoiding a churn-only rename of any agent already configured with
//     the old value) where Goose's declarative definition file registers a
//     shorter or differently-punctuated id.
//   - "dashscope" -> "alibaba": Paca's key names the API product
//     (Alibaba's DashScope), Goose's declarative/definitions/alibaba.json
//     names the vendor; same DASHSCOPE_API_KEY either way.
//   - "aiml" -> "aimlapi": declarative/definitions/aimlapi.json; Paca's
//     shorter pre-existing key is kept as-is for the same no-churn reason
//     as friendliai/together_ai/nvidia_nim above.
var gooseProviderID = map[string]string{
	"gemini":       "google",
	"deepseek":     "custom_deepseek",
	"fireworks_ai": "fireworks-ai",
	"friendliai":   "friendli",
	"together_ai":  "together",
	"nvidia_nim":   "nvidia",
	"dashscope":    "alibaba",
	"aiml":         "aimlapi",
}

// resolveProviderEnv maps a Paca llm_provider value onto the Goose
// GOOSE_PROVIDER value and the env var its API key needs. Providers outside
// the table above fall back to routing through Goose's own "openai"
// provider with an explicit base URL — the same "treat as an OpenAI-
// compatible endpoint" fallback build_llm() uses in builder.py for
// providers outside its own catalog.
func resolveProviderEnv(llmProvider string) (gooseProvider, apiKeyEnvVar string) {
	provider := strings.ToLower(llmProvider)
	envVar, ok := providerAPIKeyEnvVar[provider]
	if !ok {
		return "openai", "OPENAI_API_KEY"
	}
	if alias, ok := gooseProviderID[provider]; ok {
		provider = alias
	}
	return provider, envVar
}

// gooseCLIProviderID translates a Paca cli_provider value onto the
// GOOSE_PROVIDER id Goose actually spawns for it, for the two cases where
// they diverge — the CLI-providers counterpart to gooseProviderID above.
//
//   - "claude-code" -> "claude-acp", "codex" -> "codex-acp": Goose's raw
//     CLI-providers feature (GOOSE_PROVIDER=claude-code|codex) only ever
//     forwards the wrapped CLI's final text — confirmed directly in Goose
//     1.46.0's own source: claude_code.rs's stream() only parses
//     content_block_delta events with delta.type=="text_delta"; codex.rs's
//     extract_text_from_item only extracts item.type=="agent_message".
//     Every tool call the CLI itself makes (Claude's own tool_use content
//     blocks, Codex's own function_call items) is silently dropped before
//     it ever becomes Goose message content, so it can never surface as an
//     ACP tool_call/tool_call_update notification — live incident
//     (2026-09-04, conversation d90f372b-0abc-4d34-90b2-39919614fd8e): the
//     agent's own reply described pagination it must have done via tool
//     calls ("pageSize 5 works, let me continue paginating..."), yet zero
//     tool_call events were ever recorded. Separately, neither raw provider
//     has any real session-resume mechanism (no --resume/--session-id ever
//     passed to the wrapped CLI at spawn), which is what caused the
//     conversation-memory-loss incident this alias map was first built to
//     fix (see this map's prior, reverted claude-code-only version's own
//     history) — Goose's own provider metadata already marks claude-code
//     "[Deprecated: use claude-acp instead]" for exactly this.
//     claude-acp/codex-acp (npm packages @agentclientprotocol/
//     claude-agent-acp / @agentclientprotocol/codex-acp — see
//     services/agent-server/Dockerfile) are real ACP agent implementations
//     that relay their own session/update stream — tool calls included —
//     straight through Goose, and support real session/load, instead of
//     Goose trying to re-derive either from a wrapped CLI's raw output.
//     Verified live: claude-acp correctly recalled a planted value across a
//     resumed session where claude-code lost it every time, against the
//     exact same authenticated `claude` CLI/login state either way. codex
//     wasn't authenticated in the environment used for that verification —
//     this mapping ships on architectural parity with the verified
//     claude-acp path (Goose's own AcpProvider/extension_configs_to_
//     mcp_servers code is shared by both, not two independent
//     implementations), not an equivalent live test of codex-acp itself.
//   - cursor-agent, gemini-cli: no entry, unchanged — Goose has no ACP
//     equivalent for either (confirmed against
//     https://goose-docs.ai/docs/guides/acp-providers's own provider list:
//     only amp-acp/claude-acp/codex-acp/pi-acp exist).
//
// Paca's own cli_provider values ("claude-code", "codex", ...) deliberately
// do NOT change — still what's stored in the DB, shown in the UI, and used
// by providercli's Name()/auth-status/config-sync for all four adapters
// (all of which still target the same underlying CLI binary and its own
// config directory, unaffected by which of Goose's two provider ids drives
// it). Only the GOOSE_PROVIDER env value goes through this alias.
var gooseCLIProviderID = map[string]string{
	"claude-code": "claude-acp",
	"codex":       "codex-acp",
}

// resolveCLIProviderEnv builds the GOOSE_PROVIDER/GOOSE_MODEL/GOOSE_MODE
// env set for a provider_cli agent — the CLI-providers counterpart to
// resolveProviderEnv above, used by buildProviderCLIContainerEnv instead of
// buildAgentContainerEnv's ordinary LLM branch. cfg.CLIProvider is passed
// through to GOOSE_PROVIDER mostly verbatim (confirmed against goose-docs.
// ai's CLI-providers guide that these are literal, undisguised values —
// "claude-code", "codex", "cursor-agent", "gemini-cli") except where
// gooseCLIProviderID above aliases it onto its ACP sibling.
func resolveCLIProviderEnv(cfg agent.Config) map[string]string {
	gooseProvider := cfg.CLIProvider
	if alias, ok := gooseCLIProviderID[gooseProvider]; ok {
		gooseProvider = alias
	}
	env := map[string]string{"GOOSE_PROVIDER": gooseProvider}
	if cfg.CLIModel != "" {
		env["GOOSE_MODEL"] = cfg.CLIModel
	}
	// GOOSE_MODE=auto bypasses interactive permission prompts — confirmed
	// supported for claude-code and codex by the docs; also read by
	// claude-acp's own mode_mapping (GooseMode::Auto -> the ACP session
	// mode "bypassPermissions", confirmed against claude_acp.rs), so this
	// keeps meaning the same thing after the gooseCLIProviderID alias above
	// swaps the underlying provider. Treated as best-effort for
	// cursor-agent/gemini-cli too (an unsupported Goose env var is ignored,
	// not a hard error, for every CLI provider observed so far — worth
	// reconfirming at runtime, not independently verified for those two).
	env["GOOSE_MODE"] = "auto"
	// GOOSE_MODE=auto makes Goose's claude-code provider spawn `claude`
	// with --dangerously-skip-permissions (see aaif-goose/goose's
	// crates/goose/src/providers/claude_code.rs, apply_permission_flags) —
	// and Claude Code CLI refuses that flag outright when running as root:
	// "--dangerously-skip-permissions cannot be used with root/sudo
	// privileges for security reasons", confirmed live (the exact live
	// incident: goose's own read loop then sees immediate EOF on the
	// child's stdout, surfaced to the user as "Claude CLI process
	// terminated unexpectedly"). This whole sandbox image runs as root
	// deliberately (see services/agent-server/Dockerfile's own doc
	// comment — so a conversation can run apt-get/etc. without sudo), so
	// dropping to non-root here isn't an option. IS_SANDBOX=1 is Claude
	// Code's own confirmed escape hatch for exactly this "I know I'm root,
	// I know I'm in an isolated sandbox" case — verified directly: the
	// same invocation that failed without it exits 0 and responds
	// normally with it set. Still needed identically once claude-code is
	// aliased to claude-acp above: claude-agent-acp wraps and ultimately
	// still spawns the same `claude` binary, which enforces the same
	// root check regardless of which Goose provider is driving it. Not
	// independently verified for codex/codex-acp's own root-check behavior
	// (or lack thereof) — harmless either way, since an env var a given CLI
	// doesn't look at is simply ignored.
	env["IS_SANDBOX"] = "1"
	return env
}
