package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	docdom "github.com/Paca-AI/api/internal/domain/doc"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// docAuthorizer is the part of the IAM authorizer the by-path routes need.
// Their route gate can only check docs:read / docs:write on the project: the
// document and its folder are known once the path is resolved, so the handler
// authorizes them then, the way the DocCreate / DocChange gates do for a
// request that names its folder in the body.
type docAuthorizer interface {
	Authorize(ctx context.Context, p iam.Principal, action, resource string, opts ...iam.AuthzOption) (iam.Result, error)
	AuthorizeCreate(ctx context.Context, p iam.Principal, action, resource string, attrs map[string][]string) (iam.Result, error)
	AuthorizeChange(ctx context.Context, p iam.Principal, action, resource string, newAttrs map[string][]string) (iam.Result, error)
}

// WithDocAuthorizer sets the authorizer the by-path routes check the resolved
// folder or document with. Without one they refuse every request.
func (h *DocumentHandler) WithDocAuthorizer(a docAuthorizer) *DocumentHandler {
	h.authz = a
	return h
}

// errDocAuthzMissing fails closed when no authorizer is configured.
var errDocAuthzMissing = apierr.New(apierr.CodeInternalError, "authorization not configured")

// GetDocumentByPath handles GET /projects/:projectId/docs/by-path?path=… . A
// document the caller may not read answers 404, as a missing one does, so the
// reply never tells that it exists. ETag and If-None-Match work as on
// GET /docs/:docId.
func (h *DocumentHandler) GetDocumentByPath(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if h.authz == nil {
		presenter.Error(w, r, errDocAuthzMissing)
		return
	}
	p, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	_, d, err := h.svc.ResolveDocPath(r.Context(), projectID, r.URL.Query().Get("path"))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	res, err := h.authz.Authorize(r.Context(), p, string(iam.ActionDocsRead), docResource(projectID, d.ID))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if !res.Allowed {
		presenter.Error(w, r, docdom.ErrDocNotFound)
		return
	}
	etag := docETag(d)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if inm := r.Header.Get("If-None-Match"); inm != "" && etagMatches(inm, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	presenter.OK(w, r, dto.DocumentFromEntity(d))
}

// PutDocumentByPath handles PUT /projects/:projectId/docs/by-path: create the
// document at body.path (and any missing folder), or update it. 201 when it
// was created, 200 otherwise; the body is the document plus "changed" (false
// when the content, title, folder and position are as they were: an identical
// push, even one that records a new source commit).
func (h *DocumentHandler) PutDocumentByPath(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if h.authz == nil {
		presenter.Error(w, r, errDocAuthzMissing)
		return
	}
	p, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	ifMatch, err := ifMatchHash(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.PutDocumentByPathRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if len(req.Content) == 0 || string(req.Content) == "null" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content is required"))
		return
	}
	if err := dto.ValidateBlockNoteContent(req.Content); err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "content: "+err.Error()))
		return
	}
	if req.Lock != nil && *req.Lock && req.Source == nil {
		presenter.Error(w, r, docdom.ErrDocLockNeedsSource)
		return
	}

	var actor *uuid.UUID
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok {
		actor = &actorID
	}
	// The document as it was when it was authorized: the "old" side of the
	// activity entry.
	var oldDoc *docdom.Document
	d, change, created, err := h.svc.UpsertDocumentByPath(r.Context(), docdom.UpsertByPathInput{
		ProjectID: projectID,
		Path:      req.Path,
		Content:   req.Content,
		Source:    req.Source.Entity(),
		Lock:      req.Lock,
		IfMatch:   ifMatch,
		Actor:     actor,
		Authorize: func(ctx context.Context, folderID *uuid.UUID, existing *docdom.Document) error {
			oldDoc = existing
			return h.authorizeDocWrite(ctx, p, projectID, folderID, existing)
		},
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	w.Header().Set("ETag", docETag(d))

	if actor != nil {
		switch {
		case created:
			content, _ := json.Marshal(map[string]any{"title": d.Title})
			_ = h.activitySvc.RecordActivity(r.Context(), docdom.RecordActivityInput{
				DocumentID: d.ID, ProjectID: projectID, ActorID: actor,
				ActivityType: docdom.ActivityTypeDocCreated, Content: content,
			})
		case oldDoc != nil && change.Any():
			h.recordDocUpdate(r.Context(), projectID, oldDoc, d, change, *actor)
		}
	}

	resp := dto.DocumentWriteResponse{DocumentResponse: dto.DocumentFromEntity(d), Changed: created || change.Edited()}
	if created {
		presenter.Created(w, r, resp)
		return
	}
	presenter.OK(w, r, resp)
}

// authorizeDocWrite checks docs:write where a by-path write lands: creating a
// document in folderID (the deepest folder of the path that exists; a Deny on
// it or an ancestor applies to the folders the write creates below it), or
// changing existing.
func (h *DocumentHandler) authorizeDocWrite(ctx context.Context, p iam.Principal, projectID uuid.UUID, folderID *uuid.UUID, existing *docdom.Document) error {
	attrs := map[string][]string{}
	if folderID != nil {
		attrs["doc.folder_id"] = []string{folderID.String()}
	}
	var res iam.Result
	var err error
	if existing == nil {
		res, err = h.authz.AuthorizeCreate(ctx, p, string(iam.ActionDocsWrite), "project/"+projectID.String()+"/doc/*", attrs)
	} else {
		res, err = h.authz.AuthorizeChange(ctx, p, string(iam.ActionDocsWrite), docResource(projectID, existing.ID), attrs)
	}
	if err != nil {
		return err
	}
	if !res.Allowed {
		return apierr.New(apierr.CodeForbidden, "insufficient permissions")
	}
	return nil
}

// recordDocUpdate records the doc.updated activity of a write (none for a
// source-only change) and notifies people newly @mentioned in new content.
func (h *DocumentHandler) recordDocUpdate(ctx context.Context, projectID uuid.UUID, oldDoc, d *docdom.Document, change docdom.DocChange, actor uuid.UUID) {
	if changes := docChangedFields(oldDoc, d, change); len(changes) > 0 {
		content, _ := json.Marshal(map[string]any{"changes": changes})
		_ = h.activitySvc.RecordActivity(ctx, docdom.RecordActivityInput{
			DocumentID: d.ID, ProjectID: projectID, ActorID: &actor,
			ActivityType: docdom.ActivityTypeDocUpdated, Content: content,
		})
	}
	if h.notificationSvc != nil && change.Content {
		for _, mentionedID := range newlyMentionedUserIDs(oldDoc.Content, d.Content) {
			h.notificationSvc.NotifyDocMentioned(ctx, mentionedID, actor, projectID, d.ID)
		}
	}
}

func docResource(projectID, docID uuid.UUID) string {
	return "project/" + projectID.String() + "/doc/" + docID.String()
}
