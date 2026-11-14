package docdom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the combined persistence contract for the document aggregate.
type Repository interface {
	DocFolderRepository
	DocumentRepository
	DocSnapshotRepository
}

// DocFolderRepository defines persistence for document folders.
type DocFolderRepository interface {
	// ListAllFolders returns all folders for a project.
	ListAllFolders(ctx context.Context, projectID uuid.UUID) ([]*DocFolder, error)
	// ListFolders returns up to limit direct child folders of parentID
	// (nil = root level) in manual order (position, name, id), after the
	// folder cursorAfter names (nil = first page), plus whether more follow.
	ListFolders(ctx context.Context, projectID uuid.UUID, parentID *uuid.UUID, filter FolderFilter, cursorAfter *string, limit int) ([]*DocFolder, bool, error)
	// FolderPath returns the chain of folders from the root down to folderID
	// (inclusive), restricted to the project.
	FolderPath(ctx context.Context, projectID, folderID uuid.UUID) ([]*DocFolder, error)
	// FindFolderByID returns a single folder by ID.
	FindFolderByID(ctx context.Context, id uuid.UUID) (*DocFolder, error)
	// CreateFolder persists a new folder.
	CreateFolder(ctx context.Context, f *DocFolder) error
	// UpdateFolderAtomic loads folder id under a row lock, calls decide to
	// validate and mutate it, and writes it back in the same transaction.
	// Updates of folders in the same project are serialized (creates and
	// deletes are not) so a parent-cycle check cannot race a concurrent move. find reads a folder inside that
	// transaction. decide's error aborts the write and is returned as is.
	// Returns ErrFolderNotFound when the folder doesn't exist.
	UpdateFolderAtomic(ctx context.Context, id uuid.UUID, decide func(current *DocFolder, find func(id uuid.UUID) (*DocFolder, error)) error) (*DocFolder, error)
	// DeleteFolder permanently deletes a folder and cascades to child folders
	// and documents (folder_id is set to NULL on documents, not deleted).
	DeleteFolder(ctx context.Context, id uuid.UUID) error
}

// DocumentFilter narrows a document listing. Zero values match everything.
type DocumentFilter struct {
	// FolderID, when set, keeps only documents directly inside that folder.
	FolderID *uuid.UUID
	// RootOnly keeps only documents outside any folder (ignored when FolderID is set).
	RootOnly bool
	// Search keeps documents whose title contains it (case-insensitive).
	Search string
	// Title keeps documents whose title equals it (case-insensitive) — a
	// by-name lookup that, unlike Search, never matches a longer title.
	Title string
}

// FolderFilter narrows a folder-level listing.
type FolderFilter struct {
	// Name keeps folders whose name equals it (case-insensitive).
	Name string
}

// DocumentRepository defines persistence for documents.
type DocumentRepository interface {
	// ListDocuments returns up to limit non-deleted documents matching filter in
	// manual order (position, title, id), after the document cursorAfter names
	// (nil = first page), plus whether more follow. Document bodies are not
	// loaded; the caller's IAM scope narrows the query ahead of the cursor and
	// LIMIT so every page is a full page of allowed documents.
	ListDocuments(ctx context.Context, projectID uuid.UUID, filter DocumentFilter, cursorAfter *string, limit int) (docs []*Document, hasMore bool, err error)
	// ListAllDocuments returns every non-deleted document of a project with its
	// body, for whole-project jobs such as export. It is not exposed over the
	// API.
	ListAllDocuments(ctx context.Context, projectID uuid.UUID) ([]*Document, error)
	// SearchDocuments returns up to limit non-deleted documents of a project
	// whose title OR body text contains query (case-insensitive, matched
	// literally). Body text means the inline "text" nodes of the BlockNote
	// JSON, not its structural keys. Results include Content so the caller can
	// build snippets; ordered updated_at DESC, id ASC.
	SearchDocuments(ctx context.Context, projectID uuid.UUID, query string, limit int) ([]*Document, error)
	// FindDocumentByID returns a single non-deleted document.
	FindDocumentByID(ctx context.Context, id uuid.UUID) (*Document, error)
	// CreateDocument persists a new document.
	CreateDocument(ctx context.Context, d *Document) error
	// UpdateDocumentAtomic loads the non-deleted document id under a row lock,
	// calls decide to validate and mutate it, and writes it back in the same
	// transaction, so a concurrent save or delete can neither be reverted nor
	// have the document resurrected. Returns ErrDocNotFound when the document
	// doesn't exist or is deleted. decide's error aborts the write, except
	// ErrNoChange: the UPDATE is skipped, the transaction commits and the
	// current row is returned with a nil error.
	UpdateDocumentAtomic(ctx context.Context, id uuid.UUID, decide func(current *Document) error) (*Document, error)
	// DeleteDocument soft-deletes a document (sets deleted_at).
	DeleteDocument(ctx context.Context, id uuid.UUID) error
}

// DocSnapshotRepository defines persistence for document snapshots.
type DocSnapshotRepository interface {
	// ListSnapshots returns up to limit of a document's snapshots, newest
	// first, after the snapshot cursorAfter names (nil = the newest), plus
	// whether older ones remain. contentHash, when non-nil, keeps only the
	// snapshots holding that version (see DocSnapshot.ContentHash). Snapshot
	// bodies are not loaded — fetch one with FindSnapshotByID.
	ListSnapshots(ctx context.Context, documentID uuid.UUID, contentHash *string, cursorAfter *string, limit int) ([]*DocSnapshot, bool, error)
	// FindSnapshotByID returns a single snapshot.
	FindSnapshotByID(ctx context.Context, id uuid.UUID) (*DocSnapshot, error)
	// FindLatestSnapshot returns the snapshot with the highest snapshot_number
	// for the given document, or nil if no snapshots exist.
	FindLatestSnapshot(ctx context.Context, documentID uuid.UUID) (*DocSnapshot, error)
	// CreateSnapshot persists a new snapshot.
	CreateSnapshot(ctx context.Context, s *DocSnapshot) error
	// DeleteRecentSnapshotsExcept deletes all snapshots for a document that
	// were created at or after `since` and whose ID is not `excludeID`.
	// Used to consolidate snapshots within a time window (e.g. 3 minutes).
	DeleteRecentSnapshotsExcept(ctx context.Context, documentID uuid.UUID, excludeID uuid.UUID, since time.Time) error
}
