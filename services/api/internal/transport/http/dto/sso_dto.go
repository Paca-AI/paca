package dto

import (
	"time"

	ssodom "github.com/Paca-AI/api/internal/domain/sso"
)

// SSOProviderRequest is the body for POST/PUT /admin/sso/providers. On update
// an omitted (null) client_secret keeps the stored one.
type SSOProviderRequest struct {
	Slug           string   `json:"slug"`
	DisplayName    string   `json:"display_name"`
	IssuerURL      string   `json:"issuer_url"`
	ClientID       string   `json:"client_id"`
	ClientSecret   *string  `json:"client_secret"`
	Scopes         []string `json:"scopes"`
	Enabled        bool     `json:"enabled"`
	AutoProvision  bool     `json:"auto_provision"`
	LinkByEmail    bool     `json:"link_by_email"`
	AllowedDomains []string `json:"allowed_domains"`
}

// ToInput maps the request to the domain input.
func (r SSOProviderRequest) ToInput() ssodom.ProviderInput {
	return ssodom.ProviderInput{
		Slug:           r.Slug,
		DisplayName:    r.DisplayName,
		IssuerURL:      r.IssuerURL,
		ClientID:       r.ClientID,
		ClientSecret:   r.ClientSecret,
		Scopes:         r.Scopes,
		Enabled:        r.Enabled,
		AutoProvision:  r.AutoProvision,
		LinkByEmail:    r.LinkByEmail,
		AllowedDomains: r.AllowedDomains,
	}
}

// SSOProviderResponse is the admin view of a provider. The client secret is
// never returned — only whether one is set.
type SSOProviderResponse struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	DisplayName     string    `json:"display_name"`
	IssuerURL       string    `json:"issuer_url"`
	ClientID        string    `json:"client_id"`
	HasClientSecret bool      `json:"has_client_secret"`
	Scopes          []string  `json:"scopes"`
	Enabled         bool      `json:"enabled"`
	AutoProvision   bool      `json:"auto_provision"`
	LinkByEmail     bool      `json:"link_by_email"`
	AllowedDomains  []string  `json:"allowed_domains"`
	CallbackURL     string    `json:"callback_url"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// SSOProviderFromDomain maps p; callbackURL is the redirect URI to register
// at the provider.
func SSOProviderFromDomain(p *ssodom.Provider, callbackURL string) SSOProviderResponse {
	return SSOProviderResponse{
		ID:              p.ID.String(),
		Slug:            p.Slug,
		DisplayName:     p.DisplayName,
		IssuerURL:       p.IssuerURL,
		ClientID:        p.ClientID,
		HasClientSecret: p.ClientSecret != "",
		Scopes:          p.Scopes,
		Enabled:         p.Enabled,
		AutoProvision:   p.AutoProvision,
		LinkByEmail:     p.LinkByEmail,
		AllowedDomains:  p.AllowedDomains,
		CallbackURL:     callbackURL,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
	}
}

// SSOPublicProviderResponse is what the login page needs to render a
// "Continue with …" button.
type SSOPublicProviderResponse struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}
