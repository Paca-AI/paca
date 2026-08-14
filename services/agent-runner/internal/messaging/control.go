package messaging

import (
	"github.com/google/uuid"
)

// ControlType is one of the paca:agent:triggers stream's control-message
// "type" values — directs an already-running conversation rather than
// starting a new one. Mirrors _CONTROL_TYPES in services/ai-agent's
// core/streams.py exactly (values included).
type ControlType string

const (
	// ControlStop tears a running or paused conversation's sandbox down.
	ControlStop ControlType = "agent.stop"
	// ControlPause interrupts a running turn but keeps its sandbox alive.
	ControlPause ControlType = "agent.pause"
	// ControlHeartbeat extends a paused chat sandbox's idle timeout.
	ControlHeartbeat ControlType = "agent.heartbeat"
)

var controlTypes = map[string]ControlType{
	string(ControlStop):      ControlStop,
	string(ControlPause):     ControlPause,
	string(ControlHeartbeat): ControlHeartbeat,
}

// Control is a decoded stop/pause/heartbeat directive. Published with only
// conversation_id + project_id (see agent_service.go's StopConversation/
// PauseConversation/Heartbeat) — no agent_id, no trigger_type, which is
// exactly why these can't be decoded as a Trigger (decodeTrigger requires
// agent_id) and need their own type and routing.
type Control struct {
	Type           ControlType
	ConversationID uuid.UUID
	// ProjectID is the zero value (uuid.Nil) for a control message
	// targeting a global-chat conversation — same convention as
	// agent.Trigger.ProjectID.
	ProjectID uuid.UUID
}

func decodeControl(controlType ControlType, values map[string]interface{}) (Control, error) {
	conversationID, err := requiredUUID(values, "conversation_id")
	if err != nil {
		return Control{}, err
	}
	projectID, _ := optionalUUID(values, "project_id")
	return Control{Type: controlType, ConversationID: conversationID, ProjectID: projectID}, nil
}
