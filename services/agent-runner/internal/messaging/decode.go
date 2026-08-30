package messaging

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/agent"
)

// decodeTrigger parses one paca:agent:triggers stream entry's flat fields
// (written by Service.publishTrigger/publishChatTrigger/
// publishGlobalChatTrigger via AppendFlat — no JSON envelope) into an
// agent.Trigger. A field simply absent from the map (rather than present-
// but-empty) is how the publisher represents "not applicable to this
// trigger type" — e.g. actor_member_id is omitted entirely for an
// automation-fired task assignment — so every optional lookup below treats
// "missing" and "empty string" the same way: nil.
func decodeTrigger(values map[string]interface{}) (agent.Trigger, error) {
	conversationID, err := requiredUUID(values, "conversation_id")
	if err != nil {
		return agent.Trigger{}, err
	}
	agentID, err := requiredUUID(values, "agent_id")
	if err != nil {
		return agent.Trigger{}, err
	}

	// project_id is absent entirely for a global-chat trigger — see
	// publishGlobalChatTrigger — leaving ProjectID at its zero value
	// (uuid.Nil), the same convention agentdom.AgentConversation uses for
	// "no project" throughout this codebase.
	projectID, _ := optionalUUID(values, "project_id")

	triggerType, _ := values["trigger_type"].(string)
	message, _ := values["message"].(string)

	t := agent.Trigger{
		ConversationID: conversationID,
		ProjectID:      projectID,
		AgentID:        agentID,
		TaskID:         optionalUUIDPtr(values, "task_id"),
		CommentID:      optionalUUIDPtr(values, "comment_id"),
		ChatSessionID:  optionalUUIDPtr(values, "chat_session_id"),
		ActorMemberID:  optionalUUIDPtr(values, "actor_member_id"),
		ActorUserID:    optionalUUIDPtr(values, "actor_user_id"),
		TriggerType:    agent.TriggerType(triggerType),
		Message:        message,
	}

	if raw, ok := values["repo_plugin_ids"].(string); ok && raw != "" {
		t.RepoPluginIDs = strings.Split(raw, ",")
	}

	// environment_id/workdir are both resolved server-side by services/api
	// (see agent.Trigger.EnvironmentID's doc comment) and, like every other
	// optional field decoded above, simply absent from values entirely for
	// a trigger with no environment attached — left nil rather than parsed
	// as empty-but-present.
	t.EnvironmentID = optionalUUIDPtr(values, "environment_id")
	if workdir, ok := values["workdir"].(string); ok && workdir != "" {
		t.Workdir = &workdir
	}

	// context_items is a JSON-encoded string (AppendFlat needs plain scalar
	// field values — see Service.publishChatTrigger in services/api), not a
	// native array, so it needs an extra decode step the other optional
	// fields above don't. Malformed JSON is swallowed the same permissive
	// way every other optional field here is: left at its zero value
	// (nil) rather than failing the whole trigger decode.
	if raw, ok := values["context_items"].(string); ok && raw != "" {
		var items []agent.ContextItemRef
		if err := json.Unmarshal([]byte(raw), &items); err == nil {
			t.ContextItems = items
		}
	}

	return t, nil
}

func requiredUUID(values map[string]interface{}, key string) (uuid.UUID, error) {
	raw, _ := values[key].(string)
	if raw == "" {
		return uuid.Nil, fmt.Errorf("messaging: trigger missing required field %q", key)
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("messaging: trigger field %q is not a valid UUID: %w", key, err)
	}
	return id, nil
}

func optionalUUID(values map[string]interface{}, key string) (uuid.UUID, bool) {
	raw, _ := values[key].(string)
	if raw == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

func optionalUUIDPtr(values map[string]interface{}, key string) *uuid.UUID {
	id, ok := optionalUUID(values, key)
	if !ok {
		return nil
	}
	return &id
}
