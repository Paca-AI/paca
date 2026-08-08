package settingsdom

import (
	"context"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
)

// Service defines the workspace branding use-case contract. Logo and
// favicon uploads share one Initiate/Complete/Remove implementation
// parameterized by ImageSlot rather than being duplicated per slot.
//
// Like projectdom/userdom/agentdom's services, this returns the raw entity
// (object-storage keys, not URLs) — resolving keys to presigned display URLs
// via attachmentdom.AvatarService.ResolveAvatarURL is left to the HTTP
// handler, mirroring how every other avatar-bearing resource in this codebase
// resolves URLs at the handler/DTO layer rather than in the service.
type Service interface {
	// Get returns the current workspace settings row. Safe to call for an
	// unauthenticated caller — this backs the public branding endpoint used
	// pre-login and on every page load.
	Get(ctx context.Context) (*WorkspaceSettings, error)

	// InitiateImageUpload starts an upload for the given slot, returning a
	// presigned PUT URL.
	InitiateImageUpload(ctx context.Context, slot ImageSlot, fileName, contentType string, fileSize int64, uploadedBy uuid.UUID) (*attachmentdom.UploadSession, error)
	// CompleteImageUpload finishes an upload for the given slot, replacing
	// any previous image in that slot.
	CompleteImageUpload(ctx context.Context, slot ImageSlot, fileID uuid.UUID) (*WorkspaceSettings, error)
	// RemoveImage clears the given slot, deleting the underlying objects.
	RemoveImage(ctx context.Context, slot ImageSlot) (*WorkspaceSettings, error)

	// UpdateSettings sets the brand name and the light/dark primary accent
	// colors together. A nil/empty brandName clears the override (falling
	// back to the app default "Paca"); a nil light/dark value clears that
	// mode's color override. A non-nil color must be a "#rrggbb" hex string
	// or ErrInvalidColor is returned.
	UpdateSettings(ctx context.Context, brandName, light, dark *string, updatedBy uuid.UUID) (*WorkspaceSettings, error)
}
