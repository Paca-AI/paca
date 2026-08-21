package dto

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
)

func TestConclusionPublicationResponseOmitsPrivateSourceWhenInaccessible(t *testing.T) {
	sourceTurnID := uuid.New()
	sourceSessionID := uuid.New()
	publication := &agentdom.ConclusionPublication{
		ID: uuid.New(), TargetTaskID: uuid.New(), SourceTurnID: sourceTurnID,
		PublishedByUserID: uuid.New(), PublishedByMemberID: uuid.New(),
		GeneratedByAgentID: uuid.New(), Kind: agentdom.ConclusionPublished,
		Summary: "frozen shared summary", SummaryVersion: 1,
		SummarySHA256: strings.Repeat("a", 64),
	}

	response := ConclusionPublicationFromEntity(publication, false, &sourceSessionID, &sourceTurnID)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, sourceTurnID.String()) || strings.Contains(text, sourceSessionID.String()) ||
		strings.Contains(text, "source_turn_id") || strings.Contains(text, "source_session_id") {
		t.Fatalf("inaccessible source leaked in response: %s", text)
	}
	if !strings.Contains(text, "frozen shared summary") || !strings.Contains(text, `"source_accessible":false`) {
		t.Fatalf("shared publication fields missing: %s", text)
	}
}

func TestConclusionPublicationResponseOmitsInternalSummaryForDescriptionWriteback(t *testing.T) {
	sourceTurnID := uuid.New()
	sourceSessionID := uuid.New()
	publication := &agentdom.ConclusionPublication{
		ID: uuid.New(), TargetTaskID: uuid.New(), SourceTurnID: sourceTurnID,
		PublishedByUserID: uuid.New(), PublishedByMemberID: uuid.New(),
		GeneratedByAgentID: uuid.New(), Kind: agentdom.ConclusionPublished,
		Summary: "PRIVATE_ONLY", SummaryVersion: 2,
		SummarySHA256: strings.Repeat("b", 64), DescriptionUpdated: true,
	}

	response := ConclusionPublicationFromEntity(publication, true, &sourceSessionID, &sourceTurnID)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, "PRIVATE_ONLY") || strings.Contains(text, "summary_version") ||
		strings.Contains(text, "summary_sha256") || strings.Contains(text, `"summary"`) {
		t.Fatalf("description-only internal summary leaked in response: %s", text)
	}
	if !strings.Contains(text, `"description_updated":true`) ||
		!strings.Contains(text, sourceTurnID.String()) || !strings.Contains(text, sourceSessionID.String()) {
		t.Fatalf("description writeback audit/source fields missing: %s", text)
	}
}

func TestProjectChatTurnResultResponsePreservesAuthoritativeTerminalOutcome(t *testing.T) {
	stable := "stable answer"
	stableHash := strings.Repeat("a", 64)
	stableEventID := uuid.New()
	errorCode := "RUN_FAILED"
	errorMessage := "runner failed"
	tests := []struct {
		status agentdom.TurnStatus
		result agentdom.TurnResult
	}{
		{status: agentdom.TurnStatusSucceeded, result: agentdom.TurnResult{
			StableOutput: &stable, StableOutputSHA256: &stableHash, StableOutputEventID: &stableEventID,
		}},
		{status: agentdom.TurnStatusFailed, result: agentdom.TurnResult{
			ErrorCode: &errorCode, ErrorMessage: &errorMessage,
		}},
		{status: agentdom.TurnStatusStopped},
		{status: agentdom.TurnStatusCancelled},
		{status: agentdom.TurnStatusTimedOut},
		{status: agentdom.TurnStatusNoOutput},
	}
	for _, test := range tests {
		t.Run(string(test.status), func(t *testing.T) {
			test.result.TurnID = uuid.New()
			test.result.RunID = uuid.New()
			test.result.TerminalStatus = test.status
			test.result.GeneratedByAgentID = uuid.New()
			test.result.RuntimeDisposition = agentdom.RuntimeRetired
			test.result.CreatedAt = time.Now().UTC()
			response := ProjectChatTurnResultFromEntity(&test.result)
			if response == nil || response.TerminalStatus != test.status {
				t.Fatalf("terminal result missing from response: %#v", response)
			}
			if test.status == agentdom.TurnStatusSucceeded && response.StableOutput == nil {
				t.Fatal("succeeded result lost stable output")
			}
			if test.status != agentdom.TurnStatusSucceeded && response.StableOutput != nil {
				t.Fatal("unsuccessful result exposed stable output")
			}
		})
	}
	if ProjectChatTurnResultFromEntity(nil) != nil {
		t.Fatal("active turn should return a null result")
	}
}
