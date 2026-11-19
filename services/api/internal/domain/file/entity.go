// Package filedom defines the Drive-style file manager aggregate: a folder and
// file hierarchy per scope (one project, or the general workspace scope) laid
// over the shared `files` table that already backs every other upload.
package filedom

import (
	"time"

	"github.com/google/uuid"
)

// Kind tells a folder node from a file node.
type Kind string

// Node kinds.
const (
	KindFolder Kind = "folder"
	KindFile   Kind = "file"
)

// System keys mark the default folders the product manages itself.
const (
	// SystemKeyTaskRoot is the per-project "Task attachments" folder.
	SystemKeyTaskRoot = "task_attachments"
	// SystemKeyTask is the per-task subfolder inside it.
	SystemKeyTask = "task"
	// SystemKeyDocs holds the files embedded in the project's documents.
	SystemKeyDocs = "docs"
	// SystemKeyAnnotations holds page-annotation screenshots.
	SystemKeyAnnotations = "annotations"
)

// File sources: which part of the product a file came from.
const (
	SourceUpload         = "upload"
	SourceTaskAttachment = "task_attachment"
	SourceDoc            = "doc"
	SourceAnnotation     = "annotation"
)

// MaxNameLength bounds a file or folder name, in bytes.
const MaxNameLength = 255

// Sort keys for listings.
const (
	SortName     = "name"
	SortModified = "modified"
	SortSize     = "size"
)

// Type filters for listings (the chips of the file browser).
const (
	TypeDocument     = "document"
	TypeSpreadsheet  = "spreadsheet"
	TypePresentation = "presentation"
	TypePDF          = "pdf"
	TypeImage        = "image"
	TypeVideo        = "video"
	TypeAudio        = "audio"
	TypeArchive      = "archive"
)

// Scope is where a node lives: one project, or the general scope (ProjectID
// nil). Every service call is made within a scope taken from the route, and a
// node outside it is reported as not found.
type Scope struct {
	ProjectID *uuid.UUID
}

// General reports whether the scope is the workspace-wide one.
func (s Scope) General() bool { return s.ProjectID == nil }

// Contains reports whether the node belongs to the scope.
func (s Scope) Contains(n *Node) bool {
	switch {
	case s.ProjectID == nil && n.ProjectID == nil:
		return true
	case s.ProjectID == nil || n.ProjectID == nil:
		return false
	default:
		return *s.ProjectID == *n.ProjectID
	}
}

// Key is the storage-key segment of the scope.
func (s Scope) Key() string {
	if s.ProjectID == nil {
		return "general"
	}
	return s.ProjectID.String()
}

// Node is one folder or file.
type Node struct {
	ID        uuid.UUID
	ProjectID *uuid.UUID
	ParentID  *uuid.UUID
	Kind      Kind
	Name      string
	SystemKey string
	// Source is where a file came from (Source* constants); empty for folders.
	Source     string
	CreatedBy  *uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  *time.Time
	DeletedBy  *uuid.UUID
	OwnerName  string
	StorageKey string
	Bucket     string

	// File details (file nodes only).
	ContentType string
	Size        int64

	// ParentSystemKey is the system key of the parent folder, if it is a
	// default folder.
	ParentSystemKey string

	// Per-caller state.
	Starred    bool
	AccessedAt *time.Time
}

// IsFolder reports whether the node is a folder.
func (n *Node) IsFolder() bool { return n.Kind == KindFolder }

// Locked reports whether the node belongs to the product-managed default
// folders and so cannot be renamed or moved: the default folders themselves
// and everything inside them. A file there can still be downloaded, starred
// and deleted (see Service.Trash).
func (n *Node) Locked() bool {
	return n.SystemKey != "" || n.ParentSystemKey != ""
}

// StorageObject names an object to delete from the object store.
type StorageObject struct {
	Bucket string
	Key    string
}
