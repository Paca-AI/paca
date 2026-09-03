package attachmentdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DocOwnerChecker validates that a document belongs to a given project.
type DocOwnerChecker interface {
	// DocBelongsToProject loads the document by id and returns a non-nil
	// error when the document does not belong to projectID (or does not exist).
	DocBelongsToProject(ctx context.Context, projectID, docID uuid.UUID) error
}

// DocFileService manages files attached directly to documents.
// Unlike the task attachment flow, there is no join table — files are stored
// in the shared `files` table and referenced by their ID directly from the
// BlockNote document content.
type DocFileService interface {
	// InitiateDocUpload creates a pending File record and returns a presigned
	// upload session (single-part or multipart).
	InitiateDocUpload(ctx context.Context, in DocUploadInput) (*UploadSession, error)

	// CompleteDocUpload marks the file as uploaded and returns the file record.
	CompleteDocUpload(ctx context.Context, in DocCompleteUploadInput) (*File, error)

	// GetDocFileDownloadURL returns a short-lived presigned GET URL for the
	// given file. docID is used to verify the file belongs to the document,
	// and projectID is used to verify docID belongs to the caller's
	// authorized project.
	GetDocFileDownloadURL(ctx context.Context, projectID, docID, fileID uuid.UUID, ttl time.Duration) (string, error)

	// DeleteDocFile removes the file record and its object from storage.
	// docID is used to verify the file belongs to the document, and
	// projectID is used to verify docID belongs to the caller's authorized
	// project.
	DeleteDocFile(ctx context.Context, projectID, docID, fileID uuid.UUID) error
}

// DocUploadInput carries the client-supplied metadata for initiating a doc file upload.
type DocUploadInput struct {
	DocID       uuid.UUID
	FileName    string
	ContentType string
	FileSize    int64
	UploadedBy  uuid.UUID
}

// DocCompleteUploadInput carries parameters for finishing a doc file upload.
type DocCompleteUploadInput struct {
	FileID uuid.UUID
	// UploadID and Parts are required only for multipart uploads.
	UploadID *string
	Parts    []CompletedPart
}
