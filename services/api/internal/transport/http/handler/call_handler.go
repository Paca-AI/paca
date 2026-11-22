package handler

import (
	"io"
	"net/http"

	"github.com/Paca-AI/api/internal/apierr"
	calldom "github.com/Paca-AI/api/internal/domain/call"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// maxCallWebhookBody bounds a LiveKit webhook body.
const maxCallWebhookBody = 1 << 20

// CallHandler serves calls (LiveKit rooms) on channels and direct messages.
// It makes no authorization decision: every route is gated in the router (the
// webhook proves itself with LiveKit's signature).
type CallHandler struct {
	svc        calldom.Service
	listScoper ListScoper
}

// NewCallHandler returns a CallHandler.
func NewCallHandler(svc calldom.Service) *CallHandler { return &CallHandler{svc: svc} }

// WithCallListScoper limits the project's call list to the channels the
// caller may read, inside the query.
func (h *CallHandler) WithCallListScoper(s ListScoper) *CallHandler {
	h.listScoper = s
	return h
}

// Config handles GET /chat/calls/config.
func (h *CallHandler) Config(w http.ResponseWriter, r *http.Request) {
	presenter.OK(w, r, dto.CallConfigResponse{Enabled: h.svc.Enabled(), Recording: h.svc.RecordingEnabled()})
}

// Join handles POST .../channels/{channelId}/call/join.
func (h *CallHandler) Join(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	info, err := h.svc.Join(r.Context(), projectID, channelID, me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.CallJoinFromEntity(info))
}

// Get handles GET .../channels/{channelId}/call.
func (h *CallHandler) Get(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	c, err := h.svc.GetCall(r.Context(), projectID, channelID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.CallFromEntity(c))
}

// List handles GET /projects/{projectId}/calls: the open calls of the
// channels the caller may read.
func (h *CallHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	ctx, err := scopedContext(r, h.listScoper, iam.ActionChannelsRead, projectID, "channel")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	cs, err := h.svc.ListCalls(ctx, projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	items := make([]dto.CallResponse, len(cs))
	for i, c := range cs {
		items[i] = dto.CallFromEntity(c)
	}
	presenter.OK(w, r, items)
}

func (h *CallHandler) moderate(w http.ResponseWriter, r *http.Request, mute bool) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	targetID, err := chatURLID(r, "accountId", "account")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	if mute {
		err = h.svc.Mute(r.Context(), projectID, channelID, me, targetID)
	} else {
		err = h.svc.Remove(r.Context(), projectID, channelID, me, targetID)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// Mute handles POST .../call/participants/{accountId}/mute.
func (h *CallHandler) Mute(w http.ResponseWriter, r *http.Request) { h.moderate(w, r, true) }

// Remove handles DELETE .../call/participants/{accountId}.
func (h *CallHandler) Remove(w http.ResponseWriter, r *http.Request) { h.moderate(w, r, false) }

// StartRecording handles POST .../channels/{channelId}/call/recording.
func (h *CallHandler) StartRecording(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	me, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	rec, err := h.svc.StartRecording(r.Context(), projectID, channelID, me)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.CallRecordingFromEntity(rec))
}

// StopRecording handles DELETE .../channels/{channelId}/call/recording.
func (h *CallHandler) StopRecording(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.StopRecording(r.Context(), projectID, channelID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// ListRecordings handles GET .../channels/{channelId}/recordings.
func (h *CallHandler) ListRecordings(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	recs, err := h.svc.ListRecordings(r.Context(), projectID, channelID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.CallRecordingResponse, len(recs))
	for i, rec := range recs {
		out[i] = dto.CallRecordingFromEntity(rec)
	}
	presenter.OK(w, r, out)
}

// RecordingURL handles GET .../channels/{channelId}/recordings/{recordingId}/url;
// ?download=1 makes the link save the file instead of playing it.
func (h *CallHandler) RecordingURL(w http.ResponseWriter, r *http.Request) {
	projectID, channelID, err := chatIDs(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	id, err := chatURLID(r, "recordingId", "recording")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	url, err := h.svc.RecordingURL(r.Context(), projectID, channelID, id, r.URL.Query().Get("download") == "1")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.RecordingURLResponse{URL: url})
}

// ReceiveWebhook handles POST /webhooks/livekit. It is public: LiveKit signs
// the delivery with the API secret and the service verifies it.
func (h *CallHandler) ReceiveWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCallWebhookBody))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "webhook body is unreadable or too large"))
		return
	}
	if err := h.svc.HandleWebhook(r.Context(), r.Header.Get("Authorization"), body); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}
