package dto

import (
	"testing"

	"github.com/google/uuid"

	meetingdom "github.com/Paca-AI/api/internal/domain/meeting"
)

// What the invite picker and the call view say about an assistant (does it write
// notes, does it suggest) comes from these fields, so they must not be dropped.
func TestMeetingResponsesCarryWhatTheAssistantDoes(t *testing.T) {
	a := MeetingAgentFromEntity(&meetingdom.MeetingAgent{ID: uuid.New(), Mode: "on_demand", SummaryTarget: "channel_message", StoreTranscript: true, RetentionDays: 7})
	if a.Mode != "on_demand" || a.SummaryTarget != "channel_message" || a.RetentionDays != 7 {
		t.Errorf("picker row %+v", a)
	}
	c := CallAssistantFromEntity(&meetingdom.AgentSession{AgentID: uuid.New(), Mode: "notes_only", SummaryTarget: "none"})
	if c.Mode != "notes_only" || c.SummaryTarget != "none" {
		t.Errorf("call row %+v", c)
	}
}
