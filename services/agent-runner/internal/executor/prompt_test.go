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

func TestBuildInitialMessage_GlobalChatGetsGlobalContextNotAProjectUUID(t *testing.T) {
	trigger := agent.Trigger{
		ProjectID:   uuid.Nil,
		Message:     "what's on my plate today?",
		TriggerType: agent.TriggerChatMessage,
	}

	msg := buildInitialMessage(agent.Config{}, trigger)

	if strings.Contains(msg, uuid.Nil.String()) {
		t.Errorf("global-chat message should never mention the nil project UUID, got:\n%s", msg)
	}
	if !strings.Contains(msg, "global Paca agent") {
		t.Errorf("expected global-agent framing, got:\n%s", msg)
	}
	if !strings.Contains(msg, "call `list_projects`") {
		t.Errorf("expected global-agent framing to point the agent at list_projects, got:\n%s", msg)
	}
}

func TestBuildInitialMessage_ProjectScopedChatStillGetsProjectUUID(t *testing.T) {
	projectID := uuid.New()
	trigger := agent.Trigger{
		ProjectID:   projectID,
		Message:     "hi",
		TriggerType: agent.TriggerChatMessage,
	}

	msg := buildInitialMessage(agent.Config{}, trigger)

	if !strings.Contains(msg, projectID.String()) {
		t.Errorf("project-scoped message should mention the project UUID, got:\n%s", msg)
	}
	if strings.Contains(msg, "global Paca agent") {
		t.Errorf("project-scoped message should not use global-agent framing, got:\n%s", msg)
	}
}

func TestBuildInitialMessage_AutomationTriggerGetsNoHumanWatchingNote(t *testing.T) {
	trigger := agent.Trigger{
		TriggerType:   agent.TriggerTaskAssigned,
		ActorMemberID: nil,
		Message:       "do the thing",
	}

	msg := buildInitialMessage(agent.Config{}, trigger)

	if !strings.Contains(msg, "no human watching this conversation live") {
		t.Errorf("expected automation-invocation note, got:\n%s", msg)
	}
}

func TestBuildInitialMessage_HumanTriggerGetsNoAutomationNote(t *testing.T) {
	actorMemberID := uuid.New()
	trigger := agent.Trigger{
		TriggerType:   agent.TriggerTaskAssigned,
		ActorMemberID: &actorMemberID,
		Message:       "do the thing",
	}

	msg := buildInitialMessage(agent.Config{}, trigger)

	if strings.Contains(msg, "no human watching this conversation live") {
		t.Errorf("human-triggered message should not include the automation note, got:\n%s", msg)
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
