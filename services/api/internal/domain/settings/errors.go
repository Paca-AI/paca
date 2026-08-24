package settingsdom

import "errors"

// Sentinel domain errors for workspace settings.
var (
	// ErrInvalidColor indicates a primary color value isn't a "#rrggbb" hex string.
	ErrInvalidColor = errors.New("workspace settings: invalid color")
	// ErrBrandNameTooLong indicates a brand name exceeds the maximum length.
	ErrBrandNameTooLong = errors.New("workspace settings: brand name too long")
	// ErrSSOAdminRequired protects SSO-only installations from removing their
	// final administrator who can authenticate through the configured issuer.
	ErrSSOAdminRequired = errors.New("workspace settings: sso-only mode requires an administrator bound to the configured issuer")
)
