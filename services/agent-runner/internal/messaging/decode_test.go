package messaging

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

// TestDecodeTrigger_TaskAssignedByAutomation mirrors the exact field set
// TriggerTaskAssigned's payload map produces when triggeredByMemberID is
// nil (an automation-fired assignment) — actor_member_id is entirely
// absent from the map, not present-but-empty. See agent_service.go.
func TestDecodeTrigger_TaskAssignedByAutomation(t *testing.T) {
	convID, agentID, projectID, taskID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	values := map[string]interface{}{
		"conversation_id": convID.String(),
		"project_id":      projectID.String(),
		"agent_id":        agentID.String(),
		"task_id":         taskID.String(),
		"trigger_type":    "task_assigned",
		"message":         "",
		"repo_plugin_ids": "plugin-a,plugin-b",
		"type":            "agent.task_assigned",
	}

	trigger, err := decodeTrigger(values)
	if err != nil {
		t.Fatalf("decodeTrigger: %v", err)
	}

	if trigger.ConversationID != convID || trigger.AgentID != agentID || trigger.ProjectID != projectID {
		t.Errorf("core IDs mismatch: %+v", trigger)
	}
	if trigger.TaskID == nil || *trigger.TaskID != taskID {
		t.Errorf("TaskID = %v, want %s", trigger.TaskID, taskID)
	}
	if trigger.ActorMemberID != nil {
		t.Errorf("ActorMemberID = %v, want nil (field was absent from the map)", trigger.ActorMemberID)
	}
	if trigger.TriggerType != agent.TriggerTaskAssigned {
		t.Errorf("TriggerType = %q", trigger.TriggerType)
	}
	if got := trigger.RepoPluginIDs; len(got) != 2 || got[0] != "plugin-a" || got[1] != "plugin-b" {
		t.Errorf("RepoPluginIDs = %v", got)
	}
}

// TestDecodeTrigger_GlobalChatHasNoProjectID mirrors
// publishGlobalChatTrigger's payload: no project_id key at all, and
// actor_user_id instead of actor_member_id.
func TestDecodeTrigger_GlobalChatHasNoProjectID(t *testing.T) {
	convID, agentID, sessionID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	values := map[string]interface{}{
		"conversation_id": convID.String(),
		"agent_id":        agentID.String(),
		"chat_session_id": sessionID.String(),
		"actor_user_id":   userID.String(),
		"trigger_type":    "chat_message",
		"message":         "hi",
	}

	trigger, err := decodeTrigger(values)
	if err != nil {
		t.Fatalf("decodeTrigger: %v", err)
	}

	if trigger.ProjectID != uuid.Nil {
		t.Errorf("ProjectID = %s, want uuid.Nil for a global-chat trigger", trigger.ProjectID)
	}
	if trigger.ActorUserID == nil || *trigger.ActorUserID != userID {
		t.Errorf("ActorUserID = %v, want %s", trigger.ActorUserID, userID)
	}
	if trigger.ActorMemberID != nil {
		t.Errorf("ActorMemberID = %v, want nil", trigger.ActorMemberID)
	}
	if trigger.ChatSessionID == nil || *trigger.ChatSessionID != sessionID {
		t.Errorf("ChatSessionID = %v, want %s", trigger.ChatSessionID, sessionID)
	}
}

// TestDecodeTrigger_EnvironmentAttach mirrors resolveWorkdirForConversation's
// payload for an environment-backed conversation: environment_id and
// workdir both present.
func TestDecodeTrigger_EnvironmentAttach(t *testing.T) {
	convID, agentID, projectID, envID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	values := map[string]interface{}{
		"conversation_id": convID.String(),
		"project_id":      projectID.String(),
		"agent_id":        agentID.String(),
		"trigger_type":    "chat_message",
		"message":         "hi",
		"environment_id":  envID.String(),
		"workdir":         "/home/paca/workspaces/my-project",
	}

	trigger, err := decodeTrigger(values)
	if err != nil {
		t.Fatalf("decodeTrigger: %v", err)
	}

	if trigger.EnvironmentID == nil || *trigger.EnvironmentID != envID {
		t.Errorf("EnvironmentID = %v, want %s", trigger.EnvironmentID, envID)
	}
	if trigger.Workdir == nil || *trigger.Workdir != "/home/paca/workspaces/my-project" {
		t.Errorf("Workdir = %v, want /home/paca/workspaces/my-project", trigger.Workdir)
	}
}

// TestDecodeTrigger_NoEnvironmentLeavesFieldsNil mirrors an ordinary,
// non-environment-backed trigger: environment_id/workdir both entirely
// absent from the map, not present-but-empty.
func TestDecodeTrigger_NoEnvironmentLeavesFieldsNil(t *testing.T) {
	convID, agentID, projectID := uuid.New(), uuid.New(), uuid.New()
	values := map[string]interface{}{
		"conversation_id": convID.String(),
		"project_id":      projectID.String(),
		"agent_id":        agentID.String(),
		"trigger_type":    "chat_message",
		"message":         "hi",
	}

	trigger, err := decodeTrigger(values)
	if err != nil {
		t.Fatalf("decodeTrigger: %v", err)
	}

	if trigger.EnvironmentID != nil {
		t.Errorf("EnvironmentID = %v, want nil", trigger.EnvironmentID)
	}
	if trigger.Workdir != nil {
		t.Errorf("Workdir = %v, want nil", trigger.Workdir)
	}
}

func TestDecodeTrigger_MissingConversationIDErrors(t *testing.T) {
	_, err := decodeTrigger(map[string]interface{}{"agent_id": uuid.New().String()})
	if err == nil {
		t.Fatal("expected an error for a trigger missing conversation_id")
	}
}

func TestDecodeTrigger_EmptyRepoPluginIDsStaysNil(t *testing.T) {
	// A global-chat trigger omits repo_plugin_ids entirely (see
	// publishGlobalChatTrigger) — not present as an empty string.
	values := map[string]interface{}{
		"conversation_id": uuid.New().String(),
		"agent_id":        uuid.New().String(),
	}
	trigger, err := decodeTrigger(values)
	if err != nil {
		t.Fatalf("decodeTrigger: %v", err)
	}
	if trigger.RepoPluginIDs != nil {
		t.Errorf("RepoPluginIDs = %v, want nil", trigger.RepoPluginIDs)
	}
}
