package attachmentdom

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/storage"
)

// TaskOwnerChecker validates that a task belongs to a given project.
type TaskOwnerChecker interface {
	// TaskBelongsToProject loads the task by id and returns a non-nil error
	// when the task does not belong to projectID (or does not exist).
	TaskBelongsToProject(ctx context.Context, projectID, taskID uuid.UUID) error
}

// Service is the combined attachment management contract.
type Service interface {
	// InitiateUpload creates a pending File record and returns either a
	// single-part presigned PUT URL (files < MultipartThreshold) or the
	// multipart upload descriptor with per-part presigned URLs.
	// Verifies taskID belongs to projectID before proceeding.
	InitiateUpload(ctx context.Context, projectID uuid.UUID, in InitiateUploadInput) (*UploadSession, error)

	// CompleteUpload marks the File as uploaded and creates the
	// TaskAttachment join record.  For multipart uploads the caller must
	// supply the completed parts so the server can call
	// CompleteMultipartUpload on the object store.
	// Verifies taskID belongs to projectID before proceeding.
	CompleteUpload(ctx context.Context, projectID uuid.UUID, in CompleteUploadInput) (*TaskAttachment, error)

	// GetDownloadURL returns a short-lived presigned GET URL for the file
	// that backs the given attachment. Verifies the attachment belongs to taskID
	// and that taskID belongs to projectID.
	// Set forceDownload true to embed Content-Disposition: attachment so the
	// browser downloads the file rather than previewing it inline.
	GetDownloadURL(ctx context.Context, projectID, taskID, attachmentID uuid.UUID, ttl time.Duration, forceDownload bool) (string, error)

	// GetAttachmentContent reads the file's bytes directly from the object
	// store and returns them along with its content type. Unlike
	// GetDownloadURL (which hands back a presigned object-store URL meant for
	// a browser), this goes through the API itself — the right path for
	// server-side callers such as the MCP server, which may not have network
	// access to the object store's public-facing endpoint (e.g. when running
	// inside an agent sandbox container that can reach `api`/`gateway` by
	// their internal Docker hostnames but not the browser-facing public URL
	// presigned URLs are rewritten to). Verifies the attachment belongs to
	// taskID and that taskID belongs to projectID. Returns
	// ErrAttachmentContentTooLarge if the file exceeds MaxAttachmentContentSize.
	GetAttachmentContent(ctx context.Context, projectID, taskID, attachmentID uuid.UUID) (data []byte, contentType string, err error)

	// ListTaskAttachments returns all confirmed attachments for the given task.
	// Verifies taskID belongs to projectID before proceeding.
	ListTaskAttachments(ctx context.Context, projectID, taskID uuid.UUID) ([]*TaskAttachment, error)

	// DeleteTaskAttachment removes the task→file association for the given
	// attachment. Verifies the attachment belongs to taskID and that taskID
	// belongs to projectID before deleting.
	DeleteTaskAttachment(ctx context.Context, projectID, taskID, attachmentID uuid.UUID) error
}

// UploadSession is returned by InitiateUpload and carries everything the
// client needs to upload the file directly to the object store.
type UploadSession struct {
	FileID      uuid.UUID `json:"file_id"`
	IsMultipart bool      `json:"is_multipart"`
	// UploadURL is set for single-part uploads (IsMultipart=false).
	UploadURL string `json:"upload_url,omitempty"`
	// Multipart is set for large files (IsMultipart=true).
	Multipart *storage.MultipartUpload `json:"multipart,omitempty"`
}

// InitiateUploadInput carries the client-supplied file metadata for starting
// an upload.
type InitiateUploadInput struct {
	TaskID      uuid.UUID
	FileName    string
	ContentType string
	FileSize    int64
	UploadedBy  uuid.UUID // user performing the upload
}

// CompletedPart mirrors storage.CompletedPart for multipart completion.
type CompletedPart struct {
	PartNumber int
	ETag       string
}

// CompleteUploadInput carries parameters for finishing an upload.
type CompleteUploadInput struct {
	FileID    uuid.UUID
	TaskID    uuid.UUID
	CreatedBy uuid.UUID
	// UploadID and Parts are required only for multipart uploads.
	UploadID *string
	Parts    []CompletedPart
}
