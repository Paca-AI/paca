package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	notificationdom "github.com/Paca-AI/api/internal/domain/notification"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// DocumentHandler handles documentation endpoints.
type DocumentHandler struct {
	svc             docdom.Service
	changesSvc      docdom.ChangesService
	activitySvc     docdom.ActivityService
	avatarSvc       attachmentdom.AvatarService
	notificationSvc notificationdom.Service
	listScoper      ListScoper
}

// NewDocumentHandler returns a DocumentHandler wired to the doc service and
// activity service.
func NewDocumentHandler(svc docdom.Service, activitySvc docdom.ActivityService) *DocumentHandler {
	return &DocumentHandler{svc: svc, activitySvc: activitySvc}
}

// WithDocListScoper limits document lists and search to the documents the
// caller may read, inside the query (see scopedContext).
func (h *DocumentHandler) WithDocListScoper(s ListScoper) *DocumentHandler {
	h.listScoper = s
	return h
}

// WithDocAvatarService configures avatar URL resolution for activity
// responses (comments/system events show the actor's avatar).
func (h *DocumentHandler) WithDocAvatarService(svc attachmentdom.AvatarService) *DocumentHandler {
	h.avatarSvc = svc
	return h
}

// WithDocNotificationService configures dispatching a plugin event (no
// in-app row) when a document's content is updated to newly @mention a
// user (see notificationdom.Service.NotifyDocMentioned).
func (h *DocumentHandler) WithDocNotificationService(svc notificationdom.Service) *DocumentHandler {
	h.notificationSvc = svc
	return h
}

// toDocActivityResponse maps a to a DocActivityResponse and, if an
// AvatarService is configured, resolves the actor's avatar keys into
// presigned display URLs.
func (h *DocumentHandler) toDocActivityResponse(ctx context.Context, a *docdom.Activity) dto.DocActivityResponse {
	resp := dto.DocActivityFromEntity(a)
	if h.avatarSvc != nil {
		resp.ActorAvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, a.ActorAvatarKey)
		resp.ActorAvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, a.ActorAvatarThumbKey)
	}
	return resp
}

// =============================================================================
// Folder endpoints
// =============================================================================

// parseTreeParent reads the parent folder of a tree listing: a folder id, or
// "null"/empty for the root level.
func parseTreeParent(raw string) (*uuid.UUID, error) {
	if raw == "" || strings.EqualFold(raw, "null") {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "invalid folder id")
	}
	return &id, nil
}

// ListFolders handles GET /projects/:projectId/docs/folders — one
// cursor-paginated level of the folder tree, in manual order:
//   - parent_id=<uuid>|null  the folder whose children to list (default null = top level)
//   - name=<text>            only folders with exactly this name (case-insensitive)
//   - page_size=<1-200>      (default 50)
//   - cursor=<opaque>        from the previous page's next_cursor
//
// Responds with {"items": [...], "next_cursor": ...}.
func (h *DocumentHandler) ListFolders(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	q := r.URL.Query()
	parent, err := parseTreeParent(q.Get("parent_id"))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	pageSize, err := parsePageSize(r, 50, 200)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	cursor := parseCursorParam(r)
	folders, hasMore, err := h.svc.ListFolders(r.Context(), projectID, parent, docdom.FolderFilter{Name: strings.TrimSpace(q.Get("name"))}, cursor, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocFolderResponse, 0, len(folders))
	for _, f := range folders {
		resp = append(resp, dto.DocFolderFromEntity(f))
	}
	var nextCursor *string
	if hasMore && len(folders) > 0 {
		s := docdom.EncodeFolderTreeCursor(folders[len(folders)-1])
		nextCursor = &s
	}
	presenter.OK(w, r, map[string]any{"items": resp, "next_cursor": nextCursor})
}

// GetFolderPath handles GET /projects/:projectId/docs/folders/:folderId/path —
// the folders from the root down to this one (inclusive), for breadcrumbs.
func (h *DocumentHandler) GetFolderPath(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	folderID, err := uuid.Parse(chi.URLParam(r, "folderId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid folder id"))
		return
	}
	folders, err := h.svc.FolderPath(r.Context(), projectID, folderID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocFolderResponse, 0, len(folders))
	for _, f := range folders {
		resp = append(resp, dto.DocFolderFromEntity(f))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// CreateFolder handles POST /projects/:projectId/docs/folders.
func (h *DocumentHandler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateFolderRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name is required"))
		return
	}

	var createdBy *uuid.UUID
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		createdBy = &actorID
	}

	f, err := h.svc.CreateFolder(r.Context(), docdom.CreateFolderInput{
		ProjectID: projectID,
		ParentID:  req.ParentID,
		Name:      req.Name,
		CreatedBy: createdBy,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	presenter.Created(w, r, dto.DocFolderFromEntity(f))
}

// UpdateFolder handles PATCH /projects/:projectId/docs/folders/:folderId.
func (h *DocumentHandler) UpdateFolder(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	folderID, err := parseDocFolderID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateFolderRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	f, err := h.svc.UpdateFolder(r.Context(), folderID, docdom.UpdateFolderInput{
		ProjectID: projectID,
		Name:      req.Name,
		ParentID:  req.ParentID.Ptr(),
		Position:  req.Position,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.DocFolderFromEntity(f))
}

// DeleteFolder handles DELETE /projects/:projectId/docs/folders/:folderId.
func (h *DocumentHandler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	folderID, err := parseDocFolderID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteFolder(r.Context(), folderID, projectID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// =============================================================================
// Document endpoints
// =============================================================================

// ListDocuments handles GET /projects/:projectId/docs — one cursor-paginated
// page of documents in manual order (position, title), without their bodies:
//   - folder_id=<uuid>|null  only documents directly in that folder / outside any
//     folder; omit for documents anywhere in the project
//   - search=<text>          case-insensitive title match (substring)
//   - title=<text>           case-insensitive exact title match
//   - page_size=<1-200>      (default 50)
//   - cursor=<opaque>        from the previous page's next_cursor
//
// Responds with {"items": [...], "next_cursor": ...}.
func (h *DocumentHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	q := r.URL.Query()
	var filter docdom.DocumentFilter
	if raw, ok := q["folder_id"]; ok && len(raw) > 0 && raw[0] != "" {
		folderID, err := parseTreeParent(raw[0])
		if err != nil {
			presenter.Error(w, r, err)
			return
		}
		filter.FolderID = folderID
		filter.RootOnly = folderID == nil
	}
	filter.Search = strings.TrimSpace(q.Get("search"))
	filter.Title = strings.TrimSpace(q.Get("title"))
	pageSize, err := parsePageSize(r, 50, 200)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	cursor := parseCursorParam(r)
	ctx, err := scopedContext(r, h.listScoper, iam.ActionDocsRead, projectID, "doc")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	docs, hasMore, err := h.svc.ListDocuments(ctx, projectID, filter, cursor, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocumentListItemResponse, 0, len(docs))
	for _, d := range docs {
		resp = append(resp, dto.DocumentListItemFromEntity(d))
	}
	var nextCursor *string
	if hasMore && len(docs) > 0 {
		s := docdom.EncodeDocumentTreeCursor(docs[len(docs)-1])
		nextCursor = &s
	}
	presenter.OK(w, r, map[string]any{"items": resp, "next_cursor": nextCursor})
}

// SearchDocuments handles GET /projects/:projectId/docs/search.
// Query params: q (required) — text to find in a document's title or body,
// case-insensitive; limit (1-50, default 20).
func (h *DocumentHandler) SearchDocuments(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "q is required"))
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 50 {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "limit must be an integer between 1 and 50"))
			return
		}
		limit = n
	}
	ctx, err := scopedContext(r, h.listScoper, iam.ActionDocsRead, projectID, "doc")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	hits, err := h.svc.SearchDocuments(ctx, projectID, q, limit)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocumentSearchHitResponse, 0, len(hits))
	for _, hit := range hits {
		resp = append(resp, dto.DocumentSearchHitFromEntity(hit))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// GetDocument handles GET /projects/:projectId/docs/:docId. The ETag covers
// the whole response (content hash plus a tag over title, folder, position and
// the other fields, see docETag), so If-None-Match gets 304 with no body only
// when nothing the client would receive has changed. content_hash in the body
// is the content alone, and is what If-Match on a write compares.
func (h *DocumentHandler) GetDocument(w http.ResponseWriter, r *http.Request) {
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
	d, err := h.svc.GetDocument(r.Context(), docID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if d.ProjectID != projectID {
		presenter.Error(w, r, docdom.ErrDocNotFound)
		return
	}
	etag := docETag(d)
	w.Header().Set("ETag", etag)
	// An authenticated response: never served from a cache without revalidating.
	w.Header().Set("Cache-Control", "private, no-cache")
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	presenter.OK(w, r, dto.DocumentFromEntity(d))
}

// CreateDocument handles POST /projects/:projectId/docs.
func (h *DocumentHandler) CreateDocument(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateDocumentRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := dto.ValidateBlockNoteContent(req.Content); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content: "+err.Error()))
		return
	}

	var createdBy *uuid.UUID
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		createdBy = &actorID
	}

	d, err := h.svc.CreateDocument(r.Context(), docdom.CreateDocumentInput{
		ProjectID: projectID,
		FolderID:  req.FolderID,
		Title:     req.Title,
		Content:   req.Content,
		CreatedBy: createdBy,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record creation activity (best-effort).
	if createdBy != nil {
		content, _ := json.Marshal(map[string]string{"title": d.Title})
		_ = h.activitySvc.RecordActivity(r.Context(), docdom.RecordActivityInput{
			DocumentID:   d.ID,
			ProjectID:    projectID,
			ActorID:      createdBy,
			ActivityType: docdom.ActivityTypeDocCreated,
			Content:      content,
		})
	}

	w.Header().Set("ETag", docETag(d))
	presenter.Created(w, r, dto.DocumentFromEntity(d))
}

// UpdateDocument handles PATCH /projects/:projectId/docs/:docId. An If-Match
// content hash (or document ETag) whose content is no longer current gets 412
// with the stored version in error_details. A write that changes nothing is a
// 200 that stores nothing.
//
// If-Match is a precondition on content only: a rename or a move since the
// caller read the document does not fail it. A caller that also sends title or
// folder_id overwrites whatever is stored for those, so send only the fields
// you mean to change.
func (h *DocumentHandler) UpdateDocument(w http.ResponseWriter, r *http.Request) {
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

	ifMatch, err := ifMatchHash(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.UpdateDocumentRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Content.Set && req.Content.Value != nil {
		if err := dto.ValidateBlockNoteContent(req.Content.Value); err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content: "+err.Error()))
			return
		}
	}

	var updatedBy *uuid.UUID
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		updatedBy = &actorID
	}

	// Capture old state for activity recording.
	oldDoc, _ := h.svc.GetDocument(r.Context(), docID)

	d, change, err := h.svc.UpdateDocument(r.Context(), projectID, docID, docdom.UpdateDocumentInput{
		Title:     req.Title,
		Content:   req.Content.Ptr(),
		FolderID:  req.FolderID.Ptr(),
		Position:  req.Position,
		UpdatedBy: updatedBy,
		IfMatch:   ifMatch,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	w.Header().Set("ETag", docETag(d))

	// Record update activity (best-effort). A write that changed nothing was
	// not stored, so it records no activity (and so publishes no doc.updated
	// event) and notifies no one.
	if updatedBy != nil && oldDoc != nil && change.Any() {
		changes := docChangedFields(oldDoc, req, change)
		if len(changes) > 0 {
			content, _ := json.Marshal(map[string]any{"changes": changes})
			_ = h.activitySvc.RecordActivity(r.Context(), docdom.RecordActivityInput{
				DocumentID:   docID,
				ProjectID:    oldDoc.ProjectID,
				ActorID:      updatedBy,
				ActivityType: docdom.ActivityTypeDocUpdated,
				Content:      content,
			})
		}

		// Publish a plugin event for each user newly @mentioned in the
		// content (no in-app row — see notificationSvc.NotifyDocMentioned's
		// doc comment).
		if h.notificationSvc != nil && change.Content && req.Content.Value != nil {
			for _, mentionedID := range newlyMentionedUserIDs(oldDoc.Content, req.Content.Value) {
				h.notificationSvc.NotifyDocMentioned(r.Context(), mentionedID, *updatedBy, oldDoc.ProjectID, docID)
			}
		}
	}

	presenter.OK(w, r, dto.DocumentFromEntity(d))
}

// DeleteDocument handles DELETE /projects/:projectId/docs/:docId.
func (h *DocumentHandler) DeleteDocument(w http.ResponseWriter, r *http.Request) {
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

	if err := h.svc.DeleteDocument(r.Context(), projectID, docID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record deletion activity (best-effort).
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		_ = h.activitySvc.RecordActivity(r.Context(), docdom.RecordActivityInput{
			DocumentID:   docID,
			ProjectID:    projectID,
			ActorID:      &actorID,
			ActivityType: docdom.ActivityTypeDocDeleted,
		})
	}

	presenter.NoContent(w)
}

// =============================================================================
// Snapshot endpoints
// =============================================================================

// ListSnapshots handles GET /projects/:projectId/docs/:docId/snapshots — one
// cursor-paginated page of the document's version history, newest first,
// without the snapshot bodies (GET a single snapshot for its content):
//   - content_hash=<hex> only the snapshots holding that content version
//   - page_size=<1-100>  (default 30)
//   - cursor=<opaque>    from the previous page's next_cursor
func (h *DocumentHandler) ListSnapshots(w http.ResponseWriter, r *http.Request) {
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
	pageSize, err := parsePageSize(r, 30, 100)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	cursor := parseCursorParam(r)
	var contentHash *string
	if raw := strings.TrimSpace(r.URL.Query().Get("content_hash")); raw != "" {
		hash, ok := normalizeContentHash(raw)
		if !ok {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content_hash must be 64 hex characters"))
			return
		}
		contentHash = &hash
	}
	snaps, hasMore, err := h.svc.ListSnapshots(r.Context(), projectID, docID, contentHash, cursor, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocSnapshotResponse, 0, len(snaps))
	for _, s := range snaps {
		resp = append(resp, dto.DocSnapshotFromEntity(s))
	}
	var nextCursor *string
	if hasMore && len(snaps) > 0 {
		c := docdom.EncodeSnapshotCursor(snaps[len(snaps)-1])
		nextCursor = &c
	}
	presenter.OK(w, r, map[string]any{"items": resp, "next_cursor": nextCursor})
}

// GetSnapshot handles GET /projects/:projectId/docs/:docId/snapshots/:snapshotId.
func (h *DocumentHandler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	snapshotID, err := parseDocSnapshotID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	snap, err := h.svc.GetSnapshot(r.Context(), projectID, snapshotID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.DocSnapshotFromEntity(snap))
}

// =============================================================================
// Activity / comment endpoints
// =============================================================================

// ListActivities handles GET /projects/:projectId/docs/:docId/activities — one
// cursor-paginated page of the timeline, newest first (page_size 1-100,
// default 30; cursor from the previous page's next_cursor).
func (h *DocumentHandler) ListActivities(w http.ResponseWriter, r *http.Request) {
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
	pageSize, err := parsePageSize(r, 30, 100)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	cursor := parseCursorParam(r)
	activities, next, err := h.activitySvc.ListActivities(r.Context(), projectID, docID, cursor, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocActivityResponse, 0, len(activities))
	for _, a := range activities {
		resp = append(resp, h.toDocActivityResponse(r.Context(), a))
	}
	presenter.OK(w, r, map[string]any{"items": resp, "next_cursor": next})
}

// AddComment handles POST /projects/:projectId/docs/:docId/comments.
func (h *DocumentHandler) AddComment(w http.ResponseWriter, r *http.Request) {
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

	actorID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	var req dto.AddDocCommentRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.Content) == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content is required"))
		return
	}

	a, err := h.activitySvc.AddComment(r.Context(), docdom.AddCommentInput{
		DocumentID: docID,
		ProjectID:  projectID,
		ActorID:    actorID,
		Content:    req.Content,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, h.toDocActivityResponse(r.Context(), a))
}

// UpdateComment handles PATCH /projects/:projectId/docs/:docId/comments/:commentId.
func (h *DocumentHandler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	commentID, err := parseDocCommentID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	actorID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	var req dto.UpdateDocCommentRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.Content) == 0 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content is required"))
		return
	}

	a, err := h.activitySvc.UpdateComment(r.Context(), commentID, projectID, actorID, req.Content)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toDocActivityResponse(r.Context(), a))
}

// DeleteComment handles DELETE /projects/:projectId/docs/:docId/comments/:commentId.
func (h *DocumentHandler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	commentID, err := parseDocCommentID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	actorID, ok := middleware.ActorIDFromContext(r.Context())
	if !ok {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return
	}

	if err := h.activitySvc.DeleteComment(r.Context(), commentID, projectID, actorID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// =============================================================================
// Parse helpers
// =============================================================================

func parseDocID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "docId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid document id")
	}
	return id, nil
}

func parseDocFolderID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "folderId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid folder id")
	}
	return id, nil
}

func parseDocSnapshotID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "snapshotId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid snapshot id")
	}
	return id, nil
}

func parseDocCommentID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "commentId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid comment id")
	}
	return id, nil
}

// =============================================================================
// Activity helpers
// =============================================================================

// docChangedFields returns a FieldChange for each field the service reports
// changed, with the old value from old and the new one from the request.
// Content counts as changed only when its content hash did (change.Content),
// never because the JSON bytes differ.
func docChangedFields(old *docdom.Document, req dto.UpdateDocumentRequest, change docdom.DocChange) []docdom.FieldChange {
	var changes []docdom.FieldChange

	if change.Title && req.Title != nil {
		changes = append(changes, docdom.FieldChange{Field: "title", Old: old.Title, New: strings.TrimSpace(*req.Title)})
	}

	if change.Content && req.Content.Set {
		changes = append(changes, docdom.FieldChange{Field: "content", Old: old.Content, New: req.Content.Value})
	}

	if change.Folder && req.FolderID.Set {
		oldVal := uuidPtrToStr(old.FolderID)
		newVal := ""
		if req.FolderID.Value != nil {
			newVal = req.FolderID.Value.String()
		}
		if oldVal != newVal {
			changes = append(changes, docdom.FieldChange{Field: "folder_id", Old: oldVal, New: newVal})
		}
	}

	return changes
}
