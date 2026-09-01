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
	ActivityRepository
}

// DocFolderRepository defines persistence for document folders.
type DocFolderRepository interface {
	// ListFolders returns all folders for a project.
	ListFolders(ctx context.Context, projectID uuid.UUID) ([]*DocFolder, error)
	// FindFolderByID returns a single folder by ID.
	FindFolderByID(ctx context.Context, id uuid.UUID) (*DocFolder, error)
	// CreateFolder persists a new folder.
	CreateFolder(ctx context.Context, f *DocFolder) error
	// UpdateFolder persists mutable changes to a folder.
	UpdateFolder(ctx context.Context, f *DocFolder) error
	// DeleteFolder permanently deletes a folder and cascades to child folders
	// and documents (folder_id is set to NULL on documents, not deleted).
	DeleteFolder(ctx context.Context, id uuid.UUID) error
}

// DocumentRepository defines persistence for documents.
type DocumentRepository interface {
	// ListDocuments returns non-deleted documents for a project. folderID
	// non-nil filters to that folder. search non-nil/non-empty filters to a
	// case-insensitive title match (see postgres.escapeLikePattern).
	//
	// limit nil means "no pagination" — every matching document is returned
	// (ordered position ASC, title ASC), hasMore is always false, and cursor
	// is ignored. This is the existing behavior every current caller (the
	// doc-tree sidebar, the mention picker) relies on.
	//
	// limit non-nil switches to keyset pagination ordered title ASC, id ASC
	// instead — sensible for a search result list, independent of the doc
	// tree's manual position ordering — fetching one extra row to compute
	// hasMore and returning at most *limit. cursor, if non-nil, resumes after
	// the document EncodeDocumentCursor produced for the last row of the
	// previous page.
	ListDocuments(ctx context.Context, projectID uuid.UUID, folderID *uuid.UUID, search *string, cursor *string, limit *int) (docs []*Document, hasMore bool, err error)
	// FindDocumentByID returns a single non-deleted document.
	FindDocumentByID(ctx context.Context, id uuid.UUID) (*Document, error)
	// CreateDocument persists a new document.
	CreateDocument(ctx context.Context, d *Document) error
	// UpdateDocument persists mutable changes to a document.
	UpdateDocument(ctx context.Context, d *Document) error
	// DeleteDocument soft-deletes a document (sets deleted_at).
	DeleteDocument(ctx context.Context, id uuid.UUID) error
}

// DocSnapshotRepository defines persistence for document snapshots.
type DocSnapshotRepository interface {
	// ListSnapshots returns all snapshots for a document, newest first.
	ListSnapshots(ctx context.Context, documentID uuid.UUID) ([]*DocSnapshot, error)
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
