package acpbridge

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestVerifyTicket_RoundTrip(t *testing.T) {
	secret := []byte("shared-internal-api-key")
	envID := uuid.New()

	ticket := mintTicket(secret, ticketPurposeStats, envID, time.Now().Add(time.Minute))
	if err := verifyTicket(secret, ticketPurposeStats, ticket, envID); err != nil {
		t.Fatalf("verifyTicket: %v", err)
	}
}

func TestVerifyTicket_Expired(t *testing.T) {
	secret := []byte("shared-internal-api-key")
	envID := uuid.New()

	ticket := mintTicket(secret, ticketPurposeTerminal, envID, time.Now().Add(-time.Minute))
	if err := verifyTicket(secret, ticketPurposeTerminal, ticket, envID); err == nil {
		t.Fatal("expected an error for an expired ticket, got nil")
	}
}

func TestVerifyTicket_WrongSecret(t *testing.T) {
	envID := uuid.New()
	ticket := mintTicket([]byte("secret-a"), ticketPurposeTerminal, envID, time.Now().Add(time.Minute))
	if err := verifyTicket([]byte("secret-b"), ticketPurposeTerminal, ticket, envID); err == nil {
		t.Fatal("expected an error for a ticket signed with a different secret, got nil")
	}
}

func TestVerifyTicket_EnvironmentIDMismatch(t *testing.T) {
	secret := []byte("shared-internal-api-key")
	ticket := mintTicket(secret, ticketPurposeTerminal, uuid.New(), time.Now().Add(time.Minute))
	if err := verifyTicket(secret, ticketPurposeTerminal, ticket, uuid.New()); err == nil {
		t.Fatal("expected an error for a ticket minted for a different environment_id, got nil")
	}
}

// TestVerifyTicket_PurposeMismatch is the reason ticket.go's payload
// carries a purpose at all: a ticket minted for the terminal (which
// requires agents.write, since it grants a shell) must not also unlock
// the stats endpoint (which only requires agents.read), and vice versa.
func TestVerifyTicket_PurposeMismatch(t *testing.T) {
	secret := []byte("shared-internal-api-key")
	envID := uuid.New()
	ticket := mintTicket(secret, ticketPurposeTerminal, envID, time.Now().Add(time.Minute))
	if err := verifyTicket(secret, ticketPurposeStats, ticket, envID); err == nil {
		t.Fatal("expected an error for a ticket minted for a different purpose, got nil")
	}
}

func TestVerifyTicket_MalformedTicket(t *testing.T) {
	secret := []byte("shared-internal-api-key")
	if err := verifyTicket(secret, ticketPurposeTerminal, "not-a-valid-ticket!!!", uuid.New()); err == nil {
		t.Fatal("expected an error for a malformed ticket, got nil")
	}
	if err := verifyTicket(secret, ticketPurposeTerminal, "", uuid.New()); err == nil {
		t.Fatal("expected an error for an empty ticket, got nil")
	}
}
