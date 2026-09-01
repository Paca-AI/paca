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

// DocFileHandler handles file upload/download endpoints for documents.
type DocFileHandler struct {
	svc attachmentdom.DocFileService
}

// NewDocFileHandler returns a DocFileHandler wired to the doc file service.
func NewDocFileHandler(svc attachmentdom.DocFileService) *DocFileHandler {
	return &DocFileHandler{svc: svc}
}

// InitiateDocUpload handles POST /projects/:projectId/docs/:docId/files/initiate-upload.
// Creates a pending file record and returns presigned upload URL(s).
func (h *DocFileHandler) InitiateDocUpload(w http.ResponseWriter, r *http.Request) {
	docID, err := parseDocID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.InitiateUploadRequest
	if !middleware.BindJSON(w, r, &req) {
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

	session, err := h.svc.InitiateDocUpload(r.Context(), attachmentdom.DocUploadInput{
		DocID:       docID,
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

// CompleteDocUpload handles POST /projects/:projectId/docs/:docId/files/complete-upload.
// Marks the file as uploaded and returns the file metadata.
func (h *DocFileHandler) CompleteDocUpload(w http.ResponseWriter, r *http.Request) {
	var req dto.CompleteUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	parts := make([]attachmentdom.CompletedPart, 0, len(req.Parts))
	for _, p := range req.Parts {
		parts = append(parts, attachmentdom.CompletedPart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		})
	}

	f, err := h.svc.CompleteDocUpload(r.Context(), attachmentdom.DocCompleteUploadInput{
		FileID:   req.FileID,
		UploadID: req.UploadID,
		Parts:    parts,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.Created(w, r, dto.FileFromEntity(f))
}

// GetDocFileDownloadURL handles GET /projects/:projectId/docs/:docId/files/:fileId/download-url.
// Returns a short-lived presigned URL valid for 15 minutes.
func (h *DocFileHandler) GetDocFileDownloadURL(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	docID, err := parseDocID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	fileID, err := parseDocFileID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	url, err := h.svc.GetDocFileDownloadURL(r.Context(), projectID, docID, fileID, 15*time.Minute)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.OK(w, r, dto.DownloadURLResponse{URL: url})
}

// DeleteDocFile handles DELETE /projects/:projectId/docs/:docId/files/:fileId.
func (h *DocFileHandler) DeleteDocFile(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	docID, err := parseDocID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	fileID, err := parseDocFileID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	if err := h.svc.DeleteDocFile(r.Context(), projectID, docID, fileID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.NoContent(w)
}

// --- helpers ----------------------------------------------------------------

func parseDocFileID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid file id")
	}
	return id, nil
}
