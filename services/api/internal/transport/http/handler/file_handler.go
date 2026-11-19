package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	filedom "github.com/Paca-AI/api/internal/domain/file"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// fileDownloadTTL is how long a presigned file URL stays valid.
const fileDownloadTTL = 15 * time.Minute

// FileHandler serves the Drive-style file manager. The same handlers back the
// project scope (/projects/{projectId}/files/...) and the general scope
// (/files/...); the scope is read from the route.
type FileHandler struct {
	svc        filedom.Service
	projects   projectdom.Service
	authorizer iam.Checker
}

// NewFileHandler returns a FileHandler. projects and authorizer are used only
// by Roots, which lists the projects whose files the caller may read.
func NewFileHandler(svc filedom.Service, projects projectdom.Service, authorizer iam.Checker) *FileHandler {
	return &FileHandler{svc: svc, projects: projects, authorizer: authorizer}
}

// scopeOf reads the scope from the route: a {projectId} segment means that
// project, none the general scope. Authorization already ran in the gate.
func scopeOf(r *http.Request) (filedom.Scope, error) {
	if chi.URLParam(r, "projectId") == "" {
		return filedom.Scope{}, nil
	}
	id, err := parseProjectID(r)
	if err != nil {
		return filedom.Scope{}, err
	}
	return filedom.Scope{ProjectID: &id}, nil
}

func parseNodeID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid file id")
	}
	return id, nil
}

// fileRequest is the scope, the caller and (when the route has one) the item id.
type fileRequest struct {
	scope filedom.Scope
	actor uuid.UUID
	node  uuid.UUID
}

func fileCtx(w http.ResponseWriter, r *http.Request, withNode bool) (fileRequest, bool) {
	var fr fileRequest
	var err error
	if fr.scope, err = scopeOf(r); err != nil {
		presenter.Error(w, r, err)
		return fr, false
	}
	if withNode {
		if fr.node, err = parseNodeID(r); err != nil {
			presenter.Error(w, r, err)
			return fr, false
		}
	}
	var ok bool
	if fr.actor, ok = requireAccountID(w, r); !ok {
		return fr, false
	}
	return fr, true
}

func parseOffsetPaging(r *http.Request) (limit, offset int, err error) {
	limit, err = parsePageSize(r, 100, 500)
	if err != nil {
		return 0, 0, err
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return 0, 0, apierr.New(apierr.CodeBadRequest, "offset must be a non-negative integer")
		}
	}
	return limit, offset, nil
}

func pageOf(nodes []*filedom.Node, total, limit, offset int) map[string]any {
	return map[string]any{
		"items": dto.FileNodesFromEntities(nodes), "total_count": total,
		"page_size": limit, "offset": offset, "has_more": offset+len(nodes) < total,
	}
}

// List handles GET .../files/nodes: the children of a folder (parent_id, the
// scope root when absent) or, with q, a search over the whole scope.
func (h *FileHandler) List(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, false)
	if !ok {
		return
	}
	limit, offset, err := parseOffsetPaging(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	q := r.URL.Query()
	in := filedom.ListInput{
		Query: q.Get("q"), Type: q.Get("type"), SortBy: q.Get("sort"),
		Desc: q.Get("order") == "desc", Limit: limit, Offset: offset,
	}
	if raw := q.Get("parent_id"); raw != "" {
		id, perr := uuid.Parse(raw)
		if perr != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid parent_id"))
			return
		}
		in.ParentID = &id
	}
	nodes, total, err := h.svc.List(r.Context(), fr.scope, fr.actor, in)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, pageOf(nodes, total, limit, offset))
}

// Get handles GET .../files/nodes/{nodeId}.
func (h *FileHandler) Get(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, true)
	if !ok {
		return
	}
	n, err := h.svc.Get(r.Context(), fr.scope, fr.actor, fr.node)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.FileNodeFromEntity(n))
}

// Path handles GET .../files/nodes/{nodeId}/path: the breadcrumb, root first.
func (h *FileHandler) Path(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, true)
	if !ok {
		return
	}
	nodes, err := h.svc.Path(r.Context(), fr.scope, fr.node)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"items": dto.FileNodesFromEntities(nodes)})
}

// CreateFolder handles POST .../files/folders.
func (h *FileHandler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, false)
	if !ok {
		return
	}
	var req dto.CreateFileFolderRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	n, err := h.svc.CreateFolder(r.Context(), fr.scope, fr.actor, req.ParentID, req.Name)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.FileNodeFromEntity(n))
}

// Update handles PATCH .../files/nodes/{nodeId}: rename and/or move.
func (h *FileHandler) Update(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, true)
	if !ok {
		return
	}
	var req dto.UpdateFileNodeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Name == nil && !req.Move {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "name or move is required"))
		return
	}
	n, err := h.svc.Update(r.Context(), fr.scope, fr.actor, fr.node, req.Name, req.Move, req.ParentID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.FileNodeFromEntity(n))
}

// Trash handles DELETE .../files/nodes/{nodeId}.
func (h *FileHandler) Trash(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, true)
	if !ok {
		return
	}
	if err := h.svc.Trash(r.Context(), fr.scope, fr.actor, fr.node); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// Restore handles POST .../files/nodes/{nodeId}/restore.
func (h *FileHandler) Restore(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, true)
	if !ok {
		return
	}
	n, err := h.svc.Restore(r.Context(), fr.scope, fr.actor, fr.node)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.FileNodeFromEntity(n))
}

// DeletePermanently handles DELETE .../files/nodes/{nodeId}/permanent.
func (h *FileHandler) DeletePermanently(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, true)
	if !ok {
		return
	}
	if err := h.svc.DeletePermanently(r.Context(), fr.scope, fr.node); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// InitiateUpload handles POST .../files/uploads/initiate.
func (h *FileHandler) InitiateUpload(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, false)
	if !ok {
		return
	}
	var req dto.FileUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	session, err := h.svc.InitiateUpload(r.Context(), fr.scope, fr.actor, filedom.UploadInput{
		ParentID: req.ParentID, FileName: req.FileName, ContentType: req.ContentType, FileSize: req.FileSize,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.UploadSessionFromDomain(session))
}

// CompleteUpload handles POST .../files/uploads/complete.
func (h *FileHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, false)
	if !ok {
		return
	}
	var req dto.FileUploadCompleteRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	parts := make([]attachmentdom.CompletedPart, 0, len(req.Parts))
	for _, p := range req.Parts {
		parts = append(parts, attachmentdom.CompletedPart{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	n, err := h.svc.CompleteUpload(r.Context(), fr.scope, fr.actor, filedom.CompleteInput{
		FileID: req.FileID, UploadID: req.UploadID, Parts: parts,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.FileNodeFromEntity(n))
}

// DownloadURL handles GET .../files/nodes/{nodeId}/download-url. With
// ?download=true the URL forces a download instead of inline display.
func (h *FileHandler) DownloadURL(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, true)
	if !ok {
		return
	}
	url, n, err := h.svc.DownloadURL(r.Context(), fr.scope, fr.actor, fr.node, fileDownloadTTL, r.URL.Query().Get("download") == "true")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.FileDownloadResponse{
		URL: url, Name: n.Name, ContentType: n.ContentType, Size: n.Size,
		ExpiresAt: time.Now().Add(fileDownloadTTL),
	})
}

// SetStarred handles PUT .../files/nodes/{nodeId}/star.
func (h *FileHandler) SetStarred(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, true)
	if !ok {
		return
	}
	var req dto.StarFileNodeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.svc.SetStarred(r.Context(), fr.scope, fr.actor, fr.node, req.Starred); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// ListStarred handles GET .../files/starred.
func (h *FileHandler) ListStarred(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, false)
	if !ok {
		return
	}
	limit, offset, err := parseOffsetPaging(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	nodes, total, err := h.svc.ListStarred(r.Context(), fr.scope, fr.actor, limit, offset)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, pageOf(nodes, total, limit, offset))
}

// ListRecent handles GET .../files/recent.
func (h *FileHandler) ListRecent(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, false)
	if !ok {
		return
	}
	limit, err := parsePageSize(r, 50, 200)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	nodes, err := h.svc.ListRecent(r.Context(), fr.scope, fr.actor, limit)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, pageOf(nodes, len(nodes), limit, 0))
}

// ListTrash handles GET .../files/trash.
func (h *FileHandler) ListTrash(w http.ResponseWriter, r *http.Request) {
	fr, ok := fileCtx(w, r, false)
	if !ok {
		return
	}
	limit, offset, err := parseOffsetPaging(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	nodes, total, err := h.svc.ListTrash(r.Context(), fr.scope, fr.actor, limit, offset)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, pageOf(nodes, total, limit, offset))
}

// Roots handles GET /files/roots: the top level of the general Files view,
// the workspace's own files followed by one entry per project whose files the
// caller may read. The route is open to any signed-in caller and filters here,
// like GET /projects: a caller with access to one project's files but none of
// the general ones still has somewhere to start.
func (h *FileHandler) Roots(w http.ResponseWriter, r *http.Request) {
	accountID, ok := requireAccountID(w, r)
	if !ok {
		return
	}
	principal, err := middleware.IAMPrincipalFrom(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	roots := make([]dto.FileRootResponse, 0, 8)
	general, err := iam.AllowedAll(r.Context(), h.authorizer, principal, iam.PlatformResource(iam.ActionFilesRead), iam.ActionFilesRead)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if general {
		roots = append(roots, dto.FileRootResponse{Kind: "general", Name: "General"})
	}
	projects, err := h.accessibleProjects(r.Context(), principal, accountID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	for _, p := range projects {
		allowed, aerr := iam.AllowedAll(r.Context(), h.authorizer, principal, iam.ProjectResource(p.ID.String()), iam.ActionFilesRead)
		if aerr != nil {
			presenter.Error(w, r, aerr)
			return
		}
		if allowed {
			id := p.ID
			roots = append(roots, dto.FileRootResponse{Kind: "project", ProjectID: &id, Name: p.Name})
		}
	}
	presenter.OK(w, r, map[string]any{"items": roots})
}

// accessibleProjects lists every project when the caller holds the
// workspace-wide projects:read, else the ones they are a member of.
func (h *FileHandler) accessibleProjects(ctx context.Context, principal iam.Principal, accountID uuid.UUID) ([]*projectdom.Project, error) {
	all, err := iam.AllowedAll(ctx, h.authorizer, principal, "project", iam.ActionProjectsRead)
	if err != nil {
		return nil, err
	}
	if all {
		return pageAll(func(page int) ([]*projectdom.Project, int64, error) {
			return h.projects.List(ctx, page, accessiblePageSize)
		})
	}
	return pageAll(func(page int) ([]*projectdom.Project, int64, error) {
		return h.projects.ListAccessible(ctx, accountID, page, accessiblePageSize)
	})
}

const accessiblePageSize = 500

// pageAll walks every page of a project listing, so no project is dropped
// past a fixed cap.
func pageAll(fetch func(page int) ([]*projectdom.Project, int64, error)) ([]*projectdom.Project, error) {
	var out []*projectdom.Project
	for page := 1; ; page++ {
		items, total, err := fetch(page)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(items) == 0 || int64(len(out)) >= total {
			return out, nil
		}
	}
}
