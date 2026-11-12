// Package dto defines request and response shapes for auth endpoints.
package dto

import (
	"encoding/json"
	"time"
)

// LoginRequest is the body for POST /auth/login.
type LoginRequest struct {
	Username   string `json:"username" binding:"required"`
	Password   string `json:"password" binding:"required,min=8"`
	RememberMe bool   `json:"remember_me"`
	// OTP is the TOTP or recovery code; omit on the first attempt.
	OTP string `json:"otp,omitempty"`
}

// RefreshRequest is accepted but unused; the refresh token is read from the
// HttpOnly cookie instead. The body is ignored and this type is retained
// only for backwards-compatible API/schema compatibility.
type RefreshRequest struct{}

// SetPasswordRequest is the body for POST /auth/password/set — the public,
// unauthenticated endpoint a password-set-link email points to.
type SetPasswordRequest struct {
	Token       string `json:"token" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

// MFAVerifyRequest is the body for POST /auth/mfa/verify: the challenge
// Login returned plus a TOTP or recovery code.
type MFAVerifyRequest struct {
	Challenge string `json:"challenge" binding:"required"`
	Code      string `json:"code" binding:"required"`
}

// MFACodeRequest carries a TOTP or recovery code (confirm, disable,
// regenerate recovery codes).
type MFACodeRequest struct {
	Code string `json:"code" binding:"required"`
}

// MFASetupRequest is the body for the pre-login enrollment endpoints, which
// authenticate by password because the account cannot sign in yet.
type MFASetupRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	// Code is only read by the confirm step.
	Code string `json:"code,omitempty"`
}

// PasskeySetupFinishRequest is the body for finishing a pre-login passkey
// registration: the account's password plus the authenticator's response.
type PasskeySetupFinishRequest struct {
	Username   string          `json:"username" binding:"required"`
	Password   string          `json:"password" binding:"required"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential" binding:"required"`
}

// MFAEnrollmentResponse is returned when enrollment begins.
type MFAEnrollmentResponse struct {
	Secret string `json:"secret"`
	// URI is the otpauth:// URI to render as a QR code.
	URI string `json:"uri"`
}

// MFARecoveryCodesResponse carries recovery codes, shown exactly once.
type MFARecoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// MFAStatusResponse is the caller's MFA state. Enabled is TOTP only;
// passkeys are listed separately.
type MFAStatusResponse struct {
	Enabled           bool `json:"enabled"`
	RecoveryCodesLeft int  `json:"recovery_codes_left"`
	// PasskeysAvailable is false when the server has no relying-party
	// identity configured; clients then hide passkey controls.
	PasskeysAvailable bool `json:"passkeys_available"`
}

// PasskeyLoginOptionsRequest is the body for POST /auth/mfa/passkey/options.
type PasskeyLoginOptionsRequest struct {
	Challenge string `json:"challenge" binding:"required"`
}

// PasskeyLoginVerifyRequest is the body for POST /auth/mfa/passkey/verify:
// the login challenge plus the authenticator's assertion, verbatim.
type PasskeyLoginVerifyRequest struct {
	Challenge  string          `json:"challenge" binding:"required"`
	Credential json.RawMessage `json:"credential" binding:"required"`
}

// PasskeyRegisterFinishRequest is the body for finishing a registration.
type PasskeyRegisterFinishRequest struct {
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential" binding:"required"`
}

// PasskeyDeleteRequest re-authenticates the user before a passkey is removed:
// the authenticator's assertion over the options from
// POST /users/me/passkeys/verify/options, verbatim.
type PasskeyDeleteRequest struct {
	Credential json.RawMessage `json:"credential" binding:"required"`
}

// PasskeyResponse is one registered passkey.
type PasskeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// AuthSecurityPolicy is the instance login policy (read and write shape).
type AuthSecurityPolicy struct {
	PrivilegedRoles []string `json:"privileged_roles"`
	RequireMFA      bool     `json:"require_mfa"`
	IPAllowList     []string `json:"ip_allow_list"`
	// SSOOnly turns off password sign-in for everyone but the default super
	// admin.
	SSOOnly bool `json:"sso_only"`
}

// LoginOptionsResponse is what the (public) login page needs to know about
// the instance login policy before anyone signs in.
type LoginOptionsResponse struct {
	// SSOOnly means only SSO sign-in is offered; the password form is kept
	// for the default super admin behind a separate entry.
	SSOOnly bool `json:"sso_only"`
}

// AllowedCIDRsRequest is an account's IP/CIDR allow-list; empty is unrestricted.
type AllowedCIDRsRequest struct {
	AllowedCIDRs []string `json:"allowed_cidrs"`
}
