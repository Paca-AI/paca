// Package dto defines request and response shapes for auth endpoints.
package dto

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

// MFAStatusResponse is the caller's MFA state.
type MFAStatusResponse struct {
	Enabled           bool `json:"enabled"`
	RecoveryCodesLeft int  `json:"recovery_codes_left"`
}

// AuthSecurityPolicy is the instance login policy (read and write shape).
type AuthSecurityPolicy struct {
	PrivilegedRoles []string `json:"privileged_roles"`
	RequireMFA      bool     `json:"require_mfa"`
	IPAllowList     []string `json:"ip_allow_list"`
}

// AllowedCIDRsRequest is an account's IP/CIDR allow-list; empty is unrestricted.
type AllowedCIDRsRequest struct {
	AllowedCIDRs []string `json:"allowed_cidrs"`
}
