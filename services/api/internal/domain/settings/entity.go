// Package settingsdom provides domain entities for instance-wide workspace
// branding: a singleton logo, favicon, and per-theme primary color applied
// across every project, configured from the admin settings page.
package settingsdom

import (
	"time"

	"github.com/google/uuid"
)

// WorkspaceSettings is the singleton branding row. Image fields hold
// object-storage keys for the two server-generated variants (see
// attachmentdom.AvatarService), nil when no image has been uploaded, mirroring
// how AvatarKey/AvatarThumbKey work on users/agents/projects.
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
	BrandName *string
	UpdatedAt time.Time
	UpdatedBy *uuid.UUID
}

// ImageSlot discriminates the two image slots a WorkspaceSettings row holds.
type ImageSlot string

// ImageSlot values.
const (
	SlotLogo    ImageSlot = "logo"
	SlotFavicon ImageSlot = "favicon"
)
