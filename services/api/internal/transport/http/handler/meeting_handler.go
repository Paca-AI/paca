package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	meetingdom "github.com/Paca-AI/api/internal/domain/meeting"
	meetingsvc "github.com/Paca-AI/api/internal/service/meeting"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// workerTokenHeader carries the secret the meeting worker proves itself with.
const workerTokenHeader = "X-Meeting-Worker-Token"

const maxMeetingBody = 256 << 10

// MeetingHandler serves the meeting assistant: its settings, its place in a
// call, proposals, and the routes its worker uses. It makes no authorization
// decision: every route is gated in the router, and the worker routes prove
// themselves with the worker secret (checked by the service).
type MeetingHandler struct {
	svc meetingdom.Service
}

// NewMeetingHandler returns a MeetingHandler.
func NewMeetingHandler(svc meetingdom.Service) *MeetingHandler { return &MeetingHandler{svc: svc} }

func meetingBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMeetingBody)).Decode(v); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid request body"))
		return false
	}
	return true
}

// Config handles GET /chat/meeting/config.
func (h *MeetingHandler) Config(w http.ResponseWriter, r *http.Request) {
	presenter.OK(w, r, dto.MeetingConfigResponse{Enabled: h.svc.Enabled()})
}

// GetSettings handles GET /projects/{projectId}/agents/{agentId}/meeting-settings.
func (h *MeetingHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := chatURLID(r, "agentId", "agent")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	s, err := h.svc.GetSettings(r.Context(), projectID, agentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MeetingSettingsFromEntity(s, meetingsvc.STTProviders))
}

// UpdateSettings handles PATCH /projects/{projectId}/agents/{agentId}/meeting-settings.
func (h *MeetingHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := chatURLID(r, "agentId", "agent")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateMeetingSettingsRequest
	if !meetingBody(w, r, &req) {
		return
	}
	s, err := h.svc.UpdateSettings(r.Context(), projectID, agentID, req.Patch())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MeetingSettingsFromEntity(s, meetingsvc.STTProviders))
}

// ListAgents handles GET /projects/{projectId}/meeting-agents.
func (h *MeetingHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	as, err := h.svc.ListMeetingAgents(r.Context(), projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.MeetingAgentResponse, len(as))
	for i, a := range as {
		out[i] = dto.MeetingAgentFromEntity(a)
	}
	presenter.OK(w, r, out)
}

// ListAssistants handles GET .../channels/{channelId}/call/assistants.
func (h *MeetingHandler) ListAssistants(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	ss, err := h.svc.ListAssistants(r.Context(), projectID, channelID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.CallAssistantResponse, len(ss))
	for i, s := range ss {
		out[i] = dto.CallAssistantFromEntity(s)
	}
	presenter.OK(w, r, out)
}

// Invite handles POST .../channels/{channelId}/call/assistants/{agentId}.
func (h *MeetingHandler) Invite(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := chatURLID(r, "agentId", "agent")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	s, err := h.svc.Invite(r.Context(), projectID, channelID, agentID, me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.CallAssistantFromEntity(s))
}

// Remove handles DELETE .../channels/{channelId}/call/assistants/{agentId}.
func (h *MeetingHandler) Remove(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	agentID, err := chatURLID(r, "agentId", "agent")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.Remove(r.Context(), projectID, channelID, agentID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// ---------------------------------------------------------------- proposals

// CreateProposal handles POST /projects/{projectId}/proposals. The caller is
// the assistant itself, authenticated with its own key.
func (h *MeetingHandler) CreateProposal(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.CreateProposalRequest
	if !meetingBody(w, r, &req) {
		return
	}
	p, err := h.svc.CreateProposal(r.Context(), meetingdom.NewProposal{ProjectID: projectID, AgentID: me,
		ChannelID: req.ChannelID, Kind: req.Kind, Payload: req.Payload, TargetID: req.TargetID})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	// The assistant gets the chip only: the fields are the person's to see.
	presenter.OK(w, r, dto.ProposalChipFromEntity(p))
}

// ListChannelProposals handles GET .../channels/{channelId}/proposals: chips
// for everyone who can read the channel.
func (h *MeetingHandler) ListChannelProposals(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	ps, err := h.svc.ListProposals(r.Context(), projectID, &channelID, nil)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.ProposalChipResponse, len(ps))
	for i, p := range ps {
		out[i] = dto.ProposalChipFromEntity(p)
	}
	presenter.OK(w, r, out)
}

// ListMyProposals handles GET /projects/{projectId}/proposals/mine: the chips
// of what the caller is asked to review.
func (h *MeetingHandler) ListMyProposals(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	ps, err := h.svc.ListProposals(r.Context(), projectID, nil, &me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.ProposalChipResponse, len(ps))
	for i, p := range ps {
		out[i] = dto.ProposalChipFromEntity(p)
	}
	presenter.OK(w, r, out)
}

func proposalIDs(r *http.Request) (projectID, id uuid.UUID, err error) {
	if projectID, err = parseProjectID(r); err != nil {
		return
	}
	id, err = chatURLID(r, "proposalId", "proposal")
	return
}

// GetProposal handles GET /projects/{projectId}/proposals/{proposalId}.
func (h *MeetingHandler) GetProposal(w http.ResponseWriter, r *http.Request) {
	projectID, id, err := proposalIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	p, err := h.svc.GetProposal(r.Context(), projectID, id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProposalFromEntity(p))
}

// ClaimProposal handles POST /projects/{projectId}/proposals/{proposalId}/claim.
func (h *MeetingHandler) ClaimProposal(w http.ResponseWriter, r *http.Request) {
	projectID, id, err := proposalIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	p, err := h.svc.Claim(r.Context(), projectID, id, me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProposalFromEntity(p))
}

// ResolveProposal handles POST /projects/{projectId}/proposals/{proposalId}/resolve.
func (h *MeetingHandler) ResolveProposal(w http.ResponseWriter, r *http.Request) {
	projectID, id, err := proposalIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	var req dto.ResolveProposalRequest
	if !meetingBody(w, r, &req) {
		return
	}
	p, err := h.svc.Resolve(r.Context(), projectID, id, me, req.Status, req.Result)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ProposalFromEntity(p))
}

// ------------------------------------------------------------------ worker

func workerSession(r *http.Request) (secret string, id uuid.UUID, err error) {
	id, err = chatURLID(r, "sessionId", "session")
	return r.Header.Get(workerTokenHeader), id, err
}

// WorkerBootstrap handles POST /internal/meeting-agent/sessions/{sessionId}/bootstrap.
func (h *MeetingHandler) WorkerBootstrap(w http.ResponseWriter, r *http.Request) {
	secret, id, err := workerSession(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	b, err := h.svc.Bootstrap(r.Context(), secret, id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	// The response carries decrypted keys: it must never be cached.
	w.Header().Set("Cache-Control", "no-store")
	presenter.OK(w, r, dto.WorkerBootstrapFromEntity(b))
}

// WorkerAppendTranscript handles POST /internal/meeting-agent/sessions/{sessionId}/transcript.
func (h *MeetingHandler) WorkerAppendTranscript(w http.ResponseWriter, r *http.Request) {
	secret, id, err := workerSession(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AppendTranscriptRequest
	if !meetingBody(w, r, &req) {
		return
	}
	segs := make([]meetingdom.SegmentInput, len(req.Segments))
	for i, s := range req.Segments {
		segs[i] = meetingdom.SegmentInput{SpeakerID: s.SpeakerID, Text: s.Text, SpokenAt: s.SpokenAt}
	}
	if err := h.svc.AppendTranscript(r.Context(), secret, id, segs); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// WorkerTranscript handles GET /internal/meeting-agent/sessions/{sessionId}/transcript.
func (h *MeetingHandler) WorkerTranscript(w http.ResponseWriter, r *http.Request) {
	secret, id, err := workerSession(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	segs, err := h.svc.Transcript(r.Context(), secret, id, limit)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.TranscriptSegmentResponse, len(segs))
	for i, s := range segs {
		out[i] = dto.TranscriptSegmentResponse{SpeakerID: s.SpeakerID, Text: s.Text, SpokenAt: s.SpokenAt}
	}
	presenter.OK(w, r, out)
}

// WorkerFinish handles POST /internal/meeting-agent/sessions/{sessionId}/finish.
func (h *MeetingHandler) WorkerFinish(w http.ResponseWriter, r *http.Request) {
	secret, id, err := workerSession(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.Finish(r.Context(), secret, id); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}
