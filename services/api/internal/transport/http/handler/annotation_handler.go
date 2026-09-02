package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	annotationdom "github.com/Paca-AI/api/internal/domain/annotation"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// AnnotationHandler handles page-annotation endpoints — the Paca browser
// extension's on-page comments (apps/extension). See
// docs/ai-agent/environment-management.md's "Port Forwarding" section for
// the underlying forwarded-port feature these attach to.
type AnnotationHandler struct {
	svc       annotationdom.Service
	avatarSvc attachmentdom.AvatarService
}

// NewAnnotationHandler returns an AnnotationHandler wired to the
// annotation service.
func NewAnnotationHandler(svc annotationdom.Service) *AnnotationHandler {
	return &AnnotationHandler{svc: svc}
}

// WithAvatarService configures avatar URL resolution for commenter/author
// display — mirrors TaskHandler.WithTaskAvatarService's own optional,
// chained-onto-New* convention exactly. Without it, responses still carry
// created_by_name/created_by_username, just no avatar URL.
func (h *AnnotationHandler) WithAvatarService(svc attachmentdom.AvatarService) *AnnotationHandler {
	h.avatarSvc = svc
	return h
}

// toAnnotationResponse maps a to a PageAnnotationResponse and, if an
// AvatarService is configured, resolves the annotation's own author and
// every reply's author avatar keys into presigned display URLs — mirrors
// TaskHandler.toActivityResponse exactly.
func (h *AnnotationHandler) toAnnotationResponse(ctx context.Context, a *annotationdom.PageAnnotation) dto.PageAnnotationResponse {
	resp := dto.PageAnnotationFromEntity(a)
	if h.avatarSvc != nil {
		resp.CreatedByAvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, a.CreatedByAuthor.AvatarKey)
		resp.CreatedByAvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, a.CreatedByAuthor.AvatarThumbKey)
		for i, c := range a.Comments {
			resp.Comments[i].CreatedByAvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, c.CreatedByAuthor.AvatarKey)
			resp.Comments[i].CreatedByAvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, c.CreatedByAuthor.AvatarThumbKey)
		}
	}
	return resp
}

// toCommentResponse is toAnnotationResponse's single-comment counterpart,
// for the AddComment endpoint's own response (a bare comment, not a full
// annotation).
func (h *AnnotationHandler) toCommentResponse(ctx context.Context, c *annotationdom.AnnotationComment) dto.AnnotationCommentResponse {
	resp := dto.AnnotationCommentFromEntity(c)
	if h.avatarSvc != nil {
		resp.CreatedByAvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, c.CreatedByAuthor.AvatarKey)
		resp.CreatedByAvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, c.CreatedByAuthor.AvatarThumbKey)
	}
	return resp
}

func (h *AnnotationHandler) parseAnnotation(r *http.Request) (projectID, environmentID, annotationID uuid.UUID, err error) {
	projectID, err = parseProjectID(r)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	environmentID, err = parseParamUUID(r, "environmentId")
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	annotationID, err = parseParamUUID(r, "annotationId")
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	return projectID, environmentID, annotationID, nil
}

func callerID(r *http.Request) uuid.UUID {
	claims := middleware.ClaimsFrom(r)
	id, _ := uuid.Parse(claims.Subject)
	return id
}

// List handles GET /projects/:projectId/environments/:environmentId/annotations.
// With a page_path query param it returns just that page's annotations
// (the extension's own hot path, called on every preview page load);
// without one it returns every annotation across the whole environment
// (the web app's Comments view).
func (h *AnnotationHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	environmentID, err := parseParamUUID(r, "environmentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var annotations []*annotationdom.PageAnnotation
	if pagePath := r.URL.Query().Get("page_path"); pagePath != "" {
		annotations, err = h.svc.ListForPage(r.Context(), projectID, environmentID, pagePath)
	} else {
		annotations, err = h.svc.ListForEnvironment(r.Context(), projectID, environmentID)
	}
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	resp := make([]dto.PageAnnotationResponse, 0, len(annotations))
	for _, a := range annotations {
		resp = append(resp, h.toAnnotationResponse(r.Context(), a))
	}
	presenter.OK(w, r, map[string]any{"annotations": resp})
}

// Create handles POST /projects/:projectId/environments/:environmentId/annotations.
func (h *AnnotationHandler) Create(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	environmentID, err := parseParamUUID(r, "environmentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.CreateAnnotationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	if req.PagePath == "" || req.ElementSelector == "" || req.Body == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "page_path, element_selector, and body are required"))
		return
	}

	a, err := h.svc.Create(r.Context(), projectID, environmentID, annotationdom.CreateAnnotationInput{
		PortForwardID:     req.PortForwardID,
		PagePath:          req.PagePath,
		ElementSelector:   req.ElementSelector,
		SelectorFallbacks: req.SelectorFallbacks,
		BoundingBox:       req.BoundingBox,
		ElementSnapshot:   req.ElementSnapshot,
		ConsoleErrors:     req.ConsoleErrors,
		FailedRequests:    req.FailedRequests,
		ScreenshotFileID:  req.ScreenshotFileID,
		Body:              req.Body,
		CreatedBy:         callerID(r),
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, h.toAnnotationResponse(r.Context(), a))
}

// Resolve handles PATCH .../annotations/:annotationId/resolve.
func (h *AnnotationHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	projectID, _, annotationID, err := h.parseAnnotation(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.Resolve(r.Context(), projectID, annotationID, callerID(r))
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAnnotationResponse(r.Context(), a))
}

// Reopen handles PATCH .../annotations/:annotationId/reopen.
func (h *AnnotationHandler) Reopen(w http.ResponseWriter, r *http.Request) {
	projectID, _, annotationID, err := h.parseAnnotation(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.Reopen(r.Context(), projectID, annotationID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAnnotationResponse(r.Context(), a))
}

// AddComment handles POST .../annotations/:annotationId/comments.
func (h *AnnotationHandler) AddComment(w http.ResponseWriter, r *http.Request) {
	projectID, _, annotationID, err := h.parseAnnotation(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddAnnotationCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	c, err := h.svc.AddComment(r.Context(), projectID, annotationID, callerID(r), req.Body)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, h.toCommentResponse(r.Context(), c))
}

// CreateTask handles POST .../annotations/:annotationId/create-task.
func (h *AnnotationHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	projectID, _, annotationID, err := h.parseAnnotation(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.CreateTaskFromAnnotationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	reporterID := callerID(r)
	a, err := h.svc.CreateTaskFromAnnotation(r.Context(), projectID, annotationID, annotationdom.CreateTaskFromAnnotationInput{
		TaskTypeID:  req.TaskTypeID,
		StatusID:    req.StatusID,
		AssigneeIDs: req.AssigneeIDs,
		ReporterID:  reporterID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAnnotationResponse(r.Context(), a))
}

// InitiateScreenshotUpload handles POST
// /projects/:projectId/environments/:environmentId/annotations/upload-url.
func (h *AnnotationHandler) InitiateScreenshotUpload(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	environmentID, err := parseParamUUID(r, "environmentId")
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.InitiateScreenshotUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	session, err := h.svc.InitiateScreenshotUpload(r.Context(), projectID, environmentID, annotationdom.InitiateScreenshotUploadInput{
		FileName:    req.FileName,
		ContentType: req.ContentType,
		FileSize:    req.FileSize,
		UploadedBy:  callerID(r),
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ScreenshotUploadSessionResponse{FileID: session.FileID, UploadURL: session.UploadURL})
}

// CompleteScreenshotUpload handles POST
// .../annotations/:annotationId/complete-upload.
func (h *AnnotationHandler) CompleteScreenshotUpload(w http.ResponseWriter, r *http.Request) {
	projectID, _, annotationID, err := h.parseAnnotation(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.CompleteScreenshotUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.CompleteScreenshotUpload(r.Context(), projectID, annotationID, req.FileID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toAnnotationResponse(r.Context(), a))
}

// GetScreenshotURL handles GET
// .../annotations/:annotationId/screenshot-url.
func (h *AnnotationHandler) GetScreenshotURL(w http.ResponseWriter, r *http.Request) {
	projectID, _, annotationID, err := h.parseAnnotation(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	url, err := h.svc.GetScreenshotURL(r.Context(), projectID, annotationID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ScreenshotURLResponse{URL: url})
}

// ResolvePortForward handles GET /port-forwards/resolve?host_port=. Not
// project-scoped in the URL — it's how the extension turns "I'm on
// host:<port>" into "this is environment X in project Y" before it knows
// which project-scoped endpoint to call next, searching only the caller's
// own accessible projects (see annotationdom.Repository.ResolvePortForward).
func (h *AnnotationHandler) ResolvePortForward(w http.ResponseWriter, r *http.Request) {
	hostPort, err := strconv.Atoi(r.URL.Query().Get("host_port"))
	if err != nil || hostPort <= 0 || hostPort > 65535 {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "host_port must be a valid port number"))
		return
	}
	match, err := h.svc.ResolvePortForward(r.Context(), callerID(r), hostPort)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.ResolvePortForwardResponse{
		ProjectID:     match.ProjectID,
		EnvironmentID: match.EnvironmentID,
		PortForwardID: match.PortForwardID,
		Label:         match.Label,
	})
}
