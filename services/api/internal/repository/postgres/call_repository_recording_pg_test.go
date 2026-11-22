package postgres

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	calldom "github.com/Paca-AI/api/internal/domain/call"
)

func TestCallRepository_Recordings(t *testing.T) {
	m := newMeetingFx(t)
	repo := NewCallRepository(m.db.db)
	ctx := t.Context()

	if _, err := repo.CreateRecording(ctx, uuid.New(), m.alice); !errors.Is(err, calldom.ErrNoOpenCall) {
		t.Fatalf("a channel with no call: %v", err)
	}
	rec, err := repo.CreateRecording(ctx, m.channel, m.alice)
	if err != nil || rec.Status != calldom.RecordingStarting || rec.CallID != m.call {
		t.Fatalf("create %+v %v", rec, err)
	}
	if _, err := repo.CreateRecording(ctx, m.channel, m.bob); !errors.Is(err, calldom.ErrRecordingActive) {
		t.Fatalf("one recording at a time: %v", err)
	}
	if err := repo.SetRecordingEgress(ctx, rec.ID, "EG_9"); err != nil {
		t.Fatal(err)
	}
	if open, _ := repo.OpenRecording(ctx, m.channel); open == nil || open.ID != rec.ID || open.EgressID != "EG_9" {
		t.Fatalf("open %+v", open)
	}

	ch, err := repo.UpdateRecordingByEgress(ctx, "EG_9", calldom.RecordingActive, "", 0, 0, "")
	if err != nil || ch != m.channel {
		t.Fatalf("active: %v %v", ch, err)
	}
	if ch, _ := repo.UpdateRecordingByEgress(ctx, "EG_unknown", calldom.RecordingActive, "", 0, 0, ""); ch != uuid.Nil {
		t.Error("an egress that is not ours must change nothing")
	}
	if _, err := repo.UpdateRecordingByEgress(ctx, "EG_9", calldom.RecordingComplete, "recordings/x.mp4", 2048, 90000, ""); err != nil {
		t.Fatal(err)
	}
	// A late event cannot reopen a finished recording.
	if ch, _ := repo.UpdateRecordingByEgress(ctx, "EG_9", calldom.RecordingActive, "", 0, 0, ""); ch != uuid.Nil {
		t.Error("a finished recording must not change again")
	}
	got, err := repo.FindRecording(ctx, m.channel, rec.ID)
	if err != nil || got.Status != calldom.RecordingComplete || got.ObjectKey != "recordings/x.mp4" || got.SizeBytes != 2048 || got.DurationMs != 90000 || got.EndedAt == nil {
		t.Fatalf("found %+v %v", got, err)
	}
	if _, err := repo.FindRecording(ctx, uuid.New(), rec.ID); !errors.Is(err, calldom.ErrRecordingNotFound) {
		t.Errorf("another channel must not see it: %v", err)
	}
	if open, _ := repo.OpenRecording(ctx, m.channel); open != nil {
		t.Error("nothing is recording any more")
	}
	// The next recording of the same call can start.
	if again, err := repo.CreateRecording(ctx, m.channel, m.alice); err != nil || again.ID == rec.ID {
		t.Fatalf("a second recording after the first ended: %v", err)
	}
	if list, _ := repo.ListRecordings(ctx, m.channel, 10); len(list) != 2 {
		t.Errorf("list %d", len(list))
	}

	// A call that was only recorded still shows up in the meeting history.
	mrepo := NewMeetingRepository(m.db.db)
	if ms, _ := mrepo.ListMeetings(ctx, m.channel, 10); len(ms) != 1 || ms[0].Segments != 0 {
		t.Errorf("recorded call in history: %+v", ms)
	}
}
