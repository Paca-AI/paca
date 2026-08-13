package dto

import (
	"testing"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

// TestConversationFromEntity_ErrorMessage guards against the field silently
// going missing from the response again — it exists on the domain entity
// and in Postgres, but AgentConversationResponse originally never declared
// it at all, so every failed conversation's error was persisted correctly
// yet never reached the frontend. No compiler or json.Marshal error catches
// a dropped field like that, only a test asserting the value round-trips.
func TestConversationFromEntity_ErrorMessage(t *testing.T) {
	msg := "The LLM provider account has run out of tokens/credits or needs payment. Please check its billing or plan, then try again."
	c := &agentdom.AgentConversation{
		ID:           uuid.New(),
		AgentID:      uuid.New(),
		Status:       "failed",
		ErrorMessage: &msg,
	}

	resp := ConversationFromEntity(c)

	if resp.ErrorMessage == nil {
		t.Fatal("ErrorMessage = nil, want it populated from the entity")
	}
	if *resp.ErrorMessage != msg {
		t.Errorf("ErrorMessage = %q, want %q", *resp.ErrorMessage, msg)
	}
}

func TestConversationFromEntity_ErrorMessageNil(t *testing.T) {
	c := &agentdom.AgentConversation{
		ID:      uuid.New(),
		AgentID: uuid.New(),
		Status:  "running",
	}

	resp := ConversationFromEntity(c)

	if resp.ErrorMessage != nil {
		t.Errorf("ErrorMessage = %q, want nil for a conversation that hasn't failed", *resp.ErrorMessage)
	}
}
