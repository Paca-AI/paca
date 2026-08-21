package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestHandleTurnControlCancelsOnlyExactAuthoritativeRunFence(t *testing.T) {
	claim := authoritativeClaimFixture()
	handler := &Handler{}
	_, cancel := context.WithCancel(context.Background())
	cancelled := make(chan struct{})
	unregister := handler.registerAuthoritativeRun(claim, func() {
		cancel()
		select {
		case <-cancelled:
		default:
			close(cancelled)
		}
	})
	defer unregister()

	stale := turnControlFromClaim(claim, "stopped_by_user")
	stale.ClaimToken = uuid.New()
	if err := handler.HandleTurnControl(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
		t.Fatal("stale control cancelled the live run")
	default:
	}

	current := turnControlFromClaim(claim, "stopped_by_user")
	if err := handler.HandleTurnControl(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("current fenced control did not cancel the live run")
	}
}
