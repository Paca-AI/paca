package executor

import "testing"

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
