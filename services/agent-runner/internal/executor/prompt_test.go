package executor

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

func TestBuildInitialMessage_IncludesOnlyPerTurnContentNoSystemPromptNoSkills(t *testing.T) {
	// buildInitialMessage takes no agent.Config at all any more: neither the
	// system prompt (delivered via .goosehints — see hints.go) nor any
	// skill's content (delivered exclusively via Goose's native SKILL.md +
	// load_skill discovery — see skills.go's package doc comment) is ever
	// folded into this message. This test exists to keep it that way — if
	// buildInitialMessage's signature ever grows an agent.Config parameter
	// again, that's itself a signal something is being folded back in.
	projectID := uuid.New()
	trigger := agent.Trigger{
		ProjectID:   projectID,
		Message:     "fix the failing test",
		TriggerType: agent.TriggerChatMessage,
	}

	msg := buildInitialMessage(trigger)

	for _, want := range []string{
		projectID.String(),
		"fix the failing test",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n\ngot:\n%s", want, msg)
		}
	}
}

func TestBuildInitialMessage_TaskAssignedWithEmptyMessageUsesDefault(t *testing.T) {
	trigger := agent.Trigger{
		TriggerType: agent.TriggerTaskAssigned,
		Message:     "",
	}

	msg := buildInitialMessage(trigger)

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

	msg := buildInitialMessage(trigger)

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

	msg := buildInitialMessage(trigger)

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

	msg := buildInitialMessage(trigger)

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

	msg := buildInitialMessage(trigger)

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

func TestBuildInitialMessage_PriorHandoffs(t *testing.T) {
	trigger := agent.Trigger{
		TriggerType:   agent.TriggerTaskAssigned,
		PriorHandoffs: []string{"First conclusion.", "Second conclusion."},
	}

	msg := buildInitialMessage(trigger)

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

	msg := buildInitialMessage(trigger)

	if strings.Contains(msg, "Prior Agent Handoffs") {
		t.Errorf("should not render a handoffs section when empty, got:\n%s", msg)
	}
}
