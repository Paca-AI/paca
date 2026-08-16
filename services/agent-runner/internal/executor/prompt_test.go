package executor

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

func TestBuildInitialMessage_IncludesSystemPromptSkillsAndMessage(t *testing.T) {
	projectID := uuid.New()
	cfg := agent.Config{
		SystemPrompt: "You are Paca's developer agent.",
		Skills: []agent.Skill{
			{SkillName: "developer", SkillContent: "Write clean code.", IsEnabled: true},
			{SkillName: "disabled-skill", SkillContent: "Should not appear.", IsEnabled: false},
		},
	}
	trigger := agent.Trigger{
		ProjectID:   projectID,
		Message:     "fix the failing test",
		TriggerType: agent.TriggerChatMessage,
	}

	msg := buildInitialMessage(cfg, trigger)

	for _, want := range []string{
		"You are Paca's developer agent.",
		"Write clean code.",
		projectID.String(),
		"fix the failing test",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n\ngot:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "Should not appear.") {
		t.Errorf("message included a disabled skill's content:\n%s", msg)
	}
}

func TestBuildInitialMessage_TaskAssignedWithEmptyMessageUsesDefault(t *testing.T) {
	trigger := agent.Trigger{
		TriggerType: agent.TriggerTaskAssigned,
		Message:     "",
	}

	msg := buildInitialMessage(agent.Config{}, trigger)

	if !strings.Contains(msg, taskAssignedDefault) {
		t.Errorf("expected the task-assigned default fallback message, got:\n%s", msg)
	}
}

func TestActionTypeLabel_AutomationTriggeredTaskAssignment(t *testing.T) {
	trigger := agent.Trigger{TriggerType: agent.TriggerTaskAssigned, ActorMemberID: nil}
	if got := actionTypeLabel(trigger); got != "Automation-triggered task assignment" {
		t.Errorf("actionTypeLabel = %q", got)
	}
}

func TestActionTypeLabel_CommentMentionOnTaskVsDocument(t *testing.T) {
	taskID := uuid.New()
	onTask := agent.Trigger{TriggerType: agent.TriggerCommentMention, TaskID: &taskID}
	if got := actionTypeLabel(onTask); got != "Task comment mention" {
		t.Errorf("actionTypeLabel(task comment) = %q", got)
	}

	onDoc := agent.Trigger{TriggerType: agent.TriggerCommentMention}
	if got := actionTypeLabel(onDoc); got != "Document comment mention" {
		t.Errorf("actionTypeLabel(doc comment) = %q", got)
	}
}

func TestBuildInitialMessage_PriorHandoffs(t *testing.T) {
	trigger := agent.Trigger{
		TriggerType:   agent.TriggerTaskAssigned,
		PriorHandoffs: []string{"First conclusion.", "Second conclusion."},
	}

	msg := buildInitialMessage(agent.Config{}, trigger)

	if !strings.Contains(msg, "## Prior Agent Handoffs on This Task") {
		t.Errorf("expected prior handoffs section, got:\n%s", msg)
	}
	for _, want := range []string{"First conclusion.", "Second conclusion."} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected handoff %q, got:\n%s", want, msg)
		}
	}
}

func TestBuildInitialMessage_NoPriorHandoffs(t *testing.T) {
	trigger := agent.Trigger{TriggerType: agent.TriggerTaskAssigned}

	msg := buildInitialMessage(agent.Config{}, trigger)

	if strings.Contains(msg, "Prior Agent Handoffs") {
		t.Errorf("should not render a handoffs section when empty, got:\n%s", msg)
	}
}
