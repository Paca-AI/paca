package handler

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// AttachmentHandler handles task-attachment endpoints.
type AttachmentHandler struct {
	svc attachmentdom.Service
}

// NewAttachmentHandler returns an AttachmentHandler wired to the attachment service.
func NewAttachmentHandler(svc attachmentdom.Service) *AttachmentHandler {
	return &AttachmentHandler{svc: svc}
}

// InitiateUpload handles POST /projects/:projectId/tasks/:taskId/attachments/initiate-upload.
// It creates a pending file record and returns either a single presigned PUT URL
// (for files < 5 MiB) or a multipart upload session with per-part URLs.
func (h *AttachmentHandler) InitiateUpload(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.InitiateUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.FileName == "" || req.ContentType == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "file_name and content_type are required"))
		return
	}
	if req.FileSize <= 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "file_size must be greater than 0"))
		return
	}

	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	uploaderID, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "invalid subject in token"))
		return
	}

	session, err := h.svc.InitiateUpload(r.Context(), projectID, attachmentdom.InitiateUploadInput{
		TaskID:      taskID,
		FileName:    req.FileName,
		ContentType: req.ContentType,
		FileSize:    req.FileSize,
		UploadedBy:  uploaderID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.Created(w, r, dto.UploadSessionFromDomain(session))
}

// CompleteUpload handles POST /projects/:projectId/tasks/:taskId/attachments/complete-upload.
// The client calls this after successfully uploading the file (or all parts) to
// the object store.  For multipart uploads the completed parts (with ETags) must
// be supplied so the server can reassemble the object.
func (h *AttachmentHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CompleteUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.FileID == uuid.Nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "file_id is required"))
		return
	}

	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}
	creatorID, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "invalid subject in token"))
		return
	}

	parts := make([]attachmentdom.CompletedPart, 0, len(req.Parts))
	for _, p := range req.Parts {
		parts = append(parts, attachmentdom.CompletedPart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		})
	}

	attachment, err := h.svc.CompleteUpload(r.Context(), projectID, attachmentdom.CompleteUploadInput{
		FileID:    req.FileID,
		TaskID:    taskID,
		CreatedBy: creatorID,
		UploadID:  req.UploadID,
		Parts:     parts,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.Created(w, r, dto.TaskAttachmentFromEntity(attachment))
}

// ListTaskAttachments handles GET /projects/:projectId/tasks/:taskId/attachments.
func (h *AttachmentHandler) ListTaskAttachments(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	attachments, err := h.svc.ListTaskAttachments(r.Context(), projectID, taskID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	resp := make([]dto.TaskAttachmentResponse, 0, len(attachments))
	for _, a := range attachments {
		resp = append(resp, dto.TaskAttachmentFromEntity(a))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// GetDownloadURL handles GET /projects/:projectId/tasks/:taskId/attachments/:attachmentId/download-url.
// Returns a short-lived presigned URL valid for 15 minutes.
// Add ?download=true to receive a URL with Content-Disposition: attachment
// so the browser forces a file download instead of inline preview.
func (h *AttachmentHandler) GetDownloadURL(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	attachmentID, err := parseAttachmentID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	forceDownload := r.URL.Query().Get("download") == "true"

	url, err := h.svc.GetDownloadURL(r.Context(), projectID, taskID, attachmentID, 15*time.Minute, forceDownload)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, dto.DownloadURLResponse{URL: url})
}

// GetAttachmentContent handles GET /projects/:projectId/tasks/:taskId/attachments/:attachmentId/content.
// Unlike GetDownloadURL, this streams the file's bytes directly through the
// API rather than handing back a presigned object-store URL — the right path
// for server-side callers (e.g. the MCP server) that may not have network
// access to the object store's public-facing endpoint.
func (h *AttachmentHandler) GetAttachmentContent(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	attachmentID, err := parseAttachmentID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	data, contentType, err := h.svc.GetAttachmentContent(r.Context(), projectID, taskID, attachmentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// DeleteTaskAttachment handles DELETE /projects/:projectId/tasks/:taskId/attachments/:attachmentId.
func (h *AttachmentHandler) DeleteTaskAttachment(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	taskID, err := parseTaskID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	attachmentID, err := parseAttachmentID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	if err := h.svc.DeleteTaskAttachment(r.Context(), projectID, taskID, attachmentID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.NoContent(w)
}

// --- helpers ---------------------------------------------------------------

func parseAttachmentID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "attachmentId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid attachment id")
	}
	return id, nil
}
