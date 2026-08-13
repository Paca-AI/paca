package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/platform/mail"
	emailsvc "github.com/Paca-AI/api/internal/service/email"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// EmailSettingsService is the subset of the e-mail service the handler needs.
type EmailSettingsService interface {
	Get(ctx context.Context) (*emailsvc.SettingsView, error)
	Update(ctx context.Context, in emailsvc.UpdateInput, updatedBy uuid.UUID) (*emailsvc.SettingsView, error)
	SendTest(ctx context.Context, to string) error
}

// EmailSettingsHandler serves the admin-only SMTP e-mail settings endpoints
//: read, update, and send-test.
type EmailSettingsHandler struct {
	svc EmailSettingsService
}

// NewEmailSettingsHandler returns a handler wired to the e-mail service.
func NewEmailSettingsHandler(svc EmailSettingsService) *EmailSettingsHandler {
	return &EmailSettingsHandler{svc: svc}
}

func emailSettingsResponse(v *emailsvc.SettingsView) dto.EmailSettingsResponse {
	return dto.EmailSettingsResponse{
		FromEmail:            v.FromEmail,
		FromName:             v.FromName,
		Host:                 v.Host,
		Port:                 v.Port,
		Username:             v.Username,
		UseSSL:               v.UseSSL,
		UseTLS:               v.UseTLS,
		SkipVerify:           v.SkipVerify,
		SendUserCreatedEmail: v.SendUserCreatedEmail,
		PasswordSet:          v.PasswordSet,
		Configured:           v.Configured,
	}
}

// GetEmailSettings handles GET /admin/settings/email.
func (h *EmailSettingsHandler) GetEmailSettings(w http.ResponseWriter, r *http.Request) {
	v, err := h.svc.Get(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, emailSettingsResponse(v))
}

// UpdateEmailSettings handles PATCH /admin/settings/email.
func (h *EmailSettingsHandler) UpdateEmailSettings(w http.ResponseWriter, r *http.Request) {
	id, ok := actingUserID(w, r)
	if !ok {
		return
	}
	var req dto.UpdateEmailSettingsRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	v, err := h.svc.Update(r.Context(), emailsvc.UpdateInput{
		FromEmail:            req.FromEmail,
		FromName:             req.FromName,
		Host:                 req.Host,
		Port:                 req.Port,
		Username:             req.Username,
		Password:             req.Password,
		UseSSL:               req.UseSSL,
		UseTLS:               req.UseTLS,
		SkipVerify:           req.SkipVerify,
		SendUserCreatedEmail: req.SendUserCreatedEmail,
	}, id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, emailSettingsResponse(v))
}

// SendTestEmail handles POST /admin/settings/email/test. SMTP failures are
// surfaced to the admin as a 400 with the underlying message so they can
// debug their configuration.
func (h *EmailSettingsHandler) SendTestEmail(w http.ResponseWriter, r *http.Request) {
	var req dto.SendTestEmailRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.To == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "recipient e-mail is required"))
		return
	}
	if err := h.svc.SendTest(r.Context(), req.To); err != nil {
		if errors.Is(err, mail.ErrNotConfigured) {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "SMTP is not configured — fill in host, port and sender e-mail first"))
			return
		}
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "could not send test e-mail: "+err.Error()))
		return
	}
	presenter.OK(w, r, map[string]bool{"sent": true})
}
