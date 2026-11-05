package acpbridge

import (
	"strings"
	"testing"

	"github.com/google/uuid"

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

func TestBuildACPMessageNamesTheDocumentForADocCommentMention(t *testing.T) {
	docID := uuid.New()
	msg := BuildACPMessage(agent.Trigger{
		TriggerType: agent.TriggerCommentMention,
		ProjectID:   uuid.New(),
		DocumentID:  &docID,
		Message:     "Option A",
	}, "claude-code")
	for _, want := range []string{"Action type: Document comment mention", "Document ID: " + docID.String()} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestBuildACPMessageAddsMentionHintWhenActorKnown(t *testing.T) {
	actor := uuid.New()
	msg := BuildACPMessage(agent.Trigger{
		Message:       "Do something",
		TriggerType:   agent.TriggerChatMessage,
		ActorMemberID: &actor,
	}, "claude-code")
	if !strings.Contains(msg, "## Asking the user") {
		t.Fatalf("expected Asking the user section, got: %s", msg)
	}
	if want := "owner (id: " + actor.String() + ")"; !strings.Contains(msg, want) {
		t.Fatalf("expected %q in message, got: %s", want, msg)
	}
}

func TestBuildACPMessageOmitsMentionHintWithoutActor(t *testing.T) {
	msg := BuildACPMessage(agent.Trigger{
		Message:     "Do something",
		TriggerType: agent.TriggerChatMessage,
	}, "claude-code")
	if strings.Contains(msg, "## Asking the user") {
		t.Fatalf("unexpected Asking the user section: %s", msg)
	}
}
