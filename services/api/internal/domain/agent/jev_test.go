package agentdom

import "testing"

func TestAgentComposeJevDescription(t *testing.T) {
	acp := "claude-code"
	cli := "codex"
	cases := []struct {
		name  string
		agent Agent
		want  string
	}{
		{"name fallback", Agent{Name: "Bot"}, "Bot"},
		{"description only", Agent{Name: "Bot", Description: "  Fixes bugs  "}, "Fixes bugs"},
		{"provider only", Agent{Name: "Bot", LLMProvider: "anthropic"}, "Provider: anthropic"},
		{"provider and model", Agent{Name: "Bot", Description: "Fixes bugs", LLMProvider: "anthropic", LLMModel: "m1"},
			"Fixes bugs\nProvider: anthropic · Model: m1"},
		{"acp provider overrides llm provider", Agent{Name: "Bot", AgentType: AgentTypeACP, ACPProvider: &acp, LLMProvider: "ignored"},
			"Provider: claude-code"},
		{"acp without provider keeps llm provider", Agent{Name: "Bot", AgentType: AgentTypeACP, LLMProvider: "openai"},
			"Provider: openai"},
		{"provider cli uses cli provider and model", Agent{Name: "Bot", Description: "d", AgentType: AgentTypeProviderCLI, CLIProvider: &cli, CLIModel: "gpt", LLMProvider: "x", LLMModel: "y"},
			"d\nProvider: codex · Model: gpt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.agent.ComposeJevDescription(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
