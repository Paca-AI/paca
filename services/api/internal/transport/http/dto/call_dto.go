package dto

import (
	"time"

	"github.com/google/uuid"

	calldom "github.com/Paca-AI/api/internal/domain/call"
)

// CallJoinResponse is what a client connects to a call with.
type CallJoinResponse struct {
	URL       string    `json:"url"`
	Token     string    `json:"token"`
	Room      string    `json:"room"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CallJoinFromEntity converts join info.
func CallJoinFromEntity(j *calldom.JoinInfo) CallJoinResponse {
	return CallJoinResponse{URL: j.URL, Token: j.Token, Room: j.Room, ExpiresAt: j.ExpiresAt}
}

// CallParticipantResponse is someone in a call.
type CallParticipantResponse struct {
	AccountID uuid.UUID `json:"account_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	JoinedAt  time.Time `json:"joined_at"`
}

// CallResponse is the open call of a channel. An empty Participants list
// means nobody is in it.
type CallResponse struct {
	ChannelID    uuid.UUID                 `json:"channel_id"`
	StartedAt    *time.Time                `json:"started_at"`
	Participants []CallParticipantResponse `json:"participants"`
	// Recording is set while the call is being recorded, so everyone in it can see.
	Recording *CallRecordingResponse `json:"recording"`
}

// CallRecordingResponse is a recording of a call.
type CallRecordingResponse struct {
	ID         uuid.UUID  `json:"id"`
	CallID     uuid.UUID  `json:"call_id"`
	Status     string     `json:"status"`
	StartedBy  *uuid.UUID `json:"started_by"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at"`
	DurationMs int64      `json:"duration_ms"`
	SizeBytes  int64      `json:"size_bytes"`
	Error      string     `json:"error"`
}

// CallRecordingFromEntity converts a recording.
func CallRecordingFromEntity(r *calldom.Recording) CallRecordingResponse {
	return CallRecordingResponse{ID: r.ID, CallID: r.CallID, Status: r.Status, StartedBy: r.StartedBy, StartedAt: r.StartedAt,
		EndedAt: r.EndedAt, DurationMs: r.DurationMs, SizeBytes: r.SizeBytes, Error: r.Error}
}

// RecordingURLResponse is a short-lived link to play or save a recording.
type RecordingURLResponse struct {
	URL string `json:"url"`
}

// CallFromEntity converts a call.
func CallFromEntity(c *calldom.Call) CallResponse {
	out := CallResponse{ChannelID: c.ChannelID, Participants: make([]CallParticipantResponse, len(c.Participants))}
	if !c.StartedAt.IsZero() {
		t := c.StartedAt
		out.StartedAt = &t
	}
	if c.Recording != nil {
		rec := CallRecordingFromEntity(c.Recording)
		out.Recording = &rec
	}
	for i, p := range c.Participants {
		out.Participants[i] = CallParticipantResponse{AccountID: p.AccountID, Name: p.Name, Kind: p.Kind, JoinedAt: p.JoinedAt}
	}
	return out
}

// CallConfigResponse tells a client whether calls are available.
type CallConfigResponse struct {
	Enabled bool `json:"enabled"`
	// Recording says calls can be recorded (LiveKit egress and storage are set up).
	Recording bool `json:"recording"`
}
