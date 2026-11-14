// Package docdom defines the document aggregate and its domain contracts.
package docdom

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DocFolder represents a folder for organising documents within a project.
// Folders can be nested via ParentID (nil = root-level folder).
type DocFolder struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	ParentID  *uuid.UUID // nil = root-level folder
	Name      string
	Position  int
	CreatedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Document is a rich-text document backed by BlockNote JSON blocks.
type Document struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	FolderID  *uuid.UUID // nil = root (not in any folder)
	Title     string
	Content   json.RawMessage // BlockNote JSON array of block objects; nil = no content
	// ContentHash is the stored ContentHash(Content); nil on rows written
	// before the column existed (use Hash, which computes it then).
	ContentHash *string
	Position    int
	CreatedBy   *uuid.UUID
	UpdatedBy   *uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time // non-nil = soft-deleted
}

// DocSnapshot is a point-in-time copy of a document's content.
// Snapshots are created automatically on significant saves and can be used to
// display history and compute diffs between versions.
type DocSnapshot struct {
	ID             uuid.UUID
	DocumentID     uuid.UUID
	Title          string
	Content        json.RawMessage // BlockNote JSON at the time of the snapshot
	ContentHash    *string         // hash of Content; nil on snapshots taken before the column existed
	SnapshotNumber int64           // monotonically increasing per document
	CreatedBy      *uuid.UUID
	CreatedByName  string // denormalised from project_members → users
	CreatedAt      time.Time
}

// Hash returns the document's content hash: the stored one, or ContentHash of
// the body when the row has none yet. The body must be loaded for the computed
// case; list queries select the stored hash and leave Content empty.
func (d *Document) Hash() string {
	if d.ContentHash != nil {
		return *d.ContentHash
	}
	return ContentHash(d.Content)
}
