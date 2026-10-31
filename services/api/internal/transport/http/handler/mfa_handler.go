package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/Paca-AI/api/internal/apierr"
	domainauth "github.com/Paca-AI/api/internal/domain/auth"
	mfadom "github.com/Paca-AI/api/internal/domain/mfa"
	userdom "github.com/Paca-AI/api/internal/domain/user"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// MFAService is what MFAHandler needs from the MFA service.
type MFAService interface {
	SetupRequired(ctx context.Context, u *userdom.User) (bool, error)
	Begin(ctx context.Context, u *userdom.User) (secret, uri string, err error)
	Confirm(ctx context.Context, u *userdom.User, code string) ([]string, error)
	Disable(ctx context.Context, u *userdom.User, code string) error
	RegenerateRecoveryCodes(ctx context.Context, u *userdom.User, code string) ([]string, error)
	Status(ctx context.Context, u *userdom.User) (bool, int, error)
	Reset(ctx context.Context, userID uuid.UUID) error
	GetPolicy(ctx context.Context) (*mfadom.Policy, error)
	SetPolicy(ctx context.Context, p *mfadom.Policy, updatedBy *uuid.UUID) error
	GetUserCIDRs(ctx context.Context, userID uuid.UUID) ([]string, error)
	SetUserCIDRs(ctx context.Context, userID uuid.UUID, cidrs []string) error
}

// MFAHandler serves TOTP enrollment, the pre-login enrollment path for
// accounts that policy forces onto MFA, and the admin login-security
// endpoints. Route gates (not this handler) decide who may call what.
type MFAHandler struct {
	svc   MFAService
	users userdom.Repository
}

// NewMFAHandler returns an MFAHandler.
func NewMFAHandler(svc MFAService, users userdom.Repository) *MFAHandler {
	return &MFAHandler{svc: svc, users: users}
}

func (h *MFAHandler) me(w http.ResponseWriter, r *http.Request) (*userdom.User, bool) {
	id, ok := requireAccountID(w, r)
	if !ok {
		return nil, false
	}
	u, err := h.users.FindByID(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return nil, false
	}
	return u, true
}

// Status handles GET /users/me/mfa.
func (h *MFAHandler) Status(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	enabled, left, err := h.svc.Status(r.Context(), u)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MFAStatusResponse{Enabled: enabled, RecoveryCodesLeft: left})
}

// Begin handles POST /users/me/mfa/enroll.
func (h *MFAHandler) Begin(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	secret, uri, err := h.svc.Begin(r.Context(), u)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MFAEnrollmentResponse{Secret: secret, URI: uri})
}

// Confirm handles POST /users/me/mfa/confirm.
func (h *MFAHandler) Confirm(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	var req dto.MFACodeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	codes, err := h.svc.Confirm(r.Context(), u, req.Code)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MFARecoveryCodesResponse{RecoveryCodes: codes})
}

// Disable handles POST /users/me/mfa/disable.
func (h *MFAHandler) Disable(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	var req dto.MFACodeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.svc.Disable(r.Context(), u, req.Code); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// RegenerateRecoveryCodes handles POST /users/me/mfa/recovery-codes.
func (h *MFAHandler) RegenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	var req dto.MFACodeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	codes, err := h.svc.RegenerateRecoveryCodes(r.Context(), u, req.Code)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MFARecoveryCodesResponse{RecoveryCodes: codes})
}

// passwordUser authenticates a pre-login request by username and password.
// Any failure is the same ErrInvalidCredentials login uses.
func (h *MFAHandler) passwordUser(r *http.Request, username, password string) (*userdom.User, error) {
	u, err := h.users.FindByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, userdom.ErrNotFound) {
			return nil, domainauth.ErrInvalidCredentials
		}
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, domainauth.ErrInvalidCredentials
	}
	return u, nil
}

// requireSetup refuses password-only enrollment unless policy obliges the
// account to use MFA and it has not enrolled. Everyone else enrolls from a
// signed-in session, so a password alone can never establish a second factor
// on an account that could otherwise have kept one off.
func (h *MFAHandler) requireSetup(w http.ResponseWriter, r *http.Request, u *userdom.User) bool {
	required, err := h.svc.SetupRequired(r.Context(), u)
	if err != nil {
		presenter.Error(w, r, err)
		return false
	}
	if !required {
		presenter.Error(w, r, mfadom.ErrSetupNotRequired)
		return false
	}
	return true
}

// SetupBegin handles POST /auth/mfa/setup: enrollment for an account policy
// requires to use MFA but that cannot sign in until it has. An account that
// already has MFA enabled is refused, so a stolen password alone can never
// swap the second factor.
func (h *MFAHandler) SetupBegin(w http.ResponseWriter, r *http.Request) {
	var req dto.MFASetupRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	u, err := h.passwordUser(r, req.Username, req.Password)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if !h.requireSetup(w, r, u) {
		return
	}
	secret, uri, err := h.svc.Begin(r.Context(), u)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MFAEnrollmentResponse{Secret: secret, URI: uri})
}

// SetupConfirm handles POST /auth/mfa/setup/confirm.
func (h *MFAHandler) SetupConfirm(w http.ResponseWriter, r *http.Request) {
	var req dto.MFASetupRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if req.Code == "" {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "code is required"))
		return
	}
	u, err := h.passwordUser(r, req.Username, req.Password)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if !h.requireSetup(w, r, u) {
		return
	}
	codes, err := h.svc.Confirm(r.Context(), u, req.Code)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.MFARecoveryCodesResponse{RecoveryCodes: codes})
}

// GetPolicy handles GET /admin/auth-security.
func (h *MFAHandler) GetPolicy(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPolicy(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, policyResponse(p))
}

// PutPolicy handles PUT /admin/auth-security.
func (h *MFAHandler) PutPolicy(w http.ResponseWriter, r *http.Request) {
	var req dto.AuthSecurityPolicy
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	p := &mfadom.Policy{PrivilegedRoles: req.PrivilegedRoles, RequireMFA: req.RequireMFA, IPAllowList: req.IPAllowList}
	var by *uuid.UUID
	if id, ok := middleware.ActorIDFromContext(r.Context()); ok {
		by = &id
	}
	if err := h.svc.SetPolicy(r.Context(), p, by); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, policyResponse(p))
}

func policyResponse(p *mfadom.Policy) dto.AuthSecurityPolicy {
	out := dto.AuthSecurityPolicy{PrivilegedRoles: p.PrivilegedRoles, RequireMFA: p.RequireMFA, IPAllowList: p.IPAllowList}
	if out.PrivilegedRoles == nil {
		out.PrivilegedRoles = []string{}
	}
	if out.IPAllowList == nil {
		out.IPAllowList = []string{}
	}
	return out
}

func userIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid user id"))
		return uuid.Nil, false
	}
	return id, true
}

// GetUserCIDRs handles GET /admin/users/{userId}/allowed-cidrs.
func (h *MFAHandler) GetUserCIDRs(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDParam(w, r)
	if !ok {
		return
	}
	cidrs, err := h.svc.GetUserCIDRs(r.Context(), id)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if cidrs == nil {
		cidrs = []string{}
	}
	presenter.OK(w, r, dto.AllowedCIDRsRequest{AllowedCIDRs: cidrs})
}

// PutUserCIDRs handles PUT /admin/users/{userId}/allowed-cidrs.
func (h *MFAHandler) PutUserCIDRs(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDParam(w, r)
	if !ok {
		return
	}
	var req dto.AllowedCIDRsRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.svc.SetUserCIDRs(r.Context(), id, req.AllowedCIDRs); err != nil {
		presenter.Error(w, r, err)
		return
	}
	h.GetUserCIDRs(w, r)
}

// ResetUserMFA handles DELETE /admin/users/{userId}/mfa.
func (h *MFAHandler) ResetUserMFA(w http.ResponseWriter, r *http.Request) {
	id, ok := userIDParam(w, r)
	if !ok {
		return
	}
	if err := h.svc.Reset(r.Context(), id); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}
