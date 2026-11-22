package meetingdom_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	meetingdom "github.com/Paca-AI/api/internal/domain/meeting"
)

func TestValidatePayload(t *testing.T) {
	id := uuid.NewString()
	cases := []struct {
		name, kind, payload string
		ok                  bool
	}{
		{"create ok", meetingdom.KindTaskCreate, `{"title":" Fix login "}`, true},
		{"create needs title", meetingdom.KindTaskCreate, `{"title":"  "}`, false},
		{"create rejects unknown field", meetingdom.KindTaskCreate, `{"title":"x","project_id":"y"}`, false},
		{"create bad assignee", meetingdom.KindTaskCreate, `{"title":"x","assignee_id":"nope"}`, false},
		{"update ok", meetingdom.KindTaskUpdate, `{"task_id":"` + id + `","priority":"high"}`, true},
		{"update changes nothing", meetingdom.KindTaskUpdate, `{"task_id":"` + id + `"}`, false},
		{"update clears sprint", meetingdom.KindTaskUpdate, `{"task_id":"` + id + `","sprint_id":""}`, true},
		{"comment ok", meetingdom.KindTaskComment, `{"task_id":"` + id + `","body":"hi"}`, true},
		{"comment needs task", meetingdom.KindTaskComment, `{"task_id":"x","body":"hi"}`, false},
		{"doc ok", meetingdom.KindDocCreateFromNotes, `{"title":"Notes","markdown":"- a"}`, true},
		{"doc empty", meetingdom.KindDocCreateFromNotes, `{"title":"Notes","markdown":""}`, false},
		{"unknown kind", "task.delete", `{}`, false},
		{"trailing data", meetingdom.KindTaskCreate, `{"title":"x"}{}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := meetingdom.ValidatePayload(c.kind, []byte(c.payload))
			if c.ok {
				if err != nil || len(out) == 0 {
					t.Fatalf("want ok, got %v", err)
				}
				return
			}
			if !errors.Is(err, meetingdom.ErrProposalInvalid) {
				t.Fatalf("want ErrProposalInvalid, got %v", err)
			}
		})
	}
}

func TestDefaultSettings(t *testing.T) {
	s := meetingdom.DefaultSettings(uuid.New())
	if s.Mode != meetingdom.ModeNotesOnly || !s.StoreTranscript || len(s.ProposalKinds) != len(meetingdom.ProposalKinds) {
		t.Fatalf("unexpected defaults %+v", s)
	}
}
