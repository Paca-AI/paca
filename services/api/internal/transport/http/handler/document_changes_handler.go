package handler

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// WithDocChanges configures the change feed and batch read (ListChanges and
// BatchGetDocuments).
func (h *DocumentHandler) WithDocChanges(svc docdom.ChangesService) *DocumentHandler {
	h.changesSvc = svc
	return h
}

// ListChanges handles GET /projects/:projectId/docs/changes?since=<cursor> —
// what changed in the documents the caller can read since the cursor of its
// last call. It answers with
//
//	{"cursor": "...", "reset": false, "docs": [...], "folders": null | [...]}
//
// docs are metadata only (fetch bodies with batch-get); a deleted document is
// an entry with deleted = true. folders is the whole folder tree when a folder
// changed, else null. Without since, or when the cursor is unusable or the set
// of documents the caller may read changed, reset is true and docs is the full
// list: the client drops everything that is not in it. See
// docs/api/doc-change-feed.md.
func (h *DocumentHandler) ListChanges(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if h.changesSvc == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "the document change feed is not configured"))
		return
	}
	ctx, err := scopedContext(r, h.listScoper, iam.ActionDocsRead, projectID, "doc")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	set, err := h.changesSvc.Changes(ctx, projectID, r.URL.Query().Get("since"))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	// Per caller and always fresh: never stored or replayed by a cache.
	w.Header().Set("Cache-Control", "private, no-store")
	presenter.OK(w, r, dto.DocChangesFromEntity(set))
}

// BatchGetDocuments handles POST /projects/:projectId/docs/batch-get with
// {"ids": ["..."]} (at most 100) — the listed documents with their bodies, in
// the order asked for. An id the caller cannot read, or that is deleted,
// unknown or in another project, is left out: the reply does not say which.
func (h *DocumentHandler) BatchGetDocuments(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if h.changesSvc == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "the document batch read is not configured"))
		return
	}
	var req dto.BatchGetDocumentsRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.IDs) > docdom.MaxBatchGetIDs {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, fmt.Sprintf("at most %d ids per request", docdom.MaxBatchGetIDs)))
		return
	}
	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, raw := range req.IDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid document id"))
			return
		}
		ids = append(ids, id)
	}
	ctx, err := scopedContext(r, h.listScoper, iam.ActionDocsRead, projectID, "doc")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	docs, err := h.changesSvc.BatchGet(ctx, projectID, ids)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	items := make([]dto.DocumentResponse, 0, len(docs))
	for _, d := range docs {
		items = append(items, dto.DocumentFromEntity(d))
	}
	w.Header().Set("Cache-Control", "private, no-store")
	presenter.OK(w, r, map[string]any{"items": items})
}
