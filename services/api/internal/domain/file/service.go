package filedom

import (
	"context"
	"time"

	"github.com/google/uuid"

	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
)

// MaxFileSize is the largest file the file manager accepts.
const MaxFileSize int64 = 5 * 1024 * 1024 * 1024 // 5 GiB

// TrashRetention is how long a trashed item stays restorable.
const TrashRetention = 30 * 24 * time.Hour

// ListInput is a listing request.
type ListInput struct {
	ParentID *uuid.UUID
	Query    string
	Type     string
	SortBy   string
	Desc     bool
	Limit    int
	Offset   int
}

// UploadInput starts an upload into a folder.
type UploadInput struct {
	ParentID    *uuid.UUID
	FileName    string
	ContentType string
	FileSize    int64
}

// CompleteInput finishes an upload and creates its node.
type CompleteInput struct {
	FileID uuid.UUID
	// UploadID and Parts are required only for multipart uploads.
	UploadID *string
	Parts    []attachmentdom.CompletedPart
}

// Service is the file manager application contract. Every method works inside
// the scope the route names and treats a node of another scope as not found.
// Authorization is done by the router gates, not here.
type Service interface {
	List(ctx context.Context, scope Scope, actor uuid.UUID, in ListInput) (nodes []*Node, total int, err error)
	Get(ctx context.Context, scope Scope, actor, id uuid.UUID) (*Node, error)
	Path(ctx context.Context, scope Scope, id uuid.UUID) ([]*Node, error)

	CreateFolder(ctx context.Context, scope Scope, actor uuid.UUID, parentID *uuid.UUID, name string) (*Node, error)
	Rename(ctx context.Context, scope Scope, actor, id uuid.UUID, name string) (*Node, error)
	Move(ctx context.Context, scope Scope, actor, id uuid.UUID, parentID *uuid.UUID) (*Node, error)
	// Update renames and/or moves a node, validating both before writing either.
	Update(ctx context.Context, scope Scope, actor, id uuid.UUID, name *string, move bool, parentID *uuid.UUID) (*Node, error)

	// Trash moves an item to the trash. Files that a task, document or
	// annotation uses (Node.DeletedForGood) are deleted for good instead, and
	// leave their owner. The default folders cannot be deleted.
	Trash(ctx context.Context, scope Scope, actor, id uuid.UUID) error
	Restore(ctx context.Context, scope Scope, actor, id uuid.UUID) (*Node, error)
	// DeletePermanently removes a trashed node and its bytes for good.
	DeletePermanently(ctx context.Context, scope Scope, id uuid.UUID) error

	InitiateUpload(ctx context.Context, scope Scope, actor uuid.UUID, in UploadInput) (*attachmentdom.UploadSession, error)
	// CompleteUpload makes the uploaded file visible in the folder it was
	// initiated into.
	CompleteUpload(ctx context.Context, scope Scope, actor uuid.UUID, in CompleteInput) (*Node, error)

	// DownloadURL returns a short-lived presigned GET URL and records the
	// open in the caller's recents.
	DownloadURL(ctx context.Context, scope Scope, actor, id uuid.UUID, ttl time.Duration, forceDownload bool) (url string, node *Node, err error)

	// Relations returns the task and documents that use a file of a project.
	Relations(ctx context.Context, scope Scope, actor, id uuid.UUID) (*Relations, error)

	// RecordOpen records in the caller's recents that they opened an item that
	// has no download of its own (a document).
	RecordOpen(ctx context.Context, scope Scope, actor, id uuid.UUID) error

	SetStarred(ctx context.Context, scope Scope, actor, id uuid.UUID, starred bool) error
	ListStarred(ctx context.Context, scope Scope, actor uuid.UUID, limit, offset int) ([]*Node, int, error)
	ListRecent(ctx context.Context, scope Scope, actor uuid.UUID, limit int) ([]*Node, error)
	ListTrash(ctx context.Context, scope Scope, actor uuid.UUID, limit, offset int) ([]*Node, int, error)

	// PurgeExpired permanently removes everything trashed before the cutoff.
	PurgeExpired(ctx context.Context, before time.Time, limit int) (int, error)
}
