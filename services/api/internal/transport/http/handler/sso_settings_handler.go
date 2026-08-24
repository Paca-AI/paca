package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/service/oidc"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// SSOSettingsService is the administrator-facing portion of the live OIDC
// runtime manager.
type SSOSettingsService interface {
	AdminConfig() oidc.AdminConfig
	Update(ctx context.Context, in oidc.UpdateConfig, actor uuid.UUID) (oidc.AdminConfig, error)
}

// SSOSettingsHandler serves the protected installation-wide SSO settings API.
type SSOSettingsHandler struct {
	svc SSOSettingsService
}

func NewSSOSettingsHandler(svc SSOSettingsService) *SSOSettingsHandler {
	return &SSOSettingsHandler{svc: svc}
}

func ssoSettingsResponse(cfg oidc.AdminConfig) dto.SSOSettingsResponse {
	return dto.SSOSettingsResponse{
		Source:                          string(cfg.Source),
		Enabled:                         cfg.Enabled,
		IssuerURL:                       cfg.IssuerURL,
		ClientID:                        cfg.ClientID,
		ClientSecretConfigured:          cfg.ClientSecretConfigured,
		Scopes:                          cfg.Scopes,
		RedirectURL:                     cfg.RedirectURL,
		DisplayName:                     cfg.DisplayName,
		UsernameClaim:                   cfg.UsernameClaim,
		LocalLoginEnabled:               cfg.LocalLoginEnabled,
		EncryptedSecretStorageAvailable: cfg.EncryptedSecretStorageAvailable,
	}
}

// Get handles GET /admin/settings/sso.
func (h *SSOSettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	presenter.OK(w, r, ssoSettingsResponse(h.svc.AdminConfig()))
}

// Update handles PATCH /admin/settings/sso.
func (h *SSOSettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	actor, ok := actingUserID(w, r)
	if !ok {
		return
	}

	var req dto.UpdateSSOSettingsRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	cfg, err := h.svc.Update(r.Context(), oidc.UpdateConfig{
		Enabled:           req.Enabled,
		IssuerURL:         req.IssuerURL,
		ClientID:          req.ClientID,
		ClientSecret:      req.ClientSecret,
		Scopes:            req.Scopes,
		RedirectURL:       req.RedirectURL,
		DisplayName:       req.DisplayName,
		UsernameClaim:     req.UsernameClaim,
		LocalLoginEnabled: req.LocalLoginEnabled,
	}, actor)
	if err != nil {
		presenter.Error(w, r, publicSSOSettingsError(err))
		return
	}
	presenter.OK(w, r, ssoSettingsResponse(cfg))
}

func publicSSOSettingsError(err error) error {
	switch {
	case errors.Is(err, oidc.ErrInvalidConfig):
		return apierr.New(apierr.CodeSSOConfigInvalid, "SSO configuration is invalid")
	case errors.Is(err, oidc.ErrProviderValidation):
		// Do not log the wrapped Discovery error: provider responses and network
		// addresses are not part of the administrator API or routine logs.
		slog.Warn("OIDC provider validation failed during settings update")
		return apierr.New(apierr.CodeSSOProviderValidationFailed, "unable to validate the OIDC provider")
	case errors.Is(err, oidc.ErrEncryptionUnavailable):
		return apierr.New(apierr.CodeSSOEncryptionUnavailable, "encrypted secret storage is unavailable")
	case errors.Is(err, oidc.ErrSSOAdminRequired):
		return apierr.New(apierr.CodeSSOAdminRequired, "an SSO-bound administrator is required before disabling local login")
	default:
		return err
	}
}
