package attachmentsvc

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // register GIF decoder with image.Decode
	_ "image/jpeg" // register JPEG decoder with image.Decode
	"image/png"
	"strings"
	"time"

	"context"

	"github.com/google/uuid"
	"golang.org/x/image/draw"
	"golang.org/x/image/webp"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
)

const (
	// avatarFullSize is the pixel width/height of the "full" derived avatar
	// variant — large enough for any current header-sized display (biggest
	// today is ~56px) at retina density with headroom to spare.
	avatarFullSize = 256
	// avatarThumbSize is the pixel width/height of the "thumb" derived
	// avatar variant, used everywhere avatars render small (chips, avatar
	// stacks, activity feed, mention dropdown).
	avatarThumbSize = 64
	// avatarURLTTL is how long a presigned avatar GET URL remains valid.
	// Generous relative to typical page/query staleTime so a cached page
	// never shows a broken image before its next refetch.
	avatarURLTTL = 1 * time.Hour
)

// InitiateAvatarUpload creates a pending File record and returns a presigned
// single-part upload session. Avatars are capped well under
// storage.MultipartThreshold, so multipart is never needed here.
func (s *Service) InitiateAvatarUpload(ctx context.Context, in attachmentdom.AvatarUploadInput) (*attachmentdom.UploadSession, error) {
	if !attachmentdom.AvatarContentTypes[in.ContentType] {
		return nil, attachmentdom.ErrAvatarContentTypeInvalid
	}
	if in.FileSize <= 0 {
		return nil, attachmentdom.ErrFileSizeZero
	}
	if in.FileSize > attachmentdom.MaxAvatarUploadSize {
		return nil, attachmentdom.ErrAvatarTooLarge
	}

	fileID := uuid.New()
	storageKey := avatarOwnerPrefix(in.OwnerKind, in.OwnerID) + "/" + fileID.String() + "/upload"

	now := time.Now()
	f := &attachmentdom.File{
		ID:           fileID,
		StorageKey:   storageKey,
		Bucket:       s.bucket,
		FileName:     in.FileName,
		ContentType:  in.ContentType,
		FileSize:     in.FileSize,
		UploadStatus: attachmentdom.UploadStatusPending,
		UploadedBy:   &in.UploadedBy,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.CreateFile(ctx, f); err != nil {
		return nil, fmt.Errorf("attachment svc: create avatar file: %w", err)
	}

	uploadURL, err := s.store.PresignPutObject(ctx, s.bucket, storageKey, in.ContentType, presignedUploadTTL)
	if err != nil {
		return nil, fmt.Errorf("attachment svc: presign put for avatar: %w", err)
	}

	return &attachmentdom.UploadSession{FileID: fileID, UploadURL: uploadURL}, nil
}

// CompleteAvatarUpload downloads the raw upload, re-encodes it into "full"
// and "thumb" PNG variants, uploads both, and discards the raw upload (both
// the object and its File row) — nothing ever serves the client-uploaded
// bytes directly, only the two derived variants this method produces.
func (s *Service) CompleteAvatarUpload(ctx context.Context, in attachmentdom.AvatarCompleteInput) (*attachmentdom.AvatarKeys, error) {
	f, err := s.repo.FindFileByID(ctx, in.FileID)
	if err != nil {
		return nil, err
	}
	if f.UploadStatus != attachmentdom.UploadStatusPending {
		return nil, attachmentdom.ErrUploadNotPending
	}
	if !strings.HasPrefix(f.StorageKey, avatarOwnerPrefix(in.OwnerKind, in.OwnerID)+"/") {
		return nil, attachmentdom.ErrAvatarOwnerMismatch
	}

	bucket := f.Bucket
	if bucket == "" {
		bucket = s.bucket
	}

	raw, err := s.store.GetObject(ctx, bucket, f.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("attachment svc: download avatar upload: %w", err)
	}

	img, err := decodeAvatarImage(raw, f.ContentType)
	if err != nil {
		return nil, attachmentdom.ErrAvatarDecodeFailed
	}
	square := cropToSquare(img)

	fullBytes, err := resizeEncodePNG(square, avatarFullSize)
	if err != nil {
		return nil, fmt.Errorf("attachment svc: encode avatar full: %w", err)
	}
	thumbBytes, err := resizeEncodePNG(square, avatarThumbSize)
	if err != nil {
		return nil, fmt.Errorf("attachment svc: encode avatar thumb: %w", err)
	}

	prefix := avatarOwnerPrefix(in.OwnerKind, in.OwnerID) + "/" + in.FileID.String()
	keys := &attachmentdom.AvatarKeys{
		Key:      prefix + "/full.png",
		ThumbKey: prefix + "/thumb.png",
	}

	if err := s.store.PutObject(ctx, s.bucket, keys.Key, "image/png", fullBytes); err != nil {
		return nil, fmt.Errorf("attachment svc: upload avatar full: %w", err)
	}
	if err := s.store.PutObject(ctx, s.bucket, keys.ThumbKey, "image/png", thumbBytes); err != nil {
		return nil, fmt.Errorf("attachment svc: upload avatar thumb: %w", err)
	}

	// Best-effort cleanup: the raw upload is now fully superseded by the two
	// derived variants and must never be served, but a failure to delete it
	// (or its bookkeeping row) shouldn't fail the request — it's a harmless
	// orphan at worst, matching the DeleteTaskAttachment precedent of
	// intentionally keeping/not-chasing `files` rows once they're no longer
	// referenced.
	_ = s.store.DeleteObject(ctx, bucket, f.StorageKey)
	_ = s.repo.DeleteFile(ctx, in.FileID)

	return keys, nil
}

// ResolveAvatarURL returns a presigned GET URL for key, or nil if key is nil
// or empty (no avatar set).
func (s *Service) ResolveAvatarURL(ctx context.Context, key *string) (*string, error) {
	if key == nil || *key == "" {
		return nil, nil
	}
	url, err := s.store.PresignGetObject(ctx, s.bucket, *key, avatarURLTTL, "")
	if err != nil {
		return nil, fmt.Errorf("attachment svc: presign avatar url: %w", err)
	}
	return &url, nil
}

// DeleteAvatarObjects best-effort deletes the given storage keys. nil/empty
// keys are skipped; delete errors are intentionally swallowed (see the
// cleanup comment in CompleteAvatarUpload) so a stray already-gone object
// never blocks the caller's avatar replace/removal.
func (s *Service) DeleteAvatarObjects(ctx context.Context, keys ...*string) {
	for _, k := range keys {
		if k == nil || *k == "" {
			continue
		}
		_ = s.store.DeleteObject(ctx, s.bucket, *k)
	}
}

// avatarOwnerPrefix returns the storage-key prefix (no trailing slash) all
// of an owner's avatar objects live under.
func avatarOwnerPrefix(kind attachmentdom.AvatarOwnerKind, ownerID uuid.UUID) string {
	return fmt.Sprintf("avatars/%s/%s", kind, ownerID.String())
}

// decodeAvatarImage decodes raw image bytes. WebP needs its own decoder
// (golang.org/x/image/webp, decode-only) — png/jpeg/gif decoders are
// registered with the stdlib image.Decode dispatcher via blank imports
// above.
func decodeAvatarImage(data []byte, contentType string) (image.Image, error) {
	if contentType == "image/webp" {
		return webp.Decode(bytes.NewReader(data))
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// cropToSquare returns the center square crop of img as a fresh RGBA image.
func cropToSquare(img image.Image) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	side := w
	if h < side {
		side = h
	}
	origin := image.Pt(b.Min.X+(w-side)/2, b.Min.Y+(h-side)/2)
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	draw.Draw(dst, dst.Bounds(), img, origin, draw.Src)
	return dst
}

// resizeEncodePNG scales square (already square) down to size x size using
// a high-quality resampler and PNG-encodes the result.
func resizeEncodePNG(square image.Image, size int) ([]byte, error) {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), square, square.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
