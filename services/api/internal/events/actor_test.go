package events

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// captureSink records the stream entries FanoutTo writes.
type captureSink struct{ fields []map[string]any }

func (c *captureSink) Publish(context.Context, string, any) error { return nil }
func (c *captureSink) AppendFlat(_ context.Context, stream string, f map[string]any) error {
	if stream == StreamActivities {
		c.fields = append(c.fields, f)
	}
	return nil
}

func TestFanout_TakesActorFromRequestContext(t *testing.T) {
	user, agent := uuid.New(), uuid.New()
	cases := []struct {
		name       string
		ctx        context.Context
		wantOrigin string
		wantActor  string
		wantAgent  string
	}{
		{"no request actor", context.Background(), "system", "", ""},
		{"human", WithRequestActor(context.Background(), user, nil), "user", user.String(), ""},
		{"agent", WithRequestActor(context.Background(), user, &agent), "agent", user.String(), agent.String()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := &captureSink{}
			FanoutTo(tc.ctx, sink, Event{Topic: "sprint.created", ProjectID: uuid.New(), Payload: map[string]any{}})
			f := sink.fields[0]
			if f["origin"] != tc.wantOrigin {
				t.Errorf("origin = %v, want %v", f["origin"], tc.wantOrigin)
			}
			if got, _ := f["actor_id"].(string); got != tc.wantActor {
				t.Errorf("actor_id = %q, want %q", got, tc.wantActor)
			}
			if got, _ := f["actor_agent_id"].(string); got != tc.wantAgent {
				t.Errorf("actor_agent_id = %q, want %q", got, tc.wantAgent)
			}
		})
	}
}

func TestFanout_ExplicitAttributionWins(t *testing.T) {
	sink := &captureSink{}
	ctx := WithRequestActor(context.Background(), uuid.New(), nil)
	FanoutTo(ctx, sink, Event{Topic: "task.updated", ProjectID: uuid.New(), Payload: map[string]any{}, Origin: OriginAutomation})
	if f := sink.fields[0]; f["origin"] != "automation" || f["actor_id"] != nil {
		t.Errorf("an explicit origin must not be overridden by the request actor, got %v", f)
	}
}
