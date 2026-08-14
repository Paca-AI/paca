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
