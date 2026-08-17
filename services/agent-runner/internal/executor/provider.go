package executor

import "strings"

// providerAPIKeyEnvVar maps an agent's llm_provider to the env var Goose
// expects the API key under. Best-effort, covering the providers with an
// obvious single env var — llm_catalog.py's Python equivalent covers a much
// larger LiteLLM-backed catalog than this; extend this table as real agents
// hit an unmapped provider rather than trying to enumerate every provider
// up front.
//
// "cohere" is kept here even though Goose cannot actually run it (verified
// directly against block/goose: no dedicated Rust provider, no declarative
// JSON definition under crates/goose-providers/src/declarative/definitions/
// — "cohere" only ever appears there as a model-name prefix under other
// aggregator providers, e.g. an OpenRouter-style "cohere/command-r..." model
// id, never as a standalone GOOSE_PROVIDER value). Unlike gemini/deepseek
// below, there's no id gooseProviderID could alias it to that would work —
// removing the entry instead would silently reroute a Cohere-configured
// agent through the generic openai fallback below with OPENAI_API_KEY
// (almost certainly unset for such an agent), trading a clear "unknown
// provider: cohere" failure for a confusing OpenAI auth error. Left as a
// dead end deliberately: Paca's data/llm_models.json inherited "cohere"
// from the old LiteLLM-backed catalog, and removing it from the model
// picker (not resolvable here) is the actual fix.
var providerAPIKeyEnvVar = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"google":     "GOOGLE_API_KEY",
	"gemini":     "GOOGLE_API_KEY",
	"groq":       "GROQ_API_KEY",
	"mistral":    "MISTRAL_API_KEY",
	"cohere":     "COHERE_API_KEY",
	"deepseek":   "DEEPSEEK_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
	"xai":        "XAI_API_KEY",
}

// gooseProviderID translates a Paca llm_provider value onto the provider id
// Goose actually registers it under, for the cases where the two names
// diverge — an unmapped GOOSE_PROVIDER value here means every conversation
// for that provider fails to initialize its LLM provider at all. Both
// entries verified directly against block/goose's source, not just its
// docs (which get this wrong — see the "gemini" entry's own history):
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
var gooseProviderID = map[string]string{
	"gemini":   "google",
	"deepseek": "custom_deepseek",
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
