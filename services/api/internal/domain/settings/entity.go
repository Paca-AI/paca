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

	// SMTP e-mail configuration. All optional; e-mail sending is only
	// possible once host/port/from are set. SMTPPasswordEncrypted holds the
	// AES-256-GCM ciphertext of the SMTP password (see platform/secret.
	// Encryptor), never the plaintext.
	SMTPFromEmail         *string
	SMTPFromName          *string
	SMTPHost              *string
	SMTPPort              *int
	SMTPUsername          *string
	SMTPPasswordEncrypted *string
	SMTPUseSSL            bool
	SMTPUseTLS            bool
	// SMTPSkipVerify disables TLS certificate verification (insecure; for
	// shared hosting whose cert doesn't match the SMTP hostname).
	SMTPSkipVerify bool
	// SendUserCreatedEmail is the "Enviar e-mail de usuário criado" toggle
	// (true by default): when true, creating a user or resetting a password
	// e-mails their credentials to the user's e-mail address.
	SendUserCreatedEmail bool

	UpdatedAt time.Time
	UpdatedBy *uuid.UUID
}

// SMTPConfigured reports whether the minimum SMTP fields needed to send mail
// are present (host, port, and a from address). Username/password are
// optional — some relays accept unauthenticated localhost submission.
func (w *WorkspaceSettings) SMTPConfigured() bool {
	return w.SMTPHost != nil && *w.SMTPHost != "" &&
		w.SMTPPort != nil && *w.SMTPPort > 0 &&
		w.SMTPFromEmail != nil && *w.SMTPFromEmail != ""
}

// ImageSlot discriminates the two image slots a WorkspaceSettings row holds.
type ImageSlot string

// ImageSlot values.
const (
	SlotLogo    ImageSlot = "logo"
	SlotFavicon ImageSlot = "favicon"
)
