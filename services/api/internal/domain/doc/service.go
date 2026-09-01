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
	// ListFolders returns all folders for a project (flat list; clients build tree).
	ListFolders(ctx context.Context, projectID uuid.UUID) ([]*DocFolder, error)
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
	// ListDocuments — see Repository.ListDocuments's doc comment for the
	// limit/cursor/hasMore pagination contract.
	ListDocuments(ctx context.Context, projectID uuid.UUID, folderID *uuid.UUID, search *string, cursor *string, limit *int) (docs []*Document, hasMore bool, err error)
	// GetDocument returns a single document by ID.
	GetDocument(ctx context.Context, id uuid.UUID) (*Document, error)
	// CreateDocument creates a new document in the project.
	CreateDocument(ctx context.Context, in CreateDocumentInput) (*Document, error)
	// UpdateDocument updates a document's mutable fields and optionally
	// creates a snapshot when the content changes.
	// projectID is used to verify the document belongs to the expected project.
	UpdateDocument(ctx context.Context, projectID, id uuid.UUID, in UpdateDocumentInput) (*Document, error)
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
}

// --- Snapshot Service -------------------------------------------------------

// DocSnapshotService defines snapshot use-cases.
type DocSnapshotService interface {
	// ListSnapshots returns all snapshots for a document, newest first.
	// projectID is used to verify the document belongs to the expected project.
	ListSnapshots(ctx context.Context, projectID, documentID uuid.UUID) ([]*DocSnapshot, error)
	// GetSnapshot returns a single snapshot by ID.
	// projectID is used to verify the snapshot's document belongs to the
	// expected project.
	GetSnapshot(ctx context.Context, projectID, id uuid.UUID) (*DocSnapshot, error)
}
