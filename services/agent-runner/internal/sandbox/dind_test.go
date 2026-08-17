package sandbox

import (
	"strings"
	"testing"
)

func TestDindContainerName_IsDeterministicPerConversation(t *testing.T) {
	const convID = "68e7af93-0791-4f0e-a791-e0d8e5c8407e"
	got := dindContainerName(convID)
	if !strings.Contains(got, convID) {
		t.Errorf("dindContainerName(%q) = %q, want it to contain the conversation ID", convID, got)
	}
	if got != dindContainerName(convID) {
		t.Errorf("dindContainerName is not deterministic for the same input")
	}
}

func TestConversationNetworkName_DiffersPerConversation(t *testing.T) {
	a := conversationNetworkName("conv-a")
	b := conversationNetworkName("conv-b")
	if a == b {
		t.Errorf("conversationNetworkName should differ per conversation, got %q for both", a)
	}
}

func TestDindDockerHost_PointsAtTheSidecarContainerNameAndPort(t *testing.T) {
	const convID = "68e7af93-0791-4f0e-a791-e0d8e5c8407e"
	got := dindDockerHost(convID)
	want := "tcp://" + dindContainerName(convID) + ":2375"
	if got != want {
		t.Errorf("dindDockerHost(%q) = %q, want %q", convID, got, want)
	}
}
