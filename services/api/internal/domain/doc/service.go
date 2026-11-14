package docdom

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Service is the combined document management service contract.
type Service interface {
	DocFolderService
	DocumentService
	DocSnapshotService
}

// --- Folder Service ---------------------------------------------------------

// DocFolderService defines folder use-cases.
type DocFolderService interface {
	// ListFolders is the cursor-paginated listing of one level of the
	// folder tree — see DocFolderRepository.ListFolders.
	ListFolders(ctx context.Context, projectID uuid.UUID, parentID *uuid.UUID, filter FolderFilter, cursorAfter *string, limit int) ([]*DocFolder, bool, error)
	// FolderPath returns the folders from the root down to folderID.
	FolderPath(ctx context.Context, projectID, folderID uuid.UUID) ([]*DocFolder, error)
	// CreateFolder creates a new folder in the project.
	CreateFolder(ctx context.Context, in CreateFolderInput) (*DocFolder, error)
	// UpdateFolder updates the mutable fields of an existing folder.
	UpdateFolder(ctx context.Context, id uuid.UUID, in UpdateFolderInput) (*DocFolder, error)
	// DeleteFolder deletes a folder. Child documents have their folder_id set to NULL.
	// projectID is used to verify the folder belongs to the expected project.
	DeleteFolder(ctx context.Context, id uuid.UUID, projectID uuid.UUID) error
}

// CreateFolderInput carries fields required to create a folder.
type CreateFolderInput struct {
	ProjectID uuid.UUID
	ParentID  *uuid.UUID
	Name      string
	CreatedBy *uuid.UUID
}

// UpdateFolderInput carries mutable folder fields.
// Double-pointer for ParentID: nil = absent (no change), &nil = move to root,
// &&id = move under another folder.
// ProjectID is used to verify the folder belongs to the expected project.
type UpdateFolderInput struct {
	ProjectID uuid.UUID
	Name      string
	ParentID  **uuid.UUID
	Position  *int
}

// --- Document Service -------------------------------------------------------

// DocumentService defines document use-cases.
type DocumentService interface {
	// ListDocuments is the cursor-paginated document listing — see
	// DocumentRepository.ListDocuments.
	ListDocuments(ctx context.Context, projectID uuid.UUID, filter DocumentFilter, cursorAfter *string, limit int) (docs []*Document, hasMore bool, err error)
	// SearchDocuments finds documents whose title or body contains query and
	// returns each with a match location and excerpt. limit is clamped to
	// [1, 50] (default 20 when <= 0).
	SearchDocuments(ctx context.Context, projectID uuid.UUID, query string, limit int) ([]SearchHit, error)
	// GetDocument returns a single document by ID.
	GetDocument(ctx context.Context, id uuid.UUID) (*Document, error)
	// CreateDocument creates a new document in the project.
	CreateDocument(ctx context.Context, in CreateDocumentInput) (*Document, error)
	// UpdateDocument updates a document's mutable fields and creates a
	// snapshot when the content or title changes. Content counts as changed
	// only when its ContentHash differs. A write that changes nothing writes
	// nothing and returns the stored document with an empty DocChange. When
	// in.IfMatch is set and is not the stored content hash (or "*"), it
	// returns a *VersionConflictError. projectID is used to verify the
	// document belongs to the expected project.
	UpdateDocument(ctx context.Context, projectID, id uuid.UUID, in UpdateDocumentInput) (*Document, DocChange, error)
	// DeleteDocument soft-deletes a document.
	// projectID is used to verify the document belongs to the expected project.
	DeleteDocument(ctx context.Context, projectID, id uuid.UUID) error
}

// CreateDocumentInput carries fields required to create a document.
type CreateDocumentInput struct {
	ProjectID uuid.UUID
	FolderID  *uuid.UUID
	Title     string
	Content   json.RawMessage
	CreatedBy *uuid.UUID
}

// UpdateDocumentInput carries mutable document fields.
// Pointer fields: nil = absent (no change).
// FolderID is a double-pointer: nil = absent, &nil = move to root, &&id = move to folder.
type UpdateDocumentInput struct {
	Title     *string
	Content   *json.RawMessage
	FolderID  **uuid.UUID
	Position  *int
	UpdatedBy *uuid.UUID
	// IfMatch, when set, is the content hash the caller last saw, or "*" (any
	// version, as long as the document exists). Checked under the row lock.
	IfMatch *string
	// Source, when set, replaces the document's source (the repository file a
	// managed copy is pushed from). A write whose Source names the locked
	// document's own repository may change its content and title.
	Source *DocSource
	// Lock, when set, sets SourceLocked. Locking needs a source (the
	// document's or this input's).
	Lock *bool
}

// DocChange says which fields an UpdateDocument call actually changed.
type DocChange struct {
	Content  bool
	Title    bool
	Folder   bool
	Position bool
	// Source: the source changed. Alone it updates only the source: no
	// snapshot, no activity, and updated_at stays as it was.
	Source bool
	// Lock: source_locked changed. Like Source it leaves updated_at alone and
	// takes no snapshot, but it is recorded as an activity.
	Lock bool
}

// Any reports whether anything changed.
func (c DocChange) Any() bool {
	return c.Content || c.Title || c.Folder || c.Position || c.Source || c.Lock
}

// Edited reports whether a field a reader of the document sees changed: its
// content, title, folder or position (not its source or lock).
func (c DocChange) Edited() bool { return c.Content || c.Title || c.Folder || c.Position }

// --- Snapshot Service -------------------------------------------------------

// DocSnapshotService defines snapshot use-cases.
type DocSnapshotService interface {
	// ListSnapshots returns one cursor-paginated page of a document's snapshots,
	// newest first, without their bodies (see DocSnapshotRepository.ListSnapshots).
	// contentHash, when non-nil, keeps only the snapshots holding that version.
	// projectID is used to verify the document belongs to the expected project.
	ListSnapshots(ctx context.Context, projectID, documentID uuid.UUID, contentHash *string, cursorAfter *string, limit int) ([]*DocSnapshot, bool, error)
	// GetSnapshot returns a single snapshot by ID.
	// projectID is used to verify the snapshot's document belongs to the
	// expected project.
	GetSnapshot(ctx context.Context, projectID, id uuid.UUID) (*DocSnapshot, error)
}
