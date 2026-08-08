package handler

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// SettingsHandler handles workspace branding endpoints: the public branding
// read and the admin-only logo/favicon/primary-color writes.
type SettingsHandler struct {
	svc       settingsdom.Service
	avatarSvc attachmentdom.AvatarService
}

// NewSettingsHandler returns a SettingsHandler wired to the provided settings service.
func NewSettingsHandler(svc settingsdom.Service) *SettingsHandler {
	return &SettingsHandler{svc: svc}
}

// WithAvatarService configures logo/favicon URL resolution.
func (h *SettingsHandler) WithAvatarService(svc attachmentdom.AvatarService) *SettingsHandler {
	h.avatarSvc = svc
	return h
}

// toBrandingResponse maps ws to a BrandingResponse and, if an AvatarService
// is configured, resolves its image keys into presigned display URLs.
func (h *SettingsHandler) toBrandingResponse(ctx context.Context, ws *settingsdom.WorkspaceSettings) dto.BrandingResponse {
	resp := dto.BrandingResponse{
		BrandName:         ws.BrandName,
		PrimaryColorLight: ws.PrimaryColorLight,
		PrimaryColorDark:  ws.PrimaryColorDark,
	}
	if h.avatarSvc != nil {
		resp.LogoURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, ws.LogoKey)
		resp.LogoThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, ws.LogoThumbKey)
		resp.FaviconURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, ws.FaviconKey)
		resp.FaviconThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, ws.FaviconThumbKey)
	}
	return resp
}

// toImageResponse resolves just the given slot's keys, shaped to match the
// generic AvatarResult contract the frontend's shared avatar-upload client
// expects — see dto.AvatarShapedImageResponse.
func (h *SettingsHandler) toImageResponse(ctx context.Context, ws *settingsdom.WorkspaceSettings, slot settingsdom.ImageSlot) dto.AvatarShapedImageResponse {
	key, thumbKey := ws.LogoKey, ws.LogoThumbKey
	if slot == settingsdom.SlotFavicon {
		key, thumbKey = ws.FaviconKey, ws.FaviconThumbKey
	}
	var resp dto.AvatarShapedImageResponse
	if h.avatarSvc != nil {
		resp.AvatarURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, key)
		resp.AvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(ctx, thumbKey)
	}
	return resp
}

// GetBranding handles GET /branding. Public — no auth required, called
// pre-login and on every page load.
func (h *SettingsHandler) GetBranding(w http.ResponseWriter, r *http.Request) {
	ws, err := h.svc.Get(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toBrandingResponse(r.Context(), ws))
}

// actingUserID extracts the authenticated caller's user ID from JWT claims,
// writing an error response and returning ok=false if absent/invalid.
func actingUserID(w http.ResponseWriter, r *http.Request) (id uuid.UUID, ok bool) {
	claims := middleware.ClaimsFrom(r)
	if claims == nil {
		presenter.Error(w, r, apierr.New(apierr.CodeUnauthenticated, "unauthenticated"))
		return uuid.Nil, false
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid subject claim"))
		return uuid.Nil, false
	}
	return id, true
}

func (h *SettingsHandler) initiateUpload(w http.ResponseWriter, r *http.Request, slot settingsdom.ImageSlot) {
	id, ok := actingUserID(w, r)
	if !ok {
		return
	}

	var req dto.InitiateUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	session, err := h.svc.InitiateImageUpload(r.Context(), slot, req.FileName, req.ContentType, req.FileSize, id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.UploadSessionFromDomain(session))
}

func (h *SettingsHandler) completeUpload(w http.ResponseWriter, r *http.Request, slot settingsdom.ImageSlot) {
	if _, ok := actingUserID(w, r); !ok {
		return
	}

	var req dto.CompleteAvatarUploadRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	ws, err := h.svc.CompleteImageUpload(r.Context(), slot, req.FileID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toImageResponse(r.Context(), ws, slot))
}

func (h *SettingsHandler) deleteImage(w http.ResponseWriter, r *http.Request, slot settingsdom.ImageSlot) {
	if _, ok := actingUserID(w, r); !ok {
		return
	}

	ws, err := h.svc.RemoveImage(r.Context(), slot)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toImageResponse(r.Context(), ws, slot))
}

// InitiateLogoUpload handles POST /admin/settings/logo/avatar/initiate-upload.
func (h *SettingsHandler) InitiateLogoUpload(w http.ResponseWriter, r *http.Request) {
	h.initiateUpload(w, r, settingsdom.SlotLogo)
}

// CompleteLogoUpload handles POST /admin/settings/logo/avatar/complete-upload.
func (h *SettingsHandler) CompleteLogoUpload(w http.ResponseWriter, r *http.Request) {
	h.completeUpload(w, r, settingsdom.SlotLogo)
}

// DeleteLogo handles DELETE /admin/settings/logo/avatar.
func (h *SettingsHandler) DeleteLogo(w http.ResponseWriter, r *http.Request) {
	h.deleteImage(w, r, settingsdom.SlotLogo)
}

// InitiateFaviconUpload handles POST /admin/settings/favicon/avatar/initiate-upload.
func (h *SettingsHandler) InitiateFaviconUpload(w http.ResponseWriter, r *http.Request) {
	h.initiateUpload(w, r, settingsdom.SlotFavicon)
}

// CompleteFaviconUpload handles POST /admin/settings/favicon/avatar/complete-upload.
func (h *SettingsHandler) CompleteFaviconUpload(w http.ResponseWriter, r *http.Request) {
	h.completeUpload(w, r, settingsdom.SlotFavicon)
}

// DeleteFavicon handles DELETE /admin/settings/favicon/avatar.
func (h *SettingsHandler) DeleteFavicon(w http.ResponseWriter, r *http.Request) {
	h.deleteImage(w, r, settingsdom.SlotFavicon)
}

// UpdateSettings handles PATCH /admin/settings.
func (h *SettingsHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := actingUserID(w, r)
	if !ok {
		return
	}

	var req dto.UpdateSettingsRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	ws, err := h.svc.UpdateSettings(r.Context(), req.BrandName, req.PrimaryColorLight, req.PrimaryColorDark, id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, h.toBrandingResponse(r.Context(), ws))
}
