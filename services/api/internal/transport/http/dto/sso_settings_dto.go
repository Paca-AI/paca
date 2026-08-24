package dto

// SSOSettingsResponse is the non-secret administrator view of the effective
// OIDC configuration. Secret material is represented only by a presence flag.
type SSOSettingsResponse struct {
	Source                          string   `json:"source"`
	Enabled                         bool     `json:"enabled"`
	IssuerURL                       string   `json:"issuer_url"`
	ClientID                        string   `json:"client_id"`
	ClientSecretConfigured          bool     `json:"client_secret_configured"`
	Scopes                          []string `json:"scopes"`
	RedirectURL                     string   `json:"redirect_url"`
	DisplayName                     string   `json:"display_name"`
	UsernameClaim                   string   `json:"username_claim"`
	LocalLoginEnabled               bool     `json:"local_login_enabled"`
	EncryptedSecretStorageAvailable bool     `json:"encrypted_secret_storage_available"`
}

// UpdateSSOSettingsRequest is the body for PATCH /admin/settings/sso.
// ClientSecret is write-only; an omitted or empty value preserves the current
// effective secret.
type UpdateSSOSettingsRequest struct {
	Enabled           bool     `json:"enabled"`
	IssuerURL         string   `json:"issuer_url"`
	ClientID          string   `json:"client_id"`
	ClientSecret      string   `json:"client_secret"`
	Scopes            []string `json:"scopes"`
	RedirectURL       string   `json:"redirect_url"`
	DisplayName       string   `json:"display_name"`
	UsernameClaim     string   `json:"username_claim"`
	LocalLoginEnabled bool     `json:"local_login_enabled"`
}
