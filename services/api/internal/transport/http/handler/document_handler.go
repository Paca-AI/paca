package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// DocumentHandler handles documentation endpoints.
type DocumentHandler struct {
	svc         docdom.Service
	activitySvc docdom.ActivityService
	avatarSvc   attachmentdom.AvatarService
}

// NewDocumentHandler returns a DocumentHandler wired to the doc service and
// activity service.
func NewDocumentHandler(svc docdom.Service, activitySvc docdom.ActivityService) *DocumentHandler {
	return &DocumentHandler{svc: svc, activitySvc: activitySvc}
}

// WithDocAvatarService configures avatar URL resolution for activity
// responses (comments/system events show the actor's avatar).
func (h *DocumentHandler) WithDocAvatarService(svc attachmentdom.AvatarService) *DocumentHandler {
	h.avatarSvc = svc
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

// ListFolders handles GET /projects/:projectId/docs/folders.
func (h *DocumentHandler) ListFolders(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	folders, err := h.svc.ListFolders(r.Context(), projectID)
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

// ListDocuments handles GET /projects/:projectId/docs.
// Optional query param: folder_id — filter by folder.
func (h *DocumentHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var folderID *uuid.UUID
	if raw := r.URL.Query().Get("folder_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid folder_id"))
			return
		}
		folderID = &id
	}

	docs, err := h.svc.ListDocuments(r.Context(), projectID, folderID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocumentListItemResponse, 0, len(docs))
	for _, d := range docs {
		resp = append(resp, dto.DocumentListItemFromEntity(d))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// GetDocument handles GET /projects/:projectId/docs/:docId.
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
	var createdByAgent *uuid.UUID
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		createdBy = &actorID
	}
	if agentID, _ := middleware.AgentIDFromContext(r.Context()); agentID != uuid.Nil {
		createdByAgent = &agentID
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
			ActorAgentID: createdByAgent,
			ActivityType: docdom.ActivityTypeDocCreated,
			Content:      content,
		})
	}

	presenter.Created(w, r, dto.DocumentFromEntity(d))
}

// UpdateDocument handles PATCH /projects/:projectId/docs/:docId.
func (h *DocumentHandler) UpdateDocument(w http.ResponseWriter, r *http.Request) {
	docID, err := parseDocID(r)
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
	var updatedByAgent *uuid.UUID
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		updatedBy = &actorID
	}
	if agentID, _ := middleware.AgentIDFromContext(r.Context()); agentID != uuid.Nil {
		updatedByAgent = &agentID
	}

	// Capture old state for activity recording.
	oldDoc, _ := h.svc.GetDocument(r.Context(), docID)

	d, err := h.svc.UpdateDocument(r.Context(), docID, docdom.UpdateDocumentInput{
		Title:     req.Title,
		Content:   req.Content.Ptr(),
		FolderID:  req.FolderID.Ptr(),
		Position:  req.Position,
		UpdatedBy: updatedBy,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record update activity (best-effort).
	if updatedBy != nil && oldDoc != nil {
		changes := docChangedFields(oldDoc, req)
		if len(changes) > 0 {
			content, _ := json.Marshal(map[string]any{"changes": changes})
			_ = h.activitySvc.RecordActivity(r.Context(), docdom.RecordActivityInput{
				DocumentID:   docID,
				ProjectID:    oldDoc.ProjectID,
				ActorID:      updatedBy,
				ActorAgentID: updatedByAgent,
				ActivityType: docdom.ActivityTypeDocUpdated,
				Content:      content,
			})
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

	if err := h.svc.DeleteDocument(r.Context(), docID); err != nil {
		presenter.Error(w, r, err)
		return
	}

	// Record deletion activity (best-effort).
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		agentID, _ := middleware.AgentIDFromContext(r.Context())
		var agentIDPtr *uuid.UUID
		if agentID != uuid.Nil {
			agentIDPtr = &agentID
		}
		_ = h.activitySvc.RecordActivity(r.Context(), docdom.RecordActivityInput{
			DocumentID:   docID,
			ProjectID:    projectID,
			ActorID:      &actorID,
			ActorAgentID: agentIDPtr,
			ActivityType: docdom.ActivityTypeDocDeleted,
		})
	}

	presenter.NoContent(w)
}

// =============================================================================
// Snapshot endpoints
// =============================================================================

// ListSnapshots handles GET /projects/:projectId/docs/:docId/snapshots.
func (h *DocumentHandler) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	docID, err := parseDocID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	snaps, err := h.svc.ListSnapshots(r.Context(), docID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocSnapshotResponse, 0, len(snaps))
	for _, s := range snaps {
		resp = append(resp, dto.DocSnapshotFromEntity(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// GetSnapshot handles GET /projects/:projectId/docs/:docId/snapshots/:snapshotId.
func (h *DocumentHandler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshotID, err := parseDocSnapshotID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	snap, err := h.svc.GetSnapshot(r.Context(), snapshotID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.DocSnapshotFromEntity(snap))
}

// =============================================================================
// Activity / comment endpoints
// =============================================================================

// ListActivities handles GET /projects/:projectId/docs/:docId/activities.
func (h *DocumentHandler) ListActivities(w http.ResponseWriter, r *http.Request) {
	docID, err := parseDocID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	activities, err := h.activitySvc.ListActivities(r.Context(), docID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DocActivityResponse, 0, len(activities))
	for _, a := range activities {
		resp = append(resp, h.toDocActivityResponse(r.Context(), a))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
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

	agentID, _ := middleware.AgentIDFromContext(r.Context())
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}

	a, err := h.activitySvc.AddComment(r.Context(), docdom.AddCommentInput{
		DocumentID: docID,
		ProjectID:  projectID,
		ActorID:    actorID,
		AgentID:    agentIDPtr,
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

	agentID, _ := middleware.AgentIDFromContext(r.Context())
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}

	a, err := h.activitySvc.UpdateComment(r.Context(), commentID, projectID, actorID, agentIDPtr, req.Content)
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

	agentID, _ := middleware.AgentIDFromContext(r.Context())
	var agentIDPtr *uuid.UUID
	if agentID != uuid.Nil {
		agentIDPtr = &agentID
	}

	if err := h.activitySvc.DeleteComment(r.Context(), commentID, projectID, actorID, agentIDPtr); err != nil {
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

// docChangedFields compares the old document against the patch request and
// returns a FieldChange for each field that actually changed.
func docChangedFields(old *docdom.Document, req dto.UpdateDocumentRequest) []docdom.FieldChange {
	var changes []docdom.FieldChange

	if req.Title != nil && *req.Title != old.Title {
		changes = append(changes, docdom.FieldChange{Field: "title", Old: old.Title, New: *req.Title})
	}

	if req.Content.Set {
		if string(req.Content.Value) != string(old.Content) {
			changes = append(changes, docdom.FieldChange{Field: "content", Old: old.Content, New: req.Content.Value})
		}
	}

	if req.FolderID.Set {
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
