// Package settingssvc implements workspace branding application services.
package settingssvc

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	settingsdom "github.com/Paca-AI/api/internal/domain/settings"
)

// colorRe validates a "#rrggbb" hex color.
var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ErrAvatarServiceRequired indicates a missing AvatarService dependency when
// an image-upload path is invoked.
var ErrAvatarServiceRequired = errors.New("settings svc: avatar service required")

// workspaceOwnerID is the fixed "owner" passed to AvatarService for both
// image slots — the workspace_settings row is a singleton, so there's no
// real per-row ID to namespace storage keys by (the AvatarOwnerWorkspaceLogo/
// AvatarOwnerWorkspaceFavicon owner kinds already do that namespacing).
var workspaceOwnerID = uuid.Nil

// Service is the concrete implementation of settingsdom.Service.
type Service struct {
	repo      settingsdom.Repository
	avatarSvc attachmentdom.AvatarService
}

// New returns a configured settings service.
func New(repo settingsdom.Repository) *Service {
	return &Service{repo: repo}
}

// WithAvatarService configures logo/favicon upload support.
func (s *Service) WithAvatarService(svc attachmentdom.AvatarService) *Service {
	s.avatarSvc = svc
	return s
}

// Get returns the current workspace settings row.
func (s *Service) Get(ctx context.Context) (*settingsdom.WorkspaceSettings, error) {
	return s.repo.Get(ctx)
}

func ownerKindFor(slot settingsdom.ImageSlot) attachmentdom.AvatarOwnerKind {
	if slot == settingsdom.SlotFavicon {
		return attachmentdom.AvatarOwnerWorkspaceFavicon
	}
	return attachmentdom.AvatarOwnerWorkspaceLogo
}

// keysFor returns addressable pointers to the key/thumbKey fields on ws for
// the given slot, so Complete/RemoveImage can read and overwrite them
// without a slot switch duplicated at every call site.
func keysFor(ws *settingsdom.WorkspaceSettings, slot settingsdom.ImageSlot) (key, thumbKey **string) {
	if slot == settingsdom.SlotFavicon {
		return &ws.FaviconKey, &ws.FaviconThumbKey
	}
	return &ws.LogoKey, &ws.LogoThumbKey
}

// InitiateImageUpload starts an upload for the given slot.
func (s *Service) InitiateImageUpload(ctx context.Context, slot settingsdom.ImageSlot, fileName, contentType string, fileSize int64, uploadedBy uuid.UUID) (*attachmentdom.UploadSession, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}
	return s.avatarSvc.InitiateAvatarUpload(ctx, attachmentdom.AvatarUploadInput{
		OwnerKind:   ownerKindFor(slot),
		OwnerID:     workspaceOwnerID,
		FileName:    fileName,
		ContentType: contentType,
		FileSize:    fileSize,
		UploadedBy:  uploadedBy,
	})
}

// CompleteImageUpload finishes an upload for the given slot, replacing any
// previous image in that slot, and records updatedBy as the acting user.
// The DB read-modify-write is done under settingsdom.Repository.WithLock's
// row lock so a concurrent write (e.g. a favicon upload landing at nearly
// the same time as this logo upload) can't read the same stale snapshot and
// clobber this one — see that method's doc comment. The upload itself
// happens before the lock is taken, so the row lock isn't held across a
// network call to the object store.
func (s *Service) CompleteImageUpload(ctx context.Context, slot settingsdom.ImageSlot, fileID uuid.UUID, updatedBy uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}

	keys, err := s.avatarSvc.CompleteAvatarUpload(ctx, attachmentdom.AvatarCompleteInput{
		OwnerKind: ownerKindFor(slot),
		OwnerID:   workspaceOwnerID,
		FileID:    fileID,
	})
	if err != nil {
		return nil, err
	}

	var oldKey, oldThumbKey *string
	ws, err := s.repo.WithLock(ctx, func(ws *settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error) {
		key, thumbKey := keysFor(ws, slot)
		oldKey, oldThumbKey = *key, *thumbKey
		*key, *thumbKey = &keys.Key, &keys.ThumbKey
		ws.UpdatedAt = time.Now().UTC()
		ws.UpdatedBy = &updatedBy
		return ws, nil
	})
	if err != nil {
		return nil, err
	}

	s.avatarSvc.DeleteAvatarObjects(ctx, oldKey, oldThumbKey)
	return ws, nil
}

// RemoveImage clears the given slot, deleting the underlying objects, and
// records updatedBy as the acting user. See CompleteImageUpload's comment
// on why the mutation runs under WithLock.
func (s *Service) RemoveImage(ctx context.Context, slot settingsdom.ImageSlot, updatedBy uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	if s.avatarSvc == nil {
		return nil, ErrAvatarServiceRequired
	}

	var oldKey, oldThumbKey *string
	ws, err := s.repo.WithLock(ctx, func(ws *settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error) {
		key, thumbKey := keysFor(ws, slot)
		oldKey, oldThumbKey = *key, *thumbKey
		if oldKey == nil && oldThumbKey == nil {
			return nil, nil
		}
		*key, *thumbKey = nil, nil
		ws.UpdatedAt = time.Now().UTC()
		ws.UpdatedBy = &updatedBy
		return ws, nil
	})
	if err != nil {
		return nil, err
	}

	s.avatarSvc.DeleteAvatarObjects(ctx, oldKey, oldThumbKey)
	return ws, nil
}

// maxBrandNameLength caps the admin-set brand name.
const maxBrandNameLength = 100

// UpdateSettings sets the brand name and the light/dark primary accent
// colors together, clearing an override when passed nil or an empty string.
// See CompleteImageUpload's comment on why the mutation runs under WithLock.
func (s *Service) UpdateSettings(ctx context.Context, brandName, light, dark *string, updatedBy uuid.UUID) (*settingsdom.WorkspaceSettings, error) {
	brandName, err := normalizeBrandName(brandName)
	if err != nil {
		return nil, err
	}
	light, err = normalizeColor(light)
	if err != nil {
		return nil, err
	}
	dark, err = normalizeColor(dark)
	if err != nil {
		return nil, err
	}

	return s.repo.WithLock(ctx, func(ws *settingsdom.WorkspaceSettings) (*settingsdom.WorkspaceSettings, error) {
		ws.BrandName = brandName
		ws.PrimaryColorLight = light
		ws.PrimaryColorDark = dark
		ws.UpdatedAt = time.Now().UTC()
		ws.UpdatedBy = &updatedBy
		return ws, nil
	})
}

// normalizeColor treats nil/empty as "clear this override" (returned as
// nil), and otherwise requires a "#rrggbb" hex string.
func normalizeColor(c *string) (*string, error) {
	if c == nil || *c == "" {
		return nil, nil
	}
	if !colorRe.MatchString(*c) {
		return nil, settingsdom.ErrInvalidColor
	}
	return c, nil
}

// normalizeBrandName trims whitespace and treats nil/empty as "clear this
// override" (returned as nil).
func normalizeBrandName(n *string) (*string, error) {
	if n == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*n)
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed) > maxBrandNameLength {
		return nil, settingsdom.ErrBrandNameTooLong
	}
	return &trimmed, nil
}
