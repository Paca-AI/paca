package attachmentdom

import (
	"context"

	"github.com/google/uuid"
)

// AvatarOwnerKind discriminates which table an avatar belongs to. It is used
// only to namespace the object-storage key (avatars/{kind}/{ownerID}/...)
// and to verify a completed upload belongs to the claimed owner.
type AvatarOwnerKind string

// AvatarOwnerKind values.
const (
	AvatarOwnerUser    AvatarOwnerKind = "users"
	AvatarOwnerAgent   AvatarOwnerKind = "agents"
	AvatarOwnerProject AvatarOwnerKind = "projects"
)

// AvatarService manages avatar uploads for users and agents. Unlike the task
// attachment flow there is no join table and no client-visible "original" —
// on completion the server re-encodes the upload into two fixed-size PNG
// variants (full + thumb) and the owner (user/agent service) persists their
// storage keys directly on its own row. See service/attachment/avatar_service.go.
type AvatarService interface {
	// InitiateAvatarUpload creates a pending File record and returns a
	// presigned single-part PUT URL. Rejects non-image content types and
	// files over MaxAvatarUploadSize.
	InitiateAvatarUpload(ctx context.Context, in AvatarUploadInput) (*UploadSession, error)

	// CompleteAvatarUpload downloads the uploaded bytes, decodes them,
	// center-crops and resizes into "full" (256x256) and "thumb" (64x64)
	// PNG variants, uploads both, deletes the raw upload object, and
	// returns the two derived storage keys for the caller to persist.
	CompleteAvatarUpload(ctx context.Context, in AvatarCompleteInput) (*AvatarKeys, error)

	// ResolveAvatarURL returns a short-lived presigned GET URL for the given
	// storage key, or nil if key is nil (no avatar set). Presigning is a
	// local computation (no network round-trip), so this is cheap to call
	// per-item in list responses.
	ResolveAvatarURL(ctx context.Context, key *string) (*string, error)

	// DeleteAvatarObjects best-effort deletes the given storage keys (nil
	// and empty keys are skipped). Used to clean up the previous avatar
	// after a replace, or both keys on removal. Errors are logged, not
	// returned — a stray orphaned object must never block the request that
	// triggered the replace/removal.
	DeleteAvatarObjects(ctx context.Context, keys ...*string)
}

// MaxAvatarUploadSize caps the raw (pre-resize) avatar upload. Comfortably
// under storage.MultipartThreshold so avatars never need the multipart path.
// Enforced twice: against the client-declared size at initiate time, and
// again against the actual downloaded object at complete time — a client
// can PUT more bytes than it declared straight to the presigned URL, so the
// declared-size check alone is not sufficient.
const MaxAvatarUploadSize = 5 * 1024 * 1024 // 5 MiB

// MaxAvatarDecodeDimension caps the width/height (in pixels) an uploaded
// avatar may declare before the server will fully decode it. Checked via a
// cheap header-only DecodeConfig read, before the full pixel buffer is
// allocated — a small, highly-compressed file can otherwise declare
// dimensions large enough to exhaust server memory on decode (a
// "decompression bomb"). Comfortably above any realistic photo (a 4K photo
// is 3840x2160) while keeping the worst-case decode buffer bounded
// (8192x8192 RGBA is ~256 MiB).
const MaxAvatarDecodeDimension = 8192

// AvatarContentTypes is the whitelist of accepted raw upload content types.
var AvatarContentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

// AvatarUploadInput carries the client-supplied metadata for initiating an
// avatar upload.
type AvatarUploadInput struct {
	OwnerKind   AvatarOwnerKind
	OwnerID     uuid.UUID
	FileName    string
	ContentType string
	FileSize    int64
	UploadedBy  uuid.UUID
}

// AvatarCompleteInput carries parameters for finishing an avatar upload.
type AvatarCompleteInput struct {
	OwnerKind AvatarOwnerKind
	OwnerID   uuid.UUID
	FileID    uuid.UUID
}

// AvatarKeys holds the storage keys of the two derived avatar variants,
// meant to be persisted directly on the owning user/agent row.
type AvatarKeys struct {
	Key      string // 256x256 "full" variant
	ThumbKey string // 64x64 "thumb" variant
}
