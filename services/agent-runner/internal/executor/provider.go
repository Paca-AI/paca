package executor

import "strings"

// providerAPIKeyEnvVar maps an agent's llm_provider to the env var Goose
// expects the API key under. Best-effort, covering the providers with an
// obvious single env var — llm_catalog.py's Python equivalent covers a much
// larger LiteLLM-backed catalog than this; extend this table as real agents
// hit an unmapped provider rather than trying to enumerate every provider
// up front.
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

// resolveProviderEnv maps a Paca llm_provider value onto the Goose
// GOOSE_PROVIDER value and the env var its API key needs. Providers outside
// the table above fall back to routing through Goose's own "openai"
// provider with an explicit base URL — the same "treat as an OpenAI-
// compatible endpoint" fallback build_llm() uses in builder.py for
// providers outside its own catalog.
func resolveProviderEnv(llmProvider string) (gooseProvider, apiKeyEnvVar string) {
	provider := strings.ToLower(llmProvider)
	if envVar, ok := providerAPIKeyEnvVar[provider]; ok {
		return provider, envVar
	}
	return "openai", "OPENAI_API_KEY"
}
