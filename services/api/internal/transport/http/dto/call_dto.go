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
}

// CallFromEntity converts a call.
func CallFromEntity(c *calldom.Call) CallResponse {
	out := CallResponse{ChannelID: c.ChannelID, Participants: make([]CallParticipantResponse, len(c.Participants))}
	if !c.StartedAt.IsZero() {
		t := c.StartedAt
		out.StartedAt = &t
	}
	for i, p := range c.Participants {
		out.Participants[i] = CallParticipantResponse{AccountID: p.AccountID, Name: p.Name, Kind: p.Kind, JoinedAt: p.JoinedAt}
	}
	return out
}

// CallConfigResponse tells a client whether calls are available.
type CallConfigResponse struct {
	Enabled bool `json:"enabled"`
}
