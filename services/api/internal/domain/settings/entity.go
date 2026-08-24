// Package settingsdom provides domain entities for instance-wide workspace
// branding: a singleton logo, favicon, and per-theme primary color applied
// across every project, configured from the admin settings page.
package settingsdom

import (
	"time"

	"github.com/google/uuid"
)

// WorkspaceSettings is the singleton instance-settings row. Image fields hold
// object-storage keys for branding; OIDC fields hold administrator-managed
// authentication configuration. The OIDC client secret is encrypted before it
// reaches this entity and is never exposed by a public or admin read DTO.
type WorkspaceSettings struct {
	LogoKey           *string
	LogoThumbKey      *string
	FaviconKey        *string
	FaviconThumbKey   *string
	PrimaryColorLight *string
	PrimaryColorDark  *string
	// BrandName overrides the product name instance-wide — used as both the
	// browser tab title (<title>) and the wordmark text shown next to the
	// logo — nil meaning "use the app's default ('Paca')".
	BrandName           *string
	OIDCConfigured      bool
	OIDCEnabled         bool
	OIDCIssuerURL       *string
	OIDCClientID        *string
	OIDCClientSecretEnc *string
	OIDCScopes          *string
	OIDCRedirectURL     *string
	OIDCDisplayName     *string
	OIDCUsernameClaim   *string
	LocalLoginEnabled   bool
	UpdatedAt           time.Time
	UpdatedBy           *uuid.UUID
}

// ImageSlot discriminates the two image slots a WorkspaceSettings row holds.
type ImageSlot string

// ImageSlot values.
const (
	SlotLogo    ImageSlot = "logo"
	SlotFavicon ImageSlot = "favicon"
)
