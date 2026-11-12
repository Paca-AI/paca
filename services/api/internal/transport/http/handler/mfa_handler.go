package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
	CheckIP(ctx context.Context, u *userdom.User, clientIP string) error
	CheckPasswordAllowed(ctx context.Context, u *userdom.User) error
	Begin(ctx context.Context, u *userdom.User) (secret, uri string, err error)
	Confirm(ctx context.Context, u *userdom.User, code string) ([]string, error)
	Disable(ctx context.Context, u *userdom.User, code string) error
	RegenerateRecoveryCodes(ctx context.Context, u *userdom.User, code string) ([]string, error)
	Status(ctx context.Context, u *userdom.User) (bool, int, error)
	Reset(ctx context.Context, userID uuid.UUID) error
	PasskeysAvailable() bool
	ListPasskeys(ctx context.Context, u *userdom.User) ([]mfadom.Passkey, error)
	BeginPasskeyRegistration(ctx context.Context, u *userdom.User) (json.RawMessage, error)
	FinishPasskeyRegistration(ctx context.Context, u *userdom.User, name string, response []byte) (*mfadom.Passkey, error)
	BeginPasskeyVerification(ctx context.Context, u *userdom.User) (json.RawMessage, error)
	DeletePasskey(ctx context.Context, u *userdom.User, id uuid.UUID, response []byte) error
	GetPolicy(ctx context.Context) (*mfadom.Policy, error)
	SetPolicy(ctx context.Context, p *mfadom.Policy, updatedBy *uuid.UUID, clientIP string) error
	GetUserCIDRs(ctx context.Context, userID uuid.UUID) ([]string, error)
	SetUserCIDRs(ctx context.Context, userID uuid.UUID, cidrs []string, actor uuid.UUID, clientIP string) error
}

// MFAHandler serves TOTP enrollment, the pre-login enrollment path for
// accounts that policy forces onto MFA, and the admin login-security
// endpoints. Route gates (not this handler) decide who may call what.
type MFAHandler struct {
	svc      MFAService
	users    userdom.Repository
	throttle domainauth.LoginThrottle
}

// WithLoginThrottle shares the sign-in wrong-password limiter with the
// password-only enrollment endpoints, so they can't be used to guess around it.
func (h *MFAHandler) WithLoginThrottle(t domainauth.LoginThrottle) *MFAHandler {
	h.throttle = t
	return h
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
	presenter.OK(w, r, dto.MFAStatusResponse{Enabled: enabled, RecoveryCodesLeft: left, PasskeysAvailable: h.svc.PasskeysAvailable()})
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
	ctx := r.Context()
	key := domainauth.NormalizeLoginKey(username)
	ip := middleware.ClientIP(r)
	if h.throttle != nil {
		wait, err := h.throttle.Check(ctx, key, ip)
		if err != nil {
			slog.Warn("login throttle unavailable", "error", err)
		} else if wait > 0 {
			return nil, &mfadom.LockedError{RetryAfter: wait}
		}
	}
	u, err := h.users.FindByUsername(ctx, username)
	if err == nil && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) == nil {
		if h.throttle != nil {
			_ = h.throttle.Reset(ctx, key, ip)
		}
		return u, nil
	}
	if err != nil && !errors.Is(err, userdom.ErrNotFound) {
		return nil, err
	}
	if err != nil {
		domainauth.BurnPasswordCheck(password)
	}
	if h.throttle != nil {
		if wait, ferr := h.throttle.Fail(ctx, key, ip); ferr == nil && wait > 0 {
			return nil, &mfadom.LockedError{RetryAfter: wait}
		}
	}
	return nil, domainauth.ErrInvalidCredentials
}

// requireSetup refuses password-only enrollment unless policy obliges the
// account to use MFA and it has not enrolled. Everyone else enrolls from a
// signed-in session, so a password alone can never establish a second factor
// on an account that could otherwise have kept one off. Like a login, it is
// refused to an account SSO-only mode keeps off passwords.
func (h *MFAHandler) requireSetup(w http.ResponseWriter, r *http.Request, u *userdom.User) bool {
	if err := h.svc.CheckPasswordAllowed(r.Context(), u); err != nil {
		presenter.Error(w, r, err)
		return false
	}
	if err := h.svc.CheckIP(r.Context(), u, middleware.ClientIP(r)); err != nil {
		presenter.Error(w, r, err)
		return false
	}
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

// SetupPasskeyBegin handles POST /auth/mfa/setup/passkey/options: the
// passkey twin of SetupBegin, with the same password authentication and the
// same refusal for accounts that already have a second factor or need none.
func (h *MFAHandler) SetupPasskeyBegin(w http.ResponseWriter, r *http.Request) {
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
	opts, err := h.svc.BeginPasskeyRegistration(r.Context(), u)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"options": opts})
}

// SetupPasskeyFinish handles POST /auth/mfa/setup/passkey/finish.
func (h *MFAHandler) SetupPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	var req dto.PasskeySetupFinishRequest
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
	p, err := h.svc.FinishPasskeyRegistration(r.Context(), u, req.Name, req.Credential)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, passkeyResponse(*p))
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

// LoginOptions handles GET /auth/login-options: the part of the login policy
// the login page needs before anyone signs in. Public.
func (h *MFAHandler) LoginOptions(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPolicy(r.Context())
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.LoginOptionsResponse{SSOOnly: p.SSOOnly})
}

// PutPolicy handles PUT /admin/auth-security.
func (h *MFAHandler) PutPolicy(w http.ResponseWriter, r *http.Request) {
	var req dto.AuthSecurityPolicy
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	p := &mfadom.Policy{PrivilegedRoles: req.PrivilegedRoles, RequireMFA: req.RequireMFA, IPAllowList: req.IPAllowList, SSOOnly: req.SSOOnly}
	var by *uuid.UUID
	if id, ok := middleware.ActorIDFromContext(r.Context()); ok {
		by = &id
	}
	if err := h.svc.SetPolicy(r.Context(), p, by, middleware.ClientIP(r)); err != nil {
		presenter.Error(w, r, err)
		return
	}
	slog.Info("auth security policy updated", "actor", by, "require_mfa", p.RequireMFA, "roles", p.PrivilegedRoles, "ip_entries", len(p.IPAllowList), "sso_only", p.SSOOnly)
	presenter.OK(w, r, policyResponse(p))
}

func policyResponse(p *mfadom.Policy) dto.AuthSecurityPolicy {
	out := dto.AuthSecurityPolicy{PrivilegedRoles: p.PrivilegedRoles, RequireMFA: p.RequireMFA, IPAllowList: p.IPAllowList, SSOOnly: p.SSOOnly}
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
	if err := h.svc.SetUserCIDRs(r.Context(), id, req.AllowedCIDRs, actorID(r), middleware.ClientIP(r)); err != nil {
		presenter.Error(w, r, err)
		return
	}
	slog.Info("user allowed CIDRs updated", "actor", actorID(r), "user", id, "entries", len(req.AllowedCIDRs))
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
	slog.Info("user MFA reset by admin", "actor", actorID(r), "user", id)
	presenter.NoContent(w)
}

// actorID returns the acting account for audit logs, or the zero UUID.
func actorID(r *http.Request) uuid.UUID {
	id, _ := middleware.ActorIDFromContext(r.Context())
	return id
}

func passkeyResponse(p mfadom.Passkey) dto.PasskeyResponse {
	return dto.PasskeyResponse{ID: p.ID.String(), Name: p.Name, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt}
}

// ListPasskeys handles GET /users/me/passkeys.
func (h *MFAHandler) ListPasskeys(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	list, err := h.svc.ListPasskeys(r.Context(), u)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	out := make([]dto.PasskeyResponse, 0, len(list))
	for _, p := range list {
		out = append(out, passkeyResponse(p))
	}
	presenter.OK(w, r, out)
}

// BeginPasskeyRegistration handles POST /users/me/passkeys/register/options.
func (h *MFAHandler) BeginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	opts, err := h.svc.BeginPasskeyRegistration(r.Context(), u)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"options": opts})
}

// FinishPasskeyRegistration handles POST /users/me/passkeys/register/finish.
func (h *MFAHandler) FinishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	var req dto.PasskeyRegisterFinishRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	p, err := h.svc.FinishPasskeyRegistration(r.Context(), u, req.Name, req.Credential)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, passkeyResponse(*p))
}

// BeginPasskeyVerification handles POST /users/me/passkeys/verify/options:
// the WebAuthn request the user must answer to remove a passkey.
func (h *MFAHandler) BeginPasskeyVerification(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	opts, err := h.svc.BeginPasskeyVerification(r.Context(), u)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, map[string]any{"options": opts})
}

// DeletePasskey handles DELETE /users/me/passkeys/{passkeyId}. Like turning
// TOTP off, it needs proof beyond the session: here an assertion from one of
// the account's passkeys, over the options BeginPasskeyVerification issued.
func (h *MFAHandler) DeletePasskey(w http.ResponseWriter, r *http.Request) {
	u, ok := h.me(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "passkeyId"))
	if err != nil {
		presenter.Error(w, r, mfadom.ErrPasskeyNotFound)
		return
	}
	var req dto.PasskeyDeleteRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	if err := h.svc.DeletePasskey(r.Context(), u, id, req.Credential); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}
