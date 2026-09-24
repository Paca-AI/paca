package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"

	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	"github.com/Paca-AI/api/internal/platform/jev"
)

func TestResolveAutoAgent_NoCandidates(t *testing.T) {
	id, confidence := resolveAutoAgent(context.Background(), nil, nil, "hello")
	if id != uuid.Nil || confidence != 0 {
		t.Errorf("expected zero values for no candidates, got id=%v confidence=%v", id, confidence)
	}
}

func TestResolveAutoAgent_SingleCandidateSkipsJev(t *testing.T) {
	only := &agentdom.Agent{ID: uuid.New(), Name: "Solo"}
	// A non-nil, non-Enabled client (empty key) would error if actually
	// called — passing it here proves the single-candidate path never calls
	// Jev at all.
	id, confidence := resolveAutoAgent(context.Background(), jev.New("", "", ""), []*agentdom.Agent{only}, "hello")
	if id != only.ID {
		t.Errorf("expected the sole candidate to be picked, got %v", id)
	}
	if confidence != 0 {
		t.Errorf("expected zero confidence when Jev was never consulted, got %v", confidence)
	}
}

func TestResolveAutoAgent_UnconfiguredJevFallsBackToFirstCandidate(t *testing.T) {
	first := &agentdom.Agent{ID: uuid.New(), Name: "First"}
	second := &agentdom.Agent{ID: uuid.New(), Name: "Second"}
	id, confidence := resolveAutoAgent(context.Background(), nil, []*agentdom.Agent{first, second}, "hello")
	if id != first.ID {
		t.Errorf("expected fallback to the first candidate, got %v", id)
	}
	if confidence != 0 {
		t.Errorf("expected zero confidence on fallback, got %v", confidence)
	}
}
