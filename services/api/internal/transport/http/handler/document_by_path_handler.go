package handler

import (
	"context"
	"encoding/json"
	"errors"
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
	if err := req.Validate(); err != nil {
		presenter.Error(w, r, err)
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
		if oldDoc != nil {
			err = h.withLastNote(r.Context(), projectID, oldDoc.ID, err)
		}
		presenter.Error(w, r, err)
		return
	}
	w.Header().Set("ETag", docETag(d))

	if actor != nil {
		switch {
		case created:
			h.recordDocCreated(r.Context(), projectID, d, *actor, req.WriteNote)
		case oldDoc != nil && change.Any():
			h.recordDocUpdate(r.Context(), projectID, oldDoc, d, d.Content, change, *actor, req.WriteNote)
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

// recordDocCreated records the doc.created activity of a new document, with
// the write's note, client label and task reference.
func (h *DocumentHandler) recordDocCreated(ctx context.Context, projectID uuid.UUID, d *docdom.Document, actor uuid.UUID, note dto.WriteNote) {
	content, _ := json.Marshal(activityContent(note, "title", d.Title))
	_ = h.activitySvc.RecordActivity(ctx, docdom.RecordActivityInput{
		DocumentID: d.ID, ProjectID: projectID, ActorID: &actor,
		ActivityType: docdom.ActivityTypeDocCreated, Content: content,
	})
}

// recordDocUpdate records the doc.updated activity of a write, with its note,
// client label and task reference (no entry, so no note, for a source-only
// change), and notifies people newly @mentioned in newContent (the content
// the write sent).
func (h *DocumentHandler) recordDocUpdate(ctx context.Context, projectID uuid.UUID, oldDoc, d *docdom.Document, newContent json.RawMessage, change docdom.DocChange, actor uuid.UUID, note dto.WriteNote) {
	if changes := docChangedFields(oldDoc, d, change); len(changes) > 0 {
		content, _ := json.Marshal(activityContent(note, "changes", changes))
		_ = h.activitySvc.RecordActivity(ctx, docdom.RecordActivityInput{
			DocumentID: d.ID, ProjectID: projectID, ActorID: &actor,
			ActivityType: docdom.ActivityTypeDocUpdated, Content: content,
		})
	}
	if h.notificationSvc != nil && change.Content && newContent != nil {
		for _, mentionedID := range newlyMentionedUserIDs(oldDoc.Content, newContent) {
			h.notificationSvc.NotifyDocMentioned(ctx, mentionedID, actor, projectID, d.ID)
		}
	}
}

// activityContent is an activity entry's content: key: value plus the
// write's non-empty note, client and task.
func activityContent(note dto.WriteNote, key string, value any) map[string]any {
	out := map[string]any{key: value}
	for k, v := range note.Fields() {
		out[k] = v
	}
	return out
}

// lastNotePages bounds how far back withLastNote looks for the write that
// stored the current content.
const lastNotePages = 3

// withLastNote adds to a version conflict what the write that stored the
// current content said about itself: the note, client and task of the
// document's newest doc.created entry or doc.updated entry with a content
// change. Any other error is returned as it is. Best effort: a failed lookup
// leaves them empty.
func (h *DocumentHandler) withLastNote(ctx context.Context, projectID, docID uuid.UUID, err error) error {
	var conflict *docdom.VersionConflictError
	if !errors.As(err, &conflict) || conflict.CurrentHash == "" {
		return err
	}
	var cursor *string
	for range lastNotePages {
		page, next, lerr := h.activitySvc.ListActivities(ctx, projectID, docID, cursor, 50)
		if lerr != nil {
			return err
		}
		for _, a := range page {
			if n, ok := contentWriteNote(a); ok {
				conflict.Note, conflict.Client, conflict.Task = n.Note, n.Client, n.Task
				return err
			}
		}
		if next == nil {
			return err
		}
		cursor = next
	}
	return err
}

// contentWriteNote reports whether the entry stored content (a doc.created entry, or
// a doc.updated entry with a content change) and returns its note.
func contentWriteNote(a *docdom.Activity) (dto.WriteNote, bool) {
	var c struct {
		dto.WriteNote
		Changes []struct {
			Field string `json:"field"`
		} `json:"changes"`
	}
	switch a.ActivityType {
	case docdom.ActivityTypeDocCreated:
	case docdom.ActivityTypeDocUpdated:
		if json.Unmarshal(a.Content, &c) != nil {
			return dto.WriteNote{}, false
		}
		for _, ch := range c.Changes {
			if ch.Field == "content" {
				return c.WriteNote, true
			}
		}
		return dto.WriteNote{}, false
	default:
		return dto.WriteNote{}, false
	}
	_ = json.Unmarshal(a.Content, &c)
	return c.WriteNote, true
}

func docResource(projectID, docID uuid.UUID) string {
	return "project/" + projectID.String() + "/doc/" + docID.String()
}
