package events

import (
	"context"

	"github.com/google/uuid"
)

// requestActor is who caused the events a request produces, carried on the
// request context so a service can Fanout an attributed event without every
// method growing actor parameters.
type requestActor struct {
	userID  uuid.UUID
	agentID *uuid.UUID
}

type requestActorKey struct{}

// WithRequestActor returns ctx carrying the authenticated caller. The HTTP
// authn middleware sets it on every authenticated request; agentID is nil for
// a human session. Background workers never set it, so their events stay
// system-attributed.
func WithRequestActor(ctx context.Context, userID uuid.UUID, agentID *uuid.UUID) context.Context {
	return context.WithValue(ctx, requestActorKey{}, requestActor{userID: userID, agentID: agentID})
}

// withContextActor fills an event's actor and origin from the request context
// when its producer left all three unset. A producer that sets any of them —
// the task and doc activity services, which carry an explicit origin — is
// left untouched.
func withContextActor(ctx context.Context, e Event) Event {
	if e.ActorID != nil || e.ActorAgentID != nil || e.Origin != "" {
		return e
	}
	a, ok := ctx.Value(requestActorKey{}).(requestActor)
	if !ok {
		return e
	}
	if a.userID != uuid.Nil {
		userID := a.userID
		e.ActorID = &userID
	}
	if a.agentID != nil {
		agentID := *a.agentID
		e.ActorAgentID = &agentID
		e.Origin = OriginAgent
	} else {
		e.Origin = OriginUser
	}
	return e
}
