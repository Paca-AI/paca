package acpbridge

import (
	"strings"
	"testing"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

func TestBuildACPMessagePrefixesPacaSkill(t *testing.T) {
	trigger := agent.Trigger{Message: "Do something", TriggerType: agent.TriggerChatMessage}
	result := BuildACPMessage(trigger, "claude-code")
	if !strings.HasPrefix(result, "/paca Do something") {
		t.Fatalf("expected /paca prefix, got: %s", result)
	}
}

func TestBuildACPMessageUsesCodexSkillSyntax(t *testing.T) {
	trigger := agent.Trigger{Message: "Do something", TriggerType: agent.TriggerChatMessage}
	result := BuildACPMessage(trigger, "codex")
	if !strings.HasPrefix(result, "$paca Do something") {
		t.Fatalf("expected $paca prefix for codex provider, got: %s", result)
	}
}

func TestBuildACPMessageKeepsSlashSyntaxForOtherProviders(t *testing.T) {
	for _, provider := range []string{"claude-code", "gemini-cli", "custom", ""} {
		trigger := agent.Trigger{Message: "Do something", TriggerType: agent.TriggerChatMessage}
		result := BuildACPMessage(trigger, provider)
		if !strings.HasPrefix(result, "/paca Do something") {
			t.Fatalf("provider %q: expected /paca prefix, got: %s", provider, result)
		}
	}
}
