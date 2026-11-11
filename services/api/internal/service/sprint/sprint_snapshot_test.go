package sprintsvc

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/transport/http/dto"
)

// The snapshot must read exactly like the sprint routes' response, or a client
// patching its cache with it would drift from what a refetch returns.
func TestSprintSnapshot_MatchesTheRouteResponse(t *testing.T) {
	start := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	goal := "ship"
	for name, sp := range map[string]*sprintdom.Sprint{
		"full": {
			ID: uuid.New(), ProjectID: uuid.New(), Name: "S1", StartDate: &start,
			EndDate: &start, Goal: &goal, Status: sprintdom.SprintStatusActive,
			CreatedAt: start, UpdatedAt: start,
		},
		"bare": {
			ID: uuid.New(), ProjectID: uuid.New(), Name: "S2",
			Status: sprintdom.SprintStatusPlanned, CreatedAt: start, UpdatedAt: start,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var got, want map[string]any
			a, _ := json.Marshal(sprintSnapshot(sp))
			b, _ := json.Marshal(dto.SprintFromEntity(sp))
			if err := json.Unmarshal(a, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("snapshot = %v\nroute   = %v", got, want)
			}
		})
	}
}
